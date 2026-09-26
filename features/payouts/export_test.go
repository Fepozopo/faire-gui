package payouts

import (
	"bytes"
	"encoding/csv"
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestWriteCSVMatchesOnlyOpenInvoices verifies the import header, order-to-PO match, fixed fields, and cent rounding.
func TestWriteCSVMatchesOnlyOpenInvoices(t *testing.T) {
	t.Parallel()
	summary := "Order Number,Payout Amount\nPAID,30.00\nOPEN,363.00\nUNKNOWN,10.00\n"
	sage := "\ufeffCustomer PO No.,Invoice No.,Amount,Balance\nPAID,0000001,35,0\nOPEN,0108501,390.00000000000001,390\n"
	var out bytes.Buffer
	count, err := WriteCSV(&out, strings.NewReader(summary), strings.NewReader(sage), "CHECK01", "user, note")
	if err != nil || count != 1 {
		t.Fatalf("WriteCSV() for an open and a paid invoice = (%d, %v), want (1, nil)", count, err)
	}
	got, err := csv.NewReader(&out).ReadAll()
	if err != nil {
		t.Fatalf("read output for open invoice: %v", err)
	}
	want := [][]string{
		{"CustomerNo", "DepositType", "CheckNo", "InvoiceAmount", "InvoiceNo", "DiscountAmount", "AmountPosted", "Comment"},
		{"0090671", "C", "CHECK01", "390.00", "0108501", "27.00", "363.00", "user, note"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows for matched open invoice = %q, want %q", got, want)
	}
}

// TestWriteCSVFormatsNegativeDiscount verifies that overpayment remains a signed cent amount in the export.
func TestWriteCSVFormatsNegativeDiscount(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	count, err := WriteCSV(&out, strings.NewReader("Order Number,Payout Amount\nA,1.02\n"), strings.NewReader("Customer PO No.,Invoice No.,Amount,Balance\nA,0001,1.00,1.00\n"), "C1", "")
	if err != nil || count != 1 {
		t.Fatalf("WriteCSV(overpayment) = (%d, %v), want (1, nil)", count, err)
	}
	rows, err := csv.NewReader(&out).ReadAll()
	if err != nil || rows[1][5] != "-0.02" {
		t.Fatalf("WriteCSV(overpayment) discount = %q, parse error = %v; want -0.02", rows, err)
	}
}

// TestWriteCSVRejectsAmbiguousOrInvalidInputs verifies exports never emit a partial file when reconciliation is unsafe.
func TestWriteCSVRejectsAmbiguousOrInvalidInputs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, summary, sage, checkNo, errorPart string
	}{
		{"missing check", "Order Number,Payout Amount\nA,1\n", "Customer PO No.,Invoice No.,Amount,Balance\nA,1,2,2\n", "", "check number"},
		{"missing column", "Order Number,Payout Amount\nA,1\n", "Customer PO No.,Invoice No.,Balance\nA,1,2\n", "C1", "missing \"Amount\""},
		{"duplicate invoice", "Order Number,Payout Amount\nA,1\n", "Customer PO No.,Invoice No.,Amount,Balance\nA,1,2,2\nA,2,3,3\n", "C1", "more than one open Sage invoice"},
		{"duplicate payout", "Order Number,Payout Amount\nA,1\nA,1\n", "Customer PO No.,Invoice No.,Amount,Balance\nA,1,2,2\n", "C1", "more than one Faire payout"},
		{"invalid balance", "Order Number,Payout Amount\nA,1\n", "Customer PO No.,Invoice No.,Amount,Balance\nA,1,2,NaN\n", "C1", "balance"},
		{"invalid payout", "Order Number,Payout Amount\nA,oops\n", "Customer PO No.,Invoice No.,Amount,Balance\nA,1,2,2\n", "C1", "payout amount"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			count, err := WriteCSV(&out, strings.NewReader(tc.summary), strings.NewReader(tc.sage), tc.checkNo, "note")
			if err == nil || !strings.Contains(err.Error(), tc.errorPart) || count != 0 || out.Len() != 0 {
				t.Fatalf("WriteCSV(%s) = (%d, %v), output %q; want zero rows, error containing %q, no output", tc.name, count, err, out.String(), tc.errorPart)
			}
		})
	}
}

// TestWriteCSVNoOpenMatch verifies settled and unmatched invoices produce no output file data.
func TestWriteCSVNoOpenMatch(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	count, err := WriteCSV(&out, strings.NewReader("Order Number,Payout Amount\nA,3\n"), strings.NewReader("Customer PO No.,Invoice No.,Amount,Balance\nA,00001,3,0\n"), "C1", "")
	if err != nil || count != 0 || out.Len() != 0 {
		t.Fatalf("WriteCSV with zero balance = (%d, %v), output %q; want (0, nil) and no output", count, err, out.String())
	}
}

// TestWriteCSVWithExamples verifies the provided exports reconcile to their known open invoice and receipt values.
func TestWriteCSVWithExamples(t *testing.T) {
	t.Parallel()
	summary, err := os.Open("../../payout_examples/faire-payouts-summary-2026-08-28.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer summary.Close()
	sage, err := os.Open("../../payout_examples/faire_sage_export.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer sage.Close()
	var out bytes.Buffer
	count, err := WriteCSV(&out, summary, sage, "CHECK01", "Comment")
	if err != nil || count != 18 {
		t.Fatalf("WriteCSV(example files) = (%d, %v), want (18, nil)", count, err)
	}
	rows, err := csv.NewReader(&out).ReadAll()
	if err != nil {
		t.Fatalf("parse example output: %v", err)
	}
	want := []string{"0090671", "C", "CHECK01", "688.50", "0107257", "49.02", "639.48", "Comment"}
	if !reflect.DeepEqual(rows[1], want) || len(rows) != 19 {
		t.Fatalf("example output first row = %q and total rows = %d, want %q and 19", rows[1], len(rows), want)
	}
}

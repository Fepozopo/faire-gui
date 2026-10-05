package payouts

import (
	"bytes"
	"encoding/csv"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestWriteCSVMatchesOnlyOpenInvoices verifies the ten-column layout, matching, display-only total, payouts, discounts, and rounding.
func TestWriteCSVMatchesOnlyOpenInvoices(t *testing.T) {
	t.Parallel()
	summary := "Order Number,Payout Amount\nPAID,30.00\nOPEN,363.00\nSECOND,2.05\nUNKNOWN,10.00\n"
	sage := "\ufeffCustomer PO No.,Invoice No.,Amount,Balance\nPAID,0000001,35,0\nOPEN,0108501,390.00000000000001,390\nSECOND,0108502,3,3\n"
	var out bytes.Buffer
	count, total, err := WriteCSV(&out, strings.NewReader(summary), strings.NewReader(sage), time.Date(2024, time.February, 29, 23, 59, 0, 0, time.FixedZone("local", -5*60*60)), 0, "Deposit, description", "CHECK01", "user, note")
	if err != nil || count != 2 || total != "365.05" {
		t.Fatalf("WriteCSV() for open and paid invoices = (%d, %q, %v), want (2, 365.05, nil)", count, total, err)
	}
	got, err := csv.NewReader(&out).ReadAll()
	if err != nil {
		t.Fatalf("read output for open invoice: %v", err)
	}
	want := [][]string{
		{"FAIRE", "06000", "20240229", "Deposit, description", "363.00", "0090671", "CHECK01", "0108501", "27.00", "user, note"},
		{"FAIRE", "06001", "20240229", "Deposit, description", "2.05", "0090671", "CHECK01", "0108502", "0.95", "user, note"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows for matched open invoice = %q, want %q", got, want)
	}
}

// TestWriteCSVFormatsNegativeDiscount verifies that overpayment remains a signed cent amount in the export.
func TestWriteCSVFormatsNegativeDiscount(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	count, total, err := WriteCSV(&out, strings.NewReader("Order Number,Payout Amount\nA,1.02\n"), strings.NewReader("Customer PO No.,Invoice No.,Amount,Balance\nA,0001,1.00,1.00\n"), time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC), 0, "Deposit", "C1", "")
	if err != nil || count != 1 || total != "1.02" {
		t.Fatalf("WriteCSV(overpayment) = (%d, %q, %v), want (1, 1.02, nil)", count, total, err)
	}
	rows, err := csv.NewReader(&out).ReadAll()
	if err != nil || len(rows) != 1 || len(rows[0]) != 10 || rows[0][4] != "1.02" || rows[0][8] != "-0.02" || rows[0][9] != "" {
		t.Fatalf("WriteCSV(overpayment) row = %q, parse error = %v; want deposit 1.02, discount -0.02, and empty optional comment", rows, err)
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
		{"total overflow", "Order Number,Payout Amount\nA,40000000000000000\nB,40000000000000000\nC,40000000000000000\n", "Customer PO No.,Invoice No.,Amount,Balance\nA,1,40000000000000000,1\nB,2,40000000000000000,1\nC,3,40000000000000000,1\n", "C1", "total amount posted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			count, total, err := WriteCSV(&out, strings.NewReader(tc.summary), strings.NewReader(tc.sage), time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC), 0, "Deposit", tc.checkNo, "note")
			if err == nil || !strings.Contains(err.Error(), tc.errorPart) || count != 0 || total != "" || out.Len() != 0 {
				t.Fatalf("WriteCSV(%s) = (%d, %q, %v), output %q; want zero rows, empty total, error containing %q, no output", tc.name, count, total, err, out.String(), tc.errorPart)
			}
		})
	}
}

// TestWriteCSVNoOpenMatch verifies settled and unmatched invoices produce no output file data.
func TestWriteCSVNoOpenMatch(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	count, total, err := WriteCSV(&out, strings.NewReader("Order Number,Payout Amount\nA,3\n"), strings.NewReader("Customer PO No.,Invoice No.,Amount,Balance\nA,00001,3,0\n"), time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC), 0, "Deposit", "C1", "")
	if err != nil || count != 0 || total != "" || out.Len() != 0 {
		t.Fatalf("WriteCSV with zero balance = (%d, %q, %v), output %q; want (0, empty total, nil) and no output", count, total, err, out.String())
	}
}

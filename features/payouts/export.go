// Package payouts reconciles a Faire payout summary with unpaid Sage invoices for cash receipts import.
package payouts

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

var header = []string{"CustomerNo", "DepositType", "CheckNo", "InvoiceAmount", "InvoiceNo", "DiscountAmount", "AmountPosted", "Comment"}

// WriteCSV writes one cash-receipt row per payout whose order number uniquely matches an open Sage invoice.
// It reads the two CSVs, uses checkNo and comment for every output row, and returns the match count or a validation error.
// A non-positive Sage balance is already settled and is excluded; duplicate open PO numbers are rejected rather than guessing.
func WriteCSV(out io.Writer, summary, sage io.Reader, checkNo, comment string) (int, error) {
	if strings.TrimSpace(checkNo) == "" {
		return 0, fmt.Errorf("check number is required")
	}
	payoutReader := csv.NewReader(summary)
	payouts, err := payoutReader.ReadAll()
	if err != nil {
		return 0, fmt.Errorf("read Faire payout summary: %w", err)
	}
	sageReader := csv.NewReader(sage)
	// Sage leaves commas in Comment unquoted; they add fields after every field used for matching.
	sageReader.FieldsPerRecord = -1
	invoices, err := sageReader.ReadAll()
	if err != nil {
		return 0, fmt.Errorf("read Sage invoices: %w", err)
	}
	payoutColumns, err := columns(payouts, "Faire payout summary", "Order Number", "Payout Amount")
	if err != nil {
		return 0, err
	}
	sageColumns, err := columns(invoices, "Sage invoices", "Customer PO No.", "Invoice No.", "Amount", "Balance")
	if err != nil {
		return 0, err
	}

	wanted := make(map[string]bool, len(payouts)-1)
	for _, payout := range payouts[1:] {
		wanted[strings.TrimSpace(payout[payoutColumns["Order Number"]])] = true
	}
	open := make(map[string][]string)
	for rowNo, row := range invoices[1:] {
		balance, err := cents(row[sageColumns["Balance"]])
		if err != nil {
			return 0, fmt.Errorf("Sage row %d balance: %w", rowNo+2, err)
		}
		if balance <= 0 {
			continue
		}
		po := strings.TrimSpace(row[sageColumns["Customer PO No."]])
		if po == "" || !wanted[po] {
			continue
		}
		if _, exists := open[po]; exists {
			return 0, fmt.Errorf("more than one open Sage invoice has PO %q", po)
		}
		open[po] = row
	}

	rows := make([][]string, 0, len(payouts))
	seen := make(map[string]bool)
	for rowNo, payout := range payouts[1:] {
		order := strings.TrimSpace(payout[payoutColumns["Order Number"]])
		invoice, found := open[order]
		if !found || order == "" {
			continue
		}
		if seen[order] {
			return 0, fmt.Errorf("more than one Faire payout has order %q", order)
		}
		seen[order] = true
		amount, err := cents(invoice[sageColumns["Amount"]])
		if err != nil {
			return 0, fmt.Errorf("Sage invoice for order %q amount: %w", order, err)
		}
		posted, err := cents(payout[payoutColumns["Payout Amount"]])
		if err != nil {
			return 0, fmt.Errorf("Faire row %d payout amount: %w", rowNo+2, err)
		}
		invoiceNo := strings.TrimSpace(invoice[sageColumns["Invoice No."]])
		if invoiceNo == "" {
			return 0, fmt.Errorf("Sage invoice for order %q has no invoice number", order)
		}
		rows = append(rows, []string{"0090671", "C", checkNo, money(amount), invoiceNo, money(amount - posted), money(posted), comment})
	}
	if len(rows) == 0 {
		return 0, nil
	}
	writer := csv.NewWriter(out)
	if err := writer.Write(header); err != nil {
		return 0, fmt.Errorf("write cash receipts header: %w", err)
	}
	if err := writer.WriteAll(rows); err != nil {
		return 0, fmt.Errorf("write cash receipts rows: %w", err)
	}
	return len(rows), nil
}

// columns validates a CSV's named fields and required row width, returning field indexes for each requested name.
// It accepts a UTF-8 BOM and extra trailing Sage fields caused by unquoted commas in comments.
func columns(rows [][]string, source string, required ...string) (map[string]int, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s is empty", source)
	}
	rows[0][0] = strings.TrimPrefix(rows[0][0], "\ufeff")
	positions := make(map[string]int, len(rows[0]))
	for index, name := range rows[0] {
		positions[name] = index
	}
	for _, name := range required {
		if _, ok := positions[name]; !ok {
			return nil, fmt.Errorf("%s is missing %q column", source, name)
		}
	}
	for index, row := range rows[1:] {
		if len(row) < len(rows[0]) {
			return nil, fmt.Errorf("%s row %d has %d columns, expected at least %d", source, index+2, len(row), len(rows[0]))
		}
	}
	return positions, nil
}

// cents rounds a finite CSV decimal to the nearest cent and rejects missing, invalid, or out-of-range amounts.
// Rounding corrects Sage floating-point artifacts; the range leaves room for subtracting two cent amounts safely.
func cents(value string) (int64, error) {
	amount, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) || math.Abs(amount) >= float64(math.MaxInt64)/200 {
		return 0, fmt.Errorf("invalid money value %q", value)
	}
	return int64(math.Round(amount * 100)), nil
}

// money formats an integer cent amount for the Sage cash-receipts CSV, including negative discounts.
func money(value int64) string {
	if value < 0 {
		return "-" + money(-value)
	}
	return fmt.Sprintf("%d.%02d", value/100, value%100)
}

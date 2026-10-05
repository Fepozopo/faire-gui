// Package payouts reconciles a Faire payout summary with unpaid Sage invoices for cash receipts import.
package payouts

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	customerNumber          = "0090671"
	depositDateLayout       = "20060102"
	depositDigitCount       = 10
	depositAlphabetSize     = 26
	numericDepositSequences = depositDigitCount * depositDigitCount
	mixedDepositSequences   = depositDigitCount * depositAlphabetSize
	// MaxDepositSequences is the daily capacity of suffixes 00–99, 0A–9Z, A0–Z9, and AA–ZZ.
	MaxDepositSequences = numericDepositSequences + 2*mixedDepositSequences + depositAlphabetSize*depositAlphabetSize
)

// ErrDepositSequenceExhausted indicates matched invoices exceed the remaining daily deposit numbers.
var ErrDepositSequenceExhausted = fmt.Errorf("deposit number sequence exceeds ZZ; at most %d matched invoices are allowed per day", MaxDepositSequences)

// ErrInvalidDepositSequence indicates a starting offset outside the daily sequence's inclusive bounds.
var ErrInvalidDepositSequence = fmt.Errorf("starting deposit sequence must be between 0 and %d", MaxDepositSequences)

// WriteCSV writes headerless cash-receipt rows to out for payouts uniquely matching open Sage invoices.
// summary and sage supply the source CSVs. depositDate supplies the calendar date and day-of-year prefix;
// description and checkNo are required shared values, while comment is optional.
// Rows contain batch, deposit number/date/description/amount, customer, check, invoice, discount, and comment.
// startSequence is the next zero-based daily offset; suffixes advance through 00–99, 0A–9Z, A0–Z9, and AA–ZZ.
// An offset of MaxDepositSequences permits no matches; larger exports fail before writing output.
// It returns the match count and formatted total for display only; errors and no matches return an empty total.
// Validation finishes before output is written. Settled invoices are excluded and duplicate open POs are rejected.
func WriteCSV(out io.Writer, summary, sage io.Reader, depositDate time.Time, startSequence int, description, checkNo, comment string) (int, string, error) {
	if startSequence < 0 || startSequence > MaxDepositSequences {
		return 0, "", ErrInvalidDepositSequence
	}
	if depositDate.IsZero() {
		return 0, "", fmt.Errorf("deposit date is required")
	}
	if strings.TrimSpace(description) == "" {
		return 0, "", fmt.Errorf("deposit description is required")
	}
	if strings.TrimSpace(checkNo) == "" {
		return 0, "", fmt.Errorf("check number is required")
	}
	payoutReader := csv.NewReader(summary)
	payouts, err := payoutReader.ReadAll()
	if err != nil {
		return 0, "", fmt.Errorf("read Faire payout summary: %w", err)
	}
	sageReader := csv.NewReader(sage)
	// Sage leaves commas in Comment unquoted; they add fields after every field used for matching.
	sageReader.FieldsPerRecord = -1
	invoices, err := sageReader.ReadAll()
	if err != nil {
		return 0, "", fmt.Errorf("read Sage invoices: %w", err)
	}
	payoutColumns, err := columns(payouts, "Faire payout summary", "Order Number", "Payout Amount")
	if err != nil {
		return 0, "", err
	}
	sageColumns, err := columns(invoices, "Sage invoices", "Customer PO No.", "Invoice No.", "Amount", "Balance")
	if err != nil {
		return 0, "", err
	}

	wanted := make(map[string]bool, len(payouts)-1)
	for _, payout := range payouts[1:] {
		wanted[strings.TrimSpace(payout[payoutColumns["Order Number"]])] = true
	}
	open := make(map[string][]string)
	for rowNo, row := range invoices[1:] {
		balance, err := cents(row[sageColumns["Balance"]])
		if err != nil {
			return 0, "", fmt.Errorf("Sage row %d balance: %w", rowNo+2, err)
		}
		if balance <= 0 {
			continue
		}
		po := strings.TrimSpace(row[sageColumns["Customer PO No."]])
		if po == "" || !wanted[po] {
			continue
		}
		if _, exists := open[po]; exists {
			return 0, "", fmt.Errorf("more than one open Sage invoice has PO %q", po)
		}
		open[po] = row
	}

	rows := make([][]string, 0, len(payouts))
	dayPrefix := fmt.Sprintf("%03d", depositDate.YearDay())
	date := depositDate.Format(depositDateLayout)
	var totalPosted int64
	seen := make(map[string]bool)
	for rowNo, payout := range payouts[1:] {
		order := strings.TrimSpace(payout[payoutColumns["Order Number"]])
		invoice, found := open[order]
		if !found || order == "" {
			continue
		}
		if seen[order] {
			return 0, "", fmt.Errorf("more than one Faire payout has order %q", order)
		}
		seen[order] = true
		amount, err := cents(invoice[sageColumns["Amount"]])
		if err != nil {
			return 0, "", fmt.Errorf("Sage invoice for order %q amount: %w", order, err)
		}
		posted, err := cents(payout[payoutColumns["Payout Amount"]])
		if err != nil {
			return 0, "", fmt.Errorf("Faire row %d payout amount: %w", rowNo+2, err)
		}
		invoiceNo := strings.TrimSpace(invoice[sageColumns["Invoice No."]])
		if invoiceNo == "" {
			return 0, "", fmt.Errorf("Sage invoice for order %q has no invoice number", order)
		}
		if len(rows) == MaxDepositSequences-startSequence {
			return 0, "", ErrDepositSequenceExhausted
		}
		// Retain the total for the GUI, but export only the individual deposit amounts.
		discount := amount - posted
		if posted > 0 && totalPosted > math.MaxInt64-posted || posted < 0 && totalPosted < math.MinInt64-posted {
			return 0, "", fmt.Errorf("total amount posted exceeds supported range")
		}
		totalPosted += posted
		depositNo := dayPrefix + depositSequence(startSequence+len(rows))
		rows = append(rows, []string{"FAIRE", depositNo, date, description, money(posted), customerNumber, checkNo, invoiceNo, money(discount), comment})
	}
	if len(rows) == 0 {
		return 0, "", nil
	}
	total := money(totalPosted)
	writer := csv.NewWriter(out)
	if err := writer.WriteAll(rows); err != nil {
		return 0, "", fmt.Errorf("write cash receipts rows: %w", err)
	}
	return len(rows), total, nil
}

// depositSequence returns the two-character suffix for a zero-based daily index below MaxDepositSequences.
// Suffixes advance through 00–99, 0A–9Z, A0–Z9, and AA–ZZ, preserving the five-character deposit-number limit.
func depositSequence(index int) string {
	if index < numericDepositSequences {
		return fmt.Sprintf("%02d", index)
	}
	index -= numericDepositSequences
	if index < mixedDepositSequences {
		return string([]byte{'0' + byte(index/depositAlphabetSize), 'A' + byte(index%depositAlphabetSize)})
	}
	index -= mixedDepositSequences
	if index < mixedDepositSequences {
		return string([]byte{'A' + byte(index/depositDigitCount), '0' + byte(index%depositDigitCount)})
	}
	index -= mixedDepositSequences
	return string([]byte{'A' + byte(index/depositAlphabetSize), 'A' + byte(index%depositAlphabetSize)})
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

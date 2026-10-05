package payouts

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestWriteCSVDepositSequences verifies all suffix groups, unique five-character numbers, and atomic daily-limit rejection.
func TestWriteCSVDepositSequences(t *testing.T) {
	t.Parallel()
	for _, count := range []int{1296, 1297} {
		t.Run(fmt.Sprintf("%d invoices", count), func(t *testing.T) {
			t.Parallel()
			summary, sage := depositSources(count)
			var out bytes.Buffer
			gotCount, total, err := WriteCSV(&out, strings.NewReader(summary), strings.NewReader(sage), time.Date(2024, time.December, 31, 0, 0, 0, 0, time.UTC), 0, "Deposit", "Check", "")
			if count == 1297 {
				if !errors.Is(err, ErrDepositSequenceExhausted) || gotCount != 0 || total != "" || out.Len() != 0 {
					t.Fatalf("WriteCSV(%d invoices) = (%d, %q, %v), output bytes %d; want sequence exhaustion, zero count, empty total and output", count, gotCount, total, err, out.Len())
				}
				return
			}
			if err != nil || gotCount != 1296 || total != "1296.00" {
				t.Fatalf("WriteCSV(%d invoices) = (%d, %q, %v); want (1296, 1296.00, nil)", count, gotCount, total, err)
			}
			rows, err := csv.NewReader(&out).ReadAll()
			if err != nil || len(rows) != 1296 {
				t.Fatalf("read CSV for %d invoices = %d rows, %v; want 1296 rows", count, len(rows), err)
			}
			seen := make(map[string]int, len(rows))
			for index, row := range rows {
				if len(row) != 10 || len(row[1]) != 5 {
					t.Fatalf("row %d = %q; want ten columns with a five-character deposit number", index, row)
				}
				if previous, exists := seen[row[1]]; exists {
					t.Fatalf("deposit %q occurs at rows %d and %d; want unique deposit numbers", row[1], previous, index)
				}
				seen[row[1]] = index
			}
			for _, tc := range []struct {
				index int
				want  string
			}{{0, "36600"}, {99, "36699"}, {100, "3660A"}, {101, "3660B"}, {125, "3660Z"}, {126, "3661A"}, {359, "3669Z"}, {360, "366A0"}, {369, "366A9"}, {370, "366B0"}, {619, "366Z9"}, {620, "366AA"}, {621, "366AB"}, {645, "366AZ"}, {646, "366BA"}, {1295, "366ZZ"}} {
				if len(rows[tc.index]) != 10 || rows[tc.index][1] != tc.want {
					t.Errorf("deposit number at row %d = %q; want ten columns with deposit number %s", tc.index, rows[tc.index], tc.want)
				}
			}
		})
	}
}

// TestWriteCSVDepositDates verifies day-of-year padding and leap-year dates with an explicit zero starting offset.
func TestWriteCSVDepositDates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, date, number string
	}{
		{"first day", "20250101", "00100"},
		{"non-leap March", "20250301", "06000"},
		{"leap March", "20240301", "06100"},
		{"last non-leap day", "20251231", "36500"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			date, err := time.Parse("20060102", tc.date)
			if err != nil {
				t.Fatalf("parse fixture date %q: %v", tc.date, err)
			}
			summary, sage := depositSources(1)
			for export := range 2 {
				var out bytes.Buffer
				count, total, err := WriteCSV(&out, strings.NewReader(summary), strings.NewReader(sage), date, 0, "Deposit", "Check", "")
				if err != nil || count != 1 || total != "1.00" {
					t.Fatalf("export %d for date %s = (%d, %q, %v); want (1, 1.00, nil)", export, tc.date, count, total, err)
				}
				rows, err := csv.NewReader(&out).ReadAll()
				if err != nil || len(rows) != 1 || len(rows[0]) != 10 || rows[0][1] != tc.number || rows[0][2] != tc.date {
					t.Fatalf("export %d for date %s rows = %q, error %v; want deposit %s dated %s", export, tc.date, rows, err, tc.number, tc.date)
				}
			}
		})
	}
}

// TestWriteCSVRequiresDepositFields verifies missing dates and blank required fields fail before any output is written.
func TestWriteCSVRequiresDepositFields(t *testing.T) {
	t.Parallel()
	date := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, description, checkNo, wantError string
		date                                  time.Time
	}{
		{"zero date", "Deposit", "Check", "deposit date is required", time.Time{}},
		{"empty description", "", "Check", "deposit description is required", date},
		{"blank description", " \t", "Check", "deposit description is required", date},
		{"blank check", "Deposit", " \t", "check number is required", date},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			summary, sage := depositSources(1)
			var out bytes.Buffer
			count, total, err := WriteCSV(&out, strings.NewReader(summary), strings.NewReader(sage), tc.date, 0, tc.description, tc.checkNo, "")
			if err == nil || err.Error() != tc.wantError || count != 0 || total != "" || out.Len() != 0 {
				t.Fatalf("WriteCSV(%s) = (%d, %q, %v), output %q; want %q, zero count, empty total and output", tc.name, count, total, err, out.String(), tc.wantError)
			}
		})
	}
}

// TestWriteCSVStartingSequence verifies continued suffixes and rejection before output when the daily range is exceeded.
func TestWriteCSVStartingSequence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		start     int
		count     int
		numbers   []string
		wantError error
	}{
		{name: "continue at 32", start: 32, count: 2, numbers: []string{"00132", "00133"}},
		{name: "numeric to digit-letter", start: 99, count: 2, numbers: []string{"00199", "0010A"}},
		{name: "digit-letter continuation", start: 125, count: 2, numbers: []string{"0010Z", "0011A"}},
		{name: "digit-letter to letter-digit", start: 359, count: 2, numbers: []string{"0019Z", "001A0"}},
		{name: "letter-digit continuation", start: 369, count: 2, numbers: []string{"001A9", "001B0"}},
		{name: "letter-digit to letters", start: 619, count: 2, numbers: []string{"001Z9", "001AA"}},
		{name: "letter continuation", start: 645, count: 2, numbers: []string{"001AZ", "001BA"}},
		{name: "last suffix", start: 1295, count: 1, numbers: []string{"001ZZ"}},
		{name: "partial overflow", start: 1295, count: 2, wantError: ErrDepositSequenceExhausted},
		{name: "exhausted day", start: 1296, count: 1, wantError: ErrDepositSequenceExhausted},
		{name: "exhausted day without matches", start: 1296, count: 0},
		{name: "negative offset", start: -1, count: 1, wantError: ErrInvalidDepositSequence},
		{name: "offset beyond capacity", start: 1297, count: 1, wantError: ErrInvalidDepositSequence},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			summary, sage := depositSources(tc.count)
			var out bytes.Buffer
			count, total, err := WriteCSV(&out, strings.NewReader(summary), strings.NewReader(sage), time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC), tc.start, "Deposit", "Check", "")
			if tc.wantError != nil {
				if !errors.Is(err, tc.wantError) || count != 0 || total != "" || out.Len() != 0 {
					t.Fatalf("WriteCSV(start=%d, count=%d) = (%d, %q, %v), output %q; want zero count, empty total/output, %v", tc.start, tc.count, count, total, err, out.String(), tc.wantError)
				}
				return
			}
			wantTotal := ""
			if tc.count > 0 {
				wantTotal = fmt.Sprintf("%d.00", tc.count)
			}
			if err != nil || count != tc.count || total != wantTotal {
				t.Fatalf("WriteCSV(start=%d, count=%d) = (%d, %q, %v); want (%d, %q, nil)", tc.start, tc.count, count, total, err, tc.count, wantTotal)
			}
			rows, err := csv.NewReader(&out).ReadAll()
			if err != nil || len(rows) != len(tc.numbers) {
				t.Fatalf("CSV(start=%d, count=%d) = %q, %v; want %d rows", tc.start, tc.count, rows, err, len(tc.numbers))
			}
			for index, number := range tc.numbers {
				if rows[index][1] != number {
					t.Errorf("CSV(start=%d) row %d deposit = %q; want %q", tc.start, index, rows[index][1], number)
				}
			}
		})
	}
}

// depositSources constructs count uniquely matching one-dollar payouts and open invoices for public export tests.
func depositSources(count int) (string, string) {
	var summary, sage strings.Builder
	summary.WriteString("Order Number,Payout Amount\n")
	sage.WriteString("Customer PO No.,Invoice No.,Amount,Balance\n")
	for index := range count {
		fmt.Fprintf(&summary, "ORDER%d,1\n", index)
		fmt.Fprintf(&sage, "ORDER%d,INV%d,1,1\n", index, index)
	}
	return summary.String(), sage.String()
}

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

// TestWriteCSVDepositSequences verifies numeric/letter transitions, the five-character cap, and atomic limit rejection.
func TestWriteCSVDepositSequences(t *testing.T) {
	t.Parallel()
	for _, count := range []int{776, 777} {
		t.Run(fmt.Sprintf("%d invoices", count), func(t *testing.T) {
			t.Parallel()
			summary, sage := depositSources(count)
			var out bytes.Buffer
			gotCount, total, err := WriteCSV(&out, strings.NewReader(summary), strings.NewReader(sage), time.Date(2024, time.December, 31, 0, 0, 0, 0, time.UTC), "Deposit", "Check", "")
			if count == 777 {
				if !errors.Is(err, ErrDepositSequenceExhausted) || gotCount != 0 || total != "" || out.Len() != 0 {
					t.Fatalf("WriteCSV(%d invoices) = (%d, %q, %v), output bytes %d; want sequence exhaustion, zero count, empty total and output", count, gotCount, total, err, out.Len())
				}
				return
			}
			if err != nil || gotCount != 776 || total != "776.00" {
				t.Fatalf("WriteCSV(%d invoices) = (%d, %q, %v); want (776, 776.00, nil)", count, gotCount, total, err)
			}
			rows, err := csv.NewReader(&out).ReadAll()
			if err != nil || len(rows) != 776 {
				t.Fatalf("read CSV for %d invoices = %d rows, %v; want 776 rows", count, len(rows), err)
			}
			for _, tc := range []struct {
				index int
				want  string
			}{{0, "36600"}, {99, "36699"}, {100, "366AA"}, {101, "366AB"}, {125, "366AZ"}, {126, "366BA"}, {775, "366ZZ"}} {
				if len(rows[tc.index]) != 10 || rows[tc.index][1] != tc.want {
					t.Errorf("deposit number at row %d = %q; want ten columns with deposit number %s", tc.index, rows[tc.index], tc.want)
				}
			}
		})
	}
}

// TestWriteCSVDepositDates verifies day-of-year padding, leap-year dates, and a fresh sequence for every export.
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
				count, total, err := WriteCSV(&out, strings.NewReader(summary), strings.NewReader(sage), date, "Deposit", "Check", "")
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
			count, total, err := WriteCSV(&out, strings.NewReader(summary), strings.NewReader(sage), tc.date, tc.description, tc.checkNo, "")
			if err == nil || err.Error() != tc.wantError || count != 0 || total != "" || out.Len() != 0 {
				t.Fatalf("WriteCSV(%s) = (%d, %q, %v), output %q; want %q, zero count, empty total and output", tc.name, count, total, err, out.String(), tc.wantError)
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

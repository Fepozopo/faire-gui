package application

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWritePayoutCSVToDirectory verifies headerless exports persist the batch total and per-row amounts, while empty matches do not.
func TestWritePayoutCSVToDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	summary := filepath.Join(dir, "faire.csv")
	sage := filepath.Join(dir, "sage.csv")
	if err := os.WriteFile(summary, []byte("Order Number,Payout Amount\nORDER1,363\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sage, []byte("Customer PO No.,Invoice No.,Amount,Balance\nORDER1,0108501,390,390\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "output")
	filename, count, total, err := writePayoutCSVToDirectory(output, summary, sage, "CHECK01", "Comment")
	if err != nil || count != 1 || total != "363.00" || filename == "" {
		t.Fatalf("writePayoutCSVToDirectory(matched) = (%q, %d, %q, %v), want filename, 1, 363.00, nil", filename, count, total, err)
	}
	file, err := os.Open(filepath.Join(output, filename))
	if err != nil {
		t.Fatalf("open exported CSV %q: %v", filename, err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil || len(rows) != 1 || rows[0][0] != "0090671" || rows[0][3] != "363.00" || rows[0][5] != "27.00" || rows[0][6] != "363.00" {
		t.Fatalf("exported rows = %q, error = %v; want one headerless row with 363.00 invoice and posted, 27.00 discount", rows, err)
	}
	if err := os.WriteFile(sage, []byte("Customer PO No.,Invoice No.,Amount,Balance\nORDER1,0108501,390,0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	filename, count, total, err = writePayoutCSVToDirectory(output, summary, sage, "CHECK01", "Comment")
	if err != nil || filename != "" || count != 0 || total != "" {
		t.Fatalf("writePayoutCSVToDirectory(settled) = (%q, %d, %q, %v), want empty filename, 0, empty total, nil", filename, count, total, err)
	}
	files, err := os.ReadDir(output)
	if err != nil || len(files) != 1 {
		t.Fatalf("output files after settled invoice = %v, error = %v; want only previous CSV", files, err)
	}
}

// TestExportPayoutsShowsTotal verifies the export status and every CSV row report the same matched payout total.
func TestExportPayoutsShowsTotal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	summary := filepath.Join(home, "faire.csv")
	sage := filepath.Join(home, "sage.csv")
	if err := os.WriteFile(summary, []byte("Order Number,Payout Amount\nA,363\nB,2.05\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sage, []byte("Customer PO No.,Invoice No.,Amount,Balance\nA,0108501,390,390\nB,0108502,3,3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.payouts.summary.SetText(summary)
	ui.payouts.sage.SetText(sage)
	ui.payouts.checkNo.SetText("CHECK01")
	ui.exportPayouts()
	ui.workers.Wait()
	ui.drainPayoutResults()
	if !strings.Contains(ui.payouts.status, "Exported 2 matched payouts") || !strings.Contains(ui.payouts.status, "Total amount posted: $365.05.") {
		t.Fatalf("payout export status = %q, want two matched payouts and total amount posted $365.05", ui.payouts.status)
	}
	files, err := os.ReadDir(filepath.Join(home, "Downloads"))
	if err != nil || len(files) != 1 {
		t.Fatalf("Downloads after export = %v, error = %v; want one CSV", files, err)
	}
	file, err := os.Open(filepath.Join(home, "Downloads", files[0].Name()))
	if err != nil {
		t.Fatalf("open exported CSV %q: %v", files[0].Name(), err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("read exported CSV %q: %v", files[0].Name(), err)
	}
	if len(rows) != 2 || rows[0][3] != "365.05" || rows[1][3] != "365.05" {
		t.Fatalf("exported rows = %q, want two rows with status total 365.05 in column four", rows)
	}
}

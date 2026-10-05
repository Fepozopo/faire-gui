package application

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestWritePayoutCSVToDirectory verifies timestamped receipt filenames, the ten-column export, and display-only total.
// Empty matches must create no file.
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
	date := time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC)
	filename, count, total, err := writePayoutCSVToDirectory(output, filepath.Join(dir, "settings.json"), summary, sage, date, "Deposit", "CHECK01", "Comment")
	if err != nil || count != 1 || total != "363.00" || filename == "" {
		t.Fatalf("writePayoutCSVToDirectory(matched) = (%q, %d, %q, %v), want filename, 1, 363.00, nil", filename, count, total, err)
	}
	if !regexp.MustCompile(`^faire_cache_receipts_[0-9]+\.csv$`).MatchString(filename) {
		t.Fatalf("matched payout filename = %q; want faire_cache_receipts_<numeric timestamp>.csv", filename)
	}
	file, err := os.Open(filepath.Join(output, filename))
	if err != nil {
		t.Fatalf("open exported CSV %q: %v", filename, err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	want := [][]string{{"FAIRE", "06000", "20240229", "Deposit", "363.00", "0090671", "CHECK01", "0108501", "27.00", "Comment"}}
	if err != nil || !reflect.DeepEqual(rows, want) {
		t.Fatalf("exported rows = %q, error = %v; want %q and no error", rows, err, want)
	}
	if err := os.WriteFile(sage, []byte("Customer PO No.,Invoice No.,Amount,Balance\nORDER1,0108501,390,0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	filename, count, total, err = writePayoutCSVToDirectory(output, filepath.Join(dir, "settings.json"), summary, sage, date, "Deposit", "CHECK01", "Comment")
	if err != nil || filename != "" || count != 0 || total != "" {
		t.Fatalf("writePayoutCSVToDirectory(settled) = (%q, %d, %q, %v), want empty filename, 0, empty total, nil", filename, count, total, err)
	}
	files, err := os.ReadDir(output)
	if err != nil || len(files) != 1 {
		t.Fatalf("output files after settled invoice = %v, error = %v; want only previous CSV", files, err)
	}
}

// TestExportPayoutsRequiresFields verifies each required GUI input prevents export when blank.
func TestExportPayoutsRequiresFields(t *testing.T) {
	for _, missing := range []string{"summary", "sage", "description", "check number"} {
		t.Run(missing, func(t *testing.T) {
			ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
			ui.payouts.summary.SetText("faire.csv")
			ui.payouts.sage.SetText("sage.csv")
			ui.payouts.description.SetText("Deposit")
			ui.payouts.checkNo.SetText("Check")
			switch missing {
			case "summary":
				ui.payouts.summary.SetText("")
			case "sage":
				ui.payouts.sage.SetText("")
			case "description":
				ui.payouts.description.SetText(" \t")
			case "check number":
				ui.payouts.checkNo.SetText("")
			}
			ui.exportPayouts()
			ui.workers.Wait()
			ui.drainPayoutResults()
			want := "Select both CSV files and enter a deposit description and check number."
			if ui.payouts.status != want {
				t.Fatalf("export with missing %s status = %q; want %q", missing, ui.payouts.status, want)
			}
		})
	}
}

// TestExportPayoutsShowsTotal verifies the GUI retains the total while the CSV contains individual deposit amounts and no batch total.
func TestExportPayoutsShowsTotal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("AppData", filepath.Join(home, "config"))
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
	ui.payouts.description.SetText("Deposit")
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
	if len(rows) != 2 || len(rows[0]) != 10 || len(rows[1]) != 10 || rows[0][3] != "Deposit" || rows[1][3] != "Deposit" || rows[0][4] != "363.00" || rows[1][4] != "2.05" || rows[0][9] != "" || rows[1][9] != "" {
		t.Fatalf("exported rows = %q, want two ten-column rows with description Deposit, amounts 363.00 and 2.05, and blank optional comments", rows)
	}
}

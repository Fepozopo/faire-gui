package application

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"
)

// TestWritePayoutCSVToDirectory verifies that successful exports persist the cash-receipts file, while empty matches do not.
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
	filename, count, err := writePayoutCSVToDirectory(output, summary, sage, "CHECK01", "Comment")
	if err != nil || count != 1 || filename == "" {
		t.Fatalf("writePayoutCSVToDirectory(matched) = (%q, %d, %v), want filename, 1, nil", filename, count, err)
	}
	file, err := os.Open(filepath.Join(output, filename))
	if err != nil {
		t.Fatalf("open exported CSV %q: %v", filename, err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil || len(rows) != 2 || rows[1][5] != "27.00" {
		t.Fatalf("exported rows = %q, error = %v; want header and one row with 27.00 discount", rows, err)
	}
	if err := os.WriteFile(sage, []byte("Customer PO No.,Invoice No.,Amount,Balance\nORDER1,0108501,390,0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	filename, count, err = writePayoutCSVToDirectory(output, summary, sage, "CHECK01", "Comment")
	if err != nil || filename != "" || count != 0 {
		t.Fatalf("writePayoutCSVToDirectory(settled) = (%q, %d, %v), want empty filename, 0, nil", filename, count, err)
	}
	files, err := os.ReadDir(output)
	if err != nil || len(files) != 1 {
		t.Fatalf("output files after settled invoice = %v, error = %v; want only previous CSV", files, err)
	}
}

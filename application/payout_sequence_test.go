package application

import (
	"encoding/csv"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Fepozopo/faire-gui/features/payouts"
)

// TestPayoutSequenceDailyCheckpoint verifies persisted numeric/mixed/letter offsets, legacy defaults, and local-date resets.
func TestPayoutSequenceDailyCheckpoint(t *testing.T) {
	t.Parallel()
	local := time.FixedZone("business", -5*60*60)
	for _, tc := range []struct {
		name, checkpoint, wantNumber string
		date                         time.Time
		wantNext                     int
	}{
		{name: "missing settings", date: time.Date(2025, 1, 1, 0, 0, 0, 0, local), wantNumber: "00100", wantNext: 1},
		{name: "legacy settings", checkpoint: `{"version":1,"sageFulfillment":{"enabled":true}}`, date: time.Date(2025, 1, 1, 0, 0, 0, 0, local), wantNumber: "00100", wantNext: 1},
		{name: "same local day after UTC midnight", checkpoint: `{"version":1,"sageFulfillment":{"enabled":true},"payoutSequence":{"date":"2025-01-01","nextSequence":32}}`, date: time.Date(2025, 1, 1, 23, 59, 0, 0, local), wantNumber: "00132", wantNext: 33},
		{name: "resume digit-letter suffix", checkpoint: `{"version":1,"sageFulfillment":{"enabled":true},"payoutSequence":{"date":"2025-01-01","nextSequence":100}}`, date: time.Date(2025, 1, 1, 12, 0, 0, 0, local), wantNumber: "0010A", wantNext: 101},
		{name: "resume letter-digit suffix", checkpoint: `{"version":1,"sageFulfillment":{"enabled":true},"payoutSequence":{"date":"2025-01-01","nextSequence":360}}`, date: time.Date(2025, 1, 1, 12, 0, 0, 0, local), wantNumber: "001A0", wantNext: 361},
		{name: "resume letter suffix", checkpoint: `{"version":1,"sageFulfillment":{"enabled":true},"payoutSequence":{"date":"2025-01-01","nextSequence":620}}`, date: time.Date(2025, 1, 1, 12, 0, 0, 0, local), wantNumber: "001AA", wantNext: 621},
		{name: "last daily suffix", checkpoint: `{"version":1,"sageFulfillment":{"enabled":true},"payoutSequence":{"date":"2025-01-01","nextSequence":1295}}`, date: time.Date(2025, 1, 1, 12, 0, 0, 0, local), wantNumber: "001ZZ", wantNext: 1296},
		{name: "next local day", checkpoint: `{"version":1,"sageFulfillment":{"enabled":true},"payoutSequence":{"date":"2025-01-01","nextSequence":32}}`, date: time.Date(2025, 1, 2, 0, 0, 0, 0, local), wantNumber: "00200", wantNext: 1},
		{name: "same day of year next year", checkpoint: `{"version":1,"sageFulfillment":{"enabled":true},"payoutSequence":{"date":"2024-01-01","nextSequence":32}}`, date: time.Date(2025, 1, 1, 0, 0, 0, 0, local), wantNumber: "00100", wantNext: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			settingsFile := filepath.Join(directory, "settings.json")
			if tc.checkpoint != "" {
				if err := os.WriteFile(settingsFile, []byte(tc.checkpoint), 0o600); err != nil {
					t.Fatalf("write checkpoint %q: %v", tc.checkpoint, err)
				}
			}
			summary, sage := payoutSequenceSources(t, directory, 1)
			output := filepath.Join(directory, "output")
			filename, count, total, err := writePayoutCSVToDirectory(output, settingsFile, summary, sage, tc.date, "Deposit", "Check", "")
			if err != nil || count != 1 || total != "1.00" {
				t.Fatalf("export(%s) = (%q, %d, %q, %v); want filename, 1, 1.00, nil", tc.name, filename, count, total, err)
			}
			rows := readPayoutSequenceCSV(t, filepath.Join(output, filename))
			if len(rows) != 1 || rows[0][1] != tc.wantNumber || rows[0][2] != tc.date.Format("20060102") {
				t.Fatalf("export(%s) rows = %q; want deposit %s dated %s", tc.name, rows, tc.wantNumber, tc.date.Format("20060102"))
			}
			assertPayoutCheckpoint(t, settingsFile, tc.date.Format("2006-01-02"), tc.wantNext)
			if tc.checkpoint != "" {
				enabled, err := loadSageFulfillmentSettingsFile(settingsFile)
				if err != nil || !enabled {
					t.Fatalf("Sage setting after export(%s) = (%t, %v); want (true, nil)", tc.name, enabled, err)
				}
			}
		})
	}
}

// TestPayoutSequenceCountsMatchedExports verifies 32 matched rows use 00–31 and a later export reads 32 from disk.
func TestPayoutSequenceCountsMatchedExports(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	settingsFile := filepath.Join(directory, "settings.json")
	output := filepath.Join(directory, "output")
	date := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	summary, sage := payoutSequenceSources(t, directory, 32)
	filename, count, total, err := writePayoutCSVToDirectory(output, settingsFile, summary, sage, date, "Deposit", "Check", "")
	if err != nil || count != 32 || total != "32.00" {
		t.Fatalf("first export = (%q, %d, %q, %v); want filename, 32, 32.00, nil", filename, count, total, err)
	}
	rows := readPayoutSequenceCSV(t, filepath.Join(output, filename))
	if len(rows) != 32 || rows[0][1] != "00100" || rows[31][1] != "00131" {
		t.Fatalf("first export rows = %q; want 32 deposits from 00100 through 00131", rows)
	}
	assertPayoutCheckpoint(t, settingsFile, "2025-01-01", 32)
	// A preference update between exports must not erase the persisted offset.
	if err := saveSageFulfillmentSettingsFile(settingsFile, true); err != nil {
		t.Fatalf("enable Sage between exports: %v", err)
	}
	summary, sage = payoutSequenceSources(t, directory, 1)
	filename, count, total, err = writePayoutCSVToDirectory(output, settingsFile, summary, sage, date, "Deposit", "Check", "")
	if err != nil || count != 1 || total != "1.00" {
		t.Fatalf("second export = (%q, %d, %q, %v); want filename, 1, 1.00, nil", filename, count, total, err)
	}
	rows = readPayoutSequenceCSV(t, filepath.Join(output, filename))
	if len(rows) != 1 || rows[0][1] != "00132" {
		t.Fatalf("second export rows = %q; want one deposit numbered 00132", rows)
	}
	assertPayoutCheckpoint(t, settingsFile, "2025-01-01", 33)
}

// TestPayoutSequenceUnsuccessfulExports verifies no matches and failures leave both the checkpoint and output unchanged.
func TestPayoutSequenceUnsuccessfulExports(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		start     int
		wantError error
	}{
		{name: "no matches", start: 32},
		{name: "missing input", start: 32, wantError: os.ErrNotExist},
		{name: "invalid output directory", start: 32},
		{name: "daily capacity exhausted", start: 1296, wantError: payouts.ErrDepositSequenceExhausted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			settingsFile := filepath.Join(directory, "settings.json")
			checkpoint := fmt.Sprintf(`{"version":1,"sageFulfillment":{"enabled":true},"payoutSequence":{"date":"2025-01-01","nextSequence":%d}}`, tc.start)
			if err := os.WriteFile(settingsFile, []byte(checkpoint), 0o600); err != nil {
				t.Fatalf("write checkpoint %q: %v", checkpoint, err)
			}
			summary, sage := payoutSequenceSources(t, directory, 1)
			output := filepath.Join(directory, "output")
			switch tc.name {
			case "no matches":
				if err := os.WriteFile(sage, []byte("Customer PO No.,Invoice No.,Amount,Balance\nORDER0,INV0,1,0\n"), 0o600); err != nil {
					t.Fatalf("write settled invoice: %v", err)
				}
			case "missing input":
				summary = filepath.Join(directory, "missing.csv")
			case "invalid output directory":
				if err := os.WriteFile(output, nil, 0o600); err != nil {
					t.Fatalf("create output obstruction: %v", err)
				}
			}
			filename, count, total, err := writePayoutCSVToDirectory(output, settingsFile, summary, sage, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), "Deposit", "Check", "")
			matchesError := errors.Is(err, tc.wantError)
			wantError := fmt.Sprint(tc.wantError)
			if tc.name == "invalid output directory" {
				// MkdirAll uses different OS error codes for a file obstructing a directory; the operation and path are stable.
				var pathErr *os.PathError
				matchesError = errors.As(err, &pathErr) && pathErr.Op == "mkdir" && pathErr.Path == output
				wantError = "os.PathError for mkdir of " + output
			}
			if !matchesError || filename != "" || count != 0 || total != "" {
				t.Fatalf("export(%s) = (%q, %d, %q, %v); want empty filename/total, zero count, %s", tc.name, filename, count, total, err, wantError)
			}
			data, readErr := os.ReadFile(settingsFile)
			if readErr != nil || string(data) != checkpoint {
				t.Fatalf("checkpoint after %s = (%q, %v); want (%q, nil)", tc.name, data, readErr, checkpoint)
			}
			if tc.name != "invalid output directory" {
				files, readErr := os.ReadDir(output)
				if !errors.Is(readErr, os.ErrNotExist) && readErr != nil || len(files) != 0 {
					t.Fatalf("output after %s = (%v, %v); want no files", tc.name, files, readErr)
				}
			}
		})
	}
}

// payoutSequenceSources writes minimal matching CSV inputs plus unmatched and settled payouts, returning their paths.
func payoutSequenceSources(t *testing.T, directory string, count int) (string, string) {
	t.Helper()
	summary := "Order Number,Payout Amount\nUNMATCHED,1\nSETTLED,1\n"
	sage := "Customer PO No.,Invoice No.,Amount,Balance\nSETTLED,PAID,1,0\n"
	for index := range count {
		summary += fmt.Sprintf("ORDER%d,1\n", index)
		sage += fmt.Sprintf("ORDER%d,INV%d,1,1\n", index, index)
	}
	summaryPath, sagePath := filepath.Join(directory, "faire.csv"), filepath.Join(directory, "sage.csv")
	for _, fixture := range []struct{ path, contents string }{{summaryPath, summary}, {sagePath, sage}} {
		if err := os.WriteFile(fixture.path, []byte(fixture.contents), 0o600); err != nil {
			t.Fatalf("write payout fixture %q: %v", fixture.path, err)
		}
	}
	return summaryPath, sagePath
}

// readPayoutSequenceCSV reads an emitted receipt CSV, failing the test on filesystem or parsing errors.
func readPayoutSequenceCSV(t *testing.T, path string) [][]string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open receipt %q: %v", path, err)
	}
	t.Cleanup(func() { _ = file.Close() })
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("read receipt %q: %v", path, err)
	}
	for index, row := range rows {
		if len(row) != 10 {
			t.Fatalf("receipt %q row %d = %q; want ten columns", path, index, row)
		}
	}
	return rows
}

// assertPayoutCheckpoint checks the externally persisted date and offset without using the production settings loader.
func assertPayoutCheckpoint(t *testing.T, path, date string, next int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read checkpoint %q: %v", path, err)
	}
	var document struct {
		PayoutSequence struct {
			Date         string `json:"date"`
			NextSequence int    `json:"nextSequence"`
		} `json:"payoutSequence"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode checkpoint %q: %v", path, err)
	}
	if document.PayoutSequence.Date != date || document.PayoutSequence.NextSequence != next {
		t.Fatalf("checkpoint %q = %s; want date %s, nextSequence %d", path, data, date, next)
	}
}

package application

import (
	"fmt"
	"os"

	"path/filepath"

	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Fepozopo/faire-gui/features/payouts"
)

// payoutPageState retains the input fields, buttons, and status for the local cash-receipts workflow.
// File selection and export run off the frame goroutine, with completed results applied only on the UI thread.
type payoutPageState struct {
	list                                         widget.List
	summary, sage, description, checkNo, comment widget.Editor
	browseSummary, browseSage, export            widget.Clickable
	busy                                         bool
	status                                       string
	results                                      chan payoutResult
}

// payoutResult carries either a file picker selection or the completed export status back to Gio.
type payoutResult struct {
	field  string
	path   string
	status string
}

// layoutPayouts renders the form for choosing Faire and Sage files and exporting matched cash receipts.
// It accepts the current Gio context and returns the scrollable form dimensions, including required
// deposit description and check number editors and an optional comment editor.
func (ui *DesktopUI) layoutPayouts(gtx layout.Context) layout.Dimensions {
	page := &ui.payouts
	if !page.busy {
		if page.browseSummary.Clicked(gtx) {
			ui.choosePayoutFile("summary")
		}
		if page.browseSage.Clicked(gtx) {
			ui.choosePayoutFile("sage")
		}
		if page.export.Clicked(gtx) {
			ui.exportPayouts()
		}
	}
	return page.list.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(material.H3(ui.theme, "Payouts").Layout),
			fieldSpacer(),
			layout.Rigid(bodyText(ui.theme, "Match Faire payouts to unpaid Sage invoices and export a cash receipts CSV to Downloads.", mutedTextColor)),
			fieldSpacer(),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.payoutFileField(gtx, "Faire payout summary CSV", &page.summary, &page.browseSummary)
			}),
			fieldSpacer(),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.payoutFileField(gtx, "Sage invoices export CSV", &page.sage, &page.browseSage)
			}),
			fieldSpacer(),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.payoutTextField(gtx, "Deposit description (required)", &page.description)
			}),
			fieldSpacer(),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.payoutTextField(gtx, "Check number (required)", &page.checkNo)
			}),
			fieldSpacer(),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.payoutTextField(gtx, "Comment (optional)", &page.comment)
			}),
			fieldSpacer(),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if page.busy {
					return disabledPrimaryButton(ui.theme, "Working…")(gtx)
				}
				return primaryButton(ui.theme, &page.export, "Export cash receipts CSV")(gtx)
			}),
			fieldSpacer(),
			layout.Rigid(statusText(ui.theme, page.status)),
		)
	})
}

// payoutFileField draws a labeled path editor with an adjacent native file-picker button.
// Manual path entry remains available when a picker is unavailable on the host OS.
func (ui *DesktopUI) payoutFileField(gtx layout.Context, label string, editor *widget.Editor, browse *widget.Clickable) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(material.Body1(ui.theme, label).Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return outlinedInputField(gtx, ui.theme, editor, "CSV file path")
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if ui.payouts.busy {
						return disabledPrimaryButton(ui.theme, "Browse…")(gtx)
					}
					return primaryButton(ui.theme, browse, "Browse…")(gtx)
				}),
			)
		}),
	)
}

// payoutTextField draws a labeled single-line value shared by all matched receipt rows.
func (ui *DesktopUI) payoutTextField(gtx layout.Context, label string, editor *widget.Editor) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(material.Body1(ui.theme, label).Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return outlinedInputField(gtx, ui.theme, editor, label) }),
	)
}

// choosePayoutFile opens a platform file chooser in a worker so the application remains responsive.
// field selects either the Faire summary or Sage path editor; cancellation preserves the previous path.
func (ui *DesktopUI) choosePayoutFile(field string) {
	ui.payouts.busy = true
	ui.payouts.status = "Choose a CSV file…"
	ui.workers.Add(1)
	go func() {
		defer ui.workers.Done()
		path, err := chooseCSVFile(ui.ctx)
		result := payoutResult{field: field, path: path}
		if err != nil && ui.ctx.Err() == nil {
			result.status = "File selection was canceled or unavailable. You can enter the file path manually."
		}
		select {
		case ui.payouts.results <- result:
		case <-ui.ctx.Done():
		}
		ui.invalidate()
	}()
}

// exportPayouts validates inputs and starts a background local-only export to Downloads.
// Both file paths, deposit description, and check number are required; only the comment is optional.
// It snapshots editor values before starting the worker so subsequent edits cannot change an in-progress export.
func (ui *DesktopUI) exportPayouts() {
	page := &ui.payouts
	summary, sage := strings.TrimSpace(page.summary.Text()), strings.TrimSpace(page.sage.Text())
	description := strings.TrimSpace(page.description.Text())
	checkNo, comment := strings.TrimSpace(page.checkNo.Text()), page.comment.Text()
	if summary == "" || sage == "" || description == "" || checkNo == "" {
		page.status = "Select both CSV files and enter a deposit description and check number."
		return
	}
	page.busy = true
	page.status = "Matching payouts to open invoices…"
	ui.workers.Add(1)
	go func() {
		defer ui.workers.Done()
		filename, count, total, err := writePayoutCSV(summary, sage, description, checkNo, comment)
		result := payoutResult{}
		switch {
		case err != nil:
			result.status = "Export failed: " + err.Error()
		case count == 0:
			result.status = "No Faire payouts matched open Sage invoices; no file was created."
		default:
			result.status = fmt.Sprintf("Exported %d matched payouts to Downloads as %s. Total amount posted: $%s.", count, filename, total)
		}
		select {
		case page.results <- result:
		case <-ui.ctx.Done():
		}
		ui.invalidate()
	}()
}

// writePayoutCSV reconciles the two named CSVs and writes a complete cash-receipts file to Downloads.
// It uses today's local date and the supplied receipt fields, continuing the persisted daily sequence across restarts.
// It returns the generated filename, match count, and display-only total; errors and no matches return zero values.
// Failed exports remove any output; if removal also fails, the error identifies the file that must not be imported.
func writePayoutCSV(summaryPath, sagePath, description, checkNo, comment string) (string, int, string, error) {
	depositDate := time.Now()
	directory, err := downloadsDirectory()
	if err != nil {
		return "", 0, "", err
	}
	settingsFile, err := settingsPath()
	if err != nil {
		return "", 0, "", err
	}
	return writePayoutCSVToDirectory(directory, settingsFile, summaryPath, sagePath, depositDate, description, checkNo, comment)
}

// writePayoutCSVToDirectory reconciles summaryPath and sagePath into faire_cache_receipts_<timestamp>.csv in directory.
// settingsFile stores the daily sequence; depositDate and the supplied receipt fields determine the output values.
// Explicit paths and date permit isolated tests. Only successfully published, nonempty exports advance the checkpoint.
// It returns the filename, match count, and display-only total; failures return zero values and remove the output.
// If rollback removal also fails, the error identifies the file that must not be imported.
func writePayoutCSVToDirectory(directory, settingsFile, summaryPath, sagePath string, depositDate time.Time, description, checkNo, comment string) (string, int, string, error) {
	// Keep the read/modify/write cycle serialized with other exports and Sage preference updates.
	settingsMu.Lock()
	defer settingsMu.Unlock()
	settings, err := loadSettingsFile(settingsFile)
	if err != nil {
		return "", 0, "", err
	}
	date := depositDate.Format(time.DateOnly)
	startSequence := 0
	if settings.PayoutSequence.Date == date {
		startSequence = settings.PayoutSequence.NextSequence
	}
	summary, err := os.Open(summaryPath)
	if err != nil {
		return "", 0, "", fmt.Errorf("open Faire summary: %w", err)
	}
	defer summary.Close()
	sage, err := os.Open(sagePath)
	if err != nil {
		return "", 0, "", fmt.Errorf("open Sage export: %w", err)
	}
	defer sage.Close()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", 0, "", err
	}
	temporary, err := os.CreateTemp(directory, ".faire-cache-receipts-*.csv")
	if err != nil {
		return "", 0, "", err
	}
	defer os.Remove(temporary.Name())
	count, total, writeErr := payouts.WriteCSV(temporary, summary, sage, depositDate, startSequence, description, checkNo, comment)
	closeErr := temporary.Close()
	if writeErr != nil {
		return "", 0, "", writeErr
	}
	if closeErr != nil {
		return "", 0, "", closeErr
	}
	if count == 0 {
		return "", 0, "", nil
	}
	filename := fmt.Sprintf("faire_cache_receipts_%d.csv", time.Now().UnixNano())
	outputPath := filepath.Join(directory, filename)
	if err := os.Rename(temporary.Name(), outputPath); err != nil {
		return "", 0, "", err
	}
	settings.PayoutSequence = payoutSequenceSettings{Date: date, NextSequence: startSequence + count}
	if err := saveSettingsFile(settingsFile, settings); err != nil {
		// An untracked receipt file would reuse deposit numbers on retry, so do not leave it available for import.
		if removeErr := os.Remove(outputPath); removeErr != nil {
			return "", 0, "", fmt.Errorf("save payout checkpoint: %w; could not remove %s: %v; do not import this file", err, outputPath, removeErr)
		}
		return "", 0, "", fmt.Errorf("save payout checkpoint: %w", err)
	}
	return filename, count, total, nil
}

// drainPayoutResults applies completed worker results to persistent editors and status on the Gio frame goroutine.
func (ui *DesktopUI) drainPayoutResults() {
	for {
		select {
		case result := <-ui.payouts.results:
			ui.payouts.busy = false
			ui.payouts.status = result.status
			if result.path != "" {
				switch result.field {
				case "summary":
					ui.payouts.summary.SetText(result.path)
				case "sage":
					ui.payouts.sage.SetText(result.path)
				}
				ui.payouts.status = "Selected " + result.path
			}
		default:
			return
		}
	}
}

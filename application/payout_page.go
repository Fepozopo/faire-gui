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
	list                              widget.List
	summary, sage, checkNo, comment   widget.Editor
	browseSummary, browseSage, export widget.Clickable
	busy                              bool
	status                            string
	results                           chan payoutResult
}

// payoutResult carries either a file picker selection or the completed export status back to Gio.
type payoutResult struct {
	field  string
	path   string
	status string
}

// layoutPayouts renders the form for choosing Faire and Sage files and exporting matched cash receipts.
// It accepts the current Gio context and returns the layout dimensions of the scrollable form.
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
				return ui.payoutTextField(gtx, "Check number", &page.checkNo)
			}),
			fieldSpacer(),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.payoutTextField(gtx, "Comment", &page.comment) }),
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
// It snapshots editor values before starting the worker so subsequent edits cannot change an in-progress export.
func (ui *DesktopUI) exportPayouts() {
	page := &ui.payouts
	summary, sage := strings.TrimSpace(page.summary.Text()), strings.TrimSpace(page.sage.Text())
	checkNo, comment := strings.TrimSpace(page.checkNo.Text()), page.comment.Text()
	if summary == "" || sage == "" || checkNo == "" {
		page.status = "Select both CSV files and enter a check number."
		return
	}
	page.busy = true
	page.status = "Matching payouts to open invoices…"
	ui.workers.Add(1)
	go func() {
		defer ui.workers.Done()
		filename, count, err := writePayoutCSV(summary, sage, checkNo, comment)
		result := payoutResult{}
		switch {
		case err != nil:
			result.status = "Export failed: " + err.Error()
		case count == 0:
			result.status = "No Faire payouts matched open Sage invoices; no file was created."
		default:
			result.status = fmt.Sprintf("Exported %d matched payouts to Downloads as %s.", count, filename)
		}
		select {
		case page.results <- result:
		case <-ui.ctx.Done():
		}
		ui.invalidate()
	}()
}

// writePayoutCSV reconciles the two named CSVs and writes a complete cash-receipts file to Downloads.
// It returns the generated filename and match count, leaving no output file when there are no matches or an error occurs.
func writePayoutCSV(summaryPath, sagePath, checkNo, comment string) (string, int, error) {
	directory, err := downloadsDirectory()
	if err != nil {
		return "", 0, err
	}
	return writePayoutCSVToDirectory(directory, summaryPath, sagePath, checkNo, comment)
}

// writePayoutCSVToDirectory writes a reconciled CSV to a specified directory for isolated filesystem tests.
// It returns the filename and count, leaving the directory unchanged when validation fails or no invoices match.
func writePayoutCSVToDirectory(directory, summaryPath, sagePath, checkNo, comment string) (string, int, error) {
	summary, err := os.Open(summaryPath)
	if err != nil {
		return "", 0, fmt.Errorf("open Faire summary: %w", err)
	}
	defer summary.Close()
	sage, err := os.Open(sagePath)
	if err != nil {
		return "", 0, fmt.Errorf("open Sage export: %w", err)
	}
	defer sage.Close()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", 0, err
	}
	temporary, err := os.CreateTemp(directory, ".cash-re-check-*.csv")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(temporary.Name())
	count, writeErr := payouts.WriteCSV(temporary, summary, sage, checkNo, comment)
	closeErr := temporary.Close()
	if writeErr != nil {
		return "", 0, writeErr
	}
	if closeErr != nil {
		return "", 0, closeErr
	}
	if count == 0 {
		return "", 0, nil
	}
	filename := fmt.Sprintf("cash_re_check_%d.csv", time.Now().UnixNano())
	if err := os.Rename(temporary.Name(), filepath.Join(directory, filename)); err != nil {
		return "", 0, err
	}
	return filename, count, nil
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

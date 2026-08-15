package application

import (
	"fmt"

	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/Fepozopo/faire-gui/internal/buildinfo"
	"github.com/Fepozopo/faire-gui/updater"
)

// updateDialogState holds only public GitHub release metadata needed to offer an application update.
type updateDialogState struct {
	// open reports whether the update choice is currently blocking the application UI.
	open bool
	// update identifies the newer supported release selected by the update checker.
	update updater.Update
	// installing reports that the user accepted the update and prevents duplicate downloads.
	installing bool
	// status gives the user progress or a safe failure message while this dialog remains open.
	status string
}

// updateCheckResult transports a release-check outcome to the Gio frame goroutine.
type updateCheckResult struct {
	// update is populated only when available is true.
	update updater.Update
	// available reports that a newer compatible release asset was found.
	available bool
	// userInitiated reports whether the result must be presented to the user rather than handled silently at startup.
	userInitiated bool
	// err describes a failed check. It is displayed only for a user-initiated check so transient startup failures do not interrupt normal use.
	err error
}

// updateCheckDialogState describes the visible progress or outcome of a user-initiated update check.
type updateCheckDialogState struct {
	// open reports whether the update-check status modal is blocking application input.
	open bool
	// checking reports that the background request has not completed yet.
	checking bool
	// title identifies the update-check outcome.
	title string
	// message gives a credential-safe summary or next step for the user.
	message string
}

// updateInstallResult transports the result of a user-approved update installation to the Gio frame goroutine.
type updateInstallResult struct {
	// err is nil after the Windows restart helper has been scheduled; successful Darwin installs exec and never publish a result.
	err error
}

// startUpdateCheck checks GitHub Releases without blocking Gio's event loop.
// When userInitiated is false, errors and up-to-date results are silent so startup remains uninterrupted; explicit checks are instead reported in a modal.
func (ui *DesktopUI) startUpdateCheck(userInitiated bool) {
	go func() {
		update, available, err := updater.NewChecker(buildinfo.Version).Check(ui.ctx)
		ui.publishUpdateCheckResult(updateCheckResult{update: update, available: available, userInitiated: userInitiated, err: err})
	}()
}

// startManualUpdateCheck opens progress feedback and starts an explicit asynchronous GitHub Release check.
// It reports every outcome because users need confirmation that their manual request completed.
func (ui *DesktopUI) startManualUpdateCheck() {
	ui.updateCheckDialog = updateCheckDialogState{
		open:     true,
		checking: true,
		title:    "Checking for updates",
		message:  "Checking GitHub Releases for the latest compatible version…",
	}
	ui.startUpdateCheck(true)
	ui.invalidate()
}

// publishUpdateCheckResult sends a completed check result unless the application is already shutting down.
func (ui *DesktopUI) publishUpdateCheckResult(result updateCheckResult) {
	select {
	case ui.updateResults <- result:
	case <-ui.ctx.Done():
		return
	}
	if ui.window != nil {
		ui.window.Invalidate()
	}
}

// drainUpdateResults opens an install prompt for compatible releases and reports all outcomes for explicit checks.
// Startup checks remain silent unless a newer compatible release is available.
func (ui *DesktopUI) drainUpdateResults() {
	for {
		select {
		case result := <-ui.updateResults:
			if result.err == nil && result.available {
				ui.updateCheckDialog = updateCheckDialogState{}
				ui.updateDialog = updateDialogState{open: true, update: result.update}
				continue
			}
			if result.userInitiated {
				ui.updateCheckDialog.open = true
				ui.updateCheckDialog.checking = false
				if result.err != nil {
					ui.updateCheckDialog.title = "Unable to check for updates"
					ui.updateCheckDialog.message = "Updates could not be checked. Check your internet connection and try again."
				} else {
					ui.updateCheckDialog.title = "You're up to date"
					ui.updateCheckDialog.message = fmt.Sprintf("Version %s is the latest compatible version.", buildinfo.Version)
				}
			}
		default:
			return
		}
	}
}

// beginUpdateInstall starts the replacement process once and records visible progress before downloading.
func (ui *DesktopUI) beginUpdateInstall() {
	if ui.updateDialog.installing {
		return
	}
	ui.updateDialog.installing = true
	ui.updateDialog.status = "Downloading and installing the update…"
	ui.invalidate()
	update := ui.updateDialog.update
	go func() {
		err := updater.NewInstaller().Apply(ui.ctx, update.Asset)
		ui.publishUpdateInstallResult(updateInstallResult{err: err})
	}()
}

// publishUpdateInstallResult sends an installation result unless shutdown has already cancelled the update request.
func (ui *DesktopUI) publishUpdateInstallResult(result updateInstallResult) {
	select {
	case ui.updateInstallResults <- result:
	case <-ui.ctx.Done():
		return
	}
	if ui.window != nil {
		ui.window.Invalidate()
	}
}

// drainUpdateInstallResults closes the window after the Windows helper is ready, or restores the prompt after a failed installation.
func (ui *DesktopUI) drainUpdateInstallResults() {
	for {
		select {
		case result := <-ui.updateInstallResults:
			if result.err != nil {
				ui.updateDialog.installing = false
				ui.updateDialog.status = updateInstallErrorMessage(result.err)
				continue
			}
			ui.updateDialog.status = "Restarting with the updated application…"
			if ui.window != nil {
				ui.window.Perform(system.ActionClose)
			}
		default:
			return
		}
	}
}

// updateInstallErrorMessage returns user-safe remediation for installation failures without exposing local paths or network details.
func updateInstallErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return "The update could not be installed. Please try again or download the latest version from GitHub Releases."
}

// layoutUpdateModal lets users defer or install the newer compatible release discovered at startup or by a manual check.
func (ui *DesktopUI) layoutUpdateModal(gtx layout.Context) layout.Dimensions {
	if !ui.updateDialog.installing && ui.updateLater.Clicked(gtx) {
		ui.updateDialog = updateDialogState{}
		ui.invalidate()
	}
	if !ui.updateDialog.installing && ui.installUpdate.Clicked(gtx) {
		ui.beginUpdateInstall()
	}
	return modalPanel(gtx, ui, "Update available", func(gtx layout.Context) layout.Dimensions {
		message := fmt.Sprintf("Version %s is available. You are running version %s.", ui.updateDialog.update.Version, buildinfo.Version)
		children := []layout.FlexChild{
			layout.Rigid(bodyText(ui.theme, message, mutedTextColor)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(18)}.Layout),
		}
		if ui.updateDialog.installing {
			children = append(children, layout.Rigid(bodyText(ui.theme, ui.updateDialog.status, mutedTextColor)))
		} else {
			if ui.updateDialog.status != "" {
				children = append(children,
					layout.Rigid(bodyText(ui.theme, ui.updateDialog.status, dangerColor)),
					layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
				)
			}
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(primaryButton(ui.theme, &ui.updateLater, "Not now")),
					layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
					layout.Rigid(primaryButton(ui.theme, &ui.installUpdate, "Update and restart")),
				)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// layoutUpdateCheckStatusModal reports progress and completed no-update or failure outcomes for a manual check.
// While checking, input remains blocked to prevent duplicate GitHub requests; completed outcomes can be dismissed with Close.
func (ui *DesktopUI) layoutUpdateCheckStatusModal(gtx layout.Context) layout.Dimensions {
	if !ui.updateCheckDialog.checking && ui.closeUpdateCheckStatus.Clicked(gtx) {
		ui.updateCheckDialog = updateCheckDialogState{}
		ui.invalidate()
	}
	return modalPanel(gtx, ui, ui.updateCheckDialog.title, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(bodyText(ui.theme, ui.updateCheckDialog.message, mutedTextColor)),
		}
		if !ui.updateCheckDialog.checking {
			children = append(children,
				layout.Rigid(layout.Spacer{Height: unit.Dp(18)}.Layout),
				layout.Rigid(primaryButton(ui.theme, &ui.closeUpdateCheckStatus, "Close")),
			)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

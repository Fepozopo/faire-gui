package application

import (
	"context"

	"gioui.org/app"
	"gioui.org/op"
	"gioui.org/unit"
)

// Run starts the Faire Gio desktop application and checks GitHub Releases for a compatible update.
// It creates the window before app.Main takes control of the process main goroutine, and cancels outstanding work once the application exits.
func Run() {
	ctx, cancel := context.WithCancel(context.Background())
	manager, savedConnections, startupStatus := loadSavedConnections(ctx)
	window := new(app.Window)
	window.Option(app.Title(windowTitle), app.Size(unit.Dp(windowWidth), unit.Dp(windowHeight)))
	ui := newDesktopUI(ctx, cancel, window, manager, savedConnections, startupStatus)
	ui.startUpdateCheck(false)

	go func() {
		// Gio requires app.Main on the process main goroutine, so the event loop stays in a worker goroutine.
		_ = ui.runWindow()
	}()
	app.Main()
	cancel()
}

// runWindow handles Gio window events, drains safe background results, and submits complete frames.
// It releases cached Orders rows and cancels profile, order, and update work when Gio reports that the desktop window has been destroyed.
func (ui *DesktopUI) runWindow() error {
	defer ui.shutdown()

	var ops op.Ops
	for {
		switch event := ui.window.Event().(type) {
		case app.DestroyEvent:
			return event.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, event)
			ui.drainResults()
			ui.drainOrderResults()
			ui.drainOrderExportResults()
			ui.drainUpdateResults()
			ui.drainUpdateInstallResults()
			ui.Layout(gtx)
			event.Frame(gtx.Ops)
		}
	}
}

// shutdown cancels in-flight work and releases the session-only Orders cache and visible rows.
// It is safe to call more than once because context cancellation and assigning nil slices or maps are idempotent.
func (ui *DesktopUI) shutdown() {
	ui.cancel()
	ui.ordersCache = nil
	ui.ordersState.Rows = nil
	ui.ordersState.Cursor = ""
}

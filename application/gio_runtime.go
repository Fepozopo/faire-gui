package application

import (
	"context"

	"gioui.org/app"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/Fepozopo/faire-gui/features/orders"
	"github.com/Fepozopo/faire-gui/internal/ordersstore"
)

// Run starts the Faire Gio desktop application and checks GitHub Releases for a compatible update.
// It creates the window before app.Main takes control of the process main goroutine, and cancels outstanding work once the application exits.
func Run() {
	ctx, cancel := context.WithCancel(context.Background())
	manager, savedConnections, startupStatus := loadSavedConnections(ctx)
	store, storeErr := openOrdersStore(ctx)
	if storeErr != nil {
		startupStatus = "Local order storage is unavailable. Close the app, resolve the local data issue, then reopen it."
	}
	window := new(app.Window)
	window.Option(app.Title(windowTitle), app.Size(unit.Dp(windowWidth), unit.Dp(windowHeight)))
	ui := newDesktopUIWithOrders(ctx, cancel, window, manager, savedConnections, store, startupStatus)
	ui.startUpdateCheck(false)

	go func() {
		// Gio requires app.Main on the process main goroutine, so the event loop stays in a worker goroutine.
		_ = ui.runWindow()
	}()
	app.Main()
	cancel()
}

// runWindow handles Gio window events, drains safe background results, and submits complete frames.
// It releases in-memory Orders presentation data, closes persistent storage, and cancels background work when Gio reports window destruction.
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
			ui.drainConnectionCleanupResults()
			ui.drainOrderResults()
			ui.drainOrderDetailResults()
			ui.drainOrdersSchedule()
			ui.drainOrderExportResults()
			ui.drainUpdateResults()
			ui.drainUpdateInstallResults()
			ui.Layout(gtx)
			event.Frame(gtx.Ops)
		}
	}
}

// openOrdersStore opens and migrates the process-local private Orders database before any page can read it.
func openOrdersStore(ctx context.Context) (ordersstore.Store, error) {
	path, err := ordersstore.DefaultPath()
	if err != nil {
		return nil, err
	}
	return ordersstore.Open(ctx, path)
}

// shutdown cancels in-flight work, closes the persistent Orders store, and releases only in-memory presentation rows.
// It is safe to call more than once because context cancellation, store closing, and assigning nil slices are idempotent.
func (ui *DesktopUI) shutdown() {
	ui.cancel()
	if ui.ordersStore != nil {
		_ = ui.ordersStore.Close()
		ui.ordersStore = nil
	}
	ui.ordersState.Rows = nil
	ui.ordersState.Cursor = ""
	ui.orderDetail = orders.Detail{}
}

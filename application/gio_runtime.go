package application

import (
	"context"

	"gioui.org/app"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/Fepozopo/faire-gui/features/orders"
	"github.com/Fepozopo/faire-gui/internal/ordersstore"
)

// Run starts the Faire Gio desktop application.
// It creates the window before app.Main takes control of the process main goroutine, so startup preparation and the automatic update check begin only after Gio can render progress.
func Run() {
	ctx, cancel := context.WithCancel(context.Background())
	window := new(app.Window)
	window.Option(app.Title(windowTitle), app.Size(unit.Dp(windowWidth), unit.Dp(windowHeight)))
	ui := newDesktopUIWithOrders(ctx, cancel, window, nil, nil, nil, "Preparing local data…")
	ui.preparingStartup = true

	go func() {
		// Gio requires app.Main on the process main goroutine, so the event loop stays in a worker goroutine.
		_ = ui.runWindow()
	}()
	app.Main()
	cancel()
}

// runWindow handles Gio window events, begins startup work on its first frame, drains safe background results, and submits complete frames.
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
			ui.startStartupPreparation()
			ui.drainStartupResults()
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
// ctx cancels database initialization during shutdown, and the returned store is ready for Orders queries or an error explains why it is unavailable.
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

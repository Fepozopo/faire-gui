package application

import (
	"context"
	"os"
	"time"

	"gioui.org/app"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/Fepozopo/faire-gui/features/orders"
	"github.com/Fepozopo/faire-gui/internal/ordersstore"
)

// Run starts the Faire Gio desktop application in a maximized window.
// It creates the window before app.Main takes control of the process main goroutine, so startup preparation and the automatic update check begin only after Gio can render progress. Maximized mode fills the available work area while preserving OS chrome such as the macOS menu bar.
func Run() {
	ctx, cancel := context.WithCancel(context.Background())
	window := new(app.Window)
	window.Option(app.Title(windowTitle), app.Size(unit.Dp(windowWidth), unit.Dp(windowHeight)), app.Maximized.Option())
	ui := newDesktopUIWithOrders(ctx, cancel, window, nil, nil, nil, "Preparing local data…")
	ui.preparingStartup = true

	go func() {
		// Gio requires app.Main on the process main goroutine, so the event loop stays in a worker goroutine.
		_ = ui.runWindow()
		exitAfterWindowDestroyed()
	}()
	app.Main()
}

const (
	// shutdownWorkerWaitTimeout bounds graceful shutdown so an uncooperative dependency cannot leave the hidden process alive.
	shutdownWorkerWaitTimeout = 3 * time.Second
)

// exitAfterWindowDestroyed ends this single-window desktop process after runWindow has released application resources.
// It has no parameters or return value. Gio's app.Main owns the main OS thread and deliberately blocks after the final window closes, so returning from the window loop alone would leave a headless process alive.
func exitAfterWindowDestroyed() {
	os.Exit(0)
}

// startWorker runs work as an application-owned asynchronous operation that shutdown will cancel and drain.
// work must observe the UI context rather than mutate Gio state directly; it has no return value because workers publish safe results through UI channels.
func (ui *DesktopUI) startWorker(work func()) {
	ui.workers.Add(1)
	go func() {
		defer ui.workers.Done()
		work()
	}()
}

// waitForWorkers waits for application-owned workers to observe cancellation, returning false when the grace period expires.
// It has no parameters. The timeout preserves deterministic process exit when a third-party operation ignores context cancellation.
func (ui *DesktopUI) waitForWorkers() bool {
	if ui.workers == nil {
		return true
	}
	completed := make(chan struct{})
	go func() {
		ui.workers.Wait()
		close(completed)
	}()
	select {
	case <-completed:
		return true
	case <-time.After(shutdownWorkerWaitTimeout):
		return false
	}
}

// closePendingStartupStores releases a database opened by startup after cancellation won the result-delivery race.
// It has no parameters or return value. Workers have already stopped when this runs, so draining startupResults cannot race a later store publication.
func (ui *DesktopUI) closePendingStartupStores() {
	for {
		select {
		case result := <-ui.startupResults:
			if result.store != nil {
				_ = result.store.Close()
			}
		default:
			return
		}
	}
}

// runWindow handles Gio window events, begins startup work on its first frame, drains safe background results including Sage fulfillment requests plus shipment and item-availability submissions, and submits complete frames.
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
			ui.drainSageFulfillmentRequests()
			ui.drainResults()
			ui.drainOrdersDataActionEvents()
			ui.drainConnectionCleanupResults()
			ui.drainOrderResults()
			ui.drainOrderDetailResults()
			ui.drainShipmentSubmissionResults()
			ui.drainItemAvailabilityResults()
			ui.drainOrderProcessingResults()
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

// shutdown cancels application-owned workers, gives them a bounded grace period, then closes persistent Orders storage and releases presentation rows.
// It is safe to call more than once because cancellation, worker waiting, store closing, and assigning nil slices are idempotent. If a worker ignores cancellation, shutdown skips explicit store closure and lets imminent process exit release OS resources rather than risking a concurrent store close.
func (ui *DesktopUI) shutdown() {
	ui.cancel()
	if ui.waitForWorkers() {
		ui.closePendingStartupStores()
		if ui.orders.store != nil {
			_ = ui.orders.store.Close()
		}
	}
	ui.orders.store = nil
	ui.orders.view.state.Rows = nil
	ui.orders.view.state.Cursor = ""
	ui.orders.view.orderDetail = orders.Detail{}
}

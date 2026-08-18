package application

import (
	"github.com/Fepozopo/faire-gui/connections"
	"github.com/Fepozopo/faire-gui/internal/ordersstore"
)

// startupResult transfers fully prepared non-secret connection metadata and Orders storage to Gio's frame goroutine.
// manager and connections become UI-owned after receipt, store is closed during UI shutdown, and status is safe to display to the user.
type startupResult struct {
	manager     *connections.Manager
	connections []connections.Connection
	store       ordersstore.Store
	status      string
}

// startStartupPreparation begins connection-metadata loading, local Orders database preparation, and an automatic update check after Gio has requested its first frame.
// It has no parameters or return value; it starts only once, and completion is delivered through startupResults so only Gio's frame goroutine mutates UI state.
func (ui *DesktopUI) startStartupPreparation() {
	if ui.startupPreparationStarted {
		return
	}
	ui.startupPreparationStarted = true
	ui.preparingStartup = true
	ui.startUpdateCheck(false)
	go func() {
		manager, savedConnections, startupStatus := loadSavedConnections(ui.ctx)
		store, storeErr := openOrdersStore(ui.ctx)
		if storeErr != nil {
			startupStatus = "Local order storage is unavailable. Close the app, resolve the local data issue, then reopen it."
		}
		ui.publishStartupResult(startupResult{
			manager:     manager,
			connections: savedConnections,
			store:       store,
			status:      startupStatus,
		})
	}()
}

// publishStartupResult sends a completed startup result unless shutdown began before the UI could own its Orders store.
// result is closed here on cancellation because no frame will receive it and therefore no later shutdown path can release the store.
func (ui *DesktopUI) publishStartupResult(result startupResult) {
	if ui.ctx.Err() != nil {
		if result.store != nil {
			_ = result.store.Close()
		}
		return
	}
	select {
	case ui.startupResults <- result:
		ui.invalidate()
	case <-ui.ctx.Done():
		if result.store != nil {
			_ = result.store.Close()
		}
	}
}

// drainStartupResults applies completed startup work on the Gio frame goroutine and makes the full application interactive.
// It has no parameters or return value; it consumes every queued result, though production publishes exactly one result per application run.
func (ui *DesktopUI) drainStartupResults() {
	for {
		select {
		case result := <-ui.startupResults:
			ui.manager = result.manager
			ui.connections = result.connections
			ui.ordersStore = result.store
			ui.status = result.status
			ui.preparingStartup = false
		default:
			return
		}
	}
}

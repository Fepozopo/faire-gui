package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"

	"gioui.org/layout"
	"github.com/Fepozopo/faire-gui/connections"
	"github.com/Fepozopo/faire-gui/faire"
	"github.com/Fepozopo/faire-gui/features/orders"
	"github.com/Fepozopo/faire-gui/internal/ordersstore"
	"github.com/Fepozopo/faire-gui/internal/orderssync"
)

// orderLoadResult carries one credential-safe local Orders query or synchronization result to the frame loop.
// NewOrdersCount holds the active connection's complete locally stored New-order count when ApplyNewOrdersCount
// is true. It never holds a client, credentials, raw API response, snapshot, address, or order notes.
type orderLoadResult struct {
	RequestID           uint64
	Append              bool
	Rows                []orders.Row
	Cursor              string
	Status              string
	ApplyRows           bool
	KeepLoading         bool
	UpdatedAtMin        string
	ApplyBoundary       bool
	NewOrdersCount      int
	ApplyNewOrdersCount bool
}

// newOrdersCount returns the active connection's complete locally stored New-order count.
// Its boolean result is false when the count query fails, allowing callers to retain successful primary results.
func newOrdersCount(ctx context.Context, store ordersstore.Store, connectionID string) (int, bool) {
	// The badge represents all retained New orders, not only rows visible through the current table filters or page.
	count, err := store.CountByState(ctx, connectionID, string(faire.OrderStateNew))
	return count, err == nil
}

// attachNewOrdersCount adds the active connection's complete locally stored New-order count to result.
// If the independent count query fails, result remains usable so an already successful table read is not discarded.
func attachNewOrdersCount(ctx context.Context, store ordersstore.Store, connectionID string, result orderLoadResult) orderLoadResult {
	if count, found := newOrdersCount(ctx, store, connectionID); found {
		result.NewOrdersCount = count
		result.ApplyNewOrdersCount = true
	}
	return result
}

// orderDetailResult carries one typed local-detail result and, when available, a New-order badge count to the frame loop without exposing its serialized snapshot.
type orderDetailResult struct {
	RequestID           uint64
	ConnectionID        string
	OrderID             faire.OrderID
	Detail              orders.Detail
	Status              string
	NewOrdersCount      int
	ApplyNewOrdersCount bool
}

// localCursorPayload is the non-sensitive worker-only encoding of a local SQLite keyset cursor.
type localCursorPayload struct {
	SortAtUTC *time.Time `json:"sort_at_utc,omitempty"`
	OrderID   string     `json:"order_id"`
}

// orderExportKind identifies the server-defined set of orders written to a CSV file.
type orderExportKind string

const (
	// orderExportNew writes every currently new Faire order.
	orderExportNew orderExportKind = "new"
	// orderExportBackordered writes every currently backordered Faire order.
	orderExportBackordered orderExportKind = "backordered"
	// orderExportSelected writes every order selected in the current Orders table.
	orderExportSelected orderExportKind = "selected"
)

// orderExportResult carries credential-safe export completion, blocking, and filename state to the frame loop.
type orderExportResult struct {
	RequestID uint64
	Status    string
	Filename  string
	Blocked   bool
	Completed bool
}

// ordersLoadErrorMessage converts an Orders request failure to a user-safe status.
// It deliberately omits raw response bodies and credential-store implementation details.
func ordersLoadErrorMessage(err error) string {
	if errors.Is(err, context.Canceled) {
		return "Order loading was canceled."
	}
	var listError *orderssync.ListError
	if apiError, ok := errors.AsType[*faire.APIError](err); ok {
		switch apiError.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "Faire rejected this connection's credentials. Update the saved connection or reauthorize it."
		case http.StatusTooManyRequests:
			return "Faire is rate limiting requests. Wait a moment, then refresh orders."
		case http.StatusBadRequest:
			if errors.As(err, &listError) {
				return invalidOrdersRequestMessage(listError)
			}
			return "Faire rejected the order request as invalid. Adjust the history date and refresh again."
		default:
			return fmt.Sprintf("Faire could not load orders (HTTP %d). Try refreshing later.", apiError.StatusCode)
		}
	}
	return "Orders could not be loaded. Check the saved connection and try refreshing."
}

// invalidOrdersRequestMessage identifies only the safe synchronization phase of a rejected Faire request.
func invalidOrdersRequestMessage(listError *orderssync.ListError) string {
	phase := "order synchronization"
	switch listError.Phase {
	case orderssync.ListPhaseBootstrap:
		phase = "initial order-history synchronization"
	case orderssync.ListPhaseHistory:
		phase = "older order-history synchronization"
	case orderssync.ListPhaseIncremental:
		phase = "updated-orders synchronization"
	}
	if listError.Cursor {
		phase += " follow-up page"
	}
	return "Faire rejected the " + phase + " request as invalid. Adjust the history date or rebuild local order data before refreshing again."
}

// setActiveConnection changes the session-only active connection, clears transient Orders state, and invalidates prior detail/export completions.
// It refuses a connection switch while any local-data action runs so a rebuild cannot race a second synchronization for the same connection.
func (ui *DesktopUI) setActiveConnection(connection connections.Connection) {
	if ui.ordersDataActionConnectionID != "" {
		ui.connectionPickerOpen = false
		ui.status = "Wait for the local order-data action to finish before changing the active connection."
		ui.invalidate()
		return
	}
	ui.activeConnectionID = connection.ID
	ui.activeConnectionLabel = connection.Label
	ui.connectionPickerOpen = false
	// A completion from the prior connection must not overwrite this connection's Orders status.
	ui.exportRequestID++
	ui.detailRequestID++
	ui.ordersExporting = false
	ui.orderDetailOpen = false
	ui.orderDetailLoading = false
	ui.orderDetail = orders.Detail{}
	ui.orderDetailID = ""
	ui.orderDetailConnectionID = ""
	ui.resetOrdersState()
	ui.ordersSearchActive = false
	ui.orderSearchEditor.SetText("")
	ui.ordersList.Position.First = 0
	ui.ordersList.Position.Offset = 0
	ui.selectedTab = ordersTab
	ui.startOrdersLoad(false, false, true)
	ui.invalidate()
}

// startOrdersLoad loads a local Orders page and, when requested by selection, manual refresh, or the scheduler, runs synchronization in a worker.
// A request sequence number ensures late results never overwrite newer filters or connections.
func (ui *DesktopUI) startOrdersLoad(appendResults, refresh, synchronize bool) {
	if ui.ordersStore == nil {
		ui.ordersState.Status = "Local order storage is unavailable. Close the app, resolve the local data issue, then reopen it."
		return
	}
	if ui.activeConnectionID == "" {
		ui.ordersState.Status = "Choose an active saved connection to load orders."
		return
	}
	if ui.ordersDataActionConnectionID == ui.activeConnectionID {
		ui.ordersState.Status = "Local order data is being rebuilt. The refreshed orders will appear when it finishes."
		return
	}
	if ui.ordersState.Loading || ui.orderDetailLoading {
		return
	}
	if appendResults && ui.ordersState.Cursor == "" {
		return
	}
	ui.ordersRequestID++
	requestID := ui.ordersRequestID
	connectionID := ui.activeConnectionID
	state := ui.ordersState
	if !appendResults {
		state.Cursor = ""
	}
	restoreBoundary := !ui.ordersHistoryBoundaryKnown && !refresh
	ui.ordersState.Loading = true
	ui.ordersState.Status = "Loading locally stored orders…"
	store, manager := ui.ordersStore, ui.manager
	go ui.loadOrders(requestID, connectionID, state, appendResults, refresh, synchronize, restoreBoundary, store, manager)
}

// loadOrders performs local SQLite reads, optional Faire synchronization, final local re-queries, and New-order count reads outside the Gio frame loop.
func (ui *DesktopUI) loadOrders(requestID uint64, connectionID string, state orders.State, appendResults, refresh, synchronize, restoreBoundary bool, store ordersstore.Store, manager *connections.Manager) {
	boundary := ""
	if restoreBoundary {
		syncState, found, err := store.SyncState(ui.ctx, connectionID)
		if err != nil {
			ui.publishOrderResult(orderLoadResult{RequestID: requestID, Append: appendResults, Status: ordersStorageErrorMessage(err)})
			return
		}
		if found {
			boundary = syncState.BootstrapUpdatedAtMinUTC.Format(time.RFC3339)
			state.Query.UpdatedAtMin = boundary
		}
	}
	page, err := loadLocalPage(ui.ctx, store, connectionID, state)
	if err != nil {
		ui.publishOrderResult(orderLoadResult{RequestID: requestID, Append: appendResults, Status: ordersStorageErrorMessage(err)})
		return
	}
	localResult := func(status string, keepLoading bool) orderLoadResult {
		result := orderLoadResult{RequestID: requestID, Append: appendResults, Rows: localRows(page.Rows), Cursor: encodeLocalCursor(page.NextCursor), Status: status, ApplyRows: true, KeepLoading: keepLoading, UpdatedAtMin: boundary, ApplyBoundary: boundary != ""}
		return attachNewOrdersCount(ui.ctx, store, connectionID, result)
	}
	if appendResults || !synchronize {
		ui.publishOrderResult(localResult(localStatus(store, ui.ctx, connectionID), false))
		return
	}
	shouldSync, err := ordersNeedSync(ui.ctx, store, connectionID, time.Now().UTC())
	if err != nil {
		ui.publishOrderResult(localResult(ordersStorageErrorMessage(err), false))
		return
	}
	if !refresh && !shouldSync {
		ui.publishOrderResult(localResult(localStatus(store, ui.ctx, connectionID), false))
		return
	}
	ui.publishOrderResult(localResult("Checking Faire for updated orders…", true))
	if manager == nil {
		ui.publishOrderResult(localResult("Showing locally stored orders. Saved connections are unavailable.", false))
		return
	}
	client, _, err := manager.Client(ui.ctx, connectionID, connections.ClientOptions{})
	if err != nil {
		ui.publishOrderResult(orderLoadResult{RequestID: requestID, Status: ordersLoadErrorMessage(err)})
		return
	}
	syncer, err := orderssync.New(store, orderssync.SourceFunc(client.Orders.List), orderssync.Config{})
	if err != nil {
		ui.publishOrderResult(orderLoadResult{RequestID: requestID, Status: "Orders could not be synchronized. Try refreshing later."})
		return
	}
	var summary orderssync.Summary
	if refresh {
		historyBoundary, err := time.Parse(time.RFC3339Nano, state.Query.UpdatedAtMin)
		if err != nil {
			ui.publishOrderResult(orderLoadResult{RequestID: requestID, Status: "Enter a valid updated-at minimum before refreshing."})
			return
		}
		summary, err = syncer.SyncFromUpdatedAt(ui.ctx, connectionID, historyBoundary)
	} else {
		summary, err = syncer.Sync(ui.ctx, connectionID)
	}
	if err != nil {
		ui.publishOrderResult(orderLoadResult{RequestID: requestID, Status: ordersLoadErrorMessage(err)})
		return
	}
	if summary.Bootstrap || summary.HistoryExpanded {
		syncState, found, stateErr := store.SyncState(ui.ctx, connectionID)
		if stateErr != nil || !found {
			ui.publishOrderResult(orderLoadResult{RequestID: requestID, Status: ordersStorageErrorMessage(stateErr)})
			return
		}
		boundary = syncState.BootstrapUpdatedAtMinUTC.Format(time.RFC3339)
		state.Query.UpdatedAtMin = boundary
	}
	page, err = loadLocalPage(ui.ctx, store, connectionID, state)
	if err != nil {
		ui.publishOrderResult(orderLoadResult{RequestID: requestID, Status: ordersStorageErrorMessage(err)})
		return
	}
	status := "Orders are up to date."
	if summary.HistoryExpanded {
		status = "Older order history was added from Faire."
	} else if summary.Orders > 0 {
		status = "Orders updated from Faire."
	}
	ui.publishOrderResult(localResult(status, false))
}

// loadOrderByDisplayID searches the connection-scoped local index before using the authenticated direct-lookup fallback.
func (ui *DesktopUI) loadOrderByDisplayID() {
	if ui.activeConnectionID == "" || ui.ordersStore == nil {
		ui.ordersState.Status = "Choose an active saved connection with local order storage before searching orders."
		return
	}
	displayID, err := orders.NormalizeDisplayID(ui.orderSearchEditor.Text())
	if err != nil {
		ui.ordersState.Status = "Enter a valid order number."
		return
	}
	orderID, _ := orders.OrderIDFromDisplayID(displayID)
	ui.ordersSearchActive = true
	ui.ordersRequestID++
	requestID, connectionID := ui.ordersRequestID, ui.activeConnectionID
	ui.ordersState.Loading = true
	ui.ordersState.Status = "Searching locally stored orders…"
	store, manager := ui.ordersStore, ui.manager
	go func() {
		local, localErr := store.FindByDisplayID(ui.ctx, connectionID, displayID)
		if localErr == nil {
			result := orderLoadResult{RequestID: requestID, Rows: localRows([]ordersstore.LocalRow{local}), Status: "Showing the matching locally stored order.", ApplyRows: true}
			ui.publishOrderResult(attachNewOrdersCount(ui.ctx, store, connectionID, result))
			return
		}
		if !errors.Is(localErr, ordersstore.ErrNotFound) {
			ui.publishOrderResult(orderLoadResult{RequestID: requestID, Status: ordersStorageErrorMessage(localErr)})
			return
		}
		if manager == nil {
			ui.publishOrderResult(orderLoadResult{RequestID: requestID, Status: "Order was not found locally and saved connections are unavailable."})
			return
		}
		client, _, clientErr := manager.Client(ui.ctx, connectionID, connections.ClientOptions{})
		if clientErr != nil {
			ui.publishOrderResult(orderLoadResult{RequestID: requestID, Status: ordersLoadErrorMessage(clientErr)})
			return
		}
		order, getErr := client.Orders.Get(ui.ctx, orderID)
		if getErr != nil {
			ui.publishOrderResult(orderLoadResult{RequestID: requestID, Status: ordersLoadErrorMessage(getErr)})
			return
		}
		record, recordErr := orderssync.RecordFromOrder(connectionID, *order, time.Now().UTC())
		if recordErr != nil || store.UpsertOrders(ui.ctx, []ordersstore.OrderRecord{record}) != nil {
			ui.publishOrderResult(orderLoadResult{RequestID: requestID, Status: "Order was retrieved but could not be stored locally. Try again later."})
			return
		}
		result := orderLoadResult{RequestID: requestID, Rows: []orders.Row{orders.PresentRow(*order)}, Status: "Showing the matching order.", ApplyRows: true}
		ui.publishOrderResult(attachNewOrdersCount(ui.ctx, store, connectionID, result))
	}()
}

// publishOrderResult sends a result unless application shutdown has begun.
func (ui *DesktopUI) publishOrderResult(result orderLoadResult) {
	select {
	case ui.orderResults <- result:
	case <-ui.ctx.Done():
		return
	}
	ui.invalidate()
}

// drainOrderResults applies only the latest local-query or synchronization result, including its New-order badge count, protecting current controls from stale work.
// Rebuilds launched from Brand Profile also mirror their matching progress and completion status into that page's status area.
func (ui *DesktopUI) drainOrderResults() {
	for {
		select {
		case result := <-ui.orderResults:
			if result.RequestID != ui.ordersRequestID {
				continue
			}
			if result.RequestID == ui.ordersDataStatusRequestID {
				ui.status = result.Status
				if !result.KeepLoading {
					ui.ordersDataStatusRequestID = 0
					if ui.ordersDataActionConnectionID == ui.activeConnectionID {
						ui.ordersDataActionConnectionID = ""
					}
				}
			}
			ui.ordersState.Loading = result.KeepLoading
			if result.ApplyNewOrdersCount {
				ui.newOrdersCount = result.NewOrdersCount
			}
			if !result.ApplyRows {
				ui.ordersState.Status = result.Status
				continue
			}
			if result.ApplyBoundary {
				ui.ordersState.Query.UpdatedAtMin = result.UpdatedAtMin
				ui.updatedAtMinEditor.SetText(historyBoundaryInput(result.UpdatedAtMin))
				ui.ordersHistoryBoundaryKnown = true
			}
			if ui.ordersSearchActive {
				ui.ordersState.Rows = result.Rows
				ui.ordersState.Cursor = ""
			} else if result.Append {
				ui.ordersState.Rows = append(ui.ordersState.Rows, result.Rows...)
				ui.ordersState.Cursor = result.Cursor
			} else {
				ui.ordersState.Rows = result.Rows
				ui.ordersState.Cursor = result.Cursor
			}
			ui.ordersState.Loaded = true
			ui.ordersState.Status = result.Status
		default:
			return
		}
	}
}

// historyBoundaryInput converts a stored RFC 3339 historical boundary into the Orders date editor's local calendar input.
func historyBoundaryInput(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return ""
	}
	return parsed.In(time.Local).Format("1/2/2006")
}

// openSelectedOrder opens the locally stored snapshot for exactly one selected table row.
func (ui *DesktopUI) openSelectedOrder() {
	if ui.activeConnectionID == "" || ui.ordersStore == nil {
		ui.ordersState.Status = "Choose an active saved connection before opening an order."
		return
	}
	if len(ui.ordersState.SelectedIDs) != 1 {
		ui.ordersState.Status = "Select exactly one order to open its details."
		return
	}
	var orderID faire.OrderID
	for selectedID := range ui.ordersState.SelectedIDs {
		orderID = selectedID
	}
	ui.detailRequestID++
	requestID := ui.detailRequestID
	connectionID, store := ui.activeConnectionID, ui.ordersStore
	ui.orderDetailOpen, ui.orderDetailLoading = true, true
	ui.orderDetailID, ui.orderDetailConnectionID = orderID, connectionID
	ui.orderDetail = orders.Detail{}
	ui.orderDetailStatus = "Opening locally stored order details…"
	go loadOrderDetail(ui.ctx, store, requestID, connectionID, orderID, ui.publishOrderDetailResult)
}

// refreshOrderDetail explicitly retrieves the currently open order and atomically replaces its local snapshot without advancing the feed checkpoint.
func (ui *DesktopUI) refreshOrderDetail() {
	if ui.orderDetailID == "" || ui.orderDetailConnectionID != ui.activeConnectionID || ui.ordersStore == nil || ui.manager == nil {
		ui.orderDetailStatus = "Order details cannot be refreshed until an active saved connection is available."
		return
	}
	if ui.orderDetailLoading || ui.ordersState.Loading {
		return
	}
	ui.detailRequestID++
	requestID := ui.detailRequestID
	connectionID, orderID, store, manager := ui.activeConnectionID, ui.orderDetailID, ui.ordersStore, ui.manager
	ui.orderDetailLoading = true
	ui.orderDetailStatus = "Refreshing order details from Faire…"
	go func() {
		client, _, err := manager.Client(ui.ctx, connectionID, connections.ClientOptions{})
		if err != nil {
			ui.publishOrderDetailResult(orderDetailResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: ordersLoadErrorMessage(err)})
			return
		}
		order, err := client.Orders.Get(ui.ctx, orderID)
		if err != nil {
			ui.publishOrderDetailResult(orderDetailResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: ordersLoadErrorMessage(err)})
			return
		}
		record, err := orderssync.RecordFromOrder(connectionID, *order, time.Now().UTC())
		if err == nil {
			err = store.UpsertOrders(ui.ctx, []ordersstore.OrderRecord{record})
		}
		if err != nil {
			ui.publishOrderDetailResult(orderDetailResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: ordersStorageErrorMessage(err)})
			return
		}
		result := orderDetailResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Detail: orders.PresentDetail(*order, record.SyncedAtUTC)}
		if count, found := newOrdersCount(ui.ctx, store, connectionID); found {
			result.NewOrdersCount = count
			result.ApplyNewOrdersCount = true
		}
		ui.publishOrderDetailResult(result)
	}()
}

// loadOrderDetail reads and deserializes one private snapshot in a worker, publishing only its typed presentation model.
func loadOrderDetail(ctx context.Context, store ordersstore.Store, requestID uint64, connectionID string, orderID faire.OrderID, publish func(orderDetailResult)) {
	snapshot, err := store.Snapshot(ctx, connectionID, string(orderID))
	if err != nil {
		publish(orderDetailResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: ordersStorageErrorMessage(err)})
		return
	}
	if snapshot.SnapshotSchemaVersion != ordersstore.SnapshotSchemaVersion {
		publish(orderDetailResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: "Local order data needs to be rebuilt."})
		return
	}
	var order faire.Order
	if err := json.Unmarshal([]byte(snapshot.SnapshotJSON), &order); err != nil {
		publish(orderDetailResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: "Local order data needs to be rebuilt."})
		return
	}
	if order.ID == nil || *order.ID != orderID {
		publish(orderDetailResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: "Local order data needs to be rebuilt."})
		return
	}
	publish(orderDetailResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Detail: orders.PresentDetail(order, snapshot.SyncedAtUTC)})
}

// publishOrderDetailResult sends a typed detail result unless application shutdown has begun.
func (ui *DesktopUI) publishOrderDetailResult(result orderDetailResult) {
	select {
	case ui.orderDetailResults <- result:
	case <-ui.ctx.Done():
		return
	}
	ui.invalidate()
}

// drainOrderDetailResults accepts only results for the current detail request, connection, and selected order, including a refreshed New-order badge count.
func (ui *DesktopUI) drainOrderDetailResults() {
	for {
		select {
		case result := <-ui.orderDetailResults:
			if result.RequestID != ui.detailRequestID || result.ConnectionID != ui.activeConnectionID || result.OrderID != ui.orderDetailID {
				continue
			}
			ui.orderDetailLoading = false
			if result.ApplyNewOrdersCount {
				ui.newOrdersCount = result.NewOrdersCount
			}
			if result.Status != "" {
				ui.orderDetailStatus = result.Status
				continue
			}
			ui.orderDetail = result.Detail
			ui.orderDetailStatus = "Showing locally stored order details."
		default:
			return
		}
	}
}

// requestOrdersDataAction opens explicit confirmation for a connection-scoped local-data delete or rebuild operation.
// connectionID comes from the clicked Brand Profile card, preventing an action from affecting another connection's cached orders.
func (ui *DesktopUI) requestOrdersDataAction(connectionID string, rebuild bool) {
	if ui.ordersDataActionConnectionID != "" {
		ui.status = "Wait for the current local order-data action to finish before starting another one."
		return
	}
	if connectionID == "" || ui.ordersStore == nil {
		ui.status = "Local order storage is unavailable. Close the app, resolve the local data issue, then reopen it."
		return
	}
	ui.ordersDataDialog = ordersDataDialogState{open: true, rebuild: rebuild, connectionID: connectionID}
}

// startOrdersDataAction deletes only connectionID's private cached orders and optionally starts a fresh bootstrap.
// It marks the action connection as busy, preventing a connection switch or duplicate sync until the action has completed.
func (ui *DesktopUI) startOrdersDataAction(connectionID string, rebuild bool) {
	if ui.ordersStore == nil || connectionID == "" {
		return
	}
	// The status area is above the connection cards, so return there before the confirmation closes and background work begins.
	ui.brandsList.Position = layout.Position{}
	ui.ordersDataDialog = ordersDataDialogState{}
	ui.ordersDataActionConnectionID = connectionID
	ui.status = localDataActionStatus(rebuild)
	if connectionID != ui.activeConnectionID {
		ui.ordersDataStatusRequestID = 0
		ui.startInactiveOrdersDataAction(connectionID, rebuild)
		return
	}
	ui.ordersRequestID++
	requestID := ui.ordersRequestID
	ui.ordersDataStatusRequestID = requestID
	store, manager, state := ui.ordersStore, ui.manager, ui.ordersState
	if rebuild {
		// Rebuild intentionally starts from the current default window rather than silently preserving an older retained-history expansion.
		state.Query.UpdatedAtMin = orders.NewStateAt(time.Now(), time.Local).Query.UpdatedAtMin
		ui.ordersHistoryBoundaryKnown = false
	}
	state.Cursor = ""
	ui.ordersState.Rows = nil
	ui.ordersState.Cursor = ""
	ui.ordersState.Loaded = false
	ui.ordersState.Loading = true
	ui.ordersState.SelectedIDs = make(map[faire.OrderID]struct{})
	ui.ordersState.Status = localDataActionStatus(rebuild)
	go func() {
		if err := store.DeleteConnectionData(ui.ctx, connectionID); err != nil {
			ui.publishOrderResult(orderLoadResult{RequestID: requestID, Status: ordersStorageErrorMessage(err)})
			return
		}
		if !rebuild {
			ui.publishOrderResult(orderLoadResult{RequestID: requestID, Status: "Deleted locally stored order data for this connection.", ApplyNewOrdersCount: true})
			return
		}
		ui.loadOrders(requestID, connectionID, state, false, true, true, false, store, manager)
	}()
}

// localDataActionStatus describes the connection-scoped local-data operation currently running.
func localDataActionStatus(rebuild bool) string {
	if rebuild {
		return "Rebuilding locally stored order data…"
	}
	return "Deleting locally stored order data…"
}

// startInactiveOrdersDataAction performs a local-data action for a Brand Profile card that is not currently active in Orders.
// It reports completion through the Brand Profile result channel and avoids replacing the active Orders table.
func (ui *DesktopUI) startInactiveOrdersDataAction(connectionID string, rebuild bool) {
	ui.status = localDataActionStatus(rebuild)
	store, manager := ui.ordersStore, ui.manager
	go func() {
		if err := store.DeleteConnectionData(ui.ctx, connectionID); err != nil {
			ui.publishOrdersDataResult(connectionID, ordersStorageErrorMessage(err))
			return
		}
		if !rebuild {
			ui.publishOrdersDataResult(connectionID, "Deleted locally stored order data for this connection.")
			return
		}
		if manager == nil {
			ui.publishOrdersDataResult(connectionID, "Saved connections are unavailable. Local order data was deleted but could not be rebuilt.")
			return
		}
		client, _, err := manager.Client(ui.ctx, connectionID, connections.ClientOptions{})
		if err != nil {
			ui.publishOrdersDataResult(connectionID, ordersLoadErrorMessage(err))
			return
		}
		syncer, err := orderssync.New(store, orderssync.SourceFunc(client.Orders.List), orderssync.Config{})
		if err != nil {
			ui.publishOrdersDataResult(connectionID, "Orders could not be synchronized. Try refreshing later.")
			return
		}
		if _, err := syncer.Sync(ui.ctx, connectionID); err != nil {
			ui.publishOrdersDataResult(connectionID, ordersLoadErrorMessage(err))
			return
		}
		ui.publishOrdersDataResult(connectionID, "Local order data was rebuilt for this connection.")
	}()
}

// loadLocalPage translates UI filter state into a connection-scoped SQLite keyset query in a background worker.
func loadLocalPage(ctx context.Context, store ordersstore.Store, connectionID string, state orders.State) (ordersstore.ListPage, error) {
	var updatedAtMin *time.Time
	if state.Query.UpdatedAtMin != "" {
		parsed, err := time.Parse(time.RFC3339Nano, state.Query.UpdatedAtMin)
		if err != nil {
			return ordersstore.ListPage{}, err
		}
		updatedAtMin = &parsed
	}
	states := make([]string, 0, len(state.IncludedStates))
	for orderState := range state.IncludedStates {
		states = append(states, string(orderState))
	}
	after, err := decodeLocalCursor(state.Cursor)
	if err != nil {
		return ordersstore.ListPage{}, err
	}
	sortColumn := ordersstore.LocalSortCreatedAt
	if state.TableSort.Column == orders.TableSortColumnShipDate {
		sortColumn = ordersstore.LocalSortExpectedShipAt
	}
	return store.List(ctx, ordersstore.ListQuery{ConnectionID: connectionID, States: states, UpdatedAtMin: updatedAtMin, SortColumn: sortColumn, Descending: state.TableSort.Direction != orders.TableSortAscending, After: after, Limit: 50})
}

// localRows converts storage projections to the existing safe table presentation type outside the frame loop.
func localRows(source []ordersstore.LocalRow) []orders.Row {
	rows := make([]orders.Row, len(source))
	for index, sourceRow := range source {
		id := faire.OrderID(sourceRow.OrderID)
		order := faire.Order{ID: &id, DisplayID: optionalPointer(sourceRow.DisplayID), State: optionalOrderState(sourceRow.State), Customer: optionalCustomer(sourceRow.CustomerName), CreatedAt: formatTimestampPointer(sourceRow.CreatedAtUTC), ExpectedShipDate: formatTimestampPointer(sourceRow.ExpectedShipAtUTC), Source: optionalPointer(sourceRow.Source)}
		row := orders.PresentRow(order)
		if sourceRow.TotalDisplay != "" {
			row.Total = sourceRow.TotalDisplay
		}
		if sourceRow.CommissionDisplay != "" {
			row.Commission = sourceRow.CommissionDisplay
		}
		rows[index] = row
	}
	return rows
}

// optionalPointer returns nil for an absent local display field so the feature formatter supplies its standard placeholder.
func optionalPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// optionalOrderState maps a stored state identifier to an optional typed Faire state for feature presentation.
func optionalOrderState(value string) *faire.OrderState {
	if value == "" {
		return nil
	}
	state := faire.OrderState(value)
	return &state
}

// optionalCustomer maps a stored customer label to the table-only Faire customer projection.
func optionalCustomer(value string) *faire.Customer {
	if value == "" {
		return nil
	}
	return &faire.Customer{FirstName: &value}
}

// formatTimestampPointer preserves a storage timestamp as an RFC 3339 value for the shared table formatter.
func formatTimestampPointer(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339Nano)
	return &formatted
}

// encodeLocalCursor serializes non-sensitive local keyset state in the worker result, never on the Gio frame loop.
func encodeLocalCursor(cursor *ordersstore.KeysetCursor) string {
	if cursor == nil {
		return ""
	}
	encoded, err := json.Marshal(localCursorPayload{SortAtUTC: cursor.SortAtUTC, OrderID: cursor.OrderID})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

// decodeLocalCursor deserializes non-sensitive local keyset state before issuing a worker SQLite query.
func decodeLocalCursor(value string) (*ordersstore.KeysetCursor, error) {
	if value == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	var payload localCursorPayload
	if err := json.Unmarshal(decoded, &payload); err != nil || payload.OrderID == "" {
		return nil, fmt.Errorf("invalid local Orders cursor")
	}
	return &ordersstore.KeysetCursor{SortAtUTC: payload.SortAtUTC, OrderID: payload.OrderID}, nil
}

// ordersNeedSync determines whether the selected connection lacks a completed bootstrap or is at least one hour stale.
func ordersNeedSync(ctx context.Context, store ordersstore.Store, connectionID string, now time.Time) (bool, error) {
	state, found, err := store.SyncState(ctx, connectionID)
	if err != nil {
		return true, err
	}
	if !found {
		return true, nil
	}
	if state.LastErrorKind == "invalid_request" {
		// An unchanged automatic request would repeat the same validation failure; require an explicit user adjustment and refresh.
		return false, nil
	}
	if state.BootstrapCompletedAtUTC == nil || state.LastSuccessfulSyncAtUTC == nil {
		return true, nil
	}
	return !state.LastSuccessfulSyncAtUTC.Add(time.Hour).After(now), nil
}

// localStatus returns a safe local freshness status after a successful worker state read.
func localStatus(store ordersstore.Store, ctx context.Context, connectionID string) string {
	state, found, err := store.SyncState(ctx, connectionID)
	if err != nil || !found || state.LastSuccessfulSyncAtUTC == nil {
		if found && state.LastErrorKind == "invalid_request" {
			return "Showing locally stored orders. The last synchronization request was invalid; adjust the history date before refreshing."
		}
		return "Showing locally stored orders."
	}
	return "Showing locally stored orders. Last synced " + state.LastSuccessfulSyncAtUTC.Local().Format("Jan 2, 15:04") + "."
}

// ordersStorageErrorMessage converts storage or snapshot failures to credential-safe user feedback.
func ordersStorageErrorMessage(err error) string {
	if errors.Is(err, ordersstore.ErrCorruptData) {
		return "Local order data needs to be rebuilt."
	}
	return "Local order storage could not be read. Rebuild local order data after resolving the issue."
}

// startOrdersScheduler creates a bounded hourly wake-up channel tied to the application lifetime.
func (ui *DesktopUI) startOrdersScheduler() {
	if ui.ordersStore == nil {
		return
	}
	go func(ctx context.Context, wakeups chan<- struct{}) {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case wakeups <- struct{}{}:
					ui.invalidate()
				default:
				}
			}
		}
	}(ui.ctx, ui.ordersSchedule)
}

// drainOrdersSchedule starts at most one ordinary local-load/sync path for the active connection on an hourly wake-up.
func (ui *DesktopUI) drainOrdersSchedule() {
	for {
		select {
		case <-ui.ordersSchedule:
			if ui.activeConnectionID != "" && !ui.ordersState.Loading && !ui.orderDetailLoading {
				ui.startOrdersLoad(false, false, true)
			}
		default:
			return
		}
	}
}

// invalidate requests another Gio frame when a native window exists.
// The nil guard keeps deterministic unit tests independent of a graphical window.
func (ui *DesktopUI) invalidate() {
	if ui.window != nil {
		ui.window.Invalidate()
	}
}

// startOrderExport validates an export request and starts the required API work without blocking Gio's frame loop.
func (ui *DesktopUI) startOrderExport(kind orderExportKind) {
	// Close before validation so any actionable failure is visible on the Orders page.
	ui.exportMenuOpen = false
	if ui.manager == nil || ui.activeConnectionID == "" {
		ui.ordersState.Status = "Choose an active saved connection before exporting orders."
		return
	}
	if ui.ordersState.Loading || ui.ordersExporting {
		ui.ordersState.Status = "Wait for the current Orders operation to finish before exporting."
		return
	}
	selectedIDs := selectedOrderIDs(ui.ordersState.SelectedIDs)
	if kind == orderExportSelected && len(selectedIDs) == 0 {
		ui.ordersState.Status = "Select one or more orders before exporting selected orders."
		return
	}
	ui.ordersExporting = true
	ui.exportRequestID++
	requestID := ui.exportRequestID
	connectionID := ui.activeConnectionID
	ui.ordersState.Status = "Exporting orders…"
	go ui.exportOrders(requestID, connectionID, kind, selectedIDs)
}

// exportOrders reads the authenticated Faire brand profile, retrieves the requested full orders, and writes their CSV file outside the frame loop.
func (ui *DesktopUI) exportOrders(requestID uint64, connectionID string, kind orderExportKind, selectedIDs []faire.OrderID) {
	client, _, err := ui.manager.Client(ui.ctx, connectionID, connections.ClientOptions{})
	if err != nil {
		ui.publishOrderExportResult(orderExportResult{RequestID: requestID, Status: ordersExportErrorMessage(err)})
		return
	}
	profile, err := client.Brands.Profile(ui.ctx)
	if err != nil {
		ui.publishOrderExportResult(orderExportResult{RequestID: requestID, Status: ordersExportErrorMessage(err)})
		return
	}
	saleSource, configured := exportSalesSource(profile)
	if !configured {
		ui.publishOrderExportResult(orderExportResult{RequestID: requestID, Status: "CSV export is not configured for this connection's Faire brand.", Blocked: true})
		return
	}
	var source []faire.Order
	switch kind {
	case orderExportNew:
		source, err = exportOrdersForState(ui.ctx, client.Orders, faire.OrderStateNew)
	case orderExportBackordered:
		source, err = exportOrdersForState(ui.ctx, client.Orders, faire.OrderStateBackordered)
	case orderExportSelected:
		source, err = exportSelectedOrders(ui.ctx, client.Orders, selectedIDs)
	default:
		err = fmt.Errorf("unknown order export kind")
	}
	if err != nil {
		ui.publishOrderExportResult(orderExportResult{RequestID: requestID, Status: ordersExportErrorMessage(err)})
		return
	}
	filename, err := writeOrdersCSVToDownloads(kind, saleSource, source)
	if err != nil {
		ui.publishOrderExportResult(orderExportResult{RequestID: requestID, Status: "Could not save the order export to Downloads. Check folder permissions and try again."})
		return
	}
	ui.publishOrderExportResult(orderExportResult{RequestID: requestID, Status: "Exported " + itoa(len(source)) + " orders to Downloads as " + filename + ".", Filename: filename, Completed: true})
}

// exportSalesSource derives the CSV source from the authenticated connection's current Faire brand profile.
// A missing profile ID cannot be safely mapped and blocks the export instead of using editable saved metadata.
func exportSalesSource(profile *faire.BrandProfile) (orders.SalesSource, bool) {
	if profile == nil || profile.BrandID == nil {
		return "", false
	}
	return orders.SalesSourceForBrand(*profile.BrandID)
}

// exportOrdersForState follows every cursor page and returns only orders in state, guarding against an unexpected API filter response.
func exportOrdersForState(ctx context.Context, service *faire.OrdersService, state faire.OrderState) ([]faire.Order, error) {
	options := faire.OrderListOptions{
		Limit:          faire.Ptr(int64(50)),
		ExcludedStates: excludedOrderStates(state),
		SortBy:         faire.Ptr(faire.OrderSortByCreatedAt),
	}
	var source []faire.Order
	seenCursors := make(map[string]struct{})
	for {
		page, err := service.List(ctx, &options)
		if err != nil {
			return nil, err
		}
		for _, order := range page.Orders {
			if order.State != nil && *order.State == state {
				source = append(source, order)
			}
		}
		if page.Cursor == nil || *page.Cursor == "" {
			return source, nil
		}
		if _, found := seenCursors[*page.Cursor]; found {
			return nil, fmt.Errorf("faire returned a repeated order-export cursor")
		}
		seenCursors[*page.Cursor] = struct{}{}
		options.Cursor = page.Cursor
	}
}

// excludedOrderStates returns every feature-supported state except the state requested for an export.
func excludedOrderStates(included faire.OrderState) []faire.OrderState {
	knownStates := orders.KnownStates()
	excluded := make([]faire.OrderState, 0, len(knownStates)-1)
	for _, state := range knownStates {
		if state != included {
			excluded = append(excluded, state)
		}
	}
	return excluded
}

// exportSelectedOrders retrieves each selected order by ID so its export contains fields intentionally omitted from list rows.
func exportSelectedOrders(ctx context.Context, service *faire.OrdersService, selectedIDs []faire.OrderID) ([]faire.Order, error) {
	source := make([]faire.Order, 0, len(selectedIDs))
	for _, orderID := range selectedIDs {
		order, err := service.Get(ctx, orderID)
		if err != nil {
			return nil, err
		}
		source = append(source, *order)
	}
	return source, nil
}

// selectedOrderIDs copies and sorts selected IDs to make selected-order exports deterministic.
func selectedOrderIDs(selected map[faire.OrderID]struct{}) []faire.OrderID {
	ids := make([]faire.OrderID, 0, len(selected))
	for orderID := range selected {
		if orderID != "" {
			ids = append(ids, orderID)
		}
	}
	slices.Sort(ids)
	return ids
}

// writeOrdersCSVToDownloads atomically writes a private CSV file in the current user's Downloads directory.
func writeOrdersCSVToDownloads(kind orderExportKind, saleSource orders.SalesSource, source []faire.Order) (string, error) {
	homeDirectory, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return writeOrdersCSV(filepath.Join(homeDirectory, "Downloads"), kind, saleSource, source)
}

// writeOrdersCSV atomically writes a private CSV file in directory and returns its generated filename.
func writeOrdersCSV(directory string, kind orderExportKind, saleSource orders.SalesSource, source []faire.Order) (string, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", err
	}
	filename := "faire-" + string(kind) + "-orders-" + time.Now().Local().Format("20060102150405") + ".csv"
	finalPath := filepath.Join(directory, filename)
	temporaryFile, err := os.CreateTemp(directory, ".faire-orders-*.csv")
	if err != nil {
		return "", err
	}
	temporaryPath := temporaryFile.Name()
	defer func() {
		// Removing the temporary file is harmless after a successful rename and prevents partial PII exports on failures.
		_ = os.Remove(temporaryPath)
	}()
	if err := orders.WriteCSV(temporaryFile, saleSource, source); err != nil {
		_ = temporaryFile.Close()
		return "", err
	}
	if err := temporaryFile.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return "", err
	}
	return filename, nil
}

// ordersExportErrorMessage converts an export API failure to credential-safe user feedback.
func ordersExportErrorMessage(err error) string {
	if errors.Is(err, context.Canceled) {
		return "Order export was canceled."
	}
	if apiError, ok := errors.AsType[*faire.APIError](err); ok {
		switch apiError.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "Faire rejected this connection's credentials. Update the saved connection or reauthorize it."
		case http.StatusTooManyRequests:
			return "Faire is rate limiting requests. Wait a moment, then export again."
		default:
			return fmt.Sprintf("Faire could not export orders (HTTP %d). Try again later.", apiError.StatusCode)
		}
	}
	return "Orders could not be exported. Check the saved connection and try again."
}

// publishOrderExportResult sends an export result unless application shutdown has begun.
func (ui *DesktopUI) publishOrderExportResult(result orderExportResult) {
	select {
	case ui.orderExportResults <- result:
	case <-ui.ctx.Done():
		return
	}
	ui.invalidate()
}

// drainOrderExportResults applies only the most recent export status to the Orders page.
func (ui *DesktopUI) drainOrderExportResults() {
	for {
		select {
		case result := <-ui.orderExportResults:
			if result.RequestID != ui.exportRequestID {
				continue
			}
			ui.ordersExporting = false
			ui.ordersState.Status = result.Status
			if result.Blocked {
				ui.csvExportBlockedDialogOpen = true
			}
			if result.Completed {
				ui.csvExportCompletedFilename = result.Filename
				ui.csvExportCompletedDialogOpen = true
			}
		default:
			return
		}
	}
}

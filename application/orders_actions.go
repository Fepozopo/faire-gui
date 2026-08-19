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
	"strings"
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

// orderExportOptions describes the per-export CSV and packing-slip choices selected in the dialog.
// IncludeHeader controls the CSV header row, while IncludePackingSlips controls whether one PDF is requested per exported order.
type orderExportOptions struct {
	IncludeHeader       bool
	IncludePackingSlips bool
}

// orderExportDialogState preserves the two-step scope and configuration choices while the export modal is open.
// kind is populated only after the user chooses a scope; includeHeader and includePackingSlips reset to their defaults for each newly opened dialog.
type orderExportDialogState struct {
	open                bool
	configuring         bool
	kind                orderExportKind
	includeHeader       bool
	includePackingSlips bool
}

const (
	// orderExportNew writes every currently new Faire order.
	orderExportNew orderExportKind = "new"
	// orderExportBackordered writes every currently backordered Faire order.
	orderExportBackordered orderExportKind = "backordered"
	// orderExportSelected writes every order selected in the current Orders table.
	orderExportSelected orderExportKind = "selected"
)

// orderExportResult carries credential-safe export completion, blocking, and saved-artifact state to the frame loop.
// PackingSlipFolder is set only for packing-slip exports, while PackingSlipFailures records safe partial-completion counts.
type orderExportResult struct {
	RequestID           uint64
	Status              string
	Filename            string
	PackingSlipFolder   string
	PackingSlipCount    int
	PackingSlipFailures int
	Blocked             bool
	Completed           bool
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
	if ui.orders.dataActionConnectionID != "" {
		ui.connectionPickerOpen = false
		ui.status = "Wait for the local order-data action to finish before changing the active connection."
		ui.invalidate()
		return
	}
	ui.activeConnectionID = connection.ID
	ui.activeConnectionLabel = connection.Label
	ui.connectionPickerOpen = false
	// A completion from the prior connection must not overwrite this connection's Orders status.
	ui.orders.exportRequestID++
	ui.orders.detailRequestID++
	ui.orders.view.exporting = false
	ui.orders.view.orderDetailOpen = false
	ui.orders.view.orderDetailLoading = false
	ui.orders.view.orderDetail = orders.Detail{}
	ui.orders.view.orderDetailID = ""
	ui.orders.view.orderDetailConnectionID = ""
	ui.resetOrdersState()
	ui.orders.view.searchActive = false
	ui.orders.view.search.SetText("")
	ui.orders.view.list.Position.First = 0
	ui.orders.view.list.Position.Offset = 0
	ui.selectedTab = ordersTab
	ui.startOrdersLoad(ordersLoadInitial)
	ui.invalidate()
}

// startOrdersLoad requests one named Orders operation from the feature-owned controller.
// It captures connection-scoped presentation inputs on the frame goroutine so workers never read mutable shell state.
func (ui *DesktopUI) startOrdersLoad(kind ordersLoadKind) {
	if ui.orders.store == nil {
		ui.orders.view.state.Status = "Local order storage is unavailable. Close the app, resolve the local data issue, then reopen it."
		return
	}
	if ui.activeConnectionID == "" {
		ui.orders.view.state.Status = "Choose an active saved connection to load orders."
		return
	}
	if ui.orders.dataActionConnectionID == ui.activeConnectionID {
		ui.orders.view.state.Status = "Local order data is being rebuilt. The refreshed orders will appear when it finishes."
		return
	}
	if ui.orders.view.state.Loading || ui.orders.view.orderDetailLoading {
		return
	}
	if kind == ordersLoadNextPage && ui.orders.view.state.Cursor == "" {
		return
	}

	ui.orders.loadRequestID++
	state := ui.orders.view.state
	if kind != ordersLoadNextPage {
		state.Cursor = ""
	}
	request := ordersLoadRequest{
		RequestID:       ui.orders.loadRequestID,
		ConnectionID:    ui.activeConnectionID,
		State:           state,
		Kind:            kind,
		RestoreBoundary: !ui.orders.view.historyBoundaryKnown && (kind == ordersLoadInitial || kind == ordersLoadScheduledRefresh),
	}
	ui.orders.view.state.Loading = true
	ui.orders.view.state.Status = "Loading locally stored orders…"
	go ui.orders.loadAndMaybeSync(request)
}

// loadAndMaybeSync performs the Orders local-first workflow outside the Gio frame loop.
// request is immutable and names its operation; the worker publishes only safe presentation values.
func (controller *ordersController) loadAndMaybeSync(request ordersLoadRequest) {
	store := controller.store
	state := request.State
	appendResults := request.Kind == ordersLoadNextPage
	boundary := ""
	if request.RestoreBoundary {
		syncState, found, err := store.SyncState(controller.ctx, request.ConnectionID)
		if err != nil {
			controller.publishLoadResult(orderLoadResult{RequestID: request.RequestID, Append: appendResults, Status: ordersStorageErrorMessage(err)})
			return
		}
		if found {
			boundary = syncState.BootstrapUpdatedAtMinUTC.Format(time.RFC3339)
			state.Query.UpdatedAtMin = boundary
		}
	}
	page, err := loadLocalPage(controller.ctx, store, request.ConnectionID, state)
	if err != nil {
		controller.publishLoadResult(orderLoadResult{RequestID: request.RequestID, Append: appendResults, Status: ordersStorageErrorMessage(err)})
		return
	}
	localResult := func(status string, keepLoading bool) orderLoadResult {
		result := orderLoadResult{RequestID: request.RequestID, Append: appendResults, Rows: localRows(page.Rows), Cursor: encodeLocalCursor(page.NextCursor), Status: status, ApplyRows: true, KeepLoading: keepLoading, UpdatedAtMin: boundary, ApplyBoundary: boundary != ""}
		return attachNewOrdersCount(controller.ctx, store, request.ConnectionID, result)
	}
	if request.Kind == ordersLoadNextPage || request.Kind == ordersLoadLocalOnly {
		controller.publishLoadResult(localResult(localStatus(store, controller.ctx, request.ConnectionID), false))
		return
	}
	if request.Kind == ordersLoadInitial || request.Kind == ordersLoadScheduledRefresh {
		shouldSync, err := ordersNeedSync(controller.ctx, store, request.ConnectionID, time.Now().UTC())
		if err != nil {
			controller.publishLoadResult(localResult(ordersStorageErrorMessage(err), false))
			return
		}
		if !shouldSync {
			controller.publishLoadResult(localResult(localStatus(store, controller.ctx, request.ConnectionID), false))
			return
		}
	}
	controller.publishLoadResult(localResult("Checking Faire for updated orders…", true))
	summary, err := controller.syncConnection(request)
	if errors.Is(err, errOrdersManagerUnavailable) {
		controller.publishLoadResult(localResult("Showing locally stored orders. Saved connections are unavailable.", false))
		return
	}
	if errors.Is(err, errInvalidHistoryBoundary) {
		controller.publishLoadResult(orderLoadResult{RequestID: request.RequestID, Status: "Enter a valid updated-at minimum before refreshing."})
		return
	}
	if err != nil {
		controller.publishLoadResult(orderLoadResult{RequestID: request.RequestID, Status: ordersLoadErrorMessage(err)})
		return
	}
	if summary.Bootstrap || summary.HistoryExpanded {
		syncState, found, stateErr := store.SyncState(controller.ctx, request.ConnectionID)
		if stateErr != nil || !found {
			controller.publishLoadResult(orderLoadResult{RequestID: request.RequestID, Status: ordersStorageErrorMessage(stateErr)})
			return
		}
		boundary = syncState.BootstrapUpdatedAtMinUTC.Format(time.RFC3339)
		state.Query.UpdatedAtMin = boundary
	}
	page, err = loadLocalPage(controller.ctx, store, request.ConnectionID, state)
	if err != nil {
		controller.publishLoadResult(orderLoadResult{RequestID: request.RequestID, Status: ordersStorageErrorMessage(err)})
		return
	}
	status := "Orders are up to date."
	if summary.HistoryExpanded {
		status = "Older order history was added from Faire."
	} else if summary.Orders > 0 {
		status = "Orders updated from Faire."
	}
	controller.publishLoadResult(localResult(status, false))
}

// publishLoadResult sends a safe Orders load result unless application shutdown has begun.
func (controller *ordersController) publishLoadResult(result orderLoadResult) {
	select {
	case controller.loadResults <- result:
	case <-controller.ctx.Done():
		return
	}
	if controller.invalidate != nil {
		controller.invalidate()
	}
}

// loadOrderByDisplayID searches the connection-scoped local index before using the authenticated direct-lookup fallback.
func (ui *DesktopUI) loadOrderByDisplayID() {
	if ui.activeConnectionID == "" || ui.orders.store == nil {
		ui.orders.view.state.Status = "Choose an active saved connection with local order storage before searching orders."
		return
	}
	displayID, err := orders.NormalizeDisplayID(ui.orders.view.search.Text())
	if err != nil {
		ui.orders.view.state.Status = "Enter a valid order number."
		return
	}
	orderID, _ := orders.OrderIDFromDisplayID(displayID)
	ui.orders.view.searchActive = true
	ui.orders.loadRequestID++
	requestID, connectionID := ui.orders.loadRequestID, ui.activeConnectionID
	ui.orders.view.state.Loading = true
	ui.orders.view.state.Status = "Searching locally stored orders…"
	go ui.orders.lookupAndPersistOrder(requestID, connectionID, displayID, orderID)
}

// drainOrderResults delegates Orders result validation and view updates to the feature controller.
// The shell applies only its matching cross-feature Brand Profile status.
func (ui *DesktopUI) drainOrderResults() {
	if status, apply := ui.orders.drainLoadResults(ui.activeConnectionID); apply {
		ui.status = status
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

// openOrder opens the locally stored snapshot identified by the clicked order number.
// It resets the detail viewport so every newly opened order starts at its summary.
func (ui *DesktopUI) openOrder(orderID faire.OrderID) {
	if ui.activeConnectionID == "" || ui.orders.store == nil {
		ui.orders.view.state.Status = "Choose an active saved connection before opening an order."
		return
	}
	if orderID == "" {
		ui.orders.view.state.Status = "The selected order does not have a valid identifier."
		return
	}
	ui.orders.detailRequestID++
	requestID := ui.orders.detailRequestID
	connectionID, store := ui.activeConnectionID, ui.orders.store
	ui.orders.view.orderDetailOpen, ui.orders.view.orderDetailLoading = true, true
	ui.orders.view.orderDetailID, ui.orders.view.orderDetailConnectionID = orderID, connectionID
	ui.orders.view.orderDetail = orders.Detail{}
	ui.orders.view.detailList.Position.First = 0
	ui.orders.view.detailList.Position.Offset = 0
	ui.orders.view.orderDetailStatus = "Opening locally stored order details…"
	go loadOrderDetail(ui.ctx, store, requestID, connectionID, orderID, ui.orders.publishOrderDetailResult)
}

// refreshOrderDetail explicitly retrieves the currently open order and atomically replaces its local snapshot without advancing the feed checkpoint.
func (ui *DesktopUI) refreshOrderDetail() {
	if ui.orders.view.orderDetailID == "" || ui.orders.view.orderDetailConnectionID != ui.activeConnectionID || ui.orders.store == nil || ui.manager == nil {
		ui.orders.view.orderDetailStatus = "Order details cannot be refreshed until an active saved connection is available."
		return
	}
	if ui.orders.view.orderDetailLoading || ui.orders.view.state.Loading {
		return
	}
	ui.orders.detailRequestID++
	requestID := ui.orders.detailRequestID
	connectionID, orderID := ui.activeConnectionID, ui.orders.view.orderDetailID
	ui.orders.view.orderDetailLoading = true
	ui.orders.view.orderDetailStatus = "Refreshing order details from Faire…"
	go ui.orders.refreshAndPersistDetail(requestID, connectionID, orderID)
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

// drainOrderDetailResults delegates stale-result validation and detail presentation updates to the feature controller.
func (ui *DesktopUI) drainOrderDetailResults() {
	ui.orders.drainDetailResults(ui.activeConnectionID)
}

// requestOrdersDataAction opens explicit confirmation for a connection-scoped local-data delete or rebuild operation.
// connectionID comes from the clicked Brand Profile card, preventing an action from affecting another connection's cached orders.
func (ui *DesktopUI) requestOrdersDataAction(connectionID string, rebuild bool) {
	if ui.orders.dataActionConnectionID != "" {
		ui.status = "Wait for the current local order-data action to finish before starting another one."
		return
	}
	if connectionID == "" || ui.orders.store == nil {
		ui.status = "Local order storage is unavailable. Close the app, resolve the local data issue, then reopen it."
		return
	}
	ui.orders.view.dataDialog = ordersDataDialogState{open: true, rebuild: rebuild, connectionID: connectionID}
}

// startOrdersDataAction deletes only connectionID's private cached orders and optionally starts a fresh bootstrap.
// It marks the action connection as busy, preventing a connection switch or duplicate sync until the action has completed.
func (ui *DesktopUI) startOrdersDataAction(connectionID string, rebuild bool) {
	if ui.orders.store == nil || connectionID == "" {
		return
	}
	// The status area is above the connection cards, so return there before the confirmation closes and background work begins.
	ui.brandsList.Position = layout.Position{}
	ui.orders.view.dataDialog = ordersDataDialogState{}
	ui.orders.dataActionConnectionID = connectionID
	ui.status = localDataActionStatus(rebuild)
	if connectionID != ui.activeConnectionID {
		ui.orders.dataStatusRequestID = 0
		ui.startInactiveOrdersDataAction(connectionID, rebuild)
		return
	}
	ui.orders.loadRequestID++
	requestID := ui.orders.loadRequestID
	ui.orders.dataStatusRequestID = requestID
	state := ui.orders.view.state
	if rebuild {
		// Rebuild intentionally starts from the current default window rather than silently preserving an older retained-history expansion.
		state.Query.UpdatedAtMin = orders.NewStateAt(time.Now(), time.Local).Query.UpdatedAtMin
		ui.orders.view.historyBoundaryKnown = false
	}
	state.Cursor = ""
	ui.orders.view.state.Rows = nil
	ui.orders.view.state.Cursor = ""
	ui.orders.view.state.Loaded = false
	ui.orders.view.state.Loading = true
	ui.orders.view.state.SelectedIDs = make(map[faire.OrderID]struct{})
	ui.orders.view.state.Status = localDataActionStatus(rebuild)
	ui.orders.startActiveDataAction(ordersLoadRequest{RequestID: requestID, ConnectionID: connectionID, State: state, Kind: ordersLoadRebuild}, rebuild)
}

// localDataActionStatus describes the connection-scoped local-data operation currently running.
func localDataActionStatus(rebuild bool) string {
	if rebuild {
		return "Rebuilding locally stored order data…"
	}
	return "Deleting locally stored order data…"
}

// startInactiveOrdersDataAction starts a controller-owned cache action for a Brand Profile card that is not active in Orders.
// The controller reports an explicit connection-scoped event, so the active Orders table remains untouched.
func (ui *DesktopUI) startInactiveOrdersDataAction(connectionID string, rebuild bool) {
	ui.status = localDataActionStatus(rebuild)
	ui.orders.startInactiveDataAction(connectionID, rebuild)
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

// localRows converts source storage projections, including Faire total payouts and commission BPS, to safe table presentation rows outside the frame loop.
// It returns one presentation row per source record.
func localRows(source []ordersstore.LocalRow) []orders.Row {
	rows := make([]orders.Row, len(source))
	for index, sourceRow := range source {
		id := faire.OrderID(sourceRow.OrderID)
		order := faire.Order{ID: &id, DisplayID: optionalPointer(sourceRow.DisplayID), State: optionalOrderState(sourceRow.State), Address: optionalAddress(sourceRow.AddressName), CreatedAt: formatTimestampPointer(sourceRow.CreatedAtUTC), ExpectedShipDate: formatTimestampPointer(sourceRow.ExpectedShipAtUTC), Source: optionalPointer(sourceRow.Source)}
		row := orders.PresentRow(order)
		row.TotalPayout = orders.FormatTotal(sourceRow.TotalPayoutAmountMinor, sourceRow.TotalPayoutCurrency)
		row.Commission = orders.FormatCommissionPercentage(sourceRow.CommissionBPS)
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

// optionalAddress maps value to a table-only Faire delivery-address projection.
// It returns nil when value is absent so the presenter emits its missing-value placeholder.
func optionalAddress(value string) *faire.Address {
	if value == "" {
		return nil
	}
	return &faire.Address{Name: &value}
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

// startOrdersScheduler starts the controller-owned hourly wake-up source when local Orders storage is available.
func (ui *DesktopUI) startOrdersScheduler() {
	ui.orders.startScheduler()
}

// drainOrdersSchedule delegates queued wake-ups to the controller, then asks for one scheduled operation when the visible Orders state is idle.
func (ui *DesktopUI) drainOrdersSchedule() {
	if ui.orders.drainSchedule() && ui.activeConnectionID != "" && !ui.orders.view.state.Loading && !ui.orders.view.orderDetailLoading {
		ui.startOrdersLoad(ordersLoadScheduledRefresh)
	}
}

// invalidate requests another Gio frame when a native window exists.
// The nil guard keeps deterministic unit tests independent of a graphical window.
func (ui *DesktopUI) invalidate() {
	if ui.window != nil {
		ui.window.Invalidate()
	}
}

// startOrderExport validates one configured export and starts the required API work without blocking Gio's frame loop.
// kind identifies the order scope, options carry the per-export CSV and packing-slip choices, and it has no return value because completion is published to the frame loop.
func (ui *DesktopUI) startOrderExport(kind orderExportKind, options orderExportOptions) {
	// Close before validation so any actionable failure is visible on the Orders page.
	ui.orders.view.exportDialog = orderExportDialogState{}
	if ui.manager == nil || ui.activeConnectionID == "" {
		ui.orders.view.state.Status = "Choose an active saved connection before exporting orders."
		return
	}
	if ui.orders.view.state.Loading || ui.orders.view.exporting {
		ui.orders.view.state.Status = "Wait for the current Orders operation to finish before exporting."
		return
	}
	selectedIDs := selectedOrderIDs(ui.orders.view.state.SelectedIDs)
	if kind == orderExportSelected && len(selectedIDs) == 0 {
		ui.orders.view.state.Status = "Select one or more orders before exporting selected orders."
		return
	}
	ui.orders.view.exporting = true
	ui.orders.exportRequestID++
	requestID := ui.orders.exportRequestID
	connectionID := ui.activeConnectionID
	ui.orders.view.state.Status = "Exporting orders…"
	go ui.orders.exportOrders(requestID, connectionID, kind, selectedIDs, options)
}

// exportOrders reads the authenticated Faire brand profile, retrieves the requested full orders, and writes the selected CSV and packing-slip artifacts outside the frame loop.
// requestID identifies the current worker, connectionID scopes credentials, kind and selectedIDs identify orders, and options select the CSV header and optional PDFs; it returns no value because it publishes a safe result.
func (controller *ordersController) exportOrders(requestID uint64, connectionID string, kind orderExportKind, selectedIDs []faire.OrderID, options orderExportOptions) {
	client, _, err := controller.manager.Client(controller.ctx, connectionID, connections.ClientOptions{})
	if err != nil {
		controller.publishOrderExportResult(orderExportResult{RequestID: requestID, Status: ordersExportErrorMessage(err)})
		return
	}
	profile, err := client.Brands.Profile(controller.ctx)
	if err != nil {
		controller.publishOrderExportResult(orderExportResult{RequestID: requestID, Status: ordersExportErrorMessage(err)})
		return
	}
	saleSource, configured := exportSalesSource(profile)
	if !configured {
		controller.publishOrderExportResult(orderExportResult{RequestID: requestID, Status: "CSV export is not configured for this connection's Faire brand.", Blocked: true})
		return
	}
	var source []faire.Order
	switch kind {
	case orderExportNew:
		source, err = exportOrdersForState(controller.ctx, client.Orders, faire.OrderStateNew)
	case orderExportBackordered:
		source, err = exportOrdersForState(controller.ctx, client.Orders, faire.OrderStateBackordered)
	case orderExportSelected:
		source, err = exportSelectedOrders(controller.ctx, client.Orders, selectedIDs)
	default:
		err = fmt.Errorf("unknown order export kind")
	}
	if err != nil {
		controller.publishOrderExportResult(orderExportResult{RequestID: requestID, Status: ordersExportErrorMessage(err)})
		return
	}

	filename, packingSlipFolder, packingSlipSummary, err := writeOrderExport(controller.ctx, client.Orders, kind, saleSource, source, options)
	if err != nil {
		status := "Could not save the order export to Downloads. Check folder permissions and try again."
		if errors.Is(err, context.Canceled) {
			status = ordersExportErrorMessage(err)
		}
		controller.publishOrderExportResult(orderExportResult{RequestID: requestID, Status: status})
		return
	}
	controller.publishOrderExportResult(orderExportResult{
		RequestID:           requestID,
		Status:              orderExportCompletionStatus(len(source), filename, packingSlipFolder, packingSlipSummary),
		Filename:            filename,
		PackingSlipFolder:   packingSlipFolder,
		PackingSlipCount:    packingSlipSummary.downloaded,
		PackingSlipFailures: packingSlipSummary.failures,
		Completed:           true,
	})
}

// packingSlipSummary records only safe successful and failed PDF counts for a completed export.
// downloaded counts saved PDFs and failures counts skipped or failed PDFs without retaining private order or transport details.
type packingSlipSummary struct {
	downloaded int
	failures   int
}

// writeOrderExport writes a CSV directly to Downloads or, when requested, writes the CSV and packing slips into one unique folder.
// ctx cancels packing-slip work, service retrieves PDFs, kind and saleSource name and format the CSV, source is exported orders, options choose artifacts, and it returns artifact names plus a safe PDF summary.
func writeOrderExport(ctx context.Context, service *faire.OrdersService, kind orderExportKind, saleSource orders.SalesSource, source []faire.Order, options orderExportOptions) (filename, packingSlipFolder string, summary packingSlipSummary, err error) {
	if !options.IncludePackingSlips {
		filename, err = writeOrdersCSVToDownloads(kind, saleSource, source, options.IncludeHeader)
		return filename, "", packingSlipSummary{}, err
	}

	directory, folder, err := createPackingSlipExportDirectory(kind)
	if err != nil {
		return "", "", packingSlipSummary{}, err
	}
	filename, err = writeOrdersCSV(directory, kind, saleSource, source, options.IncludeHeader)
	if err != nil {
		// The folder contains no user-visible artifact yet, so remove it rather than leaving an empty failed export behind.
		_ = os.Remove(directory)
		return "", "", packingSlipSummary{}, err
	}
	summary, err = downloadPackingSlips(ctx, service, source, directory)
	if err != nil {
		return "", "", packingSlipSummary{}, err
	}
	return filename, folder, summary, nil
}

// downloadPackingSlips retrieves and writes one PDF per export order, retaining successful artifacts when individual requests or writes fail.
// ctx cancels the batch, service downloads PDFs using Faire's default timezone, source identifies orders, directory receives private files, and the returned summary excludes private error details.
func downloadPackingSlips(ctx context.Context, service *faire.OrdersService, source []faire.Order, directory string) (packingSlipSummary, error) {
	summary := packingSlipSummary{}
	usedNames := make(map[string]struct{}, len(source))
	for index, order := range source {
		if err := ctx.Err(); err != nil {
			return packingSlipSummary{}, err
		}
		if order.ID == nil || *order.ID == "" {
			summary.failures++
			continue
		}
		pdf, err := service.DownloadPackingSlipPDF(ctx, *order.ID)
		if err != nil {
			summary.failures++
			continue
		}
		filename := packingSlipFilename(order, index, usedNames)
		if err := os.WriteFile(filepath.Join(directory, filename), pdf, 0o600); err != nil {
			summary.failures++
			continue
		}
		summary.downloaded++
	}
	return summary, nil
}

// createPackingSlipExportDirectory creates one owner-only timestamped folder under Downloads for a CSV and its packing-slip PDFs.
// kind identifies the exported scope, and it returns the absolute directory and its user-facing folder name or a filesystem error.
func createPackingSlipExportDirectory(kind orderExportKind) (directory, folder string, err error) {
	downloadsDirectory, err := downloadsDirectory()
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(downloadsDirectory, 0o755); err != nil {
		return "", "", err
	}
	prefix := "faire-" + string(kind) + "-orders-" + time.Now().Local().Format("20060102150405")
	for suffix := 0; ; suffix++ {
		folder = prefix
		if suffix > 0 {
			folder += "-" + itoa(suffix+1)
		}
		directory = filepath.Join(downloadsDirectory, folder)
		if err := os.Mkdir(directory, 0o700); err == nil {
			return directory, folder, nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", "", err
		}
	}
}

// downloadsDirectory returns the current user's Downloads directory for user-requested export artifacts.
// It has no parameters and returns an absolute directory path or the user-home lookup error.
func downloadsDirectory() (string, error) {
	homeDirectory, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDirectory, "Downloads"), nil
}

// packingSlipFilename returns a display-ID-based, collision-safe PDF filename for one order.
// order supplies display and immutable IDs, index provides a final deterministic fallback, usedNames tracks prior names, and the returned filename is safe to join beneath the export directory.
func packingSlipFilename(order faire.Order, index int, usedNames map[string]struct{}) string {
	base := ""
	if order.DisplayID != nil {
		base = safeFilenameComponent(*order.DisplayID)
	}
	if base == "" && order.ID != nil {
		base = safeFilenameComponent(string(*order.ID))
	}
	if base == "" {
		base = "order-" + itoa(index+1)
	}
	filename := base + ".pdf"
	if _, found := usedNames[filename]; found {
		if order.ID != nil {
			if identifier := safeFilenameComponent(string(*order.ID)); identifier != "" {
				filename = base + "-" + identifier + ".pdf"
			}
		}
		for suffix := 2; ; suffix++ {
			if _, found := usedNames[filename]; !found {
				break
			}
			filename = base + "-" + itoa(suffix) + ".pdf"
		}
	}
	usedNames[filename] = struct{}{}
	return filename
}

// safeFilenameComponent converts an arbitrary display or order ID to a conservative cross-platform filename component.
// value is the untrusted identifier, and the returned string contains only letters, digits, hyphens, underscores, and periods.
func safeFilenameComponent(value string) string {
	var builder strings.Builder
	for _, character := range strings.TrimSpace(value) {
		switch {
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z', character >= '0' && character <= '9', character == '-', character == '_', character == '.':
			builder.WriteRune(character)
		default:
			builder.WriteByte('_')
		}
	}
	return strings.Trim(builder.String(), "._")
}

// orderExportCompletionStatus returns a safe completion message for CSV-only, complete packing-slip, and partial packing-slip exports.
// orderCount identifies exported orders, filename and packingSlipFolder identify user-visible artifacts, summary contains only counts, and the returned message excludes private order details.
func orderExportCompletionStatus(orderCount int, filename, packingSlipFolder string, summary packingSlipSummary) string {
	status := "Exported " + itoa(orderCount) + " orders to Downloads as " + filename + "."
	if packingSlipFolder == "" {
		return status
	}
	status += " Saved " + packingSlipCountLabel(summary.downloaded) + " in " + packingSlipFolder + "."
	if summary.failures > 0 {
		status += " " + packingSlipCountLabel(summary.failures) + " could not be downloaded."
	}
	return status
}

// packingSlipCountLabel formats a count with the correct packing-slip singular or plural noun.
// count is the number of PDFs, and the returned label is safe for user-visible export feedback.
func packingSlipCountLabel(count int) string {
	label := itoa(count) + " packing slip"
	if count != 1 {
		label += "s"
	}
	return label
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

// writeOrdersCSVToDownloads atomically writes a CSV file in the current user's Downloads directory with the selected header behavior.
// kind identifies the file, saleSource and source provide CSV values, includeHeader controls its first row, and it returns the generated filename or a filesystem error.
func writeOrdersCSVToDownloads(kind orderExportKind, saleSource orders.SalesSource, source []faire.Order, includeHeader bool) (string, error) {
	directory, err := downloadsDirectory()
	if err != nil {
		return "", err
	}
	return writeOrdersCSV(directory, kind, saleSource, source, includeHeader)
}

// writeOrdersCSV atomically writes a CSV file in directory and returns its generated filename.
// directory receives the file, kind identifies it, saleSource and source provide CSV values, includeHeader controls its first row, and it returns the filename or a filesystem error.
func writeOrdersCSV(directory string, kind orderExportKind, saleSource orders.SalesSource, source []faire.Order, includeHeader bool) (string, error) {
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
	if err := orders.WriteCSV(temporaryFile, saleSource, source, includeHeader); err != nil {
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
func (controller *ordersController) publishOrderExportResult(result orderExportResult) {
	select {
	case controller.exportResults <- result:
	case <-controller.ctx.Done():
		return
	}
	if controller.invalidate != nil {
		controller.invalidate()
	}
}

// drainOrderExportResults delegates stale-result validation and export presentation updates to the feature controller.
func (ui *DesktopUI) drainOrderExportResults() {
	ui.orders.drainExportResults()
}

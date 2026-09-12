package application

import (
	"context"
	"encoding/base64"
	"encoding/json/v2"
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
	"github.com/gpdf-dev/gpdf"
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

// shipmentSubmissionResult carries a completed batch shipment submission back to the frame loop.
// It keeps the raw API response out of view state while allowing the successful persisted detail to replace the open snapshot.
type shipmentSubmissionResult struct {
	RequestID           uint64
	ConnectionID        string
	OrderID             faire.OrderID
	Detail              orders.Detail
	Status              string
	NewOrdersCount      int
	ApplyNewOrdersCount bool
}

// itemAvailabilityResult carries a completed item-availability submission back to the frame loop.
// It contains only safe status text or a typed persisted detail model, never credentials or a raw Faire response.
type itemAvailabilityResult struct {
	RequestID           uint64
	ConnectionID        string
	OrderID             faire.OrderID
	Detail              orders.Detail
	Status              string
	NewOrdersCount      int
	ApplyNewOrdersCount bool
}

// orderProcessingResult carries one completed selected-order processing batch back to the frame loop.
// ProcessedIDs identify only successful endpoint calls, Rows replaces visible safe table projections, and Status never exposes API bodies or order details.
type orderProcessingResult struct {
	RequestID           uint64
	ConnectionID        string
	ProcessedIDs        []faire.OrderID
	Rows                map[faire.OrderID]orders.Row
	Status              string
	ExpectedDateOmitted int
	LookupFailures      int
	PersistenceFailures int
	ProcessingFailures  int
}

// localCursorPayload is the non-sensitive worker-only encoding of a local SQLite keyset cursor.
type localCursorPayload struct {
	SortAtUTC *time.Time `json:"sort_at_utc,omitempty"`
	OrderID   string     `json:"order_id"`
}

// orderExportKind identifies an order scope used by CSV exports and packing-slip artifact names.
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
	// orderExportSelected identifies every order selected in the current Orders table.
	orderExportSelected orderExportKind = "selected"
)

// orderExportResult carries credential-safe export completion, blocking, and saved-artifact state to the frame loop.
// Filename is set for CSV exports, while packing-slip fields describe the PDF folder, safe partial-completion counts, and combined-document outcome.
type orderExportResult struct {
	RequestID           uint64
	Status              string
	Filename            string
	PackingSlipFolder   string
	PackingSlipCount    int
	PackingSlipFailures int
	PackingSlipCombined bool
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

// shipmentSubmissionErrorMessage converts a shipment endpoint failure into safe, actionable UI feedback.
// It intentionally omits raw API response bodies because they can contain private order or account information.
func shipmentSubmissionErrorMessage(err error) string {
	if errors.Is(err, context.Canceled) {
		return "Adding shipment information was canceled."
	}
	if apiError, ok := errors.AsType[*faire.APIError](err); ok {
		switch apiError.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "Faire rejected this connection's credentials. Update the saved connection or reauthorize it."
		case http.StatusTooManyRequests:
			return "Faire is rate limiting requests. Wait a moment, then try again."
		case http.StatusBadRequest:
			return "Faire rejected the shipment information. Check every package and try again."
		default:
			return fmt.Sprintf("Faire could not add shipment information (HTTP %d). Try again later.", apiError.StatusCode)
		}
	}
	return "Shipment information could not be added. Check the saved connection and try again."
}

// itemAvailabilityErrorMessage converts an availability endpoint failure into safe feedback while retaining the user's draft for retry.
// It deliberately omits raw API response bodies because they can contain private order and account information.
func itemAvailabilityErrorMessage(err error) string {
	if errors.Is(err, context.Canceled) {
		return "Updating item availability was canceled."
	}
	if apiError, ok := errors.AsType[*faire.APIError](err); ok {
		switch apiError.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "Faire rejected this connection's credentials. Update the saved connection or reauthorize it."
		case http.StatusTooManyRequests:
			return "Faire is rate limiting requests. Wait a moment, then try again."
		case http.StatusBadRequest:
			return "Faire rejected the item availability update. Check the selected items and try again."
		default:
			return fmt.Sprintf("Faire could not update item availability (HTTP %d). Try again later.", apiError.StatusCode)
		}
	}
	return "Item availability could not be updated. Check the saved connection and try again."
}

// moveToProcessingErrorMessage converts a processing endpoint failure into safe, actionable UI feedback.
// It deliberately omits response bodies because they can contain private order or account information.
func moveToProcessingErrorMessage(err error) string {
	if errors.Is(err, context.Canceled) {
		return "Moving selected orders to processing was canceled."
	}
	if apiError, ok := errors.AsType[*faire.APIError](err); ok {
		switch apiError.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "Faire rejected this connection's credentials. Update the saved connection or reauthorize it."
		case http.StatusTooManyRequests:
			return "Faire is rate limiting requests. Wait a moment, then try again."
		case http.StatusBadRequest:
			return "Faire rejected one or more selected orders. Check their status and try again."
		default:
			return fmt.Sprintf("Faire could not move selected orders to processing (HTTP %d). Try again later.", apiError.StatusCode)
		}
	}
	return "Selected orders could not be moved to processing. Check the saved connection and try again."
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
	ui.orders.availabilityRequestID++
	ui.orders.view.exporting = false
	ui.orders.view.orderDetailOpen = false
	ui.orders.view.orderDetailLoading = false
	ui.orders.view.availabilitySubmitting = false
	ui.orders.view.resetPendingUnavailable()
	ui.orders.view.orderDetail = orders.Detail{}
	ui.orders.view.orderDetailID = ""
	ui.orders.view.orderDetailConnectionID = ""
	ui.resetOrdersState()
	ui.orders.view.searchActive = false
	ui.orders.view.search.SetText("")
	ui.orders.view.list.Position.First = 0
	ui.orders.view.list.Position.Offset = 0
	ui.selectedTab = ordersTab
	ui.startOrdersLoad(ordersLoadConnectionRefresh)
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
		RestoreBoundary: !ui.orders.view.historyBoundaryKnown && (kind == ordersLoadInitial || kind == ordersLoadConnectionRefresh),
	}
	ui.orders.view.state.Loading = true
	ui.orders.view.state.Status = "Loading locally stored orders…"
	ui.orders.startWorker(func() {
		ui.orders.loadAndMaybeSync(request)
	})
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
	if request.Kind == ordersLoadInitial || request.Kind == ordersLoadNextPage || request.Kind == ordersLoadLocalOnly {
		controller.publishLoadResult(localResult(localStatus(store, controller.ctx, request.ConnectionID), false))
		return
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
	controller.publishLoadResult(localResult(statusWithLastUpdatedAt(status, store, controller.ctx, request.ConnectionID), false))
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
	ui.orders.startWorker(func() {
		ui.orders.lookupAndPersistOrder(requestID, connectionID, displayID, orderID)
	})
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
	ui.orders.view.shipmentSubmitting = false
	ui.orders.view.availabilitySubmitting = false
	ui.orders.view.resetPendingUnavailable()
	ui.orders.view.resetShipmentForm()
	ui.orders.view.detailList.Position.First = 0
	ui.orders.view.detailList.Position.Offset = 0
	ui.orders.view.orderDetailStatus = "Opening locally stored order details…"
	ui.orders.startWorker(func() {
		loadOrderDetail(ui.ctx, store, requestID, connectionID, orderID, ui.orders.publishOrderDetailResult)
	})
}

// refreshOrderDetail asks the user to discard local availability choices before replacing the open order with Faire's latest data.
// A refresh never silently removes unsubmitted availability decisions.
func (ui *DesktopUI) refreshOrderDetail() {
	if ui.orders.view.availabilitySubmitting {
		return
	}
	if ui.orders.view.hasPendingUnavailable() {
		ui.orders.view.availabilityDiscardRefreshOpen = true
		return
	}
	ui.refreshOrderDetailNow()
}

// refreshOrderDetailNow explicitly retrieves the currently open order and atomically replaces its local snapshot without advancing the feed checkpoint.
// Callers must first resolve any local availability draft so the fetched detail cannot discard it without user intent.
func (ui *DesktopUI) refreshOrderDetailNow() {
	if ui.orders.view.orderDetailID == "" || ui.orders.view.orderDetailConnectionID != ui.activeConnectionID || ui.orders.store == nil || ui.manager == nil {
		ui.orders.view.orderDetailStatus = "Order details cannot be refreshed until an active saved connection is available."
		return
	}
	if ui.orders.view.orderDetailLoading || ui.orders.view.state.Loading || ui.orders.view.availabilitySubmitting {
		return
	}
	ui.orders.detailRequestID++
	requestID := ui.orders.detailRequestID
	connectionID, orderID := ui.activeConnectionID, ui.orders.view.orderDetailID
	ui.orders.view.orderDetailLoading = true
	ui.orders.view.orderDetailStatus = "Refreshing order details from Faire…"
	ui.orders.startWorker(func() {
		ui.orders.refreshAndPersistDetail(requestID, connectionID, orderID)
	})
}

// submitShipmentForm validates every visible package and starts one asynchronous Faire shipment submission.
// It rejects unaccepted orders before any request is built, retains entered controls on validation or service failure, and captures the active connection scope before work begins.
func (ui *DesktopUI) submitShipmentForm() {
	if !shipmentCreationAllowed(ui.orders.view.orderDetail) {
		ui.orders.view.orderDetailStatus = "Accept this order before adding shipment information."
		return
	}
	if ui.orders.view.shipmentSubmitting || ui.orders.view.orderDetailID == "" || ui.orders.view.orderDetailConnectionID != ui.activeConnectionID || ui.orders.store == nil || ui.manager == nil {
		return
	}
	request, valid := shipmentRequestFromForm(ui.orders.view.shipmentForm, ui.orders.view.orderDetail.TotalPayoutMinor)
	if !valid {
		return
	}
	ui.orders.shipmentRequestID++
	requestID := ui.orders.shipmentRequestID
	connectionID, orderID := ui.activeConnectionID, ui.orders.view.orderDetailID
	ui.orders.view.shipmentSubmitting = true
	ui.orders.view.orderDetailStatus = "Adding shipment information to Faire…"
	ui.orders.startWorker(func() {
		ui.orders.addShipmentsAndPersistDetail(requestID, connectionID, orderID, request)
	})
}

// submitItemAvailability validates the local unavailable-item draft and starts one asynchronous Faire availability update.
// The request is built on the frame goroutine from immutable presentation values before a worker starts.
func (ui *DesktopUI) submitItemAvailability() {
	view := &ui.orders.view
	if view.availabilitySubmitting || view.orderDetailID == "" || view.orderDetailConnectionID != ui.activeConnectionID || !itemAvailabilityVisible(view.orderDetail) || ui.orders.store == nil || ui.manager == nil {
		return
	}
	request, valid := availabilityRequestFromDraft(view.orderDetail.Items, view.pendingUnavailable)
	if !valid {
		view.orderDetailStatus = "The selected items cannot be reported unavailable. Refresh the order and try again."
		return
	}
	ui.orders.availabilityRequestID++
	requestID := ui.orders.availabilityRequestID
	connectionID, orderID := ui.activeConnectionID, view.orderDetailID
	view.availabilitySubmitting = true
	view.availabilityConfirmOpen = false
	view.orderDetailStatus = "Updating item availability with Faire…"
	ui.orders.startWorker(func() {
		ui.orders.updateItemsAvailabilityAndPersistDetail(requestID, connectionID, orderID, request)
	})
}

// addShipmentsAndPersistDetail submits a validated shipment batch, persists Faire's returned order, and publishes display-safe detail.
// request was built on the frame goroutine, and all errors are converted to safe status text before the result is queued.
func (controller *ordersController) addShipmentsAndPersistDetail(requestID uint64, connectionID string, orderID faire.OrderID, request faire.AddShipmentsRequest) {
	if controller.manager == nil {
		controller.publishShipmentResult(shipmentSubmissionResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: "Shipment information cannot be added until an active saved connection is available."})
		return
	}
	client, _, err := controller.manager.Client(controller.ctx, connectionID, connections.ClientOptions{})
	if err != nil {
		controller.publishShipmentResult(shipmentSubmissionResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: shipmentSubmissionErrorMessage(err)})
		return
	}
	order, err := client.Orders.AddShipments(controller.ctx, orderID, request)
	if err != nil {
		controller.publishShipmentResult(shipmentSubmissionResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: shipmentSubmissionErrorMessage(err)})
		return
	}
	record, err := controller.persistRemoteOrder(connectionID, order)
	if err != nil {
		controller.publishShipmentResult(shipmentSubmissionResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: ordersStorageErrorMessage(err)})
		return
	}
	result := shipmentSubmissionResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Detail: orders.PresentDetail(*order, record.SyncedAtUTC)}
	if count, found := newOrdersCount(controller.ctx, controller.store, connectionID); found {
		result.NewOrdersCount = count
		result.ApplyNewOrdersCount = true
	}
	controller.publishShipmentResult(result)
}

// updateItemsAvailabilityAndPersistDetail submits one variant-keyed availability batch, persists Faire's returned order, and publishes a display-safe result.
// request was captured on the frame goroutine, and all failures preserve only safe retry guidance for the user.
func (controller *ordersController) updateItemsAvailabilityAndPersistDetail(requestID uint64, connectionID string, orderID faire.OrderID, request faire.UpdateOrderItemsAvailabilityRequest) {
	if controller.manager == nil {
		controller.publishItemAvailabilityResult(itemAvailabilityResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: "Item availability cannot be updated until an active saved connection is available."})
		return
	}
	client, _, err := controller.manager.Client(controller.ctx, connectionID, connections.ClientOptions{})
	if err != nil {
		controller.publishItemAvailabilityResult(itemAvailabilityResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: itemAvailabilityErrorMessage(err)})
		return
	}
	order, err := client.Orders.UpdateItemsAvailability(controller.ctx, orderID, request)
	if err != nil {
		controller.publishItemAvailabilityResult(itemAvailabilityResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: itemAvailabilityErrorMessage(err)})
		return
	}
	record, err := controller.persistRemoteOrder(connectionID, order)
	if err != nil {
		controller.publishItemAvailabilityResult(itemAvailabilityResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: ordersStorageErrorMessage(err)})
		return
	}
	result := itemAvailabilityResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Detail: orders.PresentDetail(*order, record.SyncedAtUTC)}
	if count, found := newOrdersCount(controller.ctx, controller.store, connectionID); found {
		result.NewOrdersCount = count
		result.ApplyNewOrdersCount = true
	}
	controller.publishItemAvailabilityResult(result)
}

// startMoveSelectedOrdersToProcessing validates the selected order scope and starts a non-blocking bulk processing request.
// expectedShipDate is a calendar date selected in the modal; orders with matching requested and expected dates omit it so both existing dates remain unchanged.
func (ui *DesktopUI) startMoveSelectedOrdersToProcessing(expectedShipDate string) {
	ui.orders.view.shipDateDialog = shipDateDialogState{}
	if ui.manager == nil || ui.orders.store == nil || ui.activeConnectionID == "" {
		ui.orders.view.state.Status = "Choose an active saved connection with local order storage before editing ship dates."
		return
	}
	if ui.orders.view.processingOrders || ui.orders.view.state.Loading {
		ui.orders.view.state.Status = "Wait for the current Orders operation to finish before editing ship dates."
		return
	}
	selectedIDs := selectedOrderIDs(ui.orders.view.state.SelectedIDs)
	if len(selectedIDs) == 0 {
		ui.orders.view.state.Status = "Select one or more orders before editing ship dates."
		return
	}
	ui.orders.processingRequestID++
	requestID := ui.orders.processingRequestID
	connectionID := ui.activeConnectionID
	ui.orders.view.processingOrders = true
	ui.orders.view.state.Status = "Moving selected orders to processing…"
	ui.orders.startWorker(func() {
		ui.orders.moveSelectedOrdersToProcessing(requestID, connectionID, selectedIDs, expectedShipDate)
	})
}

// moveSelectedOrdersToProcessing reads each selected order's local SQLite snapshot before processing so its matching requested and expected ship dates are never modified.
// It avoids a per-order Faire GET, continues after individual failures, persists successful endpoint responses, and publishes only safe row/status data to the frame loop.
func (controller *ordersController) moveSelectedOrdersToProcessing(requestID uint64, connectionID string, selectedIDs []faire.OrderID, expectedShipDate string) {
	client, _, err := controller.manager.Client(controller.ctx, connectionID, connections.ClientOptions{})
	if err != nil {
		controller.publishOrderProcessingResult(orderProcessingResult{RequestID: requestID, ConnectionID: connectionID, Status: moveToProcessingErrorMessage(err)})
		return
	}
	result := orderProcessingResult{
		RequestID:    requestID,
		ConnectionID: connectionID,
		Rows:         make(map[faire.OrderID]orders.Row, len(selectedIDs)),
	}
	for _, orderID := range selectedIDs {
		if err := controller.ctx.Err(); err != nil {
			result.Status = moveToProcessingErrorMessage(err)
			controller.publishOrderProcessingResult(result)
			return
		}
		order, _, lookupErr := storedOrder(controller.ctx, controller.store, connectionID, orderID)
		request := processingRequestForOrder(&order, expectedShipDate)
		if lookupErr != nil {
			// When the local snapshot cannot be read, omit the optional date rather than risk replacing an unknown requested date.
			request = faire.MoveOrderToProcessingRequest{}
			result.LookupFailures++
		} else if request.ExpectedShipDate == nil {
			result.ExpectedDateOmitted++
		}
		updated, processErr := client.Orders.MoveToProcessing(controller.ctx, orderID, request)
		if processErr != nil {
			result.ProcessingFailures++
			continue
		}
		result.ProcessedIDs = append(result.ProcessedIDs, orderID)
		if _, persistErr := controller.persistRemoteOrder(connectionID, updated); persistErr != nil {
			result.PersistenceFailures++
			continue
		}
		result.Rows[orderID] = orders.PresentRow(*updated)
	}
	result.Status = processingCompletionStatus(result)
	controller.publishOrderProcessingResult(result)
}

// processingRequestForOrder builds the endpoint payload for one locally stored order.
// expectedShipDate is omitted whenever order has a requested ship date so Faire preserves the matching requested and expected dates already on that order.
func processingRequestForOrder(order *faire.Order, expectedShipDate string) faire.MoveOrderToProcessingRequest {
	if order == nil || (order.RequestedShipDate != nil && strings.TrimSpace(*order.RequestedShipDate) != "") || expectedShipDate == "" {
		return faire.MoveOrderToProcessingRequest{}
	}
	return faire.MoveOrderToProcessingRequest{ExpectedShipDate: &expectedShipDate}
}

// processingCompletionStatus summarizes a completed batch without revealing order identifiers or remote response details.
// It reports partial success and local-persistence caveats so users can safely refresh or retry the remaining selection.
func processingCompletionStatus(result orderProcessingResult) string {
	processed := len(result.ProcessedIDs)
	if processed == 0 {
		return "No selected orders could be moved to processing. The selected orders remain available to retry."
	}
	status := "Moved " + itoa(processed) + " selected order"
	if processed != 1 {
		status += "s"
	}
	status += " to processing."
	if result.ExpectedDateOmitted > 0 {
		status += " The existing requested and expected ship dates were left unchanged for " + itoa(result.ExpectedDateOmitted) + " order"
		if result.ExpectedDateOmitted != 1 {
			status += "s"
		}
		status += "."
	}
	if result.LookupFailures > 0 {
		status += " The expected ship date was omitted for " + itoa(result.LookupFailures) + " order"
		if result.LookupFailures != 1 {
			status += "s"
		}
		status += " whose locally stored details could not be read."
	}
	if result.ProcessingFailures > 0 {
		status += " " + itoa(result.ProcessingFailures) + " order"
		if result.ProcessingFailures != 1 {
			status += "s"
		}
		status += " could not be moved and remain selected."
	}
	if result.PersistenceFailures > 0 {
		status += " Local data could not be updated for " + itoa(result.PersistenceFailures) + " processed order"
		if result.PersistenceFailures != 1 {
			status += "s"
		}
		status += "; refresh orders to update the table."
	}
	return status
}

// loadOrderDetail reads and deserializes one private snapshot in a worker, publishing only its typed presentation model.
func loadOrderDetail(ctx context.Context, store ordersstore.Store, requestID uint64, connectionID string, orderID faire.OrderID, publish func(orderDetailResult)) {
	order, snapshot, err := storedOrder(ctx, store, connectionID, orderID)
	if err != nil {
		status := ordersStorageErrorMessage(err)
		if errors.Is(err, ordersstore.ErrCorruptData) {
			status = "Local order data needs to be rebuilt."
		}
		publish(orderDetailResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: status})
		return
	}
	publish(orderDetailResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Detail: orders.PresentDetail(order, snapshot.SyncedAtUTC)})
}

// storedOrder reads and validates one connection-scoped Faire order snapshot from SQLite.
// It returns ErrCorruptData for an unsupported snapshot version, malformed JSON, or mismatched ID so callers never act on ambiguous local order data.
func storedOrder(ctx context.Context, store ordersstore.Store, connectionID string, orderID faire.OrderID) (faire.Order, ordersstore.Snapshot, error) {
	snapshot, err := store.Snapshot(ctx, connectionID, string(orderID))
	if err != nil {
		return faire.Order{}, ordersstore.Snapshot{}, err
	}
	order, valid := storedOrderFromSnapshot(snapshot, orderID)
	if !valid {
		return faire.Order{}, ordersstore.Snapshot{}, ordersstore.ErrCorruptData
	}
	return order, snapshot, nil
}

// storedOrderFromSnapshot decodes and validates one private SQLite snapshot without retaining any transport metadata.
// snapshot supplies the serialized order and schema version, orderID scopes its expected identity, and valid is false for data callers must rebuild rather than trust.
func storedOrderFromSnapshot(snapshot ordersstore.Snapshot, orderID faire.OrderID) (order faire.Order, valid bool) {
	if snapshot.SnapshotSchemaVersion != ordersstore.SnapshotSchemaVersion {
		return faire.Order{}, false
	}
	if err := json.Unmarshal([]byte(snapshot.SnapshotJSON), &order); err != nil {
		return faire.Order{}, false
	}
	return order, order.ID != nil && *order.ID == orderID
}

// drainOrderDetailResults delegates stale-result validation and detail presentation updates to the feature controller.
func (ui *DesktopUI) drainOrderDetailResults() {
	ui.orders.drainDetailResults(ui.activeConnectionID)
}

// drainShipmentSubmissionResults delegates current shipment-submission result validation to the feature controller.
func (ui *DesktopUI) drainShipmentSubmissionResults() {
	ui.orders.drainShipmentResults(ui.activeConnectionID)
}

// drainItemAvailabilityResults delegates current availability-submission result validation to the feature controller.
func (ui *DesktopUI) drainItemAvailabilityResults() {
	ui.orders.drainItemAvailabilityResults(ui.activeConnectionID)
}

// drainOrderProcessingResults applies current selected-order processing outcomes on Gio's frame goroutine.
func (ui *DesktopUI) drainOrderProcessingResults() {
	ui.orders.drainProcessingResults(ui.activeConnectionID)
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

// localRows converts source storage projections, including Faire total payouts, commission percentages,
// optional first-order flat fees, and raw purchase order numbers, to safe table presentation rows outside the frame loop.
// It returns one presentation row per source record.
func localRows(source []ordersstore.LocalRow) []orders.Row {
	rows := make([]orders.Row, len(source))
	for index, sourceRow := range source {
		id := faire.OrderID(sourceRow.OrderID)
		order := faire.Order{ID: &id, DisplayID: optionalPointer(sourceRow.DisplayID), State: optionalOrderState(sourceRow.State), Address: optionalAddress(sourceRow.AddressName), CreatedAt: formatTimestampPointer(sourceRow.CreatedAtUTC), ExpectedShipDate: formatTimestampPointer(sourceRow.ExpectedShipAtUTC), Source: optionalPointer(sourceRow.Source), PurchaseOrderNumber: optionalPointer(sourceRow.PurchaseOrderNumber)}
		row := orders.PresentRow(order)
		row.TotalPayout = orders.FormatTotal(sourceRow.TotalPayoutAmountMinor, sourceRow.TotalPayoutCurrency)
		row.Commission = orders.FormatCommission(sourceRow.CommissionBPS, sourceRow.CommissionFlatFeeAmountMinor, sourceRow.CommissionFlatFeeCurrency)
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

// localStatus returns a safe local freshness status after a successful worker state read.
func localStatus(store ordersstore.Store, ctx context.Context, connectionID string) string {
	state, found, err := store.SyncState(ctx, connectionID)
	if err != nil || !found || state.LastSuccessfulSyncAtUTC == nil {
		if found && state.LastErrorKind == "invalid_request" {
			return "Showing locally stored orders. The last synchronization request was invalid; adjust the history date before refreshing."
		}
		return "Showing locally stored orders."
	}
	return "Showing locally stored orders. Last updated " + formatOrdersUpdatedAt(*state.LastSuccessfulSyncAtUTC) + "."
}

// statusWithLastUpdatedAt appends the persisted successful synchronization time to
// status when it is available, preserving the base status if storage cannot provide it.
func statusWithLastUpdatedAt(status string, store ordersstore.Store, ctx context.Context, connectionID string) string {
	state, found, err := store.SyncState(ctx, connectionID)
	if err != nil || !found || state.LastSuccessfulSyncAtUTC == nil {
		return status
	}
	return status + " Last updated " + formatOrdersUpdatedAt(*state.LastSuccessfulSyncAtUTC) + "."
}

// formatOrdersUpdatedAt renders a successful synchronization timestamp in the user's
// local time with a 12-hour clock and AM/PM marker for the Orders status message.
func formatOrdersUpdatedAt(value time.Time) string {
	return value.Local().Format("Jan 2, 3:04 PM")
}

// ordersStorageErrorMessage converts storage or snapshot failures to credential-safe user feedback.
func ordersStorageErrorMessage(err error) string {
	if errors.Is(err, ordersstore.ErrCorruptData) {
		return "Local order data needs to be rebuilt."
	}
	return "Local order storage could not be read. Rebuild local order data after resolving the issue."
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
	ui.orders.view.packingSlipsOnly = false
	ui.orders.view.exporting = true
	ui.orders.exportRequestID++
	requestID := ui.orders.exportRequestID
	connectionID := ui.activeConnectionID
	ui.orders.view.state.Status = "Exporting orders…"
	ui.orders.startWorker(func() {
		ui.orders.exportOrders(requestID, connectionID, kind, selectedIDs, options)
	})
}

// startSelectedPackingSlipExport validates the selected Orders rows and downloads their PDFs without creating a CSV.
// It captures the selection before starting a worker so frame-loop state remains safe while Faire requests and filesystem writes are in progress.
func (ui *DesktopUI) startSelectedPackingSlipExport() {
	if ui.manager == nil || ui.activeConnectionID == "" {
		ui.orders.view.state.Status = "Choose an active saved connection before printing packing slips."
		return
	}
	if ui.orders.view.state.Loading || ui.orders.view.exporting {
		ui.orders.view.state.Status = "Wait for the current Orders operation to finish before printing packing slips."
		return
	}
	selectedIDs := selectedOrderIDs(ui.orders.view.state.SelectedIDs)
	if len(selectedIDs) == 0 {
		ui.orders.view.state.Status = "Select one or more orders before printing packing slips."
		return
	}
	ui.orders.view.packingSlipsOnly = true
	ui.orders.view.exporting = true
	ui.orders.exportRequestID++
	requestID := ui.orders.exportRequestID
	connectionID := ui.activeConnectionID
	ui.orders.view.state.Status = "Downloading packing slips…"
	ui.orders.startWorker(func() {
		ui.orders.exportSelectedPackingSlips(requestID, connectionID, selectedIDs)
	})
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
		PackingSlipCombined: packingSlipSummary.combined,
		Completed:           true,
	})
}

// exportSelectedPackingSlips saves the selected orders' individual and combined packing-slip PDFs without creating a CSV or fetching order details.
// requestID identifies the worker, connectionID scopes credentials, and selectedIDs capture the immutable Orders selection used directly by Faire's packing-slip endpoint.
func (controller *ordersController) exportSelectedPackingSlips(requestID uint64, connectionID string, selectedIDs []faire.OrderID) {
	client, _, err := controller.manager.Client(controller.ctx, connectionID, connections.ClientOptions{})
	if err != nil {
		controller.publishOrderExportResult(orderExportResult{RequestID: requestID, Status: packingSlipExportErrorMessage(err)})
		return
	}
	folder, summary, err := writePackingSlipExport(controller.ctx, client.Orders, selectedIDs)
	if err != nil {
		status := "Could not save packing slips to Downloads. Check folder permissions and try again."
		if errors.Is(err, context.Canceled) {
			status = packingSlipExportErrorMessage(err)
		}
		controller.publishOrderExportResult(orderExportResult{RequestID: requestID, Status: status})
		return
	}
	controller.publishOrderExportResult(orderExportResult{
		RequestID:           requestID,
		Status:              packingSlipExportCompletionStatus(folder, summary),
		PackingSlipFolder:   folder,
		PackingSlipCount:    summary.downloaded,
		PackingSlipFailures: summary.failures,
		PackingSlipCombined: summary.combined,
		Completed:           true,
	})
}

// packingSlipSummary records safe artifact counts and combined-PDF status for a completed export.
// downloaded counts individually saved PDFs, failures counts skipped or failed downloads, and combined records whether all saved PDFs were merged without retaining private order or transport details.
type packingSlipSummary struct {
	downloaded int
	failures   int
	combined   bool
}

const combinedPackingSlipsFilename = "all-packing-slips.pdf"

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

// packingSlipRequest identifies one direct Faire packing-slip request and its collision-safe private filename.
// orderID is passed to Faire's packing-slip endpoint, while filename is written only under the user-requested export directory.
type packingSlipRequest struct {
	orderID  faire.OrderID
	filename string
}

// downloadPackingSlips retrieves and writes one PDF per export order, then merges all successfully saved PDFs into all-packing-slips.pdf.
// ctx cancels the batch, service downloads PDFs, source supplies order IDs and display-ID filenames, directory receives private files, and the returned summary excludes private order and transport details.
func downloadPackingSlips(ctx context.Context, service *faire.OrdersService, source []faire.Order, directory string) (packingSlipSummary, error) {
	usedNames := make(map[string]struct{}, len(source))
	requests := make([]packingSlipRequest, 0, len(source))
	failures := 0
	for index, order := range source {
		if order.ID == nil || *order.ID == "" {
			failures++
			continue
		}
		requests = append(requests, packingSlipRequest{orderID: *order.ID, filename: packingSlipFilename(order, index, usedNames)})
	}
	summary, err := downloadPackingSlipRequests(ctx, service, requests, directory)
	summary.failures += failures
	return summary, err
}

// downloadPackingSlipsForOrderIDs downloads a selected order ID directly from Faire's packing-slip endpoint without retrieving its order details.
// ctx cancels the batch, service performs one PDF request per ID, selectedIDs provide deterministic request order and locally derived display-ID filenames, directory receives private files, and the returned summary excludes private order details.
func downloadPackingSlipsForOrderIDs(ctx context.Context, service *faire.OrdersService, selectedIDs []faire.OrderID, directory string) (packingSlipSummary, error) {
	usedNames := make(map[string]struct{}, len(selectedIDs))
	requests := make([]packingSlipRequest, 0, len(selectedIDs))
	failures := 0
	for index, orderID := range selectedIDs {
		if orderID == "" {
			failures++
			continue
		}
		displayID := orders.DisplayIDFromOrderID(orderID)
		order := faire.Order{ID: &orderID, DisplayID: &displayID}
		requests = append(requests, packingSlipRequest{orderID: orderID, filename: packingSlipFilename(order, index, usedNames)})
	}
	summary, err := downloadPackingSlipRequests(ctx, service, requests, directory)
	summary.failures += failures
	return summary, err
}

// downloadPackingSlipRequests writes one PDF for every request and merges successful PDFs into all-packing-slips.pdf.
// ctx cancels the batch, service calls Faire's PDF endpoint once per request, requests supply IDs and filenames, directory receives private files, and the returned summary excludes private order and transport details.
func downloadPackingSlipRequests(ctx context.Context, service *faire.OrdersService, requests []packingSlipRequest, directory string) (packingSlipSummary, error) {
	summary := packingSlipSummary{}
	mergedSources := make([]gpdf.Source, 0, len(requests))
	for _, request := range requests {
		if err := ctx.Err(); err != nil {
			return packingSlipSummary{}, err
		}
		// Faire's packing-slip endpoint accepts an order ID directly, so no order-detail request is needed for this batch.
		pdf, err := service.DownloadPackingSlipPDF(ctx, request.orderID)
		if err != nil {
			summary.failures++
			continue
		}
		if err := os.WriteFile(filepath.Join(directory, request.filename), pdf, 0o600); err != nil {
			summary.failures++
			continue
		}
		mergedSources = append(mergedSources, gpdf.Source{Data: pdf})
		summary.downloaded++
	}
	if len(mergedSources) == 0 {
		return summary, nil
	}
	if err := ctx.Err(); err != nil {
		return packingSlipSummary{}, err
	}
	mergedPDF, err := gpdf.Merge(mergedSources)
	if err != nil {
		return summary, nil
	}
	if err := ctx.Err(); err != nil {
		return packingSlipSummary{}, err
	}
	if err := os.WriteFile(filepath.Join(directory, combinedPackingSlipsFilename), mergedPDF, 0o600); err != nil {
		return summary, nil
	}
	summary.combined = true
	return summary, nil
}

// writePackingSlipExport creates a selected-order Downloads folder and saves one PDF per selected ID plus the combined PDF without creating a CSV.
// ctx cancels PDF work, service retrieves the PDFs, selectedIDs are sent directly to Faire's packing-slip endpoint, and it returns the user-facing folder name with a safe summary or an error.
func writePackingSlipExport(ctx context.Context, service *faire.OrdersService, selectedIDs []faire.OrderID) (folder string, summary packingSlipSummary, err error) {
	directory, folder, err := createPackingSlipExportDirectory(orderExportSelected)
	if err != nil {
		return "", packingSlipSummary{}, err
	}
	summary, err = downloadPackingSlipsForOrderIDs(ctx, service, selectedIDs, directory)
	if err != nil {
		return "", packingSlipSummary{}, err
	}
	return folder, summary, nil
}

// createPackingSlipExportDirectory creates one owner-only timestamped folder under Downloads for packing-slip export artifacts.
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

// packingSlipExportCompletionStatus returns a safe completion message for a packing-slip-only export.
// folder identifies the user-visible Downloads subdirectory, summary contains only artifact counts and combined-file status, and the returned message excludes private order details.
func packingSlipExportCompletionStatus(folder string, summary packingSlipSummary) string {
	status := "Saved " + packingSlipCountLabel(summary.downloaded) + " in " + folder + "."
	if summary.combined {
		status += " Also created " + combinedPackingSlipsFilename + "."
	} else if summary.downloaded > 0 {
		status += " The combined packing-slip PDF could not be created."
	}
	if summary.failures > 0 {
		status += " " + packingSlipCountLabel(summary.failures) + " could not be downloaded."
	}
	return status
}

// orderExportCompletionStatus returns a safe completion message for CSV-only, complete packing-slip, and partial packing-slip exports.
// orderCount identifies exported orders, filename and packingSlipFolder identify user-visible artifacts, summary contains only counts and combined-file status, and the returned message excludes private order details.
func orderExportCompletionStatus(orderCount int, filename, packingSlipFolder string, summary packingSlipSummary) string {
	status := "Exported " + itoa(orderCount) + " orders to Downloads as " + filename + "."
	if packingSlipFolder == "" {
		return status
	}
	status += " Saved " + packingSlipCountLabel(summary.downloaded) + " in " + packingSlipFolder + "."
	if summary.combined {
		status += " Also created " + combinedPackingSlipsFilename + "."
	} else if summary.downloaded > 0 {
		status += " The combined packing-slip PDF could not be created."
	}
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

// packingSlipExportErrorMessage converts a packing-slip download failure to credential-safe user feedback.
// err is the underlying client, Faire API, or cancellation failure, and the returned message intentionally omits private API details.
func packingSlipExportErrorMessage(err error) string {
	if errors.Is(err, context.Canceled) {
		return "Packing-slip download was canceled."
	}
	if apiError, ok := errors.AsType[*faire.APIError](err); ok {
		switch apiError.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "Faire rejected this connection's credentials. Update the saved connection or reauthorize it."
		case http.StatusTooManyRequests:
			return "Faire is rate limiting requests. Wait a moment, then print packing slips again."
		default:
			return fmt.Sprintf("Faire could not download packing slips (HTTP %d). Try again later.", apiError.StatusCode)
		}
	}
	return "Packing slips could not be downloaded. Check the saved connection and try again."
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

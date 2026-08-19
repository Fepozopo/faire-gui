package application

import (
	"context"
	"errors"
	"time"

	"github.com/Fepozopo/faire-gui/connections"
	"github.com/Fepozopo/faire-gui/faire"
	"github.com/Fepozopo/faire-gui/features/orders"
	"github.com/Fepozopo/faire-gui/internal/ordersstore"
	"github.com/Fepozopo/faire-gui/internal/orderssync"
)

// ordersLoadKind identifies one user-visible local-read or synchronization operation.
type ordersLoadKind uint8

const (
	// ordersLoadInitial reads local data, restores a retained boundary when needed, and synchronizes when eligible.
	ordersLoadInitial ordersLoadKind = iota
	// ordersLoadNextPage appends one local SQLite page without synchronization.
	ordersLoadNextPage
	// ordersLoadScheduledRefresh reads local data and performs an eligible incremental synchronization.
	ordersLoadScheduledRefresh
	// ordersLoadManualRefresh expands retained history from the selected boundary.
	ordersLoadManualRefresh
	// ordersLoadLocalOnly reloads the current local query without contacting Faire.
	ordersLoadLocalOnly
	// ordersLoadRebuild refreshes an active connection from its explicitly reset history boundary after local data is deleted.
	ordersLoadRebuild
	// ordersLoadInactiveRebuild performs an inactive connection's ordinary bootstrap after local data is deleted.
	ordersLoadInactiveRebuild
)

// ordersLoadRequest is immutable worker input captured on the Gio frame goroutine before work starts.
// State contains only presentation query values, while ConnectionID scopes all storage and credential access.
type ordersLoadRequest struct {
	RequestID       uint64
	ConnectionID    string
	State           orders.State
	Kind            ordersLoadKind
	RestoreBoundary bool
}

// ordersController owns Orders dependencies, asynchronous coordination, request identities, and presentation state.
// Its workers capture immutable requests and publish safe value results; frame-loop callers alone mutate view.
type ordersController struct {
	ctx     context.Context
	store   ordersstore.Store
	manager *connections.Manager
	view    ordersViewState

	loadResults       chan orderLoadResult
	detailResults     chan orderDetailResult
	exportResults     chan orderExportResult
	dataActionResults chan ordersDataActionEvent
	schedule          chan struct{}
	invalidate        func()

	loadRequestID          uint64
	detailRequestID        uint64
	exportRequestID        uint64
	dataStatusRequestID    uint64
	dataActionConnectionID string
	schedulerStarted       bool
}

// newOrdersController constructs one feature-owned Orders component for a DesktopUI lifetime.
// ctx defines worker cancellation, store scopes local persistence, manager constructs authenticated clients by connection ID, and invalidate requests a safe redraw after a result is queued.
func newOrdersController(ctx context.Context, store ordersstore.Store, manager *connections.Manager, invalidate func()) *ordersController {
	return &ordersController{
		ctx:               ctx,
		store:             store,
		manager:           manager,
		view:              newOrdersViewState(),
		loadResults:       make(chan orderLoadResult, 4),
		detailResults:     make(chan orderDetailResult, 2),
		exportResults:     make(chan orderExportResult, 1),
		dataActionResults: make(chan ordersDataActionEvent, 2),
		schedule:          make(chan struct{}, 1),
		invalidate:        invalidate,
	}
}

var (
	// errOrdersManagerUnavailable keeps the worker's absent-manager path distinguishable without exposing credentials.
	errOrdersManagerUnavailable = errors.New("orders manager unavailable")
	// errInvalidHistoryBoundary keeps date validation at the application boundary rather than leaking parse details.
	errInvalidHistoryBoundary = errors.New("invalid history boundary")
)

// ordersDataActionEvent reports safe progress or completion for one connection-scoped local-data action.
// It contains only a connection ID and user-safe status, never cached snapshots, credentials, or transport details.
type ordersDataActionEvent struct {
	ConnectionID string
	Status       string
	Done         bool
}

// publishDataAction sends a safe cross-feature local-data event unless application shutdown has begun.
func (controller *ordersController) publishDataAction(event ordersDataActionEvent) {
	select {
	case controller.dataActionResults <- event:
	case <-controller.ctx.Done():
		return
	}
	if controller.invalidate != nil {
		controller.invalidate()
	}
}

// lookupAndPersistOrder reads a local display-ID match or fetches and stores one remote order without advancing the list checkpoint.
// Every published value is a safe list presentation result, never the fetched private snapshot.
func (controller *ordersController) lookupAndPersistOrder(requestID uint64, connectionID, displayID string, orderID faire.OrderID) {
	local, err := controller.store.FindByDisplayID(controller.ctx, connectionID, displayID)
	if err == nil {
		result := orderLoadResult{RequestID: requestID, Rows: localRows([]ordersstore.LocalRow{local}), Status: "Showing the matching locally stored order.", ApplyRows: true}
		controller.publishLoadResult(attachNewOrdersCount(controller.ctx, controller.store, connectionID, result))
		return
	}
	if !errors.Is(err, ordersstore.ErrNotFound) {
		controller.publishLoadResult(orderLoadResult{RequestID: requestID, Status: ordersStorageErrorMessage(err)})
		return
	}
	if controller.manager == nil {
		controller.publishLoadResult(orderLoadResult{RequestID: requestID, Status: "Order was not found locally and saved connections are unavailable."})
		return
	}
	client, _, err := controller.manager.Client(controller.ctx, connectionID, connections.ClientOptions{})
	if err != nil {
		controller.publishLoadResult(orderLoadResult{RequestID: requestID, Status: ordersLoadErrorMessage(err)})
		return
	}
	order, err := client.Orders.Get(controller.ctx, orderID)
	if err != nil {
		controller.publishLoadResult(orderLoadResult{RequestID: requestID, Status: ordersLoadErrorMessage(err)})
		return
	}
	_, err = controller.persistRemoteOrder(connectionID, order)
	if err != nil {
		controller.publishLoadResult(orderLoadResult{RequestID: requestID, Status: "Order was retrieved but could not be stored locally. Try again later."})
		return
	}
	result := orderLoadResult{RequestID: requestID, Rows: []orders.Row{orders.PresentRow(*order)}, Status: "Showing the matching order.", ApplyRows: true}
	controller.publishLoadResult(attachNewOrdersCount(controller.ctx, controller.store, connectionID, result))
}

// refreshAndPersistDetail fetches and stores one selected order without advancing the list checkpoint.
// It publishes only a typed detail model and a safe New-order count to the frame loop.
func (controller *ordersController) refreshAndPersistDetail(requestID uint64, connectionID string, orderID faire.OrderID) {
	client, _, err := controller.manager.Client(controller.ctx, connectionID, connections.ClientOptions{})
	if err != nil {
		controller.publishOrderDetailResult(orderDetailResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: ordersLoadErrorMessage(err)})
		return
	}
	order, err := client.Orders.Get(controller.ctx, orderID)
	if err != nil {
		controller.publishOrderDetailResult(orderDetailResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: ordersLoadErrorMessage(err)})
		return
	}
	record, err := controller.persistRemoteOrder(connectionID, order)
	if err != nil {
		controller.publishOrderDetailResult(orderDetailResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Status: ordersStorageErrorMessage(err)})
		return
	}
	result := orderDetailResult{RequestID: requestID, ConnectionID: connectionID, OrderID: orderID, Detail: orders.PresentDetail(*order, record.SyncedAtUTC)}
	if count, found := newOrdersCount(controller.ctx, controller.store, connectionID); found {
		result.NewOrdersCount = count
		result.ApplyNewOrdersCount = true
	}
	controller.publishOrderDetailResult(result)
}

// persistRemoteOrder projects and stores one directly fetched order without touching synchronization checkpoints.
// connectionID scopes the private snapshot, order supplies the fetched value, and the returned record provides its local sync timestamp for detail presentation.
func (controller *ordersController) persistRemoteOrder(connectionID string, order *faire.Order) (ordersstore.OrderRecord, error) {
	if order == nil {
		return ordersstore.OrderRecord{}, errors.New("missing remote order")
	}
	record, err := orderssync.RecordFromOrder(connectionID, *order, time.Now().UTC())
	if err != nil {
		return ordersstore.OrderRecord{}, err
	}
	if err := controller.store.UpsertOrders(controller.ctx, []ordersstore.OrderRecord{record}); err != nil {
		return ordersstore.OrderRecord{}, err
	}
	return record, nil
}

// publishOrderDetailResult sends a typed detail result unless application shutdown has begun.
func (controller *ordersController) publishOrderDetailResult(result orderDetailResult) {
	select {
	case controller.detailResults <- result:
	case <-controller.ctx.Done():
		return
	}
	if controller.invalidate != nil {
		controller.invalidate()
	}
}

// startScheduler creates the bounded Orders refresh wake-up source once local storage is available.
// It publishes no UI state directly; the frame loop drains the channel before requesting a named refresh operation.
func (controller *ordersController) startScheduler() {
	if controller.store == nil || controller.schedulerStarted {
		return
	}
	controller.schedulerStarted = true
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-controller.ctx.Done():
				return
			case <-ticker.C:
				select {
				case controller.schedule <- struct{}{}:
					if controller.invalidate != nil {
						controller.invalidate()
					}
				default:
				}
			}
		}
	}()
}

// drainSchedule discards duplicate queued wake-ups and reports whether at least one scheduled refresh is due.
// It runs on the frame goroutine; the shell supplies the active connection before requesting the named refresh.
func (controller *ordersController) drainSchedule() bool {
	due := false
	for {
		select {
		case <-controller.schedule:
			due = true
		default:
			return due
		}
	}
}

// drainLoadResults validates and applies the latest load result on the Gio frame goroutine.
// It returns a matching local-data status for the shell to mirror in Brand Profile without exposing Orders internals.
func (controller *ordersController) drainLoadResults(activeConnectionID string) (string, bool) {
	var dataStatus string
	applyDataStatus := false
	for {
		select {
		case result := <-controller.loadResults:
			if result.RequestID != controller.loadRequestID {
				continue
			}
			if result.RequestID == controller.dataStatusRequestID {
				dataStatus, applyDataStatus = result.Status, true
				if !result.KeepLoading {
					controller.dataStatusRequestID = 0
					if controller.dataActionConnectionID == activeConnectionID {
						controller.dataActionConnectionID = ""
					}
				}
			}
			controller.view.state.Loading = result.KeepLoading
			if result.ApplyNewOrdersCount {
				controller.view.newCount = result.NewOrdersCount
			}
			if !result.ApplyRows {
				controller.view.state.Status = result.Status
				continue
			}
			if result.ApplyBoundary {
				controller.view.state.Query.UpdatedAtMin = result.UpdatedAtMin
				controller.view.updatedAt.SetText(historyBoundaryInput(result.UpdatedAtMin))
				controller.view.historyBoundaryKnown = true
			}
			if controller.view.searchActive {
				controller.view.state.Rows = result.Rows
				controller.view.state.Cursor = ""
			} else if result.Append {
				controller.view.state.Rows = append(controller.view.state.Rows, result.Rows...)
				controller.view.state.Cursor = result.Cursor
			} else {
				controller.view.state.Rows = result.Rows
				controller.view.state.Cursor = result.Cursor
			}
			controller.view.state.Loaded = true
			controller.view.state.Status = result.Status
		default:
			return dataStatus, applyDataStatus
		}
	}
}

// drainExportResults accepts only results for the current export request and updates export presentation state.
// It runs on the frame goroutine and retains only intentional artifact names and safe partial-failure counts.
func (controller *ordersController) drainExportResults() {
	for {
		select {
		case result := <-controller.exportResults:
			if result.RequestID != controller.exportRequestID {
				continue
			}
			controller.view.exporting = false
			controller.view.state.Status = result.Status
			if result.Blocked {
				controller.view.csvExportBlockedOpen = true
			}
			if result.Completed {
				controller.view.csvExportCompletedFile = result.Filename
				controller.view.packingSlipExportFolder = result.PackingSlipFolder
				controller.view.packingSlipExportCount = result.PackingSlipCount
				controller.view.packingSlipExportFailure = result.PackingSlipFailures
				controller.view.csvExportCompletedOpen = true
			}
		default:
			return
		}
	}
}

// drainDetailResults accepts only results for the current detail request, connection, and selected order.
// It runs on the frame goroutine and therefore may update the Orders view state and Gio-facing detail status.
func (controller *ordersController) drainDetailResults(activeConnectionID string) {
	for {
		select {
		case result := <-controller.detailResults:
			if result.RequestID != controller.detailRequestID || result.ConnectionID != activeConnectionID || result.OrderID != controller.view.orderDetailID {
				continue
			}
			controller.view.orderDetailLoading = false
			if result.ApplyNewOrdersCount {
				controller.view.newCount = result.NewOrdersCount
			}
			if result.Status != "" {
				controller.view.orderDetailStatus = result.Status
				continue
			}
			controller.view.orderDetail = result.Detail
			controller.view.orderDetailStatus = "Showing locally stored order details."
		default:
			return
		}
	}
}

// startActiveDataAction deletes the active connection's local cache and optionally starts its named rebuild operation.
// request was captured on the frame goroutine and its results remain subject to normal request-ID validation.
func (controller *ordersController) startActiveDataAction(request ordersLoadRequest, rebuild bool) {
	go func() {
		if err := controller.store.DeleteConnectionData(controller.ctx, request.ConnectionID); err != nil {
			controller.publishLoadResult(orderLoadResult{RequestID: request.RequestID, Status: ordersStorageErrorMessage(err)})
			return
		}
		if !rebuild {
			controller.publishLoadResult(orderLoadResult{RequestID: request.RequestID, Status: "Deleted locally stored order data for this connection.", ApplyNewOrdersCount: true})
			return
		}
		controller.loadAndMaybeSync(request)
	}()
}

// startInactiveDataAction deletes one inactive connection's local cache and optionally rebuilds it in a worker.
// It emits a safe completion event for the shell and deliberately never publishes table rows for that inactive connection.
func (controller *ordersController) startInactiveDataAction(connectionID string, rebuild bool) {
	go func() {
		if err := controller.store.DeleteConnectionData(controller.ctx, connectionID); err != nil {
			controller.publishDataAction(ordersDataActionEvent{ConnectionID: connectionID, Status: ordersStorageErrorMessage(err), Done: true})
			return
		}
		if !rebuild {
			controller.publishDataAction(ordersDataActionEvent{ConnectionID: connectionID, Status: "Deleted locally stored order data for this connection.", Done: true})
			return
		}
		_, err := controller.syncConnection(ordersLoadRequest{ConnectionID: connectionID, Kind: ordersLoadInactiveRebuild})
		if errors.Is(err, errOrdersManagerUnavailable) {
			controller.publishDataAction(ordersDataActionEvent{ConnectionID: connectionID, Status: "Saved connections are unavailable. Local order data was deleted but could not be rebuilt.", Done: true})
			return
		}
		if err != nil {
			controller.publishDataAction(ordersDataActionEvent{ConnectionID: connectionID, Status: ordersLoadErrorMessage(err), Done: true})
			return
		}
		controller.publishDataAction(ordersDataActionEvent{ConnectionID: connectionID, Status: "Local order data was rebuilt for this connection.", Done: true})
	}()
}

// syncConnection constructs the authenticated client and syncer for one immutable request, then runs its selected synchronization.
// It returns only the safe summary or original error so UI code retains existing credential-safe status mapping.
func (controller *ordersController) syncConnection(request ordersLoadRequest) (orderssync.Summary, error) {
	if controller.manager == nil {
		return orderssync.Summary{}, errOrdersManagerUnavailable
	}
	client, _, err := controller.manager.Client(controller.ctx, request.ConnectionID, connections.ClientOptions{})
	if err != nil {
		return orderssync.Summary{}, err
	}
	syncer, err := orderssync.New(controller.store, orderssync.SourceFunc(client.Orders.List), orderssync.Config{})
	if err != nil {
		return orderssync.Summary{}, err
	}
	if request.Kind == ordersLoadManualRefresh || request.Kind == ordersLoadRebuild {
		boundary, err := time.Parse(time.RFC3339Nano, request.State.Query.UpdatedAtMin)
		if err != nil {
			return orderssync.Summary{}, errInvalidHistoryBoundary
		}
		return syncer.SyncFromUpdatedAt(controller.ctx, request.ConnectionID, boundary)
	}
	return syncer.Sync(controller.ctx, request.ConnectionID)
}

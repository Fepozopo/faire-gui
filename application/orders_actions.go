package application

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Fepozopo/faire-gui/connections"
	"github.com/Fepozopo/faire-gui/faire"
	"github.com/Fepozopo/faire-gui/features/orders"
)

// ordersCacheEntry retains only safe presentation rows for one in-session list query.
// It prevents repeated API calls until the user refreshes or changes connection or filters.
type ordersCacheEntry struct {
	Rows   []orders.Row
	Cursor string
}

// orderLoadResult carries one credential-safe background Orders result to the frame loop.
// It never holds a client, credentials, raw API response, address, or order notes.
type orderLoadResult struct {
	RequestID uint64
	CacheKey  string
	Append    bool
	Rows      []orders.Row
	Cursor    string
	Status    string
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
	if apiError, ok := errors.AsType[*faire.APIError](err); ok {
		switch apiError.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "Faire rejected this connection's credentials. Update the saved connection or reauthorize it."
		case http.StatusTooManyRequests:
			return "Faire is rate limiting requests. Wait a moment, then refresh orders."
		default:
			return fmt.Sprintf("Faire could not load orders (HTTP %d). Try refreshing later.", apiError.StatusCode)
		}
	}
	return "Orders could not be loaded. Check the saved connection and try refreshing."
}

// ordersCacheKey identifies an in-memory cached response by connection and supported server filters.
// State selection is included because Faire applies it as excluded states on the server.
func ordersCacheKey(connectionID string, state orders.State) string {
	excluded := state.BuildOptions().ExcludedStates
	values := make([]string, len(excluded))
	for index, excludedState := range excluded {
		values[index] = string(excludedState)
	}
	return strings.Join([]string{connectionID, state.Query.CreatedAtMin, string(state.Query.SortBy), strings.Join(values, ",")}, "|")
}

// setActiveConnection changes the session-only active connection, clears Orders view state, and rejects prior export completions.
// Cached results for other connections remain in memory but are never displayed under this connection.
func (ui *DesktopUI) setActiveConnection(connection connections.Connection) {
	ui.activeConnectionID = connection.ID
	ui.activeConnectionLabel = connection.Label
	ui.connectionPickerOpen = false
	// A completion from the prior connection must not overwrite this connection's Orders status.
	ui.exportRequestID++
	ui.ordersExporting = false
	ui.resetOrdersState()
	ui.ordersSearchActive = false
	ui.orderSearchEditor.SetText("")
	ui.ordersList.Position.First = 0
	ui.ordersList.Position.Offset = 0
	ui.selectedTab = ordersTab
	ui.startOrdersLoad(false, false)
	ui.invalidate()
}

// startOrdersLoad loads the first or next cached/API page without blocking Gio's frame loop.
// A request sequence number ensures late results never overwrite newer filters or connections.
func (ui *DesktopUI) startOrdersLoad(appendResults, refresh bool) {
	if ui.manager == nil {
		ui.ordersState.Status = "Saved connections are unavailable. Open Connections after resolving the credential-store issue."
		return
	}
	if ui.activeConnectionID == "" {
		ui.ordersState.Status = "Choose an active saved connection to load orders."
		return
	}
	if ui.ordersState.Loading {
		return
	}
	cacheKey := ordersCacheKey(ui.activeConnectionID, ui.ordersState)
	if !appendResults && !refresh {
		if cached, found := ui.ordersCache[cacheKey]; found {
			ui.ordersState.Rows = append([]orders.Row(nil), cached.Rows...)
			ui.ordersState.Cursor = cached.Cursor
			ui.ordersState.Loaded = true
			ui.ordersState.Status = "Showing cached orders. Refresh to request current data from Faire."
			return
		}
	}
	if appendResults && ui.ordersState.Cursor == "" {
		return
	}
	ui.ordersRequestID++
	requestID := ui.ordersRequestID
	connectionID := ui.activeConnectionID
	state := ui.ordersState
	state.Loading = true
	if !appendResults {
		state.Cursor = ""
	}
	ui.ordersState.Loading = true
	ui.ordersState.Status = "Loading orders…"
	go ui.loadOrders(requestID, connectionID, cacheKey, state, appendResults)
}

// loadOrders obtains a saved-connection client and one result page in the background.
// Only safe rows and sanitized status text cross the goroutine boundary.
func (ui *DesktopUI) loadOrders(requestID uint64, connectionID, cacheKey string, state orders.State, appendResults bool) {
	client, _, err := ui.manager.Client(ui.ctx, connectionID, connections.ClientOptions{})
	if err != nil {
		ui.publishOrderResult(orderLoadResult{RequestID: requestID, CacheKey: cacheKey, Append: appendResults, Status: ordersLoadErrorMessage(err)})
		return
	}
	options := state.BuildOptions()
	options.Limit = faire.Ptr(int64(50))
	page, err := client.Orders.List(ui.ctx, &options)
	if err != nil {
		ui.publishOrderResult(orderLoadResult{RequestID: requestID, CacheKey: cacheKey, Append: appendResults, Status: ordersLoadErrorMessage(err)})
		return
	}
	cursor := ""
	if page.Cursor != nil {
		cursor = *page.Cursor
	}
	ui.publishOrderResult(orderLoadResult{RequestID: requestID, CacheKey: cacheKey, Append: appendResults, Rows: orders.PresentRows(page.Orders), Cursor: cursor})
}

// loadOrderByDisplayID looks up one normalised visible order number using Faire's internal ID format.
// Direct lookup does not replace the cached list; clearing the search restores the prior list immediately.
func (ui *DesktopUI) loadOrderByDisplayID() {
	if ui.activeConnectionID == "" || ui.manager == nil {
		ui.ordersState.Status = "Choose an active saved connection before searching orders."
		return
	}
	orderID, err := orders.OrderIDFromDisplayID(ui.orderSearchEditor.Text())
	if err != nil {
		ui.ordersState.Status = "Enter a 10-character order number."
		return
	}
	ui.ordersSearchActive = true
	ui.ordersRequestID++
	requestID := ui.ordersRequestID
	connectionID := ui.activeConnectionID
	ui.ordersState.Loading = true
	ui.ordersState.Status = "Looking up order…"
	go func() {
		client, _, clientErr := ui.manager.Client(ui.ctx, connectionID, connections.ClientOptions{})
		if clientErr != nil {
			ui.publishOrderResult(orderLoadResult{RequestID: requestID, Status: ordersLoadErrorMessage(clientErr)})
			return
		}
		order, getErr := client.Orders.Get(ui.ctx, orderID)
		if getErr != nil {
			ui.publishOrderResult(orderLoadResult{RequestID: requestID, Status: ordersLoadErrorMessage(getErr)})
			return
		}
		ui.publishOrderResult(orderLoadResult{RequestID: requestID, Rows: []orders.Row{orders.PresentRow(*order)}, Status: "Showing the matching order."})
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

// drainOrderResults applies only the latest result, protecting current controls from stale API work.
func (ui *DesktopUI) drainOrderResults() {
	for {
		select {
		case result := <-ui.orderResults:
			if result.RequestID != ui.ordersRequestID {
				continue
			}
			ui.ordersState.Loading = false
			if result.Status != "" && len(result.Rows) == 0 {
				ui.ordersState.Status = result.Status
				continue
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
			if !ui.ordersSearchActive {
				ui.ordersCache[result.CacheKey] = ordersCacheEntry{Rows: append([]orders.Row(nil), ui.ordersState.Rows...), Cursor: ui.ordersState.Cursor}
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

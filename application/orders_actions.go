package application

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

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
	return strings.Join([]string{connectionID, state.Query.OrderDateMin, state.Query.ShipDateMax, string(state.Query.SortBy), strings.Join(values, ",")}, "|")
}

// setActiveConnection changes the session-only active connection and clears order-specific view state.
// Cached results for other connections remain in memory but are never displayed under this connection.
func (ui *DesktopUI) setActiveConnection(connection connections.Connection) {
	ui.activeConnectionID = connection.ID
	ui.activeConnectionLabel = connection.Label
	ui.connectionPickerOpen = false
	ui.ordersState = orders.NewState()
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

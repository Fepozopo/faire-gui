package orders

import (
	"slices"
	"time"

	"github.com/Fepozopo/faire-gui/faire"
)

// StatusTab identifies the status grouping currently shown by an orders UI.
type StatusTab string

const (
	// StatusTabAll represents every known order state.
	StatusTabAll StatusTab = "ALL"
	// StatusTabNew represents newly received orders.
	StatusTabNew StatusTab = "NEW"
	// StatusTabProcessing represents orders being prepared.
	StatusTabProcessing StatusTab = "PROCESSING"
	// StatusTabPreTransit represents orders awaiting carrier transit.
	StatusTabPreTransit StatusTab = "PRE_TRANSIT"
	// StatusTabInTransit represents orders in carrier transit.
	StatusTabInTransit StatusTab = "IN_TRANSIT"
	// StatusTabDelivered represents delivered orders.
	StatusTabDelivered StatusTab = "DELIVERED"
	// StatusTabCanceled represents canceled orders.
	StatusTabCanceled StatusTab = "CANCELED"
	// StatusTabBackordered represents orders with backordered items.
	StatusTabBackordered StatusTab = "BACKORDERED"
	// StatusTabPendingRetailerConfirmation represents orders awaiting retailer confirmation.
	StatusTabPendingRetailerConfirmation StatusTab = "PENDING_RETAILER_CONFIRMATION"
)

// ServerQuery holds the Orders update boundary and API sort choice.
// UpdatedAtMin contains an RFC 3339 timestamp produced from the user's local
// month/day/year input by NormalizeDateFilter. The application uses an earlier
// value during manual refresh to expand retained local history.
type ServerQuery struct {
	UpdatedAtMin string
	SortBy       faire.OrderSortBy
}

// State is the Gio-free state for the orders list. Rows, selection, and TableSort
// contain only safe display identifiers and values, allowing callers to cache this
// state without retaining raw API responses or sensitive order details. TableSort
// controls local row ordering independently from the server-side Query.
type State struct {
	StatusTab      StatusTab
	IncludedStates map[faire.OrderState]struct{}
	Rows           []Row
	SelectedIDs    map[faire.OrderID]struct{}
	TableSort      TableSort
	Query          ServerQuery
	Loading        bool
	Status         string
	Cursor         string
	Loaded         bool
	CacheKey       string
}

// NewState returns an orders state that initially includes all supported orders,
// starts at the 90-day updated-order lookback, uses Faire's supported update-time
// ordering, and locally orders rows by newest order date. Its initialized maps allow
// selection and filter updates without special handling by callers.
func NewState() State {
	return NewStateAt(time.Now(), time.Local)
}

// NewStateAt returns a new orders state using now and location for its default
// 90-day updated-order lookback. It initializes local table sorting to newest
// order date first, allowing callers and tests to use the same default UI state.
func NewStateAt(now time.Time, location *time.Location) State {
	_, updatedAtMin := DefaultUpdatedAtMinimum(now, location)
	includedStates := make(map[faire.OrderState]struct{}, len(KnownStates()))
	for _, state := range KnownStates() {
		includedStates[state] = struct{}{}
	}
	return State{
		StatusTab:      StatusTabAll,
		IncludedStates: includedStates,
		SelectedIDs:    make(map[faire.OrderID]struct{}),
		TableSort: TableSort{
			Column:    TableSortColumnOrderDate,
			Direction: TableSortDescending,
		},
		Query: ServerQuery{
			UpdatedAtMin: updatedAtMin,
			SortBy:       faire.OrderSortByUpdatedAt,
		},
	}
}

// KnownStates returns the complete, stable list of Faire order states supported by
// the feature. The order is retained in generated excluded-state queries so tests,
// caches, and requests remain deterministic.
func KnownStates() []faire.OrderState {
	return []faire.OrderState{
		faire.OrderStateNew,
		faire.OrderStateProcessing,
		faire.OrderStatePreTransit,
		faire.OrderStateInTransit,
		faire.OrderStateDelivered,
		faire.OrderStateCanceled,
		faire.OrderStateBackordered,
		faire.OrderStatePendingRetailerConfirmation,
	}
}

// SetIncludedStates replaces the selected states with known values from states.
// Unknown values are ignored so a stale UI value cannot create a misleading filter.
func (s *State) SetIncludedStates(states []faire.OrderState) {
	s.IncludedStates = make(map[faire.OrderState]struct{}, len(states))
	for _, state := range states {
		if isKnownState(state) {
			s.IncludedStates[state] = struct{}{}
		}
	}
}

// isKnownState reports whether state is a state the Faire API supports for orders.
func isKnownState(state faire.OrderState) bool {
	return slices.Contains(KnownStates(), state)
}

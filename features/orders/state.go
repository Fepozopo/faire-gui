package orders

import (
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

// ServerQuery holds the supported server-side order-list filters and sort choice.
// OrderDateMin and ShipDateMax must be API-compatible timestamp strings when they
// are supplied; invalid values are left to the API to reject because the feature
// does not impose a timezone or timestamp format of its own.
type ServerQuery struct {
	OrderDateMin string
	ShipDateMax  string
	SortBy       faire.OrderSortBy
}

// State is the Gio-free state for the orders list. Rows and selection contain only
// safe display identifiers and values, allowing callers to cache this state without
// retaining raw API responses or sensitive order details.
type State struct {
	StatusTab      StatusTab
	IncludedStates map[faire.OrderState]struct{}
	Rows           []Row
	SelectedIDs    map[faire.OrderID]struct{}
	Query          ServerQuery
	Loading        bool
	Status         string
	Cursor         string
	Loaded         bool
	CacheKey       string
}

// NewState returns an orders state that includes every known order state and uses
// the API's supported update-time ordering. Its initialized maps allow selection
// and filter updates without special handling by callers.
func NewState() State {
	return State{
		StatusTab:      StatusTabAll,
		IncludedStates: allIncludedStates(),
		SelectedIDs:    make(map[faire.OrderID]struct{}),
		Query: ServerQuery{
			SortBy: faire.OrderSortByUpdatedAt,
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

// allIncludedStates creates a fresh map because filter state must not be shared
// between independent UI instances.
func allIncludedStates() map[faire.OrderState]struct{} {
	states := KnownStates()
	included := make(map[faire.OrderState]struct{}, len(states))
	for _, state := range states {
		included[state] = struct{}{}
	}
	return included
}

// isKnownState reports whether state is a state the Faire API supports for orders.
func isKnownState(state faire.OrderState) bool {
	for _, knownState := range KnownStates() {
		if state == knownState {
			return true
		}
	}
	return false
}

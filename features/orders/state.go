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

// ServerQuery holds the supported server-side order-list filter and sort choice.
// CreatedAtMin contains an RFC 3339 timestamp produced from the user's local
// month/day/year input by NormalizeDateFilter before the UI sends it to Faire.
type ServerQuery struct {
	CreatedAtMin string
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

// NewState returns an orders state that initially includes only New orders,
// starts at the one-year created-order lookback, and uses Faire's supported
// creation-time ordering. Its initialized maps allow selection and filter updates
// without special handling by callers.
func NewState() State {
	return NewStateAt(time.Now(), time.Local)
}

// NewStateAt returns a new orders state using now and location for its default
// one-year created-order lookback. It exists so callers can initialize the UI and
// tests can deterministically verify the date boundary.
func NewStateAt(now time.Time, location *time.Location) State {
	_, createdAtMin := DefaultCreatedAtMinimum(now, location)
	return State{
		StatusTab: StatusTabAll,
		IncludedStates: map[faire.OrderState]struct{}{
			faire.OrderStateNew: {},
		},
		SelectedIDs: make(map[faire.OrderID]struct{}),
		Query: ServerQuery{
			CreatedAtMin: createdAtMin,
			SortBy:       faire.OrderSortByCreatedAt,
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

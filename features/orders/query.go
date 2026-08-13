package orders

import "github.com/Fepozopo/faire-gui/faire"

// BuildOrderListOptions translates feature state into the subset of Faire's
// order-list controls supported by this feature. It intentionally omits unrelated
// API controls such as page, update timestamps, and original-order IDs.
func BuildOrderListOptions(query ServerQuery, includedStates map[faire.OrderState]struct{}, cursor string) faire.OrderListOptions {
	options := faire.OrderListOptions{
		CreatedAtMin:   optionalString(query.OrderDateMin),
		ShipAfterMax:   optionalString(query.ShipDateMax),
		ExcludedStates: ExcludedStates(includedStates),
		Cursor:         optionalString(cursor),
	}
	if supportedSortBy(query.SortBy) {
		sortBy := query.SortBy
		options.SortBy = &sortBy
	}
	return options
}

// ExcludedStates returns all known states not present in includedStates. Faire's
// list endpoint expresses a status filter as exclusions, so an empty selection
// deliberately excludes every known state rather than silently broadening results.
func ExcludedStates(includedStates map[faire.OrderState]struct{}) []faire.OrderState {
	excluded := make([]faire.OrderState, 0, len(KnownStates()))
	for _, state := range KnownStates() {
		if _, included := includedStates[state]; !included {
			excluded = append(excluded, state)
		}
	}
	return excluded
}

// BuildOptions returns the current state's supported Faire order-list options.
func (s State) BuildOptions() faire.OrderListOptions {
	return BuildOrderListOptions(s.Query, s.IncludedStates, s.Cursor)
}

// optionalString returns nil for an unset server control, preserving the API's
// distinction between an omitted filter and an explicitly supplied value.
func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// supportedSortBy restricts feature requests to sort fields accepted by Faire.
func supportedSortBy(sortBy faire.OrderSortBy) bool {
	switch sortBy {
	case faire.OrderSortByCreatedAt, faire.OrderSortByUpdatedAt:
		return true
	default:
		return false
	}
}

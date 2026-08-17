package orders

import (
	"reflect"
	"testing"
	"time"

	"github.com/Fepozopo/faire-gui/faire"
)

// TestExcludedStates verifies that included UI states become the inverse API filter.
func TestExcludedStates(t *testing.T) {
	included := map[faire.OrderState]struct{}{
		faire.OrderStateNew:       {},
		faire.OrderStateDelivered: {},
	}
	want := []faire.OrderState{
		faire.OrderStateProcessing,
		faire.OrderStatePreTransit,
		faire.OrderStateInTransit,
		faire.OrderStateCanceled,
		faire.OrderStateBackordered,
		faire.OrderStatePendingRetailerConfirmation,
	}
	if got := ExcludedStates(included); !reflect.DeepEqual(got, want) {
		t.Fatalf("ExcludedStates() = %#v, want %#v", got, want)
	}
}

// TestBuildOrderListOptions verifies that only feature-supported list controls are sent.
func TestBuildOrderListOptions(t *testing.T) {
	query := ServerQuery{
		UpdatedAtMin: "2026-01-02T03:04:05Z",
		SortBy:       faire.OrderSortByUpdatedAt,
	}
	options := BuildOrderListOptions(query, map[faire.OrderState]struct{}{faire.OrderStateNew: {}}, "next-page")
	if options.UpdatedAtMin == nil || *options.UpdatedAtMin != query.UpdatedAtMin {
		t.Fatalf("UpdatedAtMin = %#v", options.UpdatedAtMin)
	}
	if options.SortBy == nil || *options.SortBy != faire.OrderSortByUpdatedAt {
		t.Fatalf("SortBy = %#v", options.SortBy)
	}
	if options.Cursor == nil || *options.Cursor != "next-page" {
		t.Fatalf("Cursor = %#v", options.Cursor)
	}
	if options.Limit != nil || options.Page != nil || options.CreatedAtMin != nil || options.ShipAfterMax != nil || options.OriginalOrderID != nil {
		t.Fatalf("unsupported options were set: %#v", options)
	}
}

// TestBuildOrderListOptionsRejectsUnsupportedSort verifies that arbitrary sort values never reach the API.
func TestBuildOrderListOptionsRejectsUnsupportedSort(t *testing.T) {
	options := BuildOrderListOptions(ServerQuery{SortBy: faire.OrderSortBy("TOTAL")}, nil, "")
	if options.SortBy != nil {
		t.Fatalf("SortBy = %q, want nil", *options.SortBy)
	}
	if options.Cursor != nil || options.CreatedAtMin != nil || options.UpdatedAtMin != nil {
		t.Fatalf("unexpected optional controls: %#v", options)
	}
}

// TestNewStateAtUsesUpdateTimeServerSortAndOneYearLookback verifies the initial
// server query uses update-time sorting and the default update boundary.
func TestNewStateAtUsesUpdateTimeServerSortAndOneYearLookback(t *testing.T) {
	location := time.FixedZone("UTC-05", -5*60*60)
	state := NewStateAt(time.Date(2026, time.March, 21, 15, 30, 0, 0, time.UTC), location)
	if state.Query.SortBy != faire.OrderSortByUpdatedAt {
		t.Fatalf("NewStateAt().Query.SortBy = %q, want %q", state.Query.SortBy, faire.OrderSortByUpdatedAt)
	}
	if state.Query.UpdatedAtMin != "2025-03-21T00:00:00-05:00" {
		t.Fatalf("NewStateAt().Query.UpdatedAtMin = %q, want one-year lookback", state.Query.UpdatedAtMin)
	}
}

// TestNewStateIncludesAllOrders verifies the initial Orders screen shows every supported state by default.
func TestNewStateIncludesAllOrders(t *testing.T) {
	state := NewState()
	wantIncluded := make(map[faire.OrderState]struct{}, len(KnownStates()))
	for _, orderState := range KnownStates() {
		wantIncluded[orderState] = struct{}{}
	}
	if !reflect.DeepEqual(state.IncludedStates, wantIncluded) {
		t.Fatalf("NewState().IncludedStates = %#v, want %#v", state.IncludedStates, wantIncluded)
	}
	if got := state.BuildOptions().ExcludedStates; len(got) != 0 {
		t.Fatalf("NewState().BuildOptions().ExcludedStates = %#v, want no exclusions", got)
	}
}

// TestStateSetIncludedStatesDropsUnknownStates verifies a stale tab cannot affect the API query.
func TestStateSetIncludedStatesDropsUnknownStates(t *testing.T) {
	state := NewState()
	state.SetIncludedStates([]faire.OrderState{faire.OrderStateProcessing, faire.OrderState("FUTURE")})
	_, included := state.IncludedStates[faire.OrderStateProcessing]
	if len(state.IncludedStates) != 1 || !included {
		t.Fatalf("IncludedStates = %#v", state.IncludedStates)
	}
}

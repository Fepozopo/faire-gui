package orders

import (
	"reflect"
	"testing"

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
		OrderDateMin: "2026-01-02T03:04:05Z",
		ShipDateMax:  "2026-02-03T04:05:06Z",
		SortBy:       faire.OrderSortByCreatedAt,
	}
	options := BuildOrderListOptions(query, map[faire.OrderState]struct{}{faire.OrderStateNew: {}}, "next-page")
	if options.CreatedAtMin == nil || *options.CreatedAtMin != query.OrderDateMin {
		t.Fatalf("CreatedAtMin = %#v", options.CreatedAtMin)
	}
	if options.ShipAfterMax == nil || *options.ShipAfterMax != query.ShipDateMax {
		t.Fatalf("ShipAfterMax = %#v", options.ShipAfterMax)
	}
	if options.SortBy == nil || *options.SortBy != faire.OrderSortByCreatedAt {
		t.Fatalf("SortBy = %#v", options.SortBy)
	}
	if options.Cursor == nil || *options.Cursor != "next-page" {
		t.Fatalf("Cursor = %#v", options.Cursor)
	}
	if options.Limit != nil || options.Page != nil || options.UpdatedAtMin != nil || options.OriginalOrderID != nil {
		t.Fatalf("unsupported options were set: %#v", options)
	}
}

// TestBuildOrderListOptionsRejectsUnsupportedSort verifies that arbitrary sort values never reach the API.
func TestBuildOrderListOptionsRejectsUnsupportedSort(t *testing.T) {
	options := BuildOrderListOptions(ServerQuery{SortBy: faire.OrderSortBy("TOTAL")}, nil, "")
	if options.SortBy != nil {
		t.Fatalf("SortBy = %q, want nil", *options.SortBy)
	}
	if options.Cursor != nil || options.CreatedAtMin != nil || options.ShipAfterMax != nil {
		t.Fatalf("unexpected optional controls: %#v", options)
	}
}

// TestNewStateUsesCreationTimeSort verifies the initial list query consistently uses the selected server sort field.
func TestNewStateUsesCreationTimeSort(t *testing.T) {
	state := NewState()
	if state.Query.SortBy != faire.OrderSortByCreatedAt {
		t.Fatalf("NewState().Query.SortBy = %q, want %q", state.Query.SortBy, faire.OrderSortByCreatedAt)
	}
}

// TestNewStateIncludesNewAndProcessingOrders verifies the initial Orders screen avoids loading terminal states by default.
func TestNewStateIncludesNewAndProcessingOrders(t *testing.T) {
	state := NewState()
	wantIncluded := map[faire.OrderState]struct{}{
		faire.OrderStateNew:        {},
		faire.OrderStateProcessing: {},
	}
	if !reflect.DeepEqual(state.IncludedStates, wantIncluded) {
		t.Fatalf("NewState().IncludedStates = %#v, want %#v", state.IncludedStates, wantIncluded)
	}
	wantExcluded := []faire.OrderState{
		faire.OrderStatePreTransit,
		faire.OrderStateInTransit,
		faire.OrderStateDelivered,
		faire.OrderStateCanceled,
		faire.OrderStateBackordered,
		faire.OrderStatePendingRetailerConfirmation,
	}
	if got := state.BuildOptions().ExcludedStates; !reflect.DeepEqual(got, wantExcluded) {
		t.Fatalf("NewState().BuildOptions().ExcludedStates = %#v, want %#v", got, wantExcluded)
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

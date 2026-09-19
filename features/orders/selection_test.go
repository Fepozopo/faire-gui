package orders

import (
	"testing"

	"github.com/Fepozopo/faire-gui/faire"
)

// TestToggleSelectionAddsAndRemovesAnOrder verifies map-backed selection toggles a valid order ID.
func TestToggleSelectionAddsAndRemovesAnOrder(t *testing.T) {
	state := State{}
	orderID := faire.OrderID("bo_first")
	state.ToggleSelection(orderID)
	if !state.IsSelected(orderID) {
		t.Fatal("selected order is missing")
	}
	state.ToggleSelection(orderID)
	if state.IsSelected(orderID) {
		t.Fatal("selected order remains after a second toggle")
	}
}

// TestSelectVisiblePreservesSelectionOutsideTheCurrentRows verifies pagination does not discard selections from another page.
func TestSelectVisiblePreservesSelectionOutsideTheCurrentRows(t *testing.T) {
	state := State{SelectedIDs: map[faire.OrderID]struct{}{"bo_previous": {}}}
	state.SelectVisible([]Row{{ID: "bo_first"}, {ID: "bo_second"}, {}})
	want := map[faire.OrderID]struct{}{"bo_previous": {}, "bo_first": {}, "bo_second": {}}
	if len(state.SelectedIDs) != len(want) {
		t.Fatalf("SelectedIDs = %#v, want %#v", state.SelectedIDs, want)
	}
	for orderID := range want {
		if !state.IsSelected(orderID) {
			t.Errorf("SelectedIDs is missing %q", orderID)
		}
	}
}

// TestClearSelectionRemovesEverySelectedOrder verifies bulk-action cleanup resets a populated selection.
func TestClearSelectionRemovesEverySelectedOrder(t *testing.T) {
	state := State{SelectedIDs: map[faire.OrderID]struct{}{"bo_first": {}, "bo_second": {}}}
	state.ClearSelection()
	if len(state.SelectedIDs) != 0 {
		t.Fatalf("SelectedIDs = %#v, want empty", state.SelectedIDs)
	}
}

// TestToggleSelectionIgnoresEmptyID verifies invalid row data cannot enable a bulk action.
func TestToggleSelectionIgnoresEmptyID(t *testing.T) {
	state := State{}
	state.ToggleSelection("")
	if len(state.SelectedIDs) != 0 {
		t.Fatalf("SelectedIDs = %#v, want empty", state.SelectedIDs)
	}
}

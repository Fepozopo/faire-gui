package orders

import (
	"testing"

	"github.com/Fepozopo/faire-gui/faire"
)

// TestSelectionOperations verifies map-backed single, visible, and cleared selection behavior.
func TestSelectionOperations(t *testing.T) {
	state := NewState()
	first := faire.OrderID("bo_first")
	second := faire.OrderID("bo_second")
	state.ToggleSelection(first)
	if !state.IsSelected(first) {
		t.Fatal("first order was not selected")
	}
	state.ToggleSelection(first)
	if state.IsSelected(first) {
		t.Fatal("first order remained selected after toggle")
	}
	state.SelectVisible([]Row{{ID: first}, {ID: second}, {}})
	if !state.IsSelected(first) || !state.IsSelected(second) || len(state.SelectedIDs) != 2 {
		t.Fatalf("SelectedIDs = %#v", state.SelectedIDs)
	}
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

package orders

import "testing"

// TestToggleTableSortReversesActiveColumnAndResetsNewColumns verifies that
// repeat selections reverse the active local sort, different columns begin
// descending, and the server query remains unchanged.
func TestToggleTableSortReversesActiveColumnAndResetsNewColumns(t *testing.T) {
	state := NewState()
	initialQuery := state.Query

	state.ToggleTableSort(TableSortColumnOrderDate)
	if state.TableSort != (TableSort{Column: TableSortColumnOrderDate, Direction: TableSortAscending}) {
		t.Fatalf("first order-date toggle = %#v, want ascending", state.TableSort)
	}

	state.ToggleTableSort(TableSortColumnOrderDate)
	if state.TableSort != (TableSort{Column: TableSortColumnOrderDate, Direction: TableSortDescending}) {
		t.Fatalf("second order-date toggle = %#v, want descending", state.TableSort)
	}

	state.ToggleTableSort(TableSortColumnCustomer)
	if state.TableSort != (TableSort{Column: TableSortColumnCustomer, Direction: TableSortDescending}) {
		t.Fatalf("customer sort = %#v, want descending", state.TableSort)
	}

	state.ToggleTableSort(TableSortColumnCustomer)
	if state.TableSort != (TableSort{Column: TableSortColumnCustomer, Direction: TableSortAscending}) {
		t.Fatalf("customer toggle = %#v, want ascending", state.TableSort)
	}

	state.ToggleTableSort(TableSortColumnTotal)
	if state.TableSort != (TableSort{Column: TableSortColumnTotal, Direction: TableSortDescending}) {
		t.Fatalf("total sort = %#v, want descending", state.TableSort)
	}
	if state.Query != initialQuery {
		t.Fatalf("Query changed during local sorting: got %#v, want %#v", state.Query, initialQuery)
	}
}

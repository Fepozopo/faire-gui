package orders

// TableSortColumn identifies a sortable column in the local Orders table.
type TableSortColumn string

const (
	// TableSortColumnOrder identifies the order display-ID column.
	TableSortColumnOrder TableSortColumn = "ORDER"
	// TableSortColumnStatus identifies the order-status column.
	TableSortColumnStatus TableSortColumn = "STATUS"
	// TableSortColumnCustomer identifies the customer-name column.
	TableSortColumnCustomer TableSortColumn = "CUSTOMER"
	// TableSortColumnTotal identifies the order-total column.
	TableSortColumnTotal TableSortColumn = "TOTAL"
	// TableSortColumnOrderDate identifies the order-created-date column.
	TableSortColumnOrderDate TableSortColumn = "ORDER_DATE"
	// TableSortColumnShipDate identifies the expected ship-date column.
	TableSortColumnShipDate TableSortColumn = "SHIP_DATE"
	// TableSortColumnCommission identifies the commission column.
	TableSortColumnCommission TableSortColumn = "COMMISSION"
	// TableSortColumnSource identifies the sales-source column.
	TableSortColumnSource TableSortColumn = "SOURCE"
)

// TableSortDirection identifies the direction used to order local Orders table rows.
type TableSortDirection string

const (
	// TableSortAscending orders values from lowest to highest, or oldest to newest for dates.
	TableSortAscending TableSortDirection = "ASC"
	// TableSortDescending orders values from highest to lowest, or newest to oldest for dates.
	TableSortDescending TableSortDirection = "DESC"
)

// TableSort holds the local ordering configuration for Orders table rows. Column
// identifies the table field to order, and Direction identifies whether its values
// are arranged ascending or descending. It is independent of ServerQuery so UI
// interactions never change the sort sent to Faire.
type TableSort struct {
	Column    TableSortColumn
	Direction TableSortDirection
}

// ToggleTableSort updates the local Orders table order for column. Selecting the
// active column reverses its direction; selecting a different column starts in
// descending order, which is newest-first for date columns.
func (s *State) ToggleTableSort(column TableSortColumn) {
	if s.TableSort.Column == column {
		// Retaining the selected column makes repeated header clicks a direction toggle.
		s.TableSort.Direction = toggledTableSortDirection(s.TableSort.Direction)
		return
	}

	// New columns begin descending so date headers consistently begin newest-first.
	s.TableSort = TableSort{
		Column:    column,
		Direction: TableSortDescending,
	}
}

// toggledTableSortDirection returns the opposite direction for direction. Any
// uninitialized direction becomes descending so a zero-value State has a stable
// first toggle without requiring UI-specific initialization.
func toggledTableSortDirection(direction TableSortDirection) TableSortDirection {
	if direction == TableSortDescending {
		return TableSortAscending
	}
	// Treat an uninitialized value as ascending before toggling for deterministic state.
	return TableSortDescending
}

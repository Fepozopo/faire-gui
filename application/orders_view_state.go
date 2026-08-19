package application

import (
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/Fepozopo/faire-gui/faire"
	"github.com/Fepozopo/faire-gui/features/orders"
)

// ordersViewState owns all Orders-only frame-loop presentation state and Gio controls.
// Its values are initialized once with the DesktopUI and may be read or mutated only on
// Gio's frame goroutine so immediate-mode controls retain their identity between frames.
type ordersViewState struct {
	state                    orders.State
	newCount                 int
	searchActive             bool
	historyBoundaryKnown     bool
	exporting                bool
	orderDetailOpen          bool
	orderDetailLoading       bool
	orderDetail              orders.Detail
	orderDetailStatus        string
	orderDetailID            faire.OrderID
	orderDetailConnectionID  string
	exportDialog             orderExportDialogState
	pendingStates            map[faire.OrderState]struct{}
	statesDialogOpen         bool
	csvExportBlockedOpen     bool
	csvExportCompletedOpen   bool
	csvExportCompletedFile   string
	packingSlipExportFolder  string
	packingSlipExportCount   int
	packingSlipExportFailure int
	dataDialog               ordersDataDialogState

	list       widget.List
	detailList widget.List
	search     widget.Editor
	updatedAt  widget.Editor

	statusTabs                [5]widget.Clickable
	refreshButton             widget.Clickable
	confirmDataAction         widget.Clickable
	cancelDataAction          widget.Clickable
	backToOrdersButton        widget.Clickable
	refreshDetailButton       widget.Clickable
	loadMoreButton            widget.Clickable
	clearSearchButton         widget.Clickable
	stateFilterButton         widget.Clickable
	applyStatesButton         widget.Clickable
	cancelStatesButton        widget.Clickable
	selectAllStatesButton     widget.Clickable
	selectNoStatesButton      widget.Clickable
	selectVisibleButton       widget.Clickable
	orderDateSortButton       widget.Clickable
	shipDateSortButton        widget.Clickable
	exportMenuButton          widget.Clickable
	exportNewButton           widget.Clickable
	exportBackorderedButton   widget.Clickable
	exportSelectedButton      widget.Clickable
	exportBackButton          widget.Clickable
	confirmExportButton       widget.Clickable
	includeCSVHeaderButton    widget.Clickable
	includePackingSlipsButton widget.Clickable
	closeExportMenuButton     widget.Clickable
	closeCSVExportBlocked     widget.Clickable
	closeCSVExportCompleted   widget.Clickable
	searchButton              widget.Clickable
	rowControls               map[faire.OrderID]*widget.Clickable
	detailControls            map[faire.OrderID]*widget.Clickable
	stateControls             map[faire.OrderState]*widget.Clickable
}

// newOrdersViewState constructs the persistent Orders controls used for the application's lifetime.
// It returns a fully initialized view state whose lists and editors are safe to retain across Gio frames.
func newOrdersViewState() ordersViewState {
	view := ordersViewState{
		pendingStates:  make(map[faire.OrderState]struct{}),
		rowControls:    make(map[faire.OrderID]*widget.Clickable),
		detailControls: make(map[faire.OrderID]*widget.Clickable),
		stateControls:  make(map[faire.OrderState]*widget.Clickable),
	}
	view.list.Axis = layout.Vertical
	view.detailList.Axis = layout.Vertical
	view.search.SingleLine = true
	view.updatedAt.SingleLine = true
	return view
}

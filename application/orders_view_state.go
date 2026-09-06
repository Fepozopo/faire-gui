package application

import (
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/Fepozopo/faire-gui/faire"
	"github.com/Fepozopo/faire-gui/features/orders"
)

// shipmentTrackingControlKey uniquely identifies one shipment link within an order detail view.
// Its order ID and stable shipment position ensure each immediate-mode link retains independent click state.
type shipmentTrackingControlKey struct {
	orderID       faire.OrderID
	shipmentIndex int
}

// shipmentFieldValidation is the most recent completed result for one shipment input.
// completed distinguishes a field still being edited from a completed invalid field; valid remains false in both cases so Confirm cannot use an out-of-date result, and message is retained only after a completed validation finds an error.
type shipmentFieldValidation struct {
	completed bool
	valid     bool
	message   string
}

// shipmentFormPackage owns the persistent controls, selected carrier, and completed validation state for one unsubmitted shipment.
// Gio requires the editors and clickables to survive frame boundaries so typed values and pointer gestures retain their identity; packages are stored by pointer to prevent slice growth from copying live widgets.
type shipmentFormPackage struct {
	carrier                            string
	trackingNumber                     widget.Editor
	labelCost                          widget.Editor
	carrierButton                      widget.Clickable
	carrierOptions                     [supportedCarrierCount]widget.Clickable
	removeButton                       widget.Clickable
	trackingFocused                    bool
	labelCostFocused                   bool
	trackingValidation                 shipmentFieldValidation
	labelCostValidation                shipmentFieldValidation
	labelCostValidationPayoutMinor     int64
	labelCostValidationPayoutAvailable bool
}

// newShipmentFormPackage creates a blank package that defaults to UPS and accepts one-line values only.
func newShipmentFormPackage() *shipmentFormPackage {
	shipment := &shipmentFormPackage{carrier: "UPS"}
	shipment.trackingNumber.SingleLine = true
	shipment.labelCost.SingleLine = true
	return shipment
}

// ordersViewState owns all Orders-only frame-loop presentation state and Gio controls.
// Its values are initialized once with the DesktopUI and may be read or mutated only on
// Gio's frame goroutine so immediate-mode controls retain their identity between frames.
type ordersViewState struct {
	state                          orders.State
	newCount                       int
	searchActive                   bool
	historyBoundaryKnown           bool
	tableFullscreen                bool
	exporting                      bool
	orderDetailOpen                bool
	orderDetailLoading             bool
	orderDetail                    orders.Detail
	orderDetailStatus              string
	orderDetailID                  faire.OrderID
	orderDetailConnectionID        string
	shipmentForm                   []*shipmentFormPackage
	carrierMenuPackage             int
	shipmentSubmitting             bool
	pendingUnavailable             map[faire.VariantID]struct{}
	availabilitySubmitting         bool
	availabilityConfirmOpen        bool
	availabilityDiscardRefreshOpen bool
	exportDialog                   orderExportDialogState
	shipDateDialog                 shipDateDialogState
	processingOrders               bool
	pendingStates                  map[faire.OrderState]struct{}
	statesDialogOpen               bool
	csvExportBlockedOpen           bool
	csvExportCompletedOpen         bool
	csvExportCompletedFile         string
	packingSlipExportFolder        string
	packingSlipExportCount         int
	packingSlipExportFailure       int
	packingSlipExportCombined      bool
	packingSlipsOnly               bool
	dataDialog                     ordersDataDialogState

	list       widget.List
	detailList widget.List
	search     widget.Editor
	updatedAt  widget.Editor

	statusTabs                  [5]widget.Clickable
	refreshButton               widget.Clickable
	confirmDataAction           widget.Clickable
	cancelDataAction            widget.Clickable
	backToOrdersButton          widget.Clickable
	refreshDetailButton         widget.Clickable
	addPackageButton            widget.Clickable
	confirmShipmentsButton      widget.Clickable
	clearAvailabilityButton     widget.Clickable
	updateAvailabilityButton    widget.Clickable
	confirmAvailabilityButton   widget.Clickable
	cancelAvailabilityButton    widget.Clickable
	confirmDiscardRefreshButton widget.Clickable
	cancelDiscardRefreshButton  widget.Clickable
	printPackingSlipsButton     widget.Clickable
	editShipDateButton          widget.Clickable
	previousShipDateMonth       widget.Clickable
	nextShipDateMonth           widget.Clickable
	confirmShipDateButton       widget.Clickable
	cancelShipDateButton        widget.Clickable
	shipDateDayButtons          [42]widget.Clickable
	loadMoreButton              widget.Clickable
	clearSearchButton           widget.Clickable
	stateFilterButton           widget.Clickable
	tableFullscreenButton       widget.Clickable
	applyStatesButton           widget.Clickable
	cancelStatesButton          widget.Clickable
	selectAllStatesButton       widget.Clickable
	selectNoStatesButton        widget.Clickable
	selectVisibleButton         widget.Clickable
	orderDateSortButton         widget.Clickable
	shipDateSortButton          widget.Clickable
	exportMenuButton            widget.Clickable
	exportNewButton             widget.Clickable
	exportBackorderedButton     widget.Clickable
	exportSelectedButton        widget.Clickable
	exportBackButton            widget.Clickable
	confirmExportButton         widget.Clickable
	includeCSVHeaderButton      widget.Clickable
	includePackingSlipsButton   widget.Clickable
	closeExportMenuButton       widget.Clickable
	closeCSVExportBlocked       widget.Clickable
	closeCSVExportCompleted     widget.Clickable
	searchButton                widget.Clickable
	rowControls                 map[faire.OrderID]*widget.Clickable
	detailControls              map[faire.OrderID]*widget.Clickable
	trackingControls            map[shipmentTrackingControlKey]*widget.Clickable
	availabilityControls        map[faire.VariantID]*widget.Clickable
	stateControls               map[faire.OrderState]*widget.Clickable
}

// newOrdersViewState constructs the persistent Orders controls used for the application's lifetime.
// It returns a fully initialized view state whose lists and editors are safe to retain across Gio frames.
func newOrdersViewState() ordersViewState {
	view := ordersViewState{
		pendingStates:        make(map[faire.OrderState]struct{}),
		pendingUnavailable:   make(map[faire.VariantID]struct{}),
		rowControls:          make(map[faire.OrderID]*widget.Clickable),
		detailControls:       make(map[faire.OrderID]*widget.Clickable),
		trackingControls:     make(map[shipmentTrackingControlKey]*widget.Clickable),
		availabilityControls: make(map[faire.VariantID]*widget.Clickable),
		stateControls:        make(map[faire.OrderState]*widget.Clickable),
		carrierMenuPackage:   -1,
	}
	view.list.Axis = layout.Vertical
	view.detailList.Axis = layout.Vertical
	view.search.SingleLine = true
	view.updatedAt.SingleLine = true
	return view
}

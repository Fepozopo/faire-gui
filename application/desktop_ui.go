package application

import (
	"context"
	"image/color"
	"time"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Fepozopo/faire-gui/connections"
	"github.com/Fepozopo/faire-gui/features/orders"
	"github.com/Fepozopo/faire-gui/internal/ordersstore"
)

const (
	// brandsTab displays non-secret profile verification for the active connection.
	brandsTab = iota
	// connectionsTab displays saved connection management.
	connectionsTab
	// ordersTab displays the read-only Orders workflow.
	ordersTab
)

// DesktopUI owns stable Gio widget state and the non-secret state needed to render the desktop application.
// Its methods run on Gio's frame goroutine, while startup, profile, order, and update work publish only safe results through channels.
type DesktopUI struct {
	ctx    context.Context
	cancel context.CancelFunc
	window *app.Window
	theme  *material.Theme

	manager                   *connections.Manager
	connections               []connections.Connection
	preparingStartup          bool
	startupPreparationStarted bool

	activeConnectionID    string
	activeConnectionLabel string
	selectedTab           int
	settingsMenuOpen      bool
	connectionPickerOpen  bool

	// orders is the feature-owned Orders component. The shell supplies only immutable connection scope and handles cross-feature status.
	orders     *ordersController
	editorMode connectionEditorMode
	editing    connections.Connection

	status           string
	managementStatus string

	labelEditor       widget.Editor
	brandIDEditor     widget.Editor
	environmentEditor widget.Editor
	accessTokenEditor widget.Editor

	brandsList             widget.List
	connectionsList        widget.List
	connectionPickerList   widget.List
	tabButtons             [3]widget.Clickable
	activeConnectionButton widget.Clickable
	closeConnectionPicker  widget.Clickable
	addConnectionButton    widget.Clickable
	saveButton             widget.Clickable
	importButton           widget.Clickable
	cancelButton           widget.Clickable
	confirmDelete          widget.Clickable
	cancelDelete           widget.Clickable
	settingsButton         widget.Clickable
	settingsBrandProfile   widget.Clickable
	settingsConnections    widget.Clickable
	checkForUpdates        widget.Clickable
	updateLater            widget.Clickable
	installUpdate          widget.Clickable
	closeUpdateCheckStatus widget.Clickable
	modalBlocker           widget.Clickable

	rowControls              map[string]*connectionRowControls
	connectionPickerControls map[string]*widget.Clickable
	deleteDialog             deleteDialogState
	updateDialog             updateDialogState
	updateCheckDialog        updateCheckDialogState
	results                  chan profileLoadResult
	connectionCleanupResults chan connectionCleanupResult
	updateResults            chan updateCheckResult
	updateInstallResults     chan updateInstallResult
	startupResults           chan startupResult
}

// connectionRowControls owns persistent click state for one saved-connection row.
// Gio requires this state to survive each immediate-mode frame so a pointer gesture keeps its identity.
type connectionRowControls struct {
	selectProfile    widget.Clickable
	rebuildLocalData widget.Clickable
	deleteLocalData  widget.Clickable
	editMetadata     widget.Clickable
	replaceToken     widget.Clickable
	delete           widget.Clickable
}

// deleteDialogState describes the metadata-only saved connection whose deletion awaits confirmation.
type deleteDialogState struct {
	open       bool
	connection connections.Connection
}

// ordersDataDialogState describes an explicitly confirmed local-only Orders cache action for one immutable connection ID.
type ordersDataDialogState struct {
	open         bool
	rebuild      bool
	connectionID string
}

// profileLoadResult transports a credential-safe asynchronous profile-loading result to the UI frame loop.
// It contains user-safe profile status only; Orders cache actions use ordersDataActionEvent instead.
type profileLoadResult struct {
	status string
}

// newDesktopUI constructs a DesktopUI without persistent Orders storage for focused UI tests.
// Production startup uses newDesktopUIWithOrders after successfully opening the process-local store.
func newDesktopUI(ctx context.Context, cancel context.CancelFunc, window *app.Window, manager *connections.Manager, savedConnections []connections.Connection, startupStatus string) *DesktopUI {
	return newDesktopUIWithOrders(ctx, cancel, window, manager, savedConnections, nil, startupStatus)
}

// newDesktopUIWithOrders constructs a DesktopUI from non-secret metadata, an optional persistent Orders store, result channels, and Gio controls.
// The returned UI opens on Orders without selecting a connection and keeps entered token text only in its masked editor until an action immediately clears it.
func newDesktopUIWithOrders(ctx context.Context, cancel context.CancelFunc, window *app.Window, manager *connections.Manager, savedConnections []connections.Connection, store ordersstore.Store, startupStatus string) *DesktopUI {
	ui := &DesktopUI{
		ctx:         ctx,
		cancel:      cancel,
		window:      window,
		theme:       material.NewTheme(),
		manager:     manager,
		connections: savedConnections,
		orders: newOrdersController(ctx, store, manager, func() {
			if window != nil {
				window.Invalidate()
			}
		}),
		status:                   startupStatus,
		managementStatus:         "Create a direct-token connection, or select an existing connection to manage it.",
		selectedTab:              ordersTab,
		rowControls:              make(map[string]*connectionRowControls),
		connectionPickerControls: make(map[string]*widget.Clickable),
		results:                  make(chan profileLoadResult, 1),
		connectionCleanupResults: make(chan connectionCleanupResult, 1),
		updateResults:            make(chan updateCheckResult, 1),
		updateInstallResults:     make(chan updateInstallResult, 1),
		startupResults:           make(chan startupResult, 1),
	}
	ui.configureEditors()
	ui.resetOrdersState()
	ui.brandsList.Axis = layout.Vertical
	ui.connectionsList.Axis = layout.Vertical
	ui.connectionPickerList.Axis = layout.Vertical
	ui.startOrdersScheduler()
	return ui
}

// configureEditors applies persistent field behavior once, rather than recreating editor state every frame.
// The masked token editor is the only UI state that can contain a direct access token.
func (ui *DesktopUI) configureEditors() {
	ui.labelEditor.SingleLine = true
	ui.brandIDEditor.SingleLine = true
	ui.environmentEditor.SingleLine = true
	ui.accessTokenEditor.SingleLine = true
	ui.accessTokenEditor.Mask = '•'
}

// resetOrdersState creates a fresh default order query, clears the connection-scoped New-order count,
// and synchronizes its 30-day updated-order lookback with the visible date editor.
func (ui *DesktopUI) resetOrdersState() {
	ui.orders.view.historyBoundaryKnown = false
	// Counts are connection-scoped, so avoid showing the prior connection's New badge during the next load.
	ui.orders.view.newCount = 0
	now := time.Now()
	updatedAtMinimumInput, _ := orders.DefaultUpdatedAtMinimum(now, time.Local)
	ui.orders.view.state = orders.NewStateAt(now, time.Local)
	ui.orders.view.updatedAt.SetText(updatedAtMinimumInput)
}

// Layout processes current-frame interaction and emits the complete desktop UI, including modal progress and inline Settings navigation.
// gtx supplies the current frame; while startup is pending it instead renders only preparation progress so no database-dependent action can run.
func (ui *DesktopUI) Layout(gtx layout.Context) layout.Dimensions {
	if ui.preparingStartup {
		return layout.Stack{Alignment: layout.NW}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				return fill(gtx, color.NRGBA{R: 250, G: 250, B: 250, A: 255})
			}),
			layout.Expanded(ui.layoutStartup),
		)
	}

	ui.handleTabClicks(gtx)
	return layout.Stack{Alignment: layout.NW}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return fill(gtx, color.NRGBA{R: 250, G: 250, B: 250, A: 255})
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(ui.layoutSidebar),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(28), Right: unit.Dp(32), Bottom: unit.Dp(28), Left: unit.Dp(32)}.Layout(gtx, ui.layoutActivePage)
				}),
			)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			switch {
			case ui.updateDialog.open:
				return ui.layoutUpdateModal(gtx)
			case ui.updateCheckDialog.open:
				return ui.layoutUpdateCheckStatusModal(gtx)
			case ui.deleteDialog.open:
				return ui.layoutDeleteModal(gtx)
			case ui.orders.view.dataDialog.open:
				return ui.layoutOrdersDataModal(gtx)
			case ui.connectionPickerOpen:
				return ui.layoutConnectionPicker(gtx)
			case ui.orders.view.statesDialogOpen:
				return ui.layoutStatesDialog(gtx)
			case ui.orders.view.exportDialog.open:
				return ui.layoutOrderExportMenu(gtx)
			case ui.orders.view.exporting:
				return ui.layoutOrderExportProgressDialog(gtx)
			case ui.orders.view.csvExportBlockedOpen:
				return ui.layoutCSVExportBlockedDialog(gtx)
			case ui.orders.view.csvExportCompletedOpen:
				return ui.layoutCSVExportCompletedDialog(gtx)
			default:
				return layout.Dimensions{}
			}
		}),
	)
}

// layoutStartup renders the non-interactive startup screen while connection metadata and local Orders data are prepared.
// gtx supplies the current Gio frame constraints, and the returned dimensions fill those constraints with a visible progress message.
func (ui *DesktopUI) layoutStartup(gtx layout.Context) layout.Dimensions {
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(material.H3(ui.theme, windowTitle).Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Rigid(bodyText(ui.theme, "Preparing local data…", mutedTextColor)),
		)
	})
}

// handleTabClicks selects a tab from persistent clickable state before laying out the active content.
// Processing clicks before rendering ensures each click affects the same frame that consumes it, unless a modal, including export progress, owns input.
func (ui *DesktopUI) handleTabClicks(gtx layout.Context) {
	if ui.updateDialog.open || ui.updateCheckDialog.open || ui.deleteDialog.open || ui.orders.view.dataDialog.open || ui.connectionPickerOpen || ui.orders.view.statesDialogOpen || ui.orders.view.exportDialog.open || ui.orders.view.exporting || ui.orders.view.csvExportBlockedOpen || ui.orders.view.csvExportCompletedOpen {
		return
	}
	for index := range ui.tabButtons {
		if ui.tabButtons[index].Clicked(gtx) {
			ui.selectedTab = index
			if index == ordersTab && ui.activeConnectionID != "" && !ui.orders.view.state.Loaded {
				ui.startOrdersLoad(ordersLoadInitial)
			}
			ui.invalidate()
		}
	}
}

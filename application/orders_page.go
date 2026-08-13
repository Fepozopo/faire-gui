package application

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Fepozopo/faire-gui/faire"
	"github.com/Fepozopo/faire-gui/features/orders"
)

// layoutOrders renders the read-only Orders workflow for the active saved connection.
// It keeps all interaction state on DesktopUI and delegates query semantics to features/orders.
func (ui *DesktopUI) layoutOrders(gtx layout.Context) layout.Dimensions {
	ui.handleOrdersControls(gtx)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(material.H3(ui.theme, "Orders").Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Min} }),
				layout.Rigid(ui.refreshOrdersControl),
			)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(5)}.Layout),
		layout.Rigid(bodyText(ui.theme, ui.ordersConnectionText(), mutedTextColor)),
		layout.Rigid(layout.Spacer{Height: unit.Dp(24)}.Layout),
		layout.Rigid(ui.layoutOrderTabs),
		layout.Rigid(layout.Spacer{Height: unit.Dp(18)}.Layout),
		layout.Rigid(ui.layoutOrderSearchAndFilters),
		layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
		layout.Rigid(ui.layoutOrderActionBar),
		layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
		layout.Flexed(1, ui.layoutOrdersTable),
	)
}

// ordersConnectionText returns a non-secret prompt or active connection label for the Orders heading.
func (ui *DesktopUI) ordersConnectionText() string {
	if ui.activeConnectionLabel == "" {
		return "Select an active saved connection from the sidebar to load its orders."
	}
	return "Active connection: " + ui.activeConnectionLabel
}

// handleOrdersControls processes controls before rendering so visible rows and filters update in the same frame.
func (ui *DesktopUI) handleOrdersControls(gtx layout.Context) {
	if ui.searchOrdersButton.Clicked(gtx) {
		ui.loadOrderByDisplayID()
		ui.invalidate()
	}
	if ui.clearOrderSearchButton.Clicked(gtx) {
		ui.orderSearchEditor.SetText("")
		ui.ordersSearchActive = false
		ui.ordersState.SelectedIDs = make(map[faire.OrderID]struct{})
		ui.startOrdersLoad(false, false)
		ui.invalidate()
	}
	if ui.applyOrderFiltersButton.Clicked(gtx) {
		ui.ordersState.Query.OrderDateMin = strings.TrimSpace(ui.orderDateEditor.Text())
		ui.ordersState.Query.ShipDateMax = strings.TrimSpace(ui.shipDateEditor.Text())
		ui.ordersState.SelectedIDs = make(map[faire.OrderID]struct{})
		ui.ordersSearchActive = false
		ui.startOrdersLoad(false, false)
		ui.invalidate()
	}
	if ui.refreshOrdersButton.Clicked(gtx) {
		ui.ordersSearchActive = false
		ui.ordersState.SelectedIDs = make(map[faire.OrderID]struct{})
		ui.startOrdersLoad(false, true)
		ui.invalidate()
	}
	if ui.loadMoreOrdersButton.Clicked(gtx) {
		ui.startOrdersLoad(true, false)
		ui.invalidate()
	}
	if ui.orderDateSortButton.Clicked(gtx) {
		if ui.ordersState.Query.SortBy == faire.OrderSortByCreatedAt {
			ui.ordersState.Query.SortBy = faire.OrderSortByUpdatedAt
		} else {
			ui.ordersState.Query.SortBy = faire.OrderSortByCreatedAt
		}
		ui.ordersState.SelectedIDs = make(map[faire.OrderID]struct{})
		ui.ordersSearchActive = false
		ui.startOrdersLoad(false, false)
		ui.invalidate()
	}
	if ui.stateFilterButton.Clicked(gtx) {
		ui.pendingStates = copyIncludedStates(ui.ordersState.IncludedStates)
		ui.statesDialogOpen = true
		ui.invalidate()
	}
	if ui.selectVisibleOrdersButton.Clicked(gtx) {
		if ui.allVisibleOrdersSelected() {
			ui.ordersState.ClearSelection()
		} else {
			ui.ordersState.SelectVisible(ui.ordersState.Rows)
		}
		ui.invalidate()
	}
}

// layoutOrderTabs draws the screenshot-inspired high-level status tabs. Selecting one maps to a supported state filter.
func (ui *DesktopUI) layoutOrderTabs(gtx layout.Context) layout.Dimensions {
	tabs := []struct {
		label string
		state *faire.OrderState
	}{
		{label: "All"},
		{label: "New", state: faire.Ptr(faire.OrderStateNew)},
		{label: "Processing", state: faire.Ptr(faire.OrderStateProcessing)},
		{label: "Fulfilled", state: faire.Ptr(faire.OrderStateDelivered)},
		{label: "Canceled", state: faire.Ptr(faire.OrderStateCanceled)},
	}
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, func() []layout.FlexChild {
		children := make([]layout.FlexChild, 0, len(tabs))
		for index, tab := range tabs {
			index, tab := index, tab
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					button := &ui.orderStatusTabs[index]
					if button.Clicked(gtx) {
						ui.ordersState.SelectedIDs = make(map[faire.OrderID]struct{})
						if tab.state == nil {
							ui.ordersState.SetIncludedStates(orders.KnownStates())
						} else {
							ui.ordersState.SetIncludedStates([]faire.OrderState{*tab.state})
						}
						ui.ordersSearchActive = false
						ui.startOrdersLoad(false, false)
						ui.invalidate()
					}
					selected := (tab.state == nil && len(ui.ordersState.IncludedStates) == len(orders.KnownStates())) || (tab.state != nil && len(ui.ordersState.IncludedStates) == 1 && stateIncluded(ui.ordersState.IncludedStates, *tab.state))
					return orderTabButton(gtx, ui.theme, button, tab.label, selected)
				})
			}))
		}
		return children
	}()...)
}

// layoutOrderSearchAndFilters renders the supported direct lookup and list filters.
func (ui *DesktopUI) layoutOrderSearchAndFilters(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(unit.Dp(260))
			gtx.Constraints.Max.X = gtx.Dp(unit.Dp(260))
			return inputField(gtx, ui.theme, &ui.orderSearchEditor, "Order number, e.g. #ANMQ69YVJB")
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(material.Button(ui.theme, &ui.searchOrdersButton, "Search").Layout),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(material.Button(ui.theme, &ui.clearOrderSearchButton, "Clear").Layout),
		layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.dateFilterField(gtx, &ui.orderDateEditor, "Order date from (RFC 3339)")
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.dateFilterField(gtx, &ui.shipDateEditor, "Ship date through (RFC 3339)")
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(material.Button(ui.theme, &ui.applyOrderFiltersButton, "Apply filters").Layout),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(material.Button(ui.theme, &ui.stateFilterButton, ui.statesButtonLabel()).Layout),
	)
}

// dateFilterField constrains a supported date-filter editor to a compact desktop control width.
func (ui *DesktopUI) dateFilterField(gtx layout.Context, editor *widget.Editor, hint string) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Dp(unit.Dp(170))
	gtx.Constraints.Max.X = gtx.Dp(unit.Dp(170))
	return inputField(gtx, ui.theme, editor, hint)
}

// layoutOrderActionBar renders the table-selection affordance without exposing unimplemented bulk actions.
func (ui *DesktopUI) layoutOrderActionBar(gtx layout.Context) layout.Dimensions {
	selection := "Select visible"
	if ui.allVisibleOrdersSelected() && len(ui.ordersState.Rows) > 0 {
		selection = "Clear selection"
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(bodyText(ui.theme, "Select orders to prepare for future actions", mutedTextColor)),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Min} }),
		layout.Rigid(material.Button(ui.theme, &ui.selectVisibleOrdersButton, selection).Layout),
	)
}

// layoutOrdersTable creates the order header, scrollable rows, and Load more action.
func (ui *DesktopUI) layoutOrdersTable(gtx layout.Context) layout.Dimensions {
	if ui.activeConnectionID == "" || (!ui.ordersState.Loaded && !ui.ordersState.Loading) {
		return emptyOrdersState(gtx, ui.theme, ui.ordersState.Status)
	}
	return roundedPanel(gtx, cardBackground, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(ui.layoutOrdersHeader),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return topRule(gtx, color.NRGBA{R: 221, G: 221, B: 221, A: 255})
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return ui.ordersList.Layout(gtx, len(ui.ordersState.Rows)+1, ui.layoutOrdersListItem)
			}),
		)
	})
}

// layoutOrdersHeader renders fixed table column labels using the same widths as row values.
func (ui *DesktopUI) layoutOrdersHeader(gtx layout.Context) layout.Dimensions {
	if ui.headerSelectVisibleOrdersButton.Clicked(gtx) {
		if ui.allVisibleOrdersSelected() {
			ui.ordersState.ClearSelection()
		} else {
			ui.ordersState.SelectVisible(ui.ordersState.Rows)
		}
		ui.invalidate()
	}
	return layout.Inset{Top: unit.Dp(12), Right: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return ui.layoutOrderColumns(gtx, []string{"", "Order", "Status", "Customer", "Total", "Order date", "Ship date", "Commission", "Source"}, true, ui.allVisibleOrdersSelected())
	})
}

// layoutOrdersListItem renders status/empty feedback followed by selectable order rows and Load more.
func (ui *DesktopUI) layoutOrdersListItem(gtx layout.Context, index int) layout.Dimensions {
	if index == len(ui.ordersState.Rows) {
		return ui.layoutOrdersFooter(gtx)
	}
	row := ui.ordersState.Rows[index]
	control := ui.orderControlFor(row.ID)
	if control.Clicked(gtx) {
		ui.ordersState.ToggleSelection(row.ID)
		ui.invalidate()
	}
	return layout.Inset{Right: unit.Dp(12), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return control.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Background{}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if ui.ordersState.IsSelected(row.ID) {
					// Clip the highlight to this row; an unbounded paint operation would tint following rows too.
					paint.FillShape(gtx.Ops, color.NRGBA{R: 243, G: 243, B: 243, A: 255}, clip.Rect(image.Rectangle{Max: gtx.Constraints.Min}).Op())
				}
				return layout.Dimensions{Size: gtx.Constraints.Min}
			}, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(13), Bottom: unit.Dp(13), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return ui.layoutOrderColumns(gtx, []string{"", row.DisplayID, row.Status, row.Customer, row.Total, row.OrderDate, row.ShipDate, row.Commission, row.Source}, false, ui.ordersState.IsSelected(row.ID))
				})
			})
		})
	})
}

// layoutOrdersFooter displays safe status feedback and appends another API page without clearing selection.
func (ui *DesktopUI) layoutOrdersFooter(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(14), Right: unit.Dp(12), Bottom: unit.Dp(14), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{}
		if ui.ordersState.Status != "" {
			children = append(children, layout.Rigid(bodyText(ui.theme, ui.ordersState.Status, mutedTextColor)))
		}
		if ui.ordersState.Loading {
			children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout), layout.Rigid(bodyText(ui.theme, "Loading…", mutedTextColor)))
		} else if ui.ordersState.Cursor != "" && !ui.ordersSearchActive {
			children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout), layout.Rigid(material.Button(ui.theme, &ui.loadMoreOrdersButton, "Load more").Layout))
		} else if len(ui.ordersState.Rows) == 0 && ui.ordersState.Loaded {
			children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout), layout.Rigid(bodyText(ui.theme, "No orders match these filters.", mutedTextColor)))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// layoutOrderColumns lays out a bounded desktop table row, truncating long values through Gio constraints.
func (ui *DesktopUI) layoutOrderColumns(gtx layout.Context, values []string, header, selected bool) layout.Dimensions {
	// Wide fixed columns preserve readable separation on the desktop-only Orders screen.
	widths := []unit.Dp{44, 150, 140, 210, 120, 125, 125, 145, 130}
	children := make([]layout.FlexChild, 0, len(values))
	for index, value := range values {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(widths[index])
			gtx.Constraints.Max.X = gtx.Dp(widths[index])
			if index == 0 {
				if header {
					return ui.headerSelectVisibleOrdersButton.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return ui.orderCheckbox(gtx, selected)
					})
				}
				return ui.orderCheckbox(gtx, selected)
			}
			if header && index == 5 {
				return ui.orderDateSortButton.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					label := value + " ↕"
					if ui.ordersState.Query.SortBy == faire.OrderSortByCreatedAt {
						label = value + " ↑"
					}
					style := material.Label(ui.theme, unit.Sp(14), label)
					style.Color = color.NRGBA{R: 60, G: 60, B: 60, A: 255}
					return style.Layout(gtx)
				})
			}
			style := material.Body1(ui.theme, value)
			style.MaxLines = 2
			if header {
				style = material.Label(ui.theme, unit.Sp(14), value)
				style.Color = color.NRGBA{R: 60, G: 60, B: 60, A: 255}
			}
			return style.Layout(gtx)
		}))
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
}

// orderCheckbox draws a larger, clipped checkbox indicator for the table header and rows.
// The caller owns click handling so row selection remains available across the complete row surface.
func (ui *DesktopUI) orderCheckbox(gtx layout.Context, selected bool) layout.Dimensions {
	size := gtx.Dp(unit.Dp(20))
	bounds := image.Rect(0, 0, size, size)
	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			paint.FillShape(gtx.Ops, color.NRGBA{R: 95, G: 95, B: 95, A: 255}, clip.Rect(bounds).Op())
			background := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
			if selected {
				background = color.NRGBA{R: 48, G: 48, B: 48, A: 255}
			}
			paint.FillShape(gtx.Ops, background, clip.Rect(image.Rect(2, 2, size-2, size-2)).Op())
			return layout.Dimensions{Size: image.Pt(size, size)}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			if !selected {
				return layout.Dimensions{Size: image.Pt(size, size)}
			}
			style := material.Label(ui.theme, unit.Sp(16), "✓")
			style.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
			return style.Layout(gtx)
		}),
	)
}

// refreshOrdersControl renders a refresh action that is disabled by behavior while an API operation is in flight.
func (ui *DesktopUI) refreshOrdersControl(gtx layout.Context) layout.Dimensions {
	return material.Button(ui.theme, &ui.refreshOrdersButton, "Refresh").Layout(gtx)
}

// orderControlFor returns the persistent clickable for a row ID, avoiding lost click state during list redraws.
func (ui *DesktopUI) orderControlFor(id faire.OrderID) *widget.Clickable {
	if control, found := ui.orderRowControls[id]; found {
		return control
	}
	control := new(widget.Clickable)
	ui.orderRowControls[id] = control
	return control
}

// allVisibleOrdersSelected reports whether every selectable visible row belongs to the current selection.
func (ui *DesktopUI) allVisibleOrdersSelected() bool {
	if len(ui.ordersState.Rows) == 0 {
		return false
	}
	for _, row := range ui.ordersState.Rows {
		if row.ID == "" || !ui.ordersState.IsSelected(row.ID) {
			return false
		}
	}
	return true
}

// statesButtonLabel communicates whether the API state filter narrows results.
func (ui *DesktopUI) statesButtonLabel() string {
	if len(ui.ordersState.IncludedStates) == len(orders.KnownStates()) {
		return "States"
	}
	return "States (" + itoa(len(ui.ordersState.IncludedStates)) + ")"
}

// emptyOrdersState renders an actionable safe message before the first connection/order request.
func emptyOrdersState(gtx layout.Context, theme *material.Theme, message string) layout.Dimensions {
	if message == "" {
		message = "Orders will appear here after a saved connection is selected."
	}
	return roundedPanel(gtx, cardBackground, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(28), Right: unit.Dp(28), Bottom: unit.Dp(28), Left: unit.Dp(28)}.Layout(gtx, bodyText(theme, message, mutedTextColor))
	})
}

// orderTabButton renders a low-profile tab with an underline for the selected server-state filter.
func orderTabButton(gtx layout.Context, theme *material.Theme, button *widget.Clickable, label string, selected bool) layout.Dimensions {
	return button.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{Alignment: layout.S}.Layout(gtx,
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				style := material.Body1(theme, label)
				if selected {
					style.Color = color.NRGBA{R: 30, G: 30, B: 30, A: 255}
				} else {
					style.Color = mutedTextColor
				}
				return layout.Inset{Top: unit.Dp(8), Right: unit.Dp(6), Bottom: unit.Dp(10), Left: unit.Dp(6)}.Layout(gtx, style.Layout)
			}),
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if !selected {
					return layout.Dimensions{Size: gtx.Constraints.Min}
				}
				return bottomRule(gtx, color.NRGBA{R: 50, G: 50, B: 50, A: 255})
			}),
		)
	})
}

// copyIncludedStates makes a modal-editable filter map without changing applied state until confirmation.
func copyIncludedStates(source map[faire.OrderState]struct{}) map[faire.OrderState]struct{} {
	copy := make(map[faire.OrderState]struct{}, len(source))
	for state := range source {
		copy[state] = struct{}{}
	}
	return copy
}

// stateIncluded reports membership without leaking map-access boilerplate into layout decisions.
func stateIncluded(states map[faire.OrderState]struct{}, state faire.OrderState) bool {
	_, included := states[state]
	return included
}

// itoa formats a small count without exposing the implementation to layout call sites.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	characters := make([]byte, 0, 3)
	for value > 0 {
		characters = append([]byte{byte('0' + value%10)}, characters...)
		value /= 10
	}
	return string(characters)
}

// topRule paints a one-pixel top separator while preserving the caller's width.
func topRule(gtx layout.Context, lineColor color.NRGBA) layout.Dimensions {
	height := min(gtx.Dp(unit.Dp(1)), gtx.Constraints.Max.Y)
	paint.FillShape(gtx.Ops, lineColor, clip.Rect(image.Rect(0, 0, gtx.Constraints.Max.X, height)).Op())
	return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, height)}
}

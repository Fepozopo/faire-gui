package application

import (
	"image"
	"image/color"
	"time"

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
		layout.Rigid(ui.layoutOrdersStatus),
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
		layout.Flexed(1, ui.layoutOrdersWorkspace),
	)
}

// layoutOrdersStatus renders credential-safe loading, success, and error feedback above the Orders title.
// A rebuild or delete in progress receives a bordered banner so it remains obvious after navigation from Brand Profile.
func (ui *DesktopUI) layoutOrdersStatus(gtx layout.Context) layout.Dimensions {
	if ui.ordersState.Status == "" {
		return layout.Dimensions{}
	}
	if ui.ordersDataActionConnectionID == ui.activeConnectionID {
		return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return outlinedPanel(gtx, selectionBarColor, panelBorderColor, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(10), Right: unit.Dp(12), Bottom: unit.Dp(10), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(material.Label(ui.theme, unit.Sp(14), "Local order data in progress").Layout),
						layout.Rigid(layout.Spacer{Height: unit.Dp(3)}.Layout),
						layout.Rigid(bodyText(ui.theme, ui.ordersState.Status, mutedTextColor)),
					)
				})
			})
		})
	}
	return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, bodyText(ui.theme, ui.ordersState.Status, mutedTextColor))
}

// layoutOrdersWorkspace groups the tabs, controls, selection bar, and table in one bordered Orders surface.
// Its flexible table region consumes the remaining height so the panel remains framed while rows scroll.
func (ui *DesktopUI) layoutOrdersWorkspace(gtx layout.Context) layout.Dimensions {
	return outlinedPanel(gtx, cardBackground, panelBorderColor, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Right: unit.Dp(20), Left: unit.Dp(20)}.Layout(gtx, ui.layoutOrderTabs)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return topRule(gtx, panelBorderColor) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(14), Right: unit.Dp(20), Bottom: unit.Dp(14), Left: unit.Dp(20)}.Layout(gtx, ui.layoutOrderSearchAndFilters)
			}),
			layout.Rigid(ui.layoutOrderActionBar),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return topRule(gtx, panelBorderColor) }),
			layout.Flexed(1, ui.layoutOrdersTable),
		)
	})
}

// ordersConnectionText returns a non-secret prompt or active connection label for the Orders heading.
func (ui *DesktopUI) ordersConnectionText() string {
	if ui.activeConnectionLabel == "" {
		return "Select an active saved connection from the sidebar to load its orders."
	}
	return "Active connection: " + ui.activeConnectionLabel
}

// handleOrdersControls processes controls before rendering so visible rows update in the same frame.
// Refresh validates the retained-history boundary and invokes the shared synchronization path.
func (ui *DesktopUI) handleOrdersControls(gtx layout.Context) {
	if ui.searchOrdersButton.Clicked(gtx) {
		ui.loadOrderByDisplayID()
		ui.invalidate()
	}
	if ui.clearOrderSearchButton.Clicked(gtx) {
		ui.orderSearchEditor.SetText("")
		ui.ordersSearchActive = false
		ui.ordersState.SelectedIDs = make(map[faire.OrderID]struct{})
		ui.startOrdersLoad(false, false, false)
		ui.invalidate()
	}
	if ui.refreshOrdersButton.Clicked(gtx) {
		updatedAtMin, err := orders.NormalizeDateFilter(ui.updatedAtMinEditor.Text(), false, time.Local)
		if err != nil {
			ui.ordersState.Status = "Enter the updated-at minimum as month/day/year, for example 3/21/2026."
			ui.invalidate()
			return
		}
		// An earlier value expands retained history after a complete all-pages refresh; a later value remains a local view boundary.
		ui.ordersState.Query.UpdatedAtMin = updatedAtMin
		ui.ordersSearchActive = false
		ui.ordersState.SelectedIDs = make(map[faire.OrderID]struct{})
		ui.startOrdersLoad(false, true, true)
		ui.invalidate()
	}
	if ui.loadMoreOrdersButton.Clicked(gtx) {
		ui.startOrdersLoad(true, false, false)
		ui.invalidate()
	}
	if ui.openSelectedOrderButton.Clicked(gtx) {
		ui.openSelectedOrder()
		ui.invalidate()
	}

	if ui.stateFilterButton.Clicked(gtx) {
		ui.pendingStates = copyIncludedStates(ui.ordersState.IncludedStates)
		ui.statesDialogOpen = true
		ui.invalidate()
	}
	if ui.exportMenuButton.Clicked(gtx) {
		ui.exportMenuOpen = true
		ui.invalidate()
	}
}

// layoutOrderTabs draws high-level status presets followed by an advanced state picker.
// The New preset includes the active connection's complete locally stored New-order count; selecting either control updates the supported API state filter.
func (ui *DesktopUI) layoutOrderTabs(gtx layout.Context) layout.Dimensions {
	tabs := []struct {
		label        string
		state        *faire.OrderState
		showNewCount bool
	}{
		{label: "All"},
		{label: "New", state: faire.Ptr(faire.OrderStateNew), showNewCount: true},
		{label: "Processing", state: faire.Ptr(faire.OrderStateProcessing)},
		{label: "Fulfilled", state: faire.Ptr(faire.OrderStateDelivered)},
		{label: "Canceled", state: faire.Ptr(faire.OrderStateCanceled)},
	}
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, func() []layout.FlexChild {
		children := make([]layout.FlexChild, 0, len(tabs)+2)
		for index, tab := range tabs {
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
						ui.startOrdersLoad(false, false, false)
						ui.invalidate()
					}
					selected := (tab.state == nil && len(ui.ordersState.IncludedStates) == len(orders.KnownStates())) || (tab.state != nil && len(ui.ordersState.IncludedStates) == 1 && stateIncluded(ui.ordersState.IncludedStates, *tab.state))
					count := -1
					if tab.showNewCount {
						count = ui.newOrdersCount
					}
					return orderTabButton(gtx, ui.theme, button, tab.label, count, selected)
				})
			}))
		}
		// Keep the advanced picker visually separate so the preset tabs remain easy to scan.
		children = append(children,
			layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &ui.stateFilterButton, ui.statesButtonLabel())),
		)
		return children
	}()...)
}

// layoutOrderSearchAndFilters renders the supported direct lookup and list filters.
func (ui *DesktopUI) layoutOrderSearchAndFilters(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(unit.Dp(260))
			gtx.Constraints.Max.X = gtx.Dp(unit.Dp(260))
			return inputField(gtx, ui.theme, &ui.orderSearchEditor, "Order number")
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(primaryButton(ui.theme, &ui.searchOrdersButton, "Search")),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(primaryButton(ui.theme, &ui.clearOrderSearchButton, "Clear")),
	)
}

// dateFilterField constrains a supported date-filter editor to a compact desktop control width.
func (ui *DesktopUI) dateFilterField(gtx layout.Context, editor *widget.Editor, hint string) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Dp(unit.Dp(170))
	gtx.Constraints.Max.X = gtx.Dp(unit.Dp(170))
	return inputField(gtx, ui.theme, editor, hint)
}

// layoutOrderActionBar renders selection, dedicated detail navigation, and export actions on a muted toolbar.
func (ui *DesktopUI) layoutOrderActionBar(gtx layout.Context) layout.Dimensions {
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return fill(gtx, selectionBarColor)
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10), Right: unit.Dp(20), Bottom: unit.Dp(10), Left: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(bodyText(ui.theme, ui.selectedOrdersLabel(), mutedTextColor)),
					// The wider gap distinguishes the selection context from the action it informs.
					layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
					layout.Rigid(primaryButton(ui.theme, &ui.openSelectedOrderButton, "Open selected")),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(primaryButton(ui.theme, &ui.exportMenuButton, "Export")),
				)
			})
		},
	)
}

// selectedOrdersLabel returns the current bulk-selection count for the Orders action bar.
func (ui *DesktopUI) selectedOrdersLabel() string {
	return itoa(len(ui.ordersState.SelectedIDs)) + " selected"
}

// layoutOrderExportMenu presents the supported full-order CSV export actions in an input-blocking modal.
func (ui *DesktopUI) layoutOrderExportMenu(gtx layout.Context) layout.Dimensions {
	if ui.closeExportMenuButton.Clicked(gtx) {
		ui.exportMenuOpen = false
		ui.invalidate()
	}
	if ui.exportNewOrdersButton.Clicked(gtx) {
		ui.startOrderExport(orderExportNew)
		ui.invalidate()
	}
	if ui.exportBackorderedOrdersButton.Clicked(gtx) {
		ui.startOrderExport(orderExportBackordered)
		ui.invalidate()
	}
	if ui.exportSelectedOrdersButton.Clicked(gtx) {
		ui.startOrderExport(orderExportSelected)
		ui.invalidate()
	}
	return modalPanel(gtx, ui, "Export orders", func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(bodyText(ui.theme, "Exports are saved as CSV files in Downloads.", mutedTextColor)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &ui.exportNewOrdersButton, "Export New Orders")),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &ui.exportBackorderedOrdersButton, "Export Backordered Orders")),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &ui.exportSelectedOrdersButton, "Export Selected Orders")),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &ui.closeExportMenuButton, "Cancel")),
		)
	})
}

// layoutCSVExportBlockedDialog explains why the current connection cannot produce a CSV export.
func (ui *DesktopUI) layoutCSVExportBlockedDialog(gtx layout.Context) layout.Dimensions {
	if ui.closeCSVExportBlockedButton.Clicked(gtx) {
		ui.csvExportBlockedDialogOpen = false
		ui.invalidate()
	}
	return modalPanel(gtx, ui, "CSV export blocked", func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(bodyText(ui.theme, "CSV export is not configured for this connection's Faire brand.", mutedTextColor)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &ui.closeCSVExportBlockedButton, "Close")),
		)
	})
}

// layoutCSVExportCompletedDialog confirms the export location after its CSV file is safely written.
func (ui *DesktopUI) layoutCSVExportCompletedDialog(gtx layout.Context) layout.Dimensions {
	if ui.closeCSVExportCompletedButton.Clicked(gtx) {
		ui.csvExportCompletedDialogOpen = false
		ui.csvExportCompletedFilename = ""
		ui.invalidate()
	}
	return modalPanel(gtx, ui, "CSV export complete", func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(bodyText(ui.theme, "Saved in Downloads as "+ui.csvExportCompletedFilename+".", mutedTextColor)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &ui.closeCSVExportCompletedButton, "Close")),
		)
	})
}

// layoutOrdersDataModal confirms a destructive local-only cache action before any private snapshots are deleted.
func (ui *DesktopUI) layoutOrdersDataModal(gtx layout.Context) layout.Dimensions {
	if ui.cancelOrdersDataAction.Clicked(gtx) {
		ui.ordersDataDialog = ordersDataDialogState{}
		ui.invalidate()
	}
	if ui.confirmOrdersDataAction.Clicked(gtx) {
		ui.startOrdersDataAction(ui.ordersDataDialog.connectionID, ui.ordersDataDialog.rebuild)
		ui.invalidate()
	}
	action := "Delete local order data"
	description := "This removes locally stored order details, including customer and shipping information, for the selected connection only. It never deletes data at Faire."
	if ui.ordersDataDialog.rebuild {
		action = "Delete and rebuild local order data"
		description += " A new 30-day local history download will begin after deletion."
	}
	return modalPanel(gtx, ui, action, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(bodyText(ui.theme, description, mutedTextColor)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(dangerButton(ui.theme, &ui.confirmOrdersDataAction, action)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &ui.cancelOrdersDataAction, "Cancel")),
		)
	})
}

// layoutOrdersTable creates the framed surface's order header, scrollable rows, and Load more action.
// Before the first request, it fills the remaining panel space with the same safe message rather than nesting another card.
func (ui *DesktopUI) layoutOrdersTable(gtx layout.Context) layout.Dimensions {
	if ui.activeConnectionID == "" || (!ui.ordersState.Loaded && !ui.ordersState.Loading) {
		return layout.Inset{Top: unit.Dp(28), Right: unit.Dp(28), Bottom: unit.Dp(28), Left: unit.Dp(28)}.Layout(gtx, bodyText(ui.theme, emptyOrdersMessage(ui.ordersState.Status), mutedTextColor))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(ui.layoutOrdersHeader),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return topRule(gtx, panelBorderColor) }),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return ui.ordersList.Layout(gtx, len(ui.ordersState.Rows)+1, ui.layoutOrdersListItem)
		}),
	)
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
	if ui.orderDateSortButton.Clicked(gtx) {
		ui.ordersState.ToggleTableSort(orders.TableSortColumnOrderDate)
		ui.ordersState.Cursor = ""
		ui.ordersSearchActive = false
		ui.startOrdersLoad(false, false, false)
		ui.invalidate()
	}
	if ui.shipDateSortButton.Clicked(gtx) {
		ui.ordersState.ToggleTableSort(orders.TableSortColumnShipDate)
		ui.ordersState.Cursor = ""
		ui.ordersSearchActive = false
		ui.startOrdersLoad(false, false, false)
		ui.invalidate()
	}
	return layout.Inset{Top: unit.Dp(12), Right: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return ui.layoutOrderColumns(gtx, []string{"", "Order", "Status", "Customer", "Total", "Order date", "Ship date", "Commission", "Source"}, true, ui.allVisibleOrdersSelected())
	})
}

// layoutOrdersListItem renders selectable order rows with a full-width divider after every row.
// The separator stays outside the selected-row fill so adjacent rows retain a clear visual boundary.
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
	return control.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Background{}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					if ui.ordersState.IsSelected(row.ID) {
						// Paint only the measured row area so its highlight never reaches neighboring entries.
						paint.FillShape(gtx.Ops, color.NRGBA{R: 243, G: 243, B: 243, A: 255}, clip.Rect(image.Rectangle{Max: gtx.Constraints.Min}).Op())
					}
					return layout.Dimensions{Size: gtx.Constraints.Min}
				}, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(13), Right: unit.Dp(12), Bottom: unit.Dp(13), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return ui.layoutOrderColumns(gtx, []string{"", row.DisplayID, row.Status, row.Customer, row.Total, row.OrderDate, row.ShipDate, row.Commission, row.Source}, false, ui.ordersState.IsSelected(row.ID))
					})
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return topRule(gtx, panelBorderColor) }),
		)
	})
}

// layoutOrdersFooter appends another local table page or an empty-result message without duplicating the page-level status.
func (ui *DesktopUI) layoutOrdersFooter(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(14), Right: unit.Dp(12), Bottom: unit.Dp(14), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{}
		if !ui.ordersState.Loading && ui.ordersState.Cursor != "" && !ui.ordersSearchActive {
			children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout), layout.Rigid(primaryButton(ui.theme, &ui.loadMoreOrdersButton, "Load more")))
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
					return ui.orderHeaderLabel(gtx, ui.sortHeaderLabel(orders.TableSortColumnOrderDate, value))
				})
			}
			if header && index == 6 {
				return ui.shipDateSortButton.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return ui.orderHeaderLabel(gtx, ui.sortHeaderLabel(orders.TableSortColumnShipDate, value))
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

// orderHeaderLabel renders one interactive Orders date-header label with the shared header typography.
func (ui *DesktopUI) orderHeaderLabel(gtx layout.Context, label string) layout.Dimensions {
	style := material.Label(ui.theme, unit.Sp(14), label)
	style.Color = color.NRGBA{R: 60, G: 60, B: 60, A: 255}
	return style.Layout(gtx)
}

// sortHeaderLabel adds a vertical direction arrow only to the selected local date-sort column.
// An upward arrow places older dates first, while a downward arrow places newer dates first.
func (ui *DesktopUI) sortHeaderLabel(column orders.TableSortColumn, label string) string {
	if ui.ordersState.TableSort.Column != column {
		return label
	}
	if ui.ordersState.TableSort.Direction == orders.TableSortAscending {
		return label + " ↑"
	}
	return label + " ↓"
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

// refreshOrdersControl renders manual synchronization and the retained-history boundary.
// Refresh is disabled by behavior while an incompatible Orders operation is in flight.
func (ui *DesktopUI) refreshOrdersControl(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical, Alignment: layout.End}.Layout(gtx,
		layout.Rigid(primaryButton(ui.theme, &ui.refreshOrdersButton, "Refresh")),
		layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
		layout.Rigid(material.Label(ui.theme, unit.Sp(12), "Updated At Minimum (earlier date adds history)").Layout),
		layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.dateFilterField(gtx, &ui.updatedAtMinEditor, "M/D/YYYY")
		}),
	)
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

// emptyOrdersMessage returns the safe placeholder text displayed before the first connection or order request.
// A non-empty status takes precedence so the caller can surface request feedback in the existing Orders panel.
func emptyOrdersMessage(message string) string {
	if message == "" {
		return "Orders will appear here after a saved connection is selected."
	}
	return message
}

// orderTabButton renders a low-profile tab with an underline for the selected server-state filter.
// A non-negative count renders a compact badge beside label; a negative count omits the badge.
func orderTabButton(gtx layout.Context, theme *material.Theme, button *widget.Clickable, label string, count int, selected bool) layout.Dimensions {
	return button.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{Alignment: layout.S}.Layout(gtx,
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				textColor := mutedTextColor
				if selected {
					textColor = color.NRGBA{R: 30, G: 30, B: 30, A: 255}
				}
				return layout.Inset{Top: unit.Dp(8), Right: unit.Dp(6), Bottom: unit.Dp(10), Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					children := []layout.FlexChild{layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						style := material.Body1(theme, label)
						style.Color = textColor
						return style.Layout(gtx)
					})}
					if count >= 0 {
						children = append(children,
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return orderTabCountBadge(gtx, theme, count) }),
						)
					}
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
				})
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

// orderTabCountBadge renders the muted pill used to display the complete locally stored New-order count.
func orderTabCountBadge(gtx layout.Context, theme *material.Theme, count int) layout.Dimensions {
	return roundedPanel(gtx, selectionBarColor, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(2), Right: unit.Dp(7), Bottom: unit.Dp(2), Left: unit.Dp(7)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			style := material.Label(theme, unit.Sp(12), itoa(count))
			style.Color = color.NRGBA{R: 30, G: 30, B: 30, A: 255}
			return style.Layout(gtx)
		})
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

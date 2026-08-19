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
	if ui.orders.view.state.Status == "" {
		return layout.Dimensions{}
	}
	if ui.orders.dataActionConnectionID == ui.activeConnectionID {
		return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return outlinedPanel(gtx, selectionBarColor, panelBorderColor, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(10), Right: unit.Dp(12), Bottom: unit.Dp(10), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(material.Label(ui.theme, unit.Sp(14), "Local order data in progress").Layout),
						layout.Rigid(layout.Spacer{Height: unit.Dp(3)}.Layout),
						layout.Rigid(bodyText(ui.theme, ui.orders.view.state.Status, mutedTextColor)),
					)
				})
			})
		})
	}
	return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, bodyText(ui.theme, ui.orders.view.state.Status, mutedTextColor))
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
	if ui.orders.view.searchButton.Clicked(gtx) {
		ui.loadOrderByDisplayID()
		ui.invalidate()
	}
	if ui.orders.view.clearSearchButton.Clicked(gtx) {
		ui.orders.view.search.SetText("")
		ui.orders.view.searchActive = false
		ui.orders.view.state.SelectedIDs = make(map[faire.OrderID]struct{})
		ui.startOrdersLoad(ordersLoadLocalOnly)
		ui.invalidate()
	}
	if ui.orders.view.refreshButton.Clicked(gtx) {
		updatedAtMin, err := orders.NormalizeDateFilter(ui.orders.view.updatedAt.Text(), false, time.Local)
		if err != nil {
			ui.orders.view.state.Status = "Enter the updated-at minimum as month/day/year, for example 3/21/2026."
			ui.invalidate()
			return
		}
		// An earlier value expands retained history after a complete all-pages refresh; a later value remains a local view boundary.
		ui.orders.view.state.Query.UpdatedAtMin = updatedAtMin
		ui.orders.view.searchActive = false
		ui.orders.view.state.SelectedIDs = make(map[faire.OrderID]struct{})
		ui.startOrdersLoad(ordersLoadManualRefresh)
		ui.invalidate()
	}
	if ui.orders.view.loadMoreButton.Clicked(gtx) {
		ui.startOrdersLoad(ordersLoadNextPage)
		ui.invalidate()
	}

	if ui.orders.view.stateFilterButton.Clicked(gtx) {
		ui.orders.view.pendingStates = copyIncludedStates(ui.orders.view.state.IncludedStates)
		ui.orders.view.statesDialogOpen = true
		ui.invalidate()
	}
	if ui.orders.view.exportMenuButton.Clicked(gtx) {
		ui.orders.view.exportDialog = orderExportDialogState{open: true, includeHeader: true}
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
					button := &ui.orders.view.statusTabs[index]
					if button.Clicked(gtx) {
						ui.orders.view.state.SelectedIDs = make(map[faire.OrderID]struct{})
						if tab.state == nil {
							ui.orders.view.state.SetIncludedStates(orders.KnownStates())
						} else {
							ui.orders.view.state.SetIncludedStates([]faire.OrderState{*tab.state})
						}
						ui.orders.view.searchActive = false
						ui.startOrdersLoad(ordersLoadLocalOnly)
						ui.invalidate()
					}
					selected := (tab.state == nil && len(ui.orders.view.state.IncludedStates) == len(orders.KnownStates())) || (tab.state != nil && len(ui.orders.view.state.IncludedStates) == 1 && stateIncluded(ui.orders.view.state.IncludedStates, *tab.state))
					count := -1
					if tab.showNewCount {
						count = ui.orders.view.newCount
					}
					return orderTabButton(gtx, ui.theme, button, tab.label, count, selected)
				})
			}))
		}
		// Keep the advanced picker visually separate so the preset tabs remain easy to scan.
		children = append(children,
			layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &ui.orders.view.stateFilterButton, ui.statesButtonLabel())),
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
			return inputField(gtx, ui.theme, &ui.orders.view.search, "Order number")
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(primaryButton(ui.theme, &ui.orders.view.searchButton, "Search")),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(primaryButton(ui.theme, &ui.orders.view.clearSearchButton, "Clear")),
	)
}

// dateFilterField constrains a supported date-filter editor to a compact desktop control width.
func (ui *DesktopUI) dateFilterField(gtx layout.Context, editor *widget.Editor, hint string) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Dp(unit.Dp(170))
	gtx.Constraints.Max.X = gtx.Dp(unit.Dp(170))
	return inputField(gtx, ui.theme, editor, hint)
}

// layoutOrderActionBar renders selection context and export actions on a muted toolbar.
// Order details are opened directly from each row's order-number control.
func (ui *DesktopUI) layoutOrderActionBar(gtx layout.Context) layout.Dimensions {
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return fill(gtx, selectionBarColor)
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10), Right: unit.Dp(20), Bottom: unit.Dp(10), Left: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(bodyText(ui.theme, ui.selectedOrdersLabel(), mutedTextColor)),
					// The wider gap separates selection feedback from its related bulk action.
					layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
					layout.Rigid(primaryButton(ui.theme, &ui.orders.view.exportMenuButton, "Export")),
				)
			})
		},
	)
}

// selectedOrdersLabel returns the current bulk-selection count for the Orders action bar.
func (ui *DesktopUI) selectedOrdersLabel() string {
	return itoa(len(ui.orders.view.state.SelectedIDs)) + " selected"
}

// layoutOrderExportMenu presents export scope first, then per-export CSV and packing-slip choices in the same input-blocking modal.
// gtx supplies the current frame, and the returned dimensions render the active dialog step while its choices remain UI-owned until export begins.
func (ui *DesktopUI) layoutOrderExportMenu(gtx layout.Context) layout.Dimensions {
	if ui.orders.view.closeExportMenuButton.Clicked(gtx) {
		ui.orders.view.exportDialog = orderExportDialogState{}
		ui.invalidate()
	}
	if !ui.orders.view.exportDialog.configuring {
		ui.handleOrderExportScopeControls(gtx)
		return modalPanel(gtx, ui, "Export orders", func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(bodyText(ui.theme, "Choose which orders to export.", mutedTextColor)),
				layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
				layout.Rigid(primaryButton(ui.theme, &ui.orders.view.exportNewButton, "New orders")),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
				layout.Rigid(primaryButton(ui.theme, &ui.orders.view.exportBackorderedButton, "Backordered orders")),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
				layout.Rigid(primaryButton(ui.theme, &ui.orders.view.exportSelectedButton, "Selected orders")),
				layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
				layout.Rigid(primaryButton(ui.theme, &ui.orders.view.closeExportMenuButton, "Cancel")),
			)
		})
	}

	if ui.orders.view.exportBackButton.Clicked(gtx) {
		ui.orders.view.exportDialog.configuring = false
		ui.invalidate()
	}
	if ui.orders.view.includeCSVHeaderButton.Clicked(gtx) {
		ui.orders.view.exportDialog.includeHeader = !ui.orders.view.exportDialog.includeHeader
		ui.invalidate()
	}
	if ui.orders.view.includePackingSlipsButton.Clicked(gtx) {
		ui.orders.view.exportDialog.includePackingSlips = !ui.orders.view.exportDialog.includePackingSlips
		ui.invalidate()
	}
	if ui.orders.view.confirmExportButton.Clicked(gtx) {
		ui.startOrderExport(ui.orders.view.exportDialog.kind, orderExportOptions{
			IncludeHeader:       ui.orders.view.exportDialog.includeHeader,
			IncludePackingSlips: ui.orders.view.exportDialog.includePackingSlips,
		})
		ui.invalidate()
	}
	return modalPanel(gtx, ui, "Configure export", func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(bodyText(ui.theme, "Export "+orderExportKindLabel(ui.orders.view.exportDialog.kind)+".", mutedTextColor)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutOrderExportOption(gtx, &ui.orders.view.includeCSVHeaderButton, ui.orders.view.exportDialog.includeHeader, "Include CSV header", "Adds the column names as the first CSV row.")
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutOrderExportOption(gtx, &ui.orders.view.includePackingSlipsButton, ui.orders.view.exportDialog.includePackingSlips, "Download packing slips", "Saves one PDF per order alongside the CSV in a new Downloads folder. This may take longer.")
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(18)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(primaryButton(ui.theme, &ui.orders.view.exportBackButton, "Back")),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(primaryButton(ui.theme, &ui.orders.view.confirmExportButton, ui.orderExportActionLabel())),
				)
			}),
		)
	})
}

// handleOrderExportScopeControls advances the export dialog from scope selection to configuration without starting work yet.
// gtx supplies the current frame; it has no return value because it updates only UI-owned dialog state.
func (ui *DesktopUI) handleOrderExportScopeControls(gtx layout.Context) {
	for _, choice := range []struct {
		button *widget.Clickable
		kind   orderExportKind
	}{
		{button: &ui.orders.view.exportNewButton, kind: orderExportNew},
		{button: &ui.orders.view.exportBackorderedButton, kind: orderExportBackordered},
		{button: &ui.orders.view.exportSelectedButton, kind: orderExportSelected},
	} {
		if choice.button.Clicked(gtx) {
			ui.orders.view.exportDialog.kind = choice.kind
			ui.orders.view.exportDialog.configuring = true
			ui.invalidate()
			return
		}
	}
}

// layoutOrderExportOption renders one selectable export option with a checkbox-style indicator and explanatory copy.
// gtx supplies the current frame, button owns the option state, selected controls the indicator, label and description are visible copy, and the returned dimensions render the option row.
func (ui *DesktopUI) layoutOrderExportOption(gtx layout.Context, button *widget.Clickable, selected bool, label, description string) layout.Dimensions {
	return clickableWithPointer(gtx, button, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.orderCheckbox(gtx, selected) }),
			layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(material.Body1(ui.theme, label).Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(2)}.Layout),
					layout.Rigid(bodyText(ui.theme, description, mutedTextColor)),
				)
			}),
		)
	})
}

// orderExportKindLabel returns the human-readable scope name used by the export configuration dialog.
// kind identifies the selected export scope, and the returned label is safe for UI presentation.
func orderExportKindLabel(kind orderExportKind) string {
	switch kind {
	case orderExportNew:
		return "new orders"
	case orderExportBackordered:
		return "backordered orders"
	case orderExportSelected:
		return "selected orders"
	default:
		return "orders"
	}
}

// orderExportActionLabel returns the primary export button text for the currently selected optional packing slips.
// It has no parameters and returns safe UI copy derived from the dialog's packing-slip choice.
func (ui *DesktopUI) orderExportActionLabel() string {
	if ui.orders.view.exportDialog.includePackingSlips {
		return "Export CSV and packing slips"
	}
	return "Export CSV"
}

// layoutCSVExportBlockedDialog explains why the current connection cannot produce a CSV export.
func (ui *DesktopUI) layoutCSVExportBlockedDialog(gtx layout.Context) layout.Dimensions {
	if ui.orders.view.closeCSVExportBlocked.Clicked(gtx) {
		ui.orders.view.csvExportBlockedOpen = false
		ui.invalidate()
	}
	return modalPanel(gtx, ui, "CSV export blocked", func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(bodyText(ui.theme, "CSV export is not configured for this connection's Faire brand.", mutedTextColor)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &ui.orders.view.closeCSVExportBlocked, "Close")),
		)
	})
}

// layoutCSVExportCompletedDialog confirms the CSV location and any complete or partial packing-slip outcome after artifacts are safely written.
// gtx supplies the current frame, and the returned dimensions render the safe completion summary and close action.
func (ui *DesktopUI) layoutCSVExportCompletedDialog(gtx layout.Context) layout.Dimensions {
	if ui.orders.view.closeCSVExportCompleted.Clicked(gtx) {
		ui.orders.view.csvExportCompletedOpen = false
		ui.orders.view.csvExportCompletedFile = ""
		ui.orders.view.packingSlipExportFolder = ""
		ui.orders.view.packingSlipExportCount = 0
		ui.orders.view.packingSlipExportFailure = 0
		ui.invalidate()
	}
	return modalPanel(gtx, ui, "Export complete", func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(bodyText(ui.theme, ui.orderExportCompletionMessage(), mutedTextColor)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &ui.orders.view.closeCSVExportCompleted, "Close")),
		)
	})
}

// orderExportCompletionMessage returns the dialog summary for a CSV-only, complete packing-slip, or partial packing-slip export.
// It has no parameters and returns safe text derived only from already-saved artifact names and counts.
func (ui *DesktopUI) orderExportCompletionMessage() string {
	message := "Saved in Downloads as " + ui.orders.view.csvExportCompletedFile + "."
	if ui.orders.view.packingSlipExportFolder == "" {
		return message
	}
	message += " Saved " + packingSlipCountLabel(ui.orders.view.packingSlipExportCount) + " in " + ui.orders.view.packingSlipExportFolder + "."
	if ui.orders.view.packingSlipExportFailure > 0 {
		message += " " + packingSlipCountLabel(ui.orders.view.packingSlipExportFailure) + " could not be downloaded."
	}
	return message
}

// layoutOrdersDataModal confirms a destructive local-only cache action before any private snapshots are deleted.
func (ui *DesktopUI) layoutOrdersDataModal(gtx layout.Context) layout.Dimensions {
	if ui.orders.view.cancelDataAction.Clicked(gtx) {
		ui.orders.view.dataDialog = ordersDataDialogState{}
		ui.invalidate()
	}
	if ui.orders.view.confirmDataAction.Clicked(gtx) {
		ui.startOrdersDataAction(ui.orders.view.dataDialog.connectionID, ui.orders.view.dataDialog.rebuild)
		ui.invalidate()
	}
	action := "Delete local order data"
	description := "This removes locally stored order details, including customer and shipping information, for the selected connection only. It never deletes data at Faire."
	if ui.orders.view.dataDialog.rebuild {
		action = "Delete and rebuild local order data"
		description += " A new 30-day local history download will begin after deletion."
	}
	return modalPanel(gtx, ui, action, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(bodyText(ui.theme, description, mutedTextColor)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(dangerButton(ui.theme, &ui.orders.view.confirmDataAction, action)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &ui.orders.view.cancelDataAction, "Cancel")),
		)
	})
}

// layoutOrdersTable creates the framed surface's order header, scrollable rows, and Load more action.
// Before the first request, it fills the remaining panel space with the same safe message rather than nesting another card.
func (ui *DesktopUI) layoutOrdersTable(gtx layout.Context) layout.Dimensions {
	if ui.activeConnectionID == "" || (!ui.orders.view.state.Loaded && !ui.orders.view.state.Loading) {
		return layout.Inset{Top: unit.Dp(28), Right: unit.Dp(28), Bottom: unit.Dp(28), Left: unit.Dp(28)}.Layout(gtx, bodyText(ui.theme, emptyOrdersMessage(ui.orders.view.state.Status), mutedTextColor))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(ui.layoutOrdersHeader),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return topRule(gtx, panelBorderColor) }),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return ui.orders.view.list.Layout(gtx, len(ui.orders.view.state.Rows)+1, ui.layoutOrdersListItem)
		}),
	)
}

// layoutOrdersHeader renders fixed table column labels using the same widths as row values.
func (ui *DesktopUI) layoutOrdersHeader(gtx layout.Context) layout.Dimensions {
	if ui.orders.view.selectVisibleButton.Clicked(gtx) {
		if ui.allVisibleOrdersSelected() {
			ui.orders.view.state.ClearSelection()
		} else {
			ui.orders.view.state.SelectVisible(ui.orders.view.state.Rows)
		}
		ui.invalidate()
	}
	if ui.orders.view.orderDateSortButton.Clicked(gtx) {
		ui.orders.view.state.ToggleTableSort(orders.TableSortColumnOrderDate)
		ui.orders.view.state.Cursor = ""
		ui.orders.view.searchActive = false
		ui.startOrdersLoad(ordersLoadLocalOnly)
		ui.invalidate()
	}
	if ui.orders.view.shipDateSortButton.Clicked(gtx) {
		ui.orders.view.state.ToggleTableSort(orders.TableSortColumnShipDate)
		ui.orders.view.state.Cursor = ""
		ui.orders.view.searchActive = false
		ui.startOrdersLoad(ordersLoadLocalOnly)
		ui.invalidate()
	}
	return layout.Inset{Top: unit.Dp(12), Right: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return ui.layoutOrderColumns(gtx, "", []string{"", "Order", "Status", "Customer", "Total payout", "Order date", "Ship date", "Commission %", "Source"}, true, ui.allVisibleOrdersSelected())
	})
}

// layoutOrdersListItem renders selectable order rows with a full-width divider after every row.
// gtx supplies the current frame, index selects a row or footer, and the returned dimensions render it; the nested order-number link underlines on hover and takes precedence so opening details does not change export selection, and the row control receives pointer feedback.
func (ui *DesktopUI) layoutOrdersListItem(gtx layout.Context, index int) layout.Dimensions {
	if index == len(ui.orders.view.state.Rows) {
		return ui.layoutOrdersFooter(gtx)
	}
	row := ui.orders.view.state.Rows[index]
	rowControl := ui.orderControlFor(row.ID)
	detailControl := ui.orderDetailControlFor(row.ID)
	if detailControl.Clicked(gtx) {
		ui.openOrder(row.ID)
		ui.invalidate()
	} else if rowControl.Clicked(gtx) {
		ui.orders.view.state.ToggleSelection(row.ID)
		ui.invalidate()
	}
	return clickableWithPointer(gtx, rowControl, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Background{}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					if ui.orders.view.state.IsSelected(row.ID) {
						// Paint only the measured row area so its highlight never reaches neighboring entries.
						paint.FillShape(gtx.Ops, color.NRGBA{R: 243, G: 243, B: 243, A: 255}, clip.Rect(image.Rectangle{Max: gtx.Constraints.Min}).Op())
					}
					return layout.Dimensions{Size: gtx.Constraints.Min}
				}, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(13), Right: unit.Dp(12), Bottom: unit.Dp(13), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return ui.layoutOrderColumns(gtx, row.ID, []string{"", row.DisplayID, row.Status, row.Customer, row.TotalPayout, row.OrderDate, row.ShipDate, row.Commission, row.Source}, false, ui.orders.view.state.IsSelected(row.ID))
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
		if !ui.orders.view.state.Loading && ui.orders.view.state.Cursor != "" && !ui.orders.view.searchActive {
			children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout), layout.Rigid(primaryButton(ui.theme, &ui.orders.view.loadMoreButton, "Load more")))
		} else if len(ui.orders.view.state.Rows) == 0 && ui.orders.view.state.Loaded {
			children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout), layout.Rigid(bodyText(ui.theme, "No orders match these filters.", mutedTextColor)))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// layoutOrderColumns lays out a bounded desktop table row, truncating long values through Gio constraints.
// gtx supplies the current frame, orderID identifies the detail link, values are cell text, header and selected control row behavior, and the returned dimensions render the columns; a non-empty order ID makes the order-number cell a detail-navigation link that underlines on hover, and every header control receives pointer feedback.
func (ui *DesktopUI) layoutOrderColumns(gtx layout.Context, orderID faire.OrderID, values []string, header, selected bool) layout.Dimensions {
	// Wide fixed columns preserve readable separation on the desktop-only Orders screen.
	widths := []unit.Dp{44, 150, 140, 210, 120, 125, 125, 145, 130}
	children := make([]layout.FlexChild, 0, len(values))
	for index, value := range values {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(widths[index])
			gtx.Constraints.Max.X = gtx.Dp(widths[index])
			if index == 0 {
				if header {
					return clickableWithPointer(gtx, &ui.orders.view.selectVisibleButton, func(gtx layout.Context) layout.Dimensions {
						return ui.orderCheckbox(gtx, selected)
					})
				}
				return ui.orderCheckbox(gtx, selected)
			}
			if !header && index == 1 {
				return linkLabel(gtx, ui.theme, ui.orderDetailControlFor(orderID), value)
			}

			if header && index == 5 {
				return clickableWithPointer(gtx, &ui.orders.view.orderDateSortButton, func(gtx layout.Context) layout.Dimensions {
					return ui.orderHeaderLabel(gtx, ui.sortHeaderLabel(orders.TableSortColumnOrderDate, value))
				})
			}
			if header && index == 6 {
				return clickableWithPointer(gtx, &ui.orders.view.shipDateSortButton, func(gtx layout.Context) layout.Dimensions {
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
	if ui.orders.view.state.TableSort.Column != column {
		return label
	}
	if ui.orders.view.state.TableSort.Direction == orders.TableSortAscending {
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
		layout.Rigid(primaryButton(ui.theme, &ui.orders.view.refreshButton, "Refresh")),
		layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
		layout.Rigid(material.Label(ui.theme, unit.Sp(12), "Updated At Minimum (earlier date adds history)").Layout),
		layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.dateFilterField(gtx, &ui.orders.view.updatedAt, "M/D/YYYY")
		}),
	)
}

// orderControlFor returns the persistent clickable for a row ID, avoiding lost click state during list redraws.
func (ui *DesktopUI) orderControlFor(id faire.OrderID) *widget.Clickable {
	if control, found := ui.orders.view.rowControls[id]; found {
		return control
	}
	control := new(widget.Clickable)
	ui.orders.view.rowControls[id] = control
	return control
}

// orderDetailControlFor returns the persistent order-number clickable for an order row.
// Its state survives immediate-mode redraws so navigation gestures keep their identity.
func (ui *DesktopUI) orderDetailControlFor(id faire.OrderID) *widget.Clickable {
	if control, found := ui.orders.view.detailControls[id]; found {
		return control
	}
	control := new(widget.Clickable)
	ui.orders.view.detailControls[id] = control
	return control
}

// allVisibleOrdersSelected reports whether every selectable visible row belongs to the current selection.
func (ui *DesktopUI) allVisibleOrdersSelected() bool {
	if len(ui.orders.view.state.Rows) == 0 {
		return false
	}
	for _, row := range ui.orders.view.state.Rows {
		if row.ID == "" || !ui.orders.view.state.IsSelected(row.ID) {
			return false
		}
	}
	return true
}

// statesButtonLabel communicates whether the API state filter narrows results.
func (ui *DesktopUI) statesButtonLabel() string {
	if len(ui.orders.view.state.IncludedStates) == len(orders.KnownStates()) {
		return "States"
	}
	return "States (" + itoa(len(ui.orders.view.state.IncludedStates)) + ")"
}

// emptyOrdersMessage returns the safe placeholder text displayed before the first connection or order request.
// A non-empty status takes precedence so the caller can surface request feedback in the existing Orders panel.
func emptyOrdersMessage(message string) string {
	if message == "" {
		return "Orders will appear here after a saved connection is selected."
	}
	return message
}

// orderTabButton renders a low-profile tab with an underline for the selected server-state filter and a pointer cursor.
// gtx supplies the current frame, theme controls styling, button owns interaction state, label and count identify the preset, selected controls emphasis, and the returned dimensions match the tab.
func orderTabButton(gtx layout.Context, theme *material.Theme, button *widget.Clickable, label string, count int, selected bool) layout.Dimensions {
	return clickableWithPointer(gtx, button, func(gtx layout.Context) layout.Dimensions {
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

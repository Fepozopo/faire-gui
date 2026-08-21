package application

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Fepozopo/faire-gui/faire"
)

// layoutSidebar renders the persistent application navigation, active-connection switcher, and Settings entry.
// Brand profile and Connections are available only through Settings, while supported but unimplemented routes remain visually present but non-interactive.
func (ui *DesktopUI) layoutSidebar(gtx layout.Context) layout.Dimensions {
	width := gtx.Dp(unit.Dp(220))
	gtx.Constraints.Min.X = width
	gtx.Constraints.Max.X = width
	return layout.Background{}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		paint.Fill(gtx.Ops, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		paint.FillShape(gtx.Ops, color.NRGBA{R: 225, G: 225, B: 225, A: 255}, clip.Rect(image.Rect(width-1, 0, width, gtx.Constraints.Min.Y)).Op())
		return layout.Dimensions{Size: gtx.Constraints.Min}
	}, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(28), Right: unit.Dp(16), Bottom: unit.Dp(24), Left: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(ui.layoutConnectionSwitcher),
				layout.Rigid(layout.Spacer{Height: unit.Dp(20)}.Layout),
				layout.Rigid(ui.layoutUnavailableNavigation),
				layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
				layout.Rigid(ui.layoutSettingsNavigation),
				layout.Rigid(ui.layoutSettingsSubmenu),
			)
		})
	})
}

// layoutConnectionSwitcher opens the session-only saved-connection picker with link-style pointer feedback.
// gtx supplies the current frame, and the returned dimensions render the complete connection-switcher target.
func (ui *DesktopUI) layoutConnectionSwitcher(gtx layout.Context) layout.Dimensions {
	if ui.activeConnectionButton.Clicked(gtx) {
		ui.connectionPickerOpen = true
		ui.invalidate()
	}
	label := "Choose connection"
	if ui.activeConnectionLabel != "" {
		label = ui.activeConnectionLabel
	}
	return clickableWithPointer(gtx, &ui.activeConnectionButton, func(gtx layout.Context) layout.Dimensions {
		return roundedPanel(gtx, selectionBarColor, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Right: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(material.Label(ui.theme, unit.Sp(12), "ACTIVE CONNECTION").Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(3)}.Layout),
					layout.Rigid(material.Body1(ui.theme, label).Layout),
				)
			})
		})
	})
}

// layoutNavigationItem draws one functional route button with the shared surface for selected or hovered states and a pointer cursor.
// gtx supplies the current frame, route identifies the destination, label is visible text, and the returned dimensions match the navigation target.
func (ui *DesktopUI) layoutNavigationItem(gtx layout.Context, route int, label string) layout.Dimensions {
	button := &ui.tabButtons[route]
	if button.Clicked(gtx) {
		ui.selectedTab = route
		if route == ordersTab && ui.activeConnectionID != "" && !ui.orders.view.state.Loaded {
			ui.startOrdersLoad(ordersLoadInitial)
		}
		ui.invalidate()
	}
	return clickableWithPointer(gtx, button, func(gtx layout.Context) layout.Dimensions {
		return roundedPanel(gtx, navigationHighlight(ui.selectedTab == route || button.Hovered()), func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(11), Right: unit.Dp(12), Bottom: unit.Dp(11), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				style := material.Body1(ui.theme, label)
				if ui.selectedTab == route {
					style.Color = color.NRGBA{R: 45, G: 45, B: 45, A: 255}
				}
				return style.Layout(gtx)
			})
		})
	})
}

// layoutUnavailableNavigation renders Orders together with supported routes that are not implemented as pages yet.
// Only API-backed destinations are shown, and the unavailable routes are intentionally non-interactive.
func (ui *DesktopUI) layoutUnavailableNavigation(gtx layout.Context) layout.Dimensions {
	labels := []string{"Orders", "Products", "Customers", "Analytics"}
	children := make([]layout.FlexChild, 0, len(labels)*2)
	for index, label := range labels {
		index, label := index, label
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if index == 0 {
				return ui.layoutNavigationItem(gtx, ordersTab, label)
			}
			style := material.Body1(ui.theme, label)
			style.Color = color.NRGBA{R: 130, G: 130, B: 130, A: 255}
			return layout.Inset{Top: unit.Dp(10), Right: unit.Dp(12), Bottom: unit.Dp(10), Left: unit.Dp(12)}.Layout(gtx, style.Layout)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// layoutSettingsNavigation toggles the inline Settings submenu from the persistent left navigation.
// gtx supplies the current frame; its shared highlight surface, chevron, and pointer cursor make expanded and hovered states visible, and the returned dimensions match the Settings target.
func (ui *DesktopUI) layoutSettingsNavigation(gtx layout.Context) layout.Dimensions {
	if ui.settingsButton.Clicked(gtx) {
		ui.settingsMenuOpen = !ui.settingsMenuOpen
		ui.invalidate()
	}
	chevron := "⌄"
	if ui.settingsMenuOpen {
		chevron = "⌃"
	}
	return clickableWithPointer(gtx, &ui.settingsButton, func(gtx layout.Context) layout.Dimensions {
		return roundedPanel(gtx, navigationHighlight(ui.settingsMenuOpen || ui.settingsButton.Hovered()), func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(11), Right: unit.Dp(12), Bottom: unit.Dp(11), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, material.Body1(ui.theme, "Settings").Layout),
					layout.Rigid(material.Body1(ui.theme, chevron).Layout),
				)
			})
		})
	})
}

// layoutSettingsSubmenu renders the Settings destinations directly beneath the expanded Settings header.
// Brand profile and Connections select their existing pages; Check for updates starts the existing asynchronous checker without collapsing the group.
func (ui *DesktopUI) layoutSettingsSubmenu(gtx layout.Context) layout.Dimensions {
	if !ui.settingsMenuOpen {
		return layout.Dimensions{}
	}
	if ui.settingsBrandProfile.Clicked(gtx) {
		ui.selectedTab = brandsTab
		ui.invalidate()
	}
	if ui.settingsConnections.Clicked(gtx) {
		ui.selectedTab = connectionsTab
		ui.invalidate()
	}
	if ui.checkForUpdates.Clicked(gtx) {
		ui.startManualUpdateCheck()
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.layoutSettingsSubmenuItem(gtx, &ui.settingsBrandProfile, "Brand profile")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.layoutSettingsSubmenuItem(gtx, &ui.settingsConnections, "Connections")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.layoutSettingsSubmenuItem(gtx, &ui.checkForUpdates, "Check for updates")
		}),
	)
}

// layoutSettingsSubmenuItem renders one indented Settings destination with the shared hover surface and pointer cursor while preserving its clickable state.
// gtx supplies the active frame, button owns the item state, label is the visible destination, and the returned dimensions match the rendered item.
func (ui *DesktopUI) layoutSettingsSubmenuItem(gtx layout.Context, button *widget.Clickable, label string) layout.Dimensions {
	return clickableWithPointer(gtx, button, func(gtx layout.Context) layout.Dimensions {
		return roundedPanel(gtx, navigationHighlight(button.Hovered()), func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10), Right: unit.Dp(12), Bottom: unit.Dp(10), Left: unit.Dp(32)}.Layout(gtx, material.Body1(ui.theme, label).Layout)
		})
	})
}

// navigationHighlight returns the shared darker neutral-gray selection surface for active or hovered sidebar navigation items.
// An inactive item remains transparent so the white sidebar background continues to show through.
func navigationHighlight(active bool) color.NRGBA {
	if active {
		return selectionBarColor
	}
	return color.NRGBA{}
}

// layoutActivePage lays out the currently selected functional route inside the shared sidebar shell.
func (ui *DesktopUI) layoutActivePage(gtx layout.Context) layout.Dimensions {
	if ui.orders.view.orderDetailOpen && ui.selectedTab == ordersTab {
		return ui.layoutOrderDetail(gtx)
	}
	switch ui.selectedTab {
	case connectionsTab:
		return ui.layoutConnections(gtx)
	case brandsTab:
		return ui.layoutBrands(gtx)
	default:
		return ui.layoutOrders(gtx)
	}
}

// layoutConnectionPicker displays saved non-secret connection metadata for session-only switching.
func (ui *DesktopUI) layoutConnectionPicker(gtx layout.Context) layout.Dimensions {
	if ui.closeConnectionPicker.Clicked(gtx) {
		ui.connectionPickerOpen = false
		ui.invalidate()
	}
	if ui.addConnectionButton.Clicked(gtx) {
		ui.connectionPickerOpen = false
		ui.selectedTab = connectionsTab
		ui.invalidate()
	}
	return modalPanel(gtx, ui, "Switch connection", func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(bodyText(ui.theme, "The active connection is used only for this app session.", mutedTextColor)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(330))
				return ui.connectionPickerList.Layout(gtx, len(ui.connections), func(gtx layout.Context, index int) layout.Dimensions {
					connection := ui.connections[index]
					control := ui.connectionPickerControlFor(connection.ID)
					if control.Clicked(gtx) {
						ui.setActiveConnection(connection)
					}
					label := connection.Label
					if connection.ID == ui.activeConnectionID {
						label += " (active)"
					}
					return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, primaryButton(ui.theme, control, label))
				})
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(primaryButton(ui.theme, &ui.addConnectionButton, "Add connection")),
					layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
					layout.Rigid(primaryButton(ui.theme, &ui.closeConnectionPicker, "Close")),
				)
			}),
		)
	})
}

// connectionPickerControlFor returns picker-specific click state for a saved connection.
// It must not share the Brand Profile control because modal input must win over the page underneath it.
func (ui *DesktopUI) connectionPickerControlFor(connectionID string) *widget.Clickable {
	if control, found := ui.connectionPickerControls[connectionID]; found {
		return control
	}
	control := new(widget.Clickable)
	ui.connectionPickerControls[connectionID] = control
	return control
}

// layoutStatesDialog applies a multi-select state filter by expressing unselected known states as API exclusions.
// Its state controls are presented alphabetically by their user-facing labels for quick scanning.
func (ui *DesktopUI) layoutStatesDialog(gtx layout.Context) layout.Dimensions {
	if ui.orders.view.cancelStatesButton.Clicked(gtx) {
		ui.orders.view.statesDialogOpen = false
		ui.invalidate()
	}
	if ui.orders.view.selectAllStatesButton.Clicked(gtx) {
		ui.orders.view.pendingStates = make(map[faire.OrderState]struct{}, len(ordersKnownStates()))
		for _, state := range ordersKnownStates() {
			ui.orders.view.pendingStates[state] = struct{}{}
		}
		ui.invalidate()
	}
	if ui.orders.view.selectNoStatesButton.Clicked(gtx) {
		ui.orders.view.pendingStates = make(map[faire.OrderState]struct{})
		ui.invalidate()
	}
	if ui.orders.view.applyStatesButton.Clicked(gtx) {
		ui.orders.view.state.SetIncludedStates(mapKeys(ui.orders.view.pendingStates))
		ui.orders.view.state.SelectedIDs = make(map[faire.OrderID]struct{})
		ui.orders.view.searchActive = false
		ui.orders.view.statesDialogOpen = false
		ui.startOrdersLoad(ordersLoadLocalOnly)
		ui.invalidate()
	}
	return modalPanel(gtx, ui, "Filter states", func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(bodyText(ui.theme, "Choose one or more states. Faire receives the remaining known states as exclusions.", mutedTextColor)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(primaryButton(ui.theme, &ui.orders.view.selectAllStatesButton, "Select all")),
					layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
					layout.Rigid(primaryButton(ui.theme, &ui.orders.view.selectNoStatesButton, "Select none")),
				)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
		}
		for _, state := range ordersKnownStates() {
			state := state
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				control := ui.stateControlFor(state)
				if control.Clicked(gtx) {
					if _, selected := ui.orders.view.pendingStates[state]; selected {
						delete(ui.orders.view.pendingStates, state)
					} else {
						ui.orders.view.pendingStates[state] = struct{}{}
					}
					ui.invalidate()
				}
				label := displayOrderState(state)
				if _, selected := ui.orders.view.pendingStates[state]; selected {
					label = "✓ " + label
				}
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, primaryButton(ui.theme, control, label))
			}))
		}
		children = append(children,
			layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(primaryButton(ui.theme, &ui.orders.view.applyStatesButton, "Apply states")),
					layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
					layout.Rigid(primaryButton(ui.theme, &ui.orders.view.cancelStatesButton, "Cancel")),
				)
			}),
		)
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// modalPanel provides shared modal input isolation and a bounded white card.
func modalPanel(gtx layout.Context, ui *DesktopUI, title string, content layout.Widget) layout.Dimensions {
	fullSize := ui.modalBlocker.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return fill(gtx, modalScrimColor)
	})
	layout.Stack{Alignment: layout.Center}.Layout(gtx, layout.Stacked(func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(540)))
		gtx.Constraints.Min.X = 0
		return roundedPanel(gtx, cardBackground, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(24), Right: unit.Dp(24), Bottom: unit.Dp(24), Left: unit.Dp(24)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(material.H4(ui.theme, title).Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
					layout.Rigid(content),
				)
			})
		})
	}))
	return fullSize
}

// stateControlFor returns the persistent multi-select control for one API state.
func (ui *DesktopUI) stateControlFor(state faire.OrderState) *widget.Clickable {
	if control, found := ui.orders.view.stateControls[state]; found {
		return control
	}
	control := new(widget.Clickable)
	ui.orders.view.stateControls[state] = control
	return control
}

// ordersKnownStates returns the state-picker options in alphabetical user-facing label order.
// The returned slice contains each supported API state exactly once for selection and exclusion construction.
func ordersKnownStates() []faire.OrderState {
	return []faire.OrderState{
		faire.OrderStateBackordered,
		faire.OrderStateCanceled,
		faire.OrderStateDelivered,
		faire.OrderStateInTransit,
		faire.OrderStateNew,
		faire.OrderStatePendingRetailerConfirmation,
		faire.OrderStatePreTransit,
		faire.OrderStateProcessing,
	}
}

// mapKeys converts confirmed state selections to an unordered slice accepted by feature state validation.
func mapKeys(states map[faire.OrderState]struct{}) []faire.OrderState {
	keys := make([]faire.OrderState, 0, len(states))
	for state := range states {
		keys = append(keys, state)
	}
	return keys
}

// displayOrderState renders API state enum values without passing raw enum spelling into user-facing controls.
func displayOrderState(state faire.OrderState) string {
	switch state {
	case faire.OrderStateNew:
		return "New"
	case faire.OrderStateProcessing:
		return "Processing"
	case faire.OrderStatePreTransit:
		return "Pre-transit"
	case faire.OrderStateInTransit:
		return "In transit"
	case faire.OrderStateDelivered:
		return "Delivered"
	case faire.OrderStateCanceled:
		return "Canceled"
	case faire.OrderStateBackordered:
		return "Backordered"
	default:
		return "Pending retailer confirmation"
	}
}

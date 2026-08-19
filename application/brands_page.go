package application

import (
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// layoutBrands renders a vertically scrollable selector of saved connections and the current safe profile status.
// Each row uses stable controls keyed by connection ID so scrolling does not change pointer interaction identity.
func (ui *DesktopUI) layoutBrands(gtx layout.Context) layout.Dimensions {
	itemCount := len(ui.connections) + 1
	if len(ui.connections) == 0 {
		itemCount++
	}
	return ui.brandsList.Layout(gtx, itemCount, func(gtx layout.Context, index int) layout.Dimensions {
		if index == 0 {
			return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(material.H3(ui.theme, "Choose a saved brand connection").Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
					layout.Rigid(bodyText(ui.theme, "Select a connection to verify its read-only Faire brand profile.", mutedTextColor)),
					layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
					layout.Rigid(ui.layoutBrandStatus),
				)
			})
		}
		if index == 1 && len(ui.connections) == 0 {
			return bodyText(ui.theme, "No connections have been saved yet. Use the Connections tab to add one.", mutedTextColor)(gtx)
		}
		connectionIndex := index - 1
		if len(ui.connections) == 0 {
			connectionIndex--
		}
		connection := ui.connections[connectionIndex]
		controls := ui.rowControlsFor(connection.ID)
		if controls.selectProfile.Clicked(gtx) {
			ui.selectConnection(connection.ID)
		}
		if controls.rebuildLocalData.Clicked(gtx) {
			ui.requestOrdersDataAction(connection.ID, true)
			ui.invalidate()
		}
		if controls.deleteLocalData.Clicked(gtx) {
			ui.requestOrdersDataAction(connection.ID, false)
			ui.invalidate()
		}
		return layout.Inset{Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return card(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(16), Left: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(material.H5(ui.theme, connection.Label).Layout),
						layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
						layout.Rigid(bodyText(ui.theme, connectionDetails(connection), mutedTextColor)),
						layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.layoutBrandProfileActions(gtx, controls)
						}),
					)
				})
			})
		})
	})
}

// layoutBrandStatus renders Brand Profile feedback and highlights a local-data action without changing the normal status layout.
// An active rebuild uses an amber background directly behind the same body text that renders completed and error statuses.
func (ui *DesktopUI) layoutBrandStatus(gtx layout.Context) layout.Dimensions {
	if ui.status == "" {
		return layout.Dimensions{}
	}
	if ui.orders.dataActionConnectionID == "" {
		return statusText(ui.theme, ui.status)(gtx)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(material.H6(ui.theme, "Status").Layout),
		layout.Rigid(layout.Spacer{Height: unit.Dp(3)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return roundedPanel(gtx, activityColor, bodyText(ui.theme, ui.status, mutedTextColor))
		}),
	)
}

// layoutBrandProfileActions renders equal-width, connection-scoped profile and local-data actions.
// The controls remain associated with their card so destructive actions never use another connection's cached Orders data.
func (ui *DesktopUI) layoutBrandProfileActions(gtx layout.Context, controls *connectionRowControls) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return primaryButton(ui.theme, &controls.selectProfile, "Load brand profile")(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return primaryButton(ui.theme, &controls.rebuildLocalData, "Rebuild local data")(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return dangerButton(ui.theme, &controls.deleteLocalData, "Delete local data")(gtx)
		}),
	)
}

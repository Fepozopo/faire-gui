package application

import (
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/Fepozopo/faire-gui/features/orders"
)

// layoutOrderDetail renders the typed local-first Order detail screen without accepting raw snapshots or Faire API values.
func (ui *DesktopUI) layoutOrderDetail(gtx layout.Context) layout.Dimensions {
	if ui.backToOrdersButton.Clicked(gtx) {
		ui.orderDetailOpen = false
		ui.invalidate()
	}
	if ui.refreshOrderDetailButton.Clicked(gtx) {
		ui.refreshOrderDetail()
		ui.invalidate()
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(primaryButton(ui.theme, &ui.backToOrdersButton, "Back to Orders")),
				layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
				layout.Rigid(material.H3(ui.theme, "Order details").Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Min} }),
				layout.Rigid(primaryButton(ui.theme, &ui.refreshOrderDetailButton, "Refresh order")),
			)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
		layout.Rigid(statusText(ui.theme, ui.orderDetailStatus)),
		layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if ui.orderDetailLoading || ui.orderDetail.OrderID == "" {
				return bodyText(ui.theme, "Order details will appear here when the local snapshot is available.", mutedTextColor)(gtx)
			}
			return outlinedPanel(gtx, cardBackground, panelBorderColor, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(20), Right: unit.Dp(20), Bottom: unit.Dp(20), Left: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layoutOrderDetailContent(gtx, ui, ui.orderDetail)
				})
			})
		}),
	)
}

// layoutOrderDetailContent lays out all explicitly approved detail values from a typed presentation model.
func layoutOrderDetailContent(gtx layout.Context, ui *DesktopUI, detail orders.Detail) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(material.H4(ui.theme, detail.DisplayID).Layout),
		layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
		layout.Rigid(detailLine(ui, "Status", detail.Status)),
		layout.Rigid(detailLine(ui, "Created", detail.CreatedAt)),
		layout.Rigid(detailLine(ui, "Updated", detail.UpdatedAt)),
		layout.Rigid(detailLine(ui, "Local data synced", detail.SyncedAt)),
		layout.Rigid(detailLine(ui, "Customer", detail.Customer)),
		layout.Rigid(detailLine(ui, "Total", detail.Total)),
		layout.Rigid(detailLine(ui, "Commission", detail.Commission)),
		layout.Rigid(detailLine(ui, "Total payout", detail.TotalPayout)),
		layout.Rigid(detailLine(ui, "Source", detail.Source)),
		layout.Rigid(detailLine(ui, "Purchase order", detail.PurchaseOrderNumber)),
		layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
		layout.Rigid(material.H6(ui.theme, "Shipping address").Layout),
		layout.Rigid(detailLine(ui, "Recipient", detail.ShippingAddress.Name)),
		layout.Rigid(detailLine(ui, "Company", detail.ShippingAddress.CompanyName)),
		layout.Rigid(detailLine(ui, "Address", detail.ShippingAddress.Address1)),
		layout.Rigid(detailLine(ui, "Address 2", detail.ShippingAddress.Address2)),
		layout.Rigid(detailLine(ui, "City", detail.ShippingAddress.City)),
		layout.Rigid(detailLine(ui, "State", detail.ShippingAddress.State)),
		layout.Rigid(detailLine(ui, "Postal code", detail.ShippingAddress.PostalCode)),
		layout.Rigid(detailLine(ui, "Country", detail.ShippingAddress.Country)),
		layout.Rigid(detailLine(ui, "Phone", detail.ShippingAddress.PhoneNumber)),
		layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
		layout.Rigid(material.H6(ui.theme, "Items").Layout),
	}
	for _, item := range detail.Items {
		item := item
		children = append(children,
			layout.Rigid(detailLine(ui, "Item", item.ProductName+" · "+item.VariantName)),
			layout.Rigid(detailLine(ui, "Quantity / price", item.Quantity+" · "+item.Price)),
			layout.Rigid(detailLine(ui, "SKU / status", item.SKU+" · "+item.Status)),
		)
		for _, customization := range item.Customizations {
			customization := customization
			children = append(children, layout.Rigid(detailLine(ui, "Customization: "+customization.Type, customization.Value)))
		}
	}
	children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout), layout.Rigid(material.H6(ui.theme, "Shipments").Layout))
	for _, shipment := range detail.Shipments {
		shipment := shipment
		children = append(children, layout.Rigid(detailLine(ui, "Shipment", shipment.Carrier+" · "+shipment.ShippingType)), layout.Rigid(detailLine(ui, "Tracking", shipment.TrackingCode)), layout.Rigid(detailLine(ui, "Maker cost", shipment.MakerCost)))
	}
	children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout), layout.Rigid(material.H6(ui.theme, "Order notes").Layout), layout.Rigid(bodyText(ui.theme, detail.Notes, mutedTextColor)))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// detailLine renders one compact detail label and value supplied by the typed detail model.
func detailLine(ui *DesktopUI, label, value string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(unit.Dp(150))
					gtx.Constraints.Max.X = gtx.Dp(unit.Dp(150))
					return material.Label(ui.theme, unit.Sp(13), label).Layout(gtx)
				}),
				layout.Flexed(1, bodyText(ui.theme, value, mutedTextColor)),
			)
		})
	}
}

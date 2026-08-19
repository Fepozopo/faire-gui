package application

import (
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/Fepozopo/faire-gui/features/orders"
)

// layoutOrderDetail renders the typed local-first Order detail screen without accepting raw snapshots or Faire API values.
// Its detail panel scrolls independently so the header controls remain available for long orders.
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
			return ui.orderDetailList.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
				return outlinedPanel(gtx, cardBackground, panelBorderColor, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(20), Right: unit.Dp(20), Bottom: unit.Dp(20), Left: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layoutOrderDetailContent(gtx, ui, ui.orderDetail)
					})
				})
			})
		}),
	)
}

// layoutOrderDetailContent lays out approved values from detail, with updated and local-sync timestamps preceding the order's creation date and the free-shipping reason following its eligibility.
// It uses ui for themed controls and returns the rendered content dimensions; each order item is a separate card for scanability.
func layoutOrderDetailContent(gtx layout.Context, ui *DesktopUI, detail orders.Detail) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(material.H4(ui.theme, detail.DisplayID).Layout),
		layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
		layout.Rigid(detailLine(ui, "Status", detail.Status)),
		layout.Rigid(detailLine(ui, "Original order ID", detail.OriginalOrderID)),
		layout.Rigid(detailLine(ui, "Updated", detail.UpdatedAt)),
		layout.Rigid(detailLine(ui, "Local data synced", detail.SyncedAt)),
		layout.Rigid(detailLine(ui, "Created", detail.CreatedAt)),
		layout.Rigid(detailLine(ui, "Ship after", detail.ShipAfter)),
		layout.Rigid(detailLine(ui, "Requested ship date", detail.RequestedShipDate)),
		layout.Rigid(detailLine(ui, "Expected ship date", detail.ExpectedShipDate)),
		layout.Rigid(detailLine(ui, "Customer", detail.Customer)),
		layout.Rigid(detailLine(ui, "Commission", detail.Commission)),
		layout.Rigid(detailLine(ui, "Total payout", detail.TotalPayout)),
		layout.Rigid(detailLine(ui, "Source", detail.Source)),
		layout.Rigid(detailLine(ui, "Purchase order", detail.PurchaseOrderNumber)),
		layout.Rigid(detailLine(ui, "Sales rep name", detail.SalesRepName)),
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
		layout.Rigid(detailLine(ui, "Is free shipping", detail.IsFreeShipping)),
		layout.Rigid(detailLine(ui, "Free shipping reason", detail.FreeShippingReason)),
		layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
		layout.Rigid(material.H6(ui.theme, "Items").Layout),
	}
	for index, item := range detail.Items {
		item, itemIndex := item, index
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layoutOrderItem(gtx, ui, item, itemIndex, len(detail.Items))
		}))
		if itemIndex < len(detail.Items)-1 {
			children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout))
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

// layoutOrderItem renders one product or variant in a lightly tinted, bordered card.
// The SKU is the card heading, while the item index and total provide a position label only for multi-item orders.
func layoutOrderItem(gtx layout.Context, ui *DesktopUI, item orders.DetailItem, index, total int) layout.Dimensions {
	positionLabel := "Item"
	if total > 1 {
		positionLabel += " " + itoa(index+1) + " of " + itoa(total)
	}
	productLabel := item.ProductName
	if item.VariantName != "" {
		productLabel += " · " + item.VariantName
	}
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			style := material.Label(ui.theme, unit.Sp(12), positionLabel)
			style.Color = mutedTextColor
			return style.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
		layout.Rigid(material.H6(ui.theme, item.SKU).Layout),
		layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
		layout.Rigid(detailLine(ui, "Product / variant", productLabel)),
		layout.Rigid(detailLine(ui, "Quantity / price", item.Quantity+" · "+item.Price)),
		layout.Rigid(detailLine(ui, "Status", item.Status)),
	}
	if len(item.Customizations) > 0 {
		children = append(children,
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(material.Label(ui.theme, unit.Sp(13), "Customizations").Layout),
		)
		for _, customization := range item.Customizations {
			customization := customization
			children = append(children, layout.Rigid(detailLine(ui, customization.Type, customization.Value)))
		}
	}
	return outlinedPanel(gtx, selectionBarColor, panelBorderColor, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(14), Right: unit.Dp(14), Bottom: unit.Dp(14), Left: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
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

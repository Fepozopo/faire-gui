package application

import (
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/Fepozopo/faire-gui/faire"
	"github.com/Fepozopo/faire-gui/features/orders"
)

// layoutOrderDetail renders the typed local-first Order detail screen without accepting raw snapshots or Faire API values.
// Its detail panel scrolls independently so the header controls remain available for long orders, while original-order and official-carrier tracking links and the empty-shipment form keep their own actions.
func (ui *DesktopUI) layoutOrderDetail(gtx layout.Context) layout.Dimensions {
	if len(ui.orders.view.orderDetail.Shipments) == 0 && ui.orders.view.orderDetail.OrderID != "" {
		ui.handleShipmentFormEvents(gtx)
	}
	if originalOrderID := ui.orders.view.orderDetail.OriginalOrderID; originalOrderID != "" && ui.orderDetailControlFor(originalOrderID).Clicked(gtx) {
		ui.openOrder(originalOrderID)
		ui.invalidate()
	}
	for index, shipment := range ui.orders.view.orderDetail.Shipments {
		if shipment.TrackingURL != "" && ui.shipmentTrackingControlFor(ui.orders.view.orderDetail.OrderID, index).Clicked(gtx) {
			ui.openTrackingURL(shipment.TrackingURL)
		}
	}
	if ui.orders.view.backToOrdersButton.Clicked(gtx) {
		ui.orders.view.orderDetailOpen = false
		ui.invalidate()
	}
	if ui.orders.view.refreshDetailButton.Clicked(gtx) {
		ui.refreshOrderDetail()
		ui.invalidate()
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(primaryButton(ui.theme, &ui.orders.view.backToOrdersButton, "Back to Orders")),
				layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
				layout.Rigid(material.H3(ui.theme, "Order details").Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Min} }),
				layout.Rigid(primaryButton(ui.theme, &ui.orders.view.refreshDetailButton, "Refresh order")),
			)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
		layout.Rigid(statusText(ui.theme, ui.orders.view.orderDetailStatus)),
		layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if ui.orders.view.orderDetailLoading || ui.orders.view.orderDetail.OrderID == "" {
				return bodyText(ui.theme, "Order details will appear here when the local snapshot is available.", mutedTextColor)(gtx)
			}
			return ui.orders.view.detailList.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
				return outlinedPanel(gtx, cardBackground, panelBorderColor, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(20), Right: unit.Dp(20), Bottom: unit.Dp(20), Left: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layoutOrderDetailContent(gtx, ui, ui.orders.view.orderDetail)
					})
				})
			})
		}),
	)
}

// layoutOrderDetailContent lays out approved values from detail, with a clickable original-order ID, updated and local-sync timestamps preceding the order's creation date, free-shipping reason following its eligibility, and shipments between order notes and items.
// It uses ui for themed controls and returns the rendered content dimensions; each order item is a separate card for scanability.
func layoutOrderDetailContent(gtx layout.Context, ui *DesktopUI, detail orders.Detail) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(material.H4(ui.theme, detail.DisplayID).Layout),
		layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
		layout.Rigid(detailLine(ui, "Status", detail.Status)),
		layout.Rigid(detailOriginalOrderIDLine(ui, detail.OriginalOrderID, detail.OriginalOrderDisplayID)),
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
		layout.Rigid(material.H6(ui.theme, "Order notes").Layout),
		layout.Rigid(bodyText(ui.theme, detail.Notes, mutedTextColor)),
		layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
		layout.Rigid(material.H6(ui.theme, "Shipments").Layout),
	}
	children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout), layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return shipmentPanel(gtx, func(gtx layout.Context) layout.Dimensions {
			if len(detail.Shipments) == 0 {
				return layoutShipmentForm(gtx, ui)
			}
			return layoutExistingShipments(gtx, ui, detail)
		})
	}))
	children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout), layout.Rigid(material.H6(ui.theme, "Items").Layout))
	for index, item := range detail.Items {
		item, itemIndex := item, index
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layoutOrderItem(gtx, ui, item, itemIndex, len(detail.Items))
		}))
		if itemIndex < len(detail.Items)-1 {
			children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout))
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// handleShipmentFormEvents applies empty-shipment form interactions before the form is laid out for the current frame.
// It drains every action event even while confirmation is disabled, preventing a prior disabled click from submitting a later valid form.
func (ui *DesktopUI) handleShipmentFormEvents(gtx layout.Context) {
	view := &ui.orders.view
	view.normalizeShipmentFormBlurredFields(gtx)
	addPackageClicked := view.addPackageButton.Clicked(gtx)
	confirmShipmentsClicked := view.confirmShipmentsButton.Clicked(gtx)
	if view.shipmentSubmitting {
		return
	}

	for packageIndex := range view.shipmentForm {
		shipment := view.shipmentForm[packageIndex]
		if shipment == nil {
			continue
		}
		if len(view.shipmentForm) > 1 && shipment.removeButton.Clicked(gtx) {
			view.shipmentForm = append(view.shipmentForm[:packageIndex], view.shipmentForm[packageIndex+1:]...)
			if view.carrierMenuPackage == packageIndex {
				view.carrierMenuPackage = -1
			} else if view.carrierMenuPackage > packageIndex {
				view.carrierMenuPackage--
			}
			ui.invalidate()
			return
		}
		if shipment.carrierButton.Clicked(gtx) {
			if view.carrierMenuPackage == packageIndex {
				view.carrierMenuPackage = -1
			} else {
				view.carrierMenuPackage = packageIndex
			}
			ui.invalidate()
		}
		if view.carrierMenuPackage != packageIndex {
			continue
		}
		for carrierIndex := range supportedCarriers {
			if shipment.carrierOptions[carrierIndex].Clicked(gtx) {
				shipment.carrier = supportedCarriers[carrierIndex].Value
				view.carrierMenuPackage = -1
				ui.invalidate()
				return
			}
		}
	}

	if addPackageClicked {
		view.shipmentForm = append(view.shipmentForm, newShipmentFormPackage())
		view.carrierMenuPackage = -1
		ui.invalidate()
		return
	}
	if confirmShipmentsClicked && shipmentFormIsValid(view.shipmentForm, view.orderDetail.TotalPayoutMinor) {
		ui.submitShipmentForm()
		ui.invalidate()
	}
}

// layoutShipmentForm renders every package required to create the first shipment for an otherwise unshipped order.
// It exposes a readable, alphabetically sorted carrier menu and requires every package to be complete before confirmation can be enabled.
func layoutShipmentForm(gtx layout.Context, ui *DesktopUI) layout.Dimensions {
	view := &ui.orders.view
	children := []layout.FlexChild{
		layout.Rigid(bodyText(ui.theme, "Add shipment information to confirm fulfillment.", mutedTextColor)),
	}
	for packageIndex := range view.shipmentForm {
		shipment := view.shipmentForm[packageIndex]
		if shipment == nil {
			continue
		}
		if packageIndex > 0 {
			children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout))
		}
		children = append(children,
			layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, material.H6(ui.theme, "Package "+itoa(packageIndex+1)).Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if len(view.shipmentForm) == 1 {
							return layout.Dimensions{}
						}
						return underlinedTextAction(ui.theme, &shipment.removeButton, "Remove package")(gtx)
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(material.Label(ui.theme, unit.Sp(13), "Carrier").Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return carrierSelectField(gtx, ui.theme, shipment, carrierLabel(shipment.carrier))
			}),
		)
		if view.carrierMenuPackage == packageIndex {
			children = append(children,
				layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return carrierMenu(gtx, ui.theme, shipment)
				}),
			)
		}
		children = append(children,
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
					layout.Flexed(3, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(material.Label(ui.theme, unit.Sp(13), "Tracking number").Layout),
							layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return outlinedInputField(gtx, ui.theme, &shipment.trackingNumber, "Tracking number")
							}),
						)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
					layout.Flexed(2, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(material.Label(ui.theme, unit.Sp(13), "Label cost").Layout),
							layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return dollarInputField(gtx, ui.theme, &shipment.labelCost, "0.00")
							}),
						)
					}),
				)
			}),
		)
	}
	children = append(children,
		layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(underlinedTextAction(ui.theme, &view.addPackageButton, "Add package")),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Min} }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if shipmentFormIsValid(view.shipmentForm, view.orderDetail.TotalPayoutMinor) && !view.shipmentSubmitting {
						return primaryButton(ui.theme, &view.confirmShipmentsButton, "Confirm")(gtx)
					}
					return disabledShipmentButton(ui.theme, "Confirm")(gtx)
				}),
			)
		}),
	)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// shipmentPanel groups existing or newly entered shipment information on a neutral surface that does not compete with item cards.
// child supplies the appropriate current-shipment summary or package-entry form, and the returned dimensions include consistent section padding.
func shipmentPanel(gtx layout.Context, child layout.Widget) layout.Dimensions {
	return roundedPanel(gtx, shipmentPanelBackground, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(16), Left: unit.Dp(16)}.Layout(gtx, child)
	})
}

// layoutExistingShipments renders persisted shipment values within the same neutral section used for new shipment entry.
// Carrier identifiers are uppercased for consistent visual treatment with the documented API values.
func layoutExistingShipments(gtx layout.Context, ui *DesktopUI, detail orders.Detail) layout.Dimensions {
	children := make([]layout.FlexChild, 0, len(detail.Shipments)*4)
	for index, shipment := range detail.Shipments {
		if index > 0 {
			children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout))
		}
		children = append(children,
			layout.Rigid(detailLine(ui, "Shipment", strings.ToUpper(shipment.Carrier)+" · "+shipment.ShippingType)),
			layout.Rigid(detailTrackingLine(ui, detail.OrderID, index, shipment)),
			layout.Rigid(detailLine(ui, "Maker cost", shipment.MakerCost)),
		)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// carrierLabel returns the readable display label for an API carrier value, retaining unknown text to avoid hiding an unexpected selection.
func carrierLabel(value string) string {
	for _, carrier := range supportedCarriers {
		if carrier.Value == value {
			return carrier.Label
		}
	}
	return value
}

// carrierSelectField draws a carrier dropdown trigger using the same outlined border as the Orders search field.
// shipment retains its click state and label identifies the currently selected documented carrier.
func carrierSelectField(gtx layout.Context, theme *material.Theme, shipment *shipmentFormPackage, label string) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return clickableWithPointer(gtx, &shipment.carrierButton, func(gtx layout.Context) layout.Dimensions {
		return outlinedPanel(gtx, cardBackground, panelBorderColor, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10), Right: unit.Dp(12), Bottom: unit.Dp(10), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, material.Body1(theme, label).Layout),
					layout.Rigid(material.Body1(theme, "⌄").Layout),
				)
			})
		})
	})
}

// carrierMenu draws each documented carrier as a persistent clickable option in alphabetical display order.
func carrierMenu(gtx layout.Context, theme *material.Theme, shipment *shipmentFormPackage) layout.Dimensions {
	children := make([]layout.FlexChild, 0, len(supportedCarriers))
	for carrierIndex := range supportedCarriers {
		carrierIndex := carrierIndex
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			style := material.Button(theme, &shipment.carrierOptions[carrierIndex], supportedCarriers[carrierIndex].Label)
			style.Background = cardBackground
			style.Color = mutedTextColor
			style.CornerRadius = 0
			style.Inset = layout.Inset{Top: unit.Dp(6), Right: unit.Dp(12), Bottom: unit.Dp(6), Left: unit.Dp(12)}
			return pointerCursor(gtx, style.Layout)
		}))
	}
	return outlinedPanel(gtx, cardBackground, panelBorderColor, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// dollarInputField draws a dollar-prefixed editor with the application's standard subtle outlined border.
// editor retains the user-entered decimal value, while the prefix communicates that confirmation serializes USD cents.
func dollarInputField(gtx layout.Context, theme *material.Theme, editor *widget.Editor, hint string) layout.Dimensions {
	return outlinedPanel(gtx, cardBackground, panelBorderColor, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(10), Right: unit.Dp(12), Bottom: unit.Dp(10), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(material.Body1(theme, "$").Layout),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Flexed(1, material.Editor(theme, editor, hint).Layout),
			)
		})
	})
}

// disabledShipmentButton draws a static neutral-gray confirmation affordance with no pointer target or hover state.
// label remains visible for layout continuity while the absence of a clickable region makes the unavailable state unambiguous.
func disabledShipmentButton(theme *material.Theme, label string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return roundedPanel(gtx, disabledButtonColor, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10), Right: unit.Dp(16), Bottom: unit.Dp(10), Left: unit.Dp(16)}.Layout(gtx, bodyText(theme, label, disabledButtonTextColor))
		})
	}
}

// layoutOrderItem renders one product or variant in a lightly tinted, bordered card.
// Its first two rows align the item position, product, and price labels with their values, while any Customizations remain below.
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
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					style := material.Label(ui.theme, unit.Sp(12), positionLabel)
					style.Color = mutedTextColor
					return style.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
				layout.Flexed(2, func(gtx layout.Context) layout.Dimensions {
					style := material.Label(ui.theme, unit.Sp(12), "Product / variant")
					style.Color = mutedTextColor
					return style.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					style := material.Label(ui.theme, unit.Sp(12), "Quantity / price")
					style.Color = mutedTextColor
					return style.Layout(gtx)
				}),
			)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
				layout.Flexed(1, material.H6(ui.theme, item.SKU).Layout),
				layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
				layout.Flexed(2, bodyText(ui.theme, productLabel, mutedTextColor)),
				layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
				layout.Flexed(1, bodyText(ui.theme, item.Quantity+" · "+item.Price, mutedTextColor)),
			)
		}),
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

// detailOriginalOrderIDLine renders the original order ID as the same link style used by Orders table rows.
// ui supplies the persistent navigation target, originalOrderID is the raw ID used to open the order, displayID is its formatted label or missing-value placeholder, and the returned widget preserves detail-field alignment.
func detailOriginalOrderIDLine(ui *DesktopUI, originalOrderID faire.OrderID, displayID string) layout.Widget {
	if originalOrderID == "" {
		return detailLine(ui, "Original order ID", displayID)
	}
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(unit.Dp(150))
					gtx.Constraints.Max.X = gtx.Dp(unit.Dp(150))
					return material.Label(ui.theme, unit.Sp(13), "Original order ID").Layout(gtx)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return linkLabel(gtx, ui.theme, ui.orderDetailControlFor(originalOrderID), displayID)
				}),
			)
		})
	}
}

// detailTrackingLine renders a shipment tracking number as a link only when the detail model resolved an official-carrier URL.
// ui supplies persistent click state, orderID and shipmentIndex uniquely identify the link, shipment contains display-safe text and its optional allowlisted destination, and the returned widget preserves detail-field alignment.
func detailTrackingLine(ui *DesktopUI, orderID faire.OrderID, shipmentIndex int, shipment orders.DetailShipment) layout.Widget {
	if shipment.TrackingURL == "" {
		return detailLine(ui, "Tracking", shipment.TrackingCode)
	}
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Dp(unit.Dp(150))
					gtx.Constraints.Max.X = gtx.Dp(unit.Dp(150))
					return material.Label(ui.theme, unit.Sp(13), "Tracking").Layout(gtx)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return linkLabel(gtx, ui.theme, ui.shipmentTrackingControlFor(orderID, shipmentIndex), shipment.TrackingCode)
				}),
			)
		})
	}
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

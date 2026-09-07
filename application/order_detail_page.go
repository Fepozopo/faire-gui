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

// orderItemAvailabilityColumnWidth reserves space for the longest availability action so changing its label never reflows adjacent item details.
const orderItemAvailabilityColumnWidth = unit.Dp(256)

// layoutOrderDetail renders the typed local-first Order detail screen without accepting raw snapshots or Faire API values.
// Its detail panel scrolls independently so the header controls remain available for long orders, while original-order and official-carrier tracking links and the empty-shipment form keep their own actions.
func (ui *DesktopUI) layoutOrderDetail(gtx layout.Context) layout.Dimensions {
	if itemAvailabilityVisible(ui.orders.view.orderDetail) {
		ui.handleItemAvailabilityEvents(gtx)
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
	if !ui.orders.view.availabilitySubmitting && ui.orders.view.refreshDetailButton.Clicked(gtx) {
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
				layout.Rigid(ui.layoutItemAvailabilityHeaderActions),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if ui.orders.view.availabilitySubmitting {
						return disabledPrimaryButton(ui.theme, "Refresh order")(gtx)
					}
					return primaryButton(ui.theme, &ui.orders.view.refreshDetailButton, "Refresh order")(gtx)
				}),
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

// handleItemAvailabilityEvents applies the header and item-card availability events before their controls render for this frame.
// It uses one shared control per variant so duplicated order rows always toggle together, matching Faire's variant-keyed request contract.
func (ui *DesktopUI) handleItemAvailabilityEvents(gtx layout.Context) {
	view := &ui.orders.view
	clearClicked := view.clearAvailabilityButton.Clicked(gtx)
	updateClicked := view.updateAvailabilityButton.Clicked(gtx)
	if view.availabilitySubmitting {
		seen := make(map[faire.VariantID]struct{}, len(view.orderDetail.Items))
		for _, item := range view.orderDetail.Items {
			if !item.AvailabilityEligible {
				continue
			}
			if _, duplicate := seen[item.VariantID]; duplicate {
				continue
			}
			seen[item.VariantID] = struct{}{}
			_ = view.availabilityControlFor(item.VariantID).Clicked(gtx)
		}
		return
	}
	if view.availabilityConfirmOpen || view.availabilityDiscardRefreshOpen {
		return
	}
	if clearClicked && view.hasPendingUnavailable() {
		view.resetPendingUnavailable()
		ui.invalidate()
		return
	}
	if updateClicked && view.hasPendingUnavailable() {
		view.availabilityConfirmOpen = true
		ui.invalidate()
		return
	}
	seen := make(map[faire.VariantID]struct{}, len(view.orderDetail.Items))
	for _, item := range view.orderDetail.Items {
		if !item.AvailabilityEligible {
			continue
		}
		if _, duplicate := seen[item.VariantID]; duplicate {
			continue
		}
		seen[item.VariantID] = struct{}{}
		if view.availabilityControlFor(item.VariantID).Clicked(gtx) {
			view.togglePendingUnavailable(item)
			ui.invalidate()
			return
		}
	}
}

// layoutItemAvailabilityHeaderActions renders the compact pending-draft controls that stay visible above the independently scrolling order detail.
// It deliberately hides them for shipped orders because item availability is editable only before a shipment exists.
func (ui *DesktopUI) layoutItemAvailabilityHeaderActions(gtx layout.Context) layout.Dimensions {
	view := &ui.orders.view
	if !itemAvailabilityVisible(view.orderDetail) || !view.hasPendingUnavailable() {
		return layout.Dimensions{}
	}
	updateLabel := "Update availability (" + itoa(view.pendingUnavailableCount()) + ")"
	if view.availabilitySubmitting {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(disabledPrimaryButton(ui.theme, "Clear")),
			layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
			layout.Rigid(disabledPrimaryButton(ui.theme, updateLabel)),
			layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
		)
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(primaryButton(ui.theme, &view.clearAvailabilityButton, "Clear")),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(primaryButton(ui.theme, &view.updateAvailabilityButton, updateLabel)),
		layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
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
	availabilityVisible := itemAvailabilityVisible(detail)
	children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout), layout.Rigid(material.H6(ui.theme, "Items").Layout))
	for index, item := range detail.Items {
		item, itemIndex := item, index
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layoutOrderItem(gtx, ui, item, itemIndex, len(detail.Items), availabilityVisible)
		}))
		if itemIndex < len(detail.Items)-1 {
			children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout))
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// handleShipmentFormEvents applies empty-shipment form interactions before the form is laid out for the current frame.
// It updates field validation from editor events first and drains every action event even while confirmation is disabled, preventing a prior disabled click from submitting a later valid form.
func (ui *DesktopUI) handleShipmentFormEvents(gtx layout.Context) {
	view := &ui.orders.view
	view.updateShipmentFormValidation(gtx, view.orderDetail.TotalPayoutMinor)
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
				shipment.setCarrier(supportedCarriers[carrierIndex].Value)
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
	if confirmShipmentsClicked && shipmentConfirmationAllowed(view) && shipmentFormReadyForConfirmation(view.shipmentForm, view.orderDetail.TotalPayoutMinor) && shipmentFormIsValid(view.shipmentForm, view.orderDetail.TotalPayoutMinor) {
		ui.submitShipmentForm()
		ui.invalidate()
	}
}

// layoutShipmentForm renders every package required to create the first shipment for an otherwise unshipped order.
// It exposes a readable, alphabetically sorted carrier menu, blur-based validation feedback, and enables confirmation only after every required field has a current valid result. Availability feedback uses the existing top-row space so it never changes the form height.
func layoutShipmentForm(gtx layout.Context, ui *DesktopUI) layout.Dimensions {
	view := &ui.orders.view
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, bodyText(ui.theme, "Add shipment information to confirm fulfillment.", mutedTextColor)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if shipmentConfirmationAllowed(view) {
						return layout.Dimensions{}
					}
					warning := material.Body1(ui.theme, "Resolve pending item availability changes before confirming shipment.")
					warning.Color = dangerColor
					return warning.Layout(gtx)
				}),
			)
		}),
	}
	for packageIndex := range view.shipmentForm {
		shipment := view.shipmentForm[packageIndex]
		if shipment == nil {
			continue
		}
		if packageIndex > 0 {
			children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout))
		}
		validationMessage := shipmentValidationMessage(shipment)
		children = append(children,
			layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutShipmentPackageHeader(gtx, ui, shipment, packageIndex+1, len(view.shipmentForm), validationMessage)
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
					if shipmentFormReadyForConfirmation(view.shipmentForm, view.orderDetail.TotalPayoutMinor) && !view.shipmentSubmitting && shipmentConfirmationAllowed(view) {
						return primaryButton(ui.theme, &view.confirmShipmentsButton, "Confirm")(gtx)
					}
					return disabledShipmentButton(ui.theme, "Confirm")(gtx)
				}),
			)
		}),
	)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// layoutShipmentPackageHeader renders a package number, any compact field-validation message, and the optional removal action.
// validationMessage is empty while fields are untouched or pending after edits, keeping active input visually quiet until blur.
func layoutShipmentPackageHeader(gtx layout.Context, ui *DesktopUI, shipment *shipmentFormPackage, packageNumber, packageCount int, validationMessage string) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(material.H6(ui.theme, "Package "+itoa(packageNumber)).Layout),
	}
	if validationMessage != "" {
		children = append(children,
			layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				style := material.Label(ui.theme, unit.Sp(12), validationMessage)
				style.Color = dangerColor
				style.MaxLines = 2
				return style.Layout(gtx)
			}),
		)
	} else {
		children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}))
	}
	if packageCount > 1 {
		children = append(children,
			layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
			layout.Rigid(underlinedTextAction(ui.theme, &shipment.removeButton, "Remove package")),
		)
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
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

// layoutOrderItem renders one product or variant in a bordered card with an optional pre-shipment availability action.
// Matching variants share a draft state and visual treatment because Faire accepts one availability value per variant rather than per order row.
// Visible actions use a fixed-width fourth column so their state-specific labels cannot reflow the other columns.
func layoutOrderItem(gtx layout.Context, ui *DesktopUI, item orders.DetailItem, index, total int, availabilityVisible bool) layout.Dimensions {
	positionLabel := "Item"
	if total > 1 {
		positionLabel += " " + itoa(index+1) + " of " + itoa(total)
	}
	productLabel := item.ProductName
	if item.VariantName != "" {
		productLabel += " · " + item.VariantName
	}
	selected := availabilityVisible && item.AvailabilityEligible
	if selected {
		_, selected = ui.orders.view.pendingUnavailable[item.VariantID]
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
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if !availabilityVisible {
						return layout.Dimensions{}
					}
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return orderItemAvailabilityColumn(gtx, func(gtx layout.Context) layout.Dimensions {
								style := material.Label(ui.theme, unit.Sp(12), "Availability")
								style.Color = mutedTextColor
								return style.Layout(gtx)
							})
						}),
					)
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
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if !availabilityVisible {
						return layout.Dimensions{}
					}
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return orderItemAvailabilityColumn(gtx, func(gtx layout.Context) layout.Dimensions {
								return layoutOrderItemAvailabilityAction(gtx, ui, item, true, selected)
							})
						}),
					)
				}),
			)
		}),
	}
	if availabilityVisible && !item.AvailabilityEligible {
		children = append(children,
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(bodyText(ui.theme, "Availability cannot be updated because this item is missing a variant ID or ordered quantity.", mutedTextColor)),
		)
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
	background, border := selectionBarColor, panelBorderColor
	if selected {
		background, border = unavailableDraftBackground, dangerColor
	}
	return outlinedPanel(gtx, background, border, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(14), Right: unit.Dp(14), Bottom: unit.Dp(14), Left: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

// orderItemAvailabilityColumn lays out an availability label or action at the right edge of a stable column.
// child renders the label or action, while the fixed column reserves room for the longest action label and keeps all preceding item columns aligned across availability states.
func orderItemAvailabilityColumn(gtx layout.Context, child layout.Widget) layout.Dimensions {
	width := gtx.Dp(orderItemAvailabilityColumnWidth)
	if width > gtx.Constraints.Max.X {
		width = gtx.Constraints.Max.X
	}
	gtx.Constraints.Min.X = width
	gtx.Constraints.Max.X = width
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}),
		layout.Rigid(child),
	)
}

// layoutOrderItemAvailabilityAction renders a non-interactive placeholder for ineligible or submitting items and a reversible action for eligible drafts.
// Its natural-width button is right-aligned by orderItemAvailabilityColumn, so a label change does not change the item card's column geometry.
func layoutOrderItemAvailabilityAction(gtx layout.Context, ui *DesktopUI, item orders.DetailItem, visible, selected bool) layout.Dimensions {
	if !visible {
		return layout.Dimensions{}
	}
	if !item.AvailabilityEligible {
		return disabledPrimaryButton(ui.theme, "Unavailable")(gtx)
	}
	if ui.orders.view.availabilitySubmitting {
		return disabledPrimaryButton(ui.theme, "Updating availability")(gtx)
	}
	control := ui.orders.view.availabilityControlFor(item.VariantID)
	if selected {
		return dangerButton(ui.theme, control, "Marked out of stock · Undo")(gtx)
	}
	return primaryButton(ui.theme, control, "Mark out of stock")(gtx)
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

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

// itemAvailabilityVisible reports whether an accepted order remains eligible for item-availability reporting before fulfillment begins.
// Faire availability controls are intentionally unavailable for new or shipped orders, so fulfillment details cannot change until the brand accepts the order.
func itemAvailabilityVisible(detail orders.Detail) bool {
	return detail.OrderID != "" && detail.State != faire.OrderStateNew && len(detail.Shipments) == 0
}

// shipmentCreationAllowed reports whether an order has been accepted and remains unshipped.
// Faire represents an unaccepted order with the New state, so shipment controls remain unavailable until the order enters a subsequent fulfillment state.
func shipmentCreationAllowed(detail orders.Detail) bool {
	return detail.OrderID != "" && detail.State != faire.OrderStateNew && len(detail.Shipments) == 0
}

// shipmentConfirmationAllowed reports whether an availability draft permits a shipment confirmation.
// A pending or in-flight availability request must resolve first so the user cannot submit conflicting fulfillment actions.
func shipmentConfirmationAllowed(view *ordersViewState) bool {
	return !view.hasPendingUnavailable() && !view.availabilitySubmitting
}

// availabilityControlFor returns the persistent control for one Faire variant.
// A variant-level control is intentionally shared by every matching order row because Faire's availability endpoint accepts one decision per variant ID.
func (view *ordersViewState) availabilityControlFor(variantID faire.VariantID) *widget.Clickable {
	if control, found := view.availabilityControls[variantID]; found {
		return control
	}
	control := new(widget.Clickable)
	view.availabilityControls[variantID] = control
	return control
}

// hasPendingUnavailable reports whether the open order has at least one local, unsubmitted unavailable decision.
func (view *ordersViewState) hasPendingUnavailable() bool {
	return len(view.pendingUnavailable) > 0
}

// pendingUnavailableCount returns the number of distinct variants that will be included in the next availability request.
func (view *ordersViewState) pendingUnavailableCount() int {
	return len(view.pendingUnavailable)
}

// togglePendingUnavailable adds or removes one eligible variant from the local availability draft.
// Eligibility is checked at the interaction boundary so incomplete historical snapshots cannot create an invalid Faire request.
func (view *ordersViewState) togglePendingUnavailable(item orders.DetailItem) {
	if !item.AvailabilityEligible {
		return
	}
	if _, selected := view.pendingUnavailable[item.VariantID]; selected {
		delete(view.pendingUnavailable, item.VariantID)
		return
	}
	view.pendingUnavailable[item.VariantID] = struct{}{}
}

// resetPendingUnavailable discards every local availability decision and closes availability-only dialogs.
// It is used only when an order is replaced, a user explicitly clears the draft, or Faire accepts the batch.
func (view *ordersViewState) resetPendingUnavailable() {
	clear(view.pendingUnavailable)
	view.availabilityConfirmOpen = false
	view.availabilityDiscardRefreshOpen = false
}

// availabilityRequestFromDraft converts valid selected item variants into Faire's single batched availability request.
// It returns false unless every selected variant is present in the current detail with a positive ordered quantity; Faire permits zero but not a quantity equal to or above the ordered amount.
func availabilityRequestFromDraft(items []orders.DetailItem, pending map[faire.VariantID]struct{}) (faire.UpdateOrderItemsAvailabilityRequest, bool) {
	if len(pending) == 0 {
		return faire.UpdateOrderItemsAvailabilityRequest{}, false
	}
	availabilities := make(map[faire.VariantID]faire.ItemAvailability, len(pending))
	for _, item := range items {
		if !item.AvailabilityEligible || item.VariantID == "" || item.OrderedQuantity <= 0 {
			continue
		}
		if _, selected := pending[item.VariantID]; !selected {
			continue
		}
		if _, added := availabilities[item.VariantID]; added {
			continue
		}
		zero := int64(0)
		availabilities[item.VariantID] = faire.ItemAvailability{AvailableQuantity: &zero}
	}
	if len(availabilities) != len(pending) {
		return faire.UpdateOrderItemsAvailabilityRequest{}, false
	}
	return faire.UpdateOrderItemsAvailabilityRequest{Availabilities: availabilities}, true
}

// availabilityDraftLabels returns one readable label per selected variant for the confirmation dialog.
// It deduplicates repeated order rows to match the variant-keyed Faire request exactly.
func availabilityDraftLabels(items []orders.DetailItem, pending map[faire.VariantID]struct{}) []string {
	labels := make([]string, 0, len(pending))
	seen := make(map[faire.VariantID]struct{}, len(pending))
	for _, item := range items {
		if _, selected := pending[item.VariantID]; !selected {
			continue
		}
		if _, alreadyIncluded := seen[item.VariantID]; alreadyIncluded {
			continue
		}
		seen[item.VariantID] = struct{}{}
		label := item.SKU
		if item.ProductName != "" && item.ProductName != "—" {
			if label != "" && label != "—" {
				label += " · "
			} else {
				label = ""
			}
			label += item.ProductName
		}
		if item.VariantName != "" && item.VariantName != "—" {
			label += " · " + item.VariantName
		}
		if label == "" || label == "—" {
			label = string(item.VariantID)
		}
		labels = append(labels, label)
	}
	return labels
}

// availabilityDraftSummary formats selected variant labels as a compact, readable confirmation list.
func availabilityDraftSummary(items []orders.DetailItem, pending map[faire.VariantID]struct{}) string {
	return strings.Join(availabilityDraftLabels(items, pending), "\n")
}

// layoutItemAvailabilityConfirmationModal lists the exact selected variants and starts the one allowed availability submission only after explicit confirmation.
func (ui *DesktopUI) layoutItemAvailabilityConfirmationModal(gtx layout.Context) layout.Dimensions {
	view := &ui.orders.view
	if view.cancelAvailabilityButton.Clicked(gtx) {
		view.availabilityConfirmOpen = false
		ui.invalidate()
	}
	if view.confirmAvailabilityButton.Clicked(gtx) && !view.availabilitySubmitting && view.hasPendingUnavailable() {
		ui.submitItemAvailability()
		ui.invalidate()
	}
	count := view.pendingUnavailableCount()
	title := "Report " + itoa(count) + " item"
	if count != 1 {
		title += "s"
	}
	title += " unavailable?"
	description := "Faire will receive an available quantity of 0 for each selected variant. This action cannot be undone from this screen after submission."
	labels := availabilityDraftSummary(view.orderDetail.Items, view.pendingUnavailable)
	return modalPanel(gtx, ui, title, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(bodyText(ui.theme, description, mutedTextColor)),
		}
		if labels != "" {
			children = append(children,
				layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
				layout.Rigid(material.Label(ui.theme, unit.Sp(13), "Selected variants").Layout),
				layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
				layout.Rigid(bodyText(ui.theme, labels, mutedTextColor)),
			)
		}
		children = append(children,
			layout.Rigid(layout.Spacer{Height: unit.Dp(18)}.Layout),
			layout.Rigid(dangerButton(ui.theme, &view.confirmAvailabilityButton, "Confirm update")),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &view.cancelAvailabilityButton, "Cancel")),
		)
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// layoutAvailabilityDiscardRefreshModal asks before a refresh replaces a local draft that Faire has not received.
func (ui *DesktopUI) layoutAvailabilityDiscardRefreshModal(gtx layout.Context) layout.Dimensions {
	view := &ui.orders.view
	if view.cancelDiscardRefreshButton.Clicked(gtx) {
		view.availabilityDiscardRefreshOpen = false
		ui.invalidate()
	}
	if view.confirmDiscardRefreshButton.Clicked(gtx) {
		view.resetPendingUnavailable()
		ui.refreshOrderDetailNow()
		ui.invalidate()
	}
	return modalPanel(gtx, ui, "Discard pending availability changes?", func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(bodyText(ui.theme, "Refreshing replaces the current order detail with Faire's latest data and discards the selected item availability changes.", mutedTextColor)),
			layout.Rigid(layout.Spacer{Height: unit.Dp(18)}.Layout),
			layout.Rigid(dangerButton(ui.theme, &view.confirmDiscardRefreshButton, "Discard and refresh")),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(primaryButton(ui.theme, &view.cancelDiscardRefreshButton, "Keep editing")),
		)
	})
}

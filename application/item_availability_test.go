package application

import (
	"context"
	"testing"

	"github.com/Fepozopo/faire-gui/faire"
	"github.com/Fepozopo/faire-gui/features/orders"
)

// TestAvailabilityRequestFromDraftBuildsOneZeroQuantityEntryPerVariant verifies duplicate order rows collapse into Faire's variant-keyed request.
func TestAvailabilityRequestFromDraftBuildsOneZeroQuantityEntryPerVariant(t *testing.T) {
	items := []orders.DetailItem{
		{VariantID: "variant-a", OrderedQuantity: 3, AvailabilityEligible: true},
		{VariantID: "variant-a", OrderedQuantity: 3, AvailabilityEligible: true},
		{VariantID: "variant-b", OrderedQuantity: 1, AvailabilityEligible: true},
	}
	pending := map[faire.VariantID]struct{}{"variant-a": {}, "variant-b": {}}

	request, valid := availabilityRequestFromDraft(items, pending)

	if !valid || len(request.Availabilities) != 2 {
		t.Fatalf("availability request = %#v, valid=%t, want two valid variants", request, valid)
	}
	for variantID, availability := range request.Availabilities {
		if availability.AvailableQuantity == nil || *availability.AvailableQuantity != 0 || availability.Discontinued != nil || availability.BackorderedUntil != nil {
			t.Fatalf("availability for %q = %#v, want only available_quantity: 0", variantID, availability)
		}
	}
}

// TestAvailabilityRequestFromDraftRejectsStaleOrIneligibleSelections verifies an incomplete detail cannot submit a partial or invalid availability update.
func TestAvailabilityRequestFromDraftRejectsStaleOrIneligibleSelections(t *testing.T) {
	items := []orders.DetailItem{{VariantID: "variant-a", OrderedQuantity: 2, AvailabilityEligible: true}}
	pending := map[faire.VariantID]struct{}{"variant-a": {}, "variant-missing": {}}

	if request, valid := availabilityRequestFromDraft(items, pending); valid || len(request.Availabilities) != 0 {
		t.Fatalf("availability request = %#v, valid=%t, want rejected stale selection", request, valid)
	}
}

// TestItemAvailabilityDraftIsVariantScoped verifies selecting a duplicated variant changes one shared decision without affecting other variants.
func TestItemAvailabilityDraftIsVariantScoped(t *testing.T) {
	view := newOrdersViewState()
	first := orders.DetailItem{VariantID: "variant-a", OrderedQuantity: 2, AvailabilityEligible: true}
	second := orders.DetailItem{VariantID: "variant-b", OrderedQuantity: 2, AvailabilityEligible: true}

	view.togglePendingUnavailable(first)
	view.togglePendingUnavailable(first)
	view.togglePendingUnavailable(second)

	if len(view.pendingUnavailable) != 1 {
		t.Fatalf("pending unavailable = %#v, want only variant-b", view.pendingUnavailable)
	}
	if _, found := view.pendingUnavailable["variant-b"]; !found {
		t.Fatalf("pending unavailable = %#v, want variant-b", view.pendingUnavailable)
	}
}

// TestAvailabilityAndShipmentEligibilityGuards verifies availability is limited to unshipped orders and pending work blocks shipment confirmation.
func TestAvailabilityAndShipmentEligibilityGuards(t *testing.T) {
	unshipped := orders.Detail{OrderID: "order-1"}
	shipped := orders.Detail{OrderID: "order-1", Shipments: []orders.DetailShipment{{TrackingCode: "TRACK-1"}}}
	view := newOrdersViewState()

	if !itemAvailabilityVisible(unshipped) || itemAvailabilityVisible(shipped) {
		t.Fatalf("availability visibility = {unshipped:%t shipped:%t}, want {true false}", itemAvailabilityVisible(unshipped), itemAvailabilityVisible(shipped))
	}
	if !shipmentConfirmationAllowed(&view) {
		t.Fatal("shipmentConfirmationAllowed() = false with no pending availability work, want true")
	}
	view.pendingUnavailable["variant-a"] = struct{}{}
	if shipmentConfirmationAllowed(&view) {
		t.Fatal("shipmentConfirmationAllowed() = true with a pending availability draft, want false")
	}
	view.resetPendingUnavailable()
	view.availabilitySubmitting = true
	if shipmentConfirmationAllowed(&view) {
		t.Fatal("shipmentConfirmationAllowed() = true while availability submits, want false")
	}
}

// TestDrainItemAvailabilityResultsAppliesCurrentResult verifies a successful current submission replaces detail and clears only the confirmed draft.
func TestDrainItemAvailabilityResultsAppliesCurrentResult(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.activeConnectionID = "connection-a"
	ui.orders.availabilityRequestID = 2
	ui.orders.view.orderDetailID = "order-a"
	ui.orders.view.availabilitySubmitting = true
	ui.orders.view.pendingUnavailable["variant-a"] = struct{}{}
	ui.orders.availabilityResults <- itemAvailabilityResult{RequestID: 2, ConnectionID: "connection-a", OrderID: "order-a", Detail: orders.Detail{DisplayID: "ORDER-A"}, NewOrdersCount: 3, ApplyNewOrdersCount: true}

	ui.drainItemAvailabilityResults()

	if ui.orders.view.availabilitySubmitting || ui.orders.view.orderDetail.DisplayID != "ORDER-A" || ui.orders.view.hasPendingUnavailable() || ui.orders.view.orderDetailStatus != "Item availability was updated." || ui.orders.view.newCount != 3 {
		t.Fatalf("availability result application = {submitting:%t detail:%#v pending:%#v status:%q new:%d}", ui.orders.view.availabilitySubmitting, ui.orders.view.orderDetail, ui.orders.view.pendingUnavailable, ui.orders.view.orderDetailStatus, ui.orders.view.newCount)
	}
}

// TestDrainItemAvailabilityResultsRetainsDraftOnFailure verifies retryable failures clear only the in-flight flag, not the user's selected variants.
func TestDrainItemAvailabilityResultsRetainsDraftOnFailure(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.activeConnectionID = "connection-a"
	ui.orders.availabilityRequestID = 1
	ui.orders.view.orderDetailID = "order-a"
	ui.orders.view.availabilitySubmitting = true
	ui.orders.view.pendingUnavailable["variant-a"] = struct{}{}
	ui.orders.availabilityResults <- itemAvailabilityResult{RequestID: 1, ConnectionID: "connection-a", OrderID: "order-a", Status: "Item availability could not be updated."}

	ui.drainItemAvailabilityResults()

	if ui.orders.view.availabilitySubmitting || !ui.orders.view.hasPendingUnavailable() || ui.orders.view.orderDetailStatus != "Item availability could not be updated." {
		t.Fatalf("availability failure state = {submitting:%t pending:%#v status:%q}", ui.orders.view.availabilitySubmitting, ui.orders.view.pendingUnavailable, ui.orders.view.orderDetailStatus)
	}
}

// TestDrainItemAvailabilityResultsRejectsStaleSelection verifies an obsolete completion cannot mutate the active order's draft or presentation.
func TestDrainItemAvailabilityResultsRejectsStaleSelection(t *testing.T) {
	ui := newDesktopUI(context.Background(), func() {}, nil, nil, nil, "")
	ui.activeConnectionID = "connection-b"
	ui.orders.availabilityRequestID = 2
	ui.orders.view.orderDetailID = "order-b"
	ui.orders.view.availabilitySubmitting = true
	ui.orders.view.pendingUnavailable["variant-b"] = struct{}{}
	ui.orders.view.orderDetailStatus = "Updating item availability with Faire…"
	ui.orders.availabilityResults <- itemAvailabilityResult{RequestID: 1, ConnectionID: "connection-a", OrderID: "order-a", Detail: orders.Detail{DisplayID: "STALE"}}

	ui.drainItemAvailabilityResults()

	if !ui.orders.view.availabilitySubmitting || !ui.orders.view.hasPendingUnavailable() || ui.orders.view.orderDetail.DisplayID != "" || ui.orders.view.orderDetailStatus != "Updating item availability with Faire…" {
		t.Fatalf("stale availability result changed state: %#v", ui.orders.view)
	}
}

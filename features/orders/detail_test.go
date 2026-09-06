package orders

import (
	"testing"
	"time"

	"github.com/Fepozopo/faire-gui/faire"
)

// TestPresentDetailMapsApprovedNestedOrderData verifies locally stored detail data is transformed into typed display values.
func TestPresentDetailMapsApprovedNestedOrderData(t *testing.T) {
	orderID := faire.OrderID("order-1")
	originalOrderID := faire.OrderID("bo_original-order-1")
	displayID := "ORDER-1"
	state := faire.OrderStateProcessing
	createdAt := "2026-01-02T03:04:05Z"
	shipAfter := "2026-01-05T00:00:00Z"
	requestedShipDate := "2026-01-06T00:00:00Z"
	expectedShipDate := "2026-01-07T00:00:00Z"
	updatedAt := "2026-01-03T04:05:06Z"
	firstName, lastName := "Ada", "Lovelace"
	name, address1, city := "Ada Lovelace", "1 Computing Lane", "London"
	quantity, itemPrice, commission, payout := int64(2), int64(1234), int64(250), int64(999)
	currency := "USD"
	product, variant, sku, variantID := "Widget", "Large", "SKU-1", faire.VariantID("variant-1")
	customizationType, customizationValue := "Message", "Hello\x00 world"
	carrier, tracking := "Carrier", "TRACK-1"
	notes := "Leave at desk\x00"
	salesRepName := "Grace Hopper"
	isFreeShipping := true
	freeShippingReason := faire.FreeShippingReasonThreshold
	order := faire.Order{
		ID: &orderID, OriginalOrderID: &originalOrderID, DisplayID: &displayID, State: &state, CreatedAt: &createdAt, ShipAfter: &shipAfter, RequestedShipDate: &requestedShipDate, ExpectedShipDate: &expectedShipDate, UpdatedAt: &updatedAt,
		Customer: &faire.Customer{FirstName: &firstName, LastName: &lastName}, Notes: &notes, SalesRepName: &salesRepName, IsFreeShipping: &isFreeShipping, FreeShippingReason: &freeShippingReason,
		Items:       []faire.OrderItem{{ProductName: &product, VariantName: &variant, SKU: &sku, VariantID: &variantID, Quantity: &quantity, Price: &faire.Money{AmountMinor: &itemPrice, Currency: &currency}, Customizations: []faire.Customization{{Type: &customizationType, Value: &customizationValue}}}},
		Shipments:   []faire.Shipment{{Carrier: &carrier, TrackingCode: &tracking}},
		Address:     &faire.Address{Name: &name, Address1: &address1, City: &city},
		PayoutCosts: &faire.PayoutCosts{Commission: &faire.Money{AmountMinor: &commission, Currency: &currency}, TotalPayout: &faire.Money{AmountMinor: &payout, Currency: &currency}},
	}
	detail := PresentDetail(order, time.Date(2026, 1, 4, 5, 6, 0, 0, time.UTC))
	if detail.OrderID != orderID || detail.DisplayID != displayID || detail.Status != "Processing" || detail.Customer != "Ada Lovelace" || detail.Commission != "$2.50" || detail.TotalPayout != "$9.99" || detail.TotalPayoutMinor == nil || *detail.TotalPayoutMinor != payout {
		t.Fatalf("detail = %#v", detail)
	}
	if detail.ShippingAddress.Address1 != address1 || len(detail.Items) != 1 || detail.Items[0].Quantity != "2" || detail.Items[0].Price != "$12.34" || detail.Items[0].VariantID != variantID || detail.Items[0].OrderedQuantity != quantity || !detail.Items[0].AvailabilityEligible || detail.Items[0].Customizations[0].Value != "Hello world" || len(detail.Shipments) != 1 || detail.Shipments[0].TrackingCode != tracking {
		t.Fatalf("nested detail = %#v", detail)
	}
	if detail.Notes != "Leave at desk" || detail.OriginalOrderID != originalOrderID || detail.OriginalOrderDisplayID != "ORIGINAL-ORDER-1" || detail.ShipAfter != "2026-01-05" || detail.RequestedShipDate != "2026-01-06" || detail.ExpectedShipDate != "2026-01-07" || detail.SalesRepName != "Grace Hopper" || detail.IsFreeShipping != "Yes" || detail.FreeShippingReason != "Free Shipping Threshold" || detail.UpdatedAt != "2026-01-03 04:05 UTC" || detail.SyncedAt != "2026-01-04 05:06 UTC" {
		t.Fatalf("freshness or safety fields = %#v", detail)
	}
}

// TestPresentDetailMarksItemsWithoutActionableAvailabilityDataIneligible verifies incomplete or zero-quantity items cannot create an invalid availability update.
func TestPresentDetailMarksItemsWithoutActionableAvailabilityDataIneligible(t *testing.T) {
	variantID := faire.VariantID("variant-1")
	zero := int64(0)
	detail := PresentDetail(faire.Order{Items: []faire.OrderItem{{VariantID: &variantID}, {Quantity: &zero}}}, time.Time{})
	if len(detail.Items) != 2 || detail.Items[0].AvailabilityEligible || detail.Items[1].AvailabilityEligible {
		t.Fatalf("detail items = %#v, want ineligible items", detail.Items)
	}
}

// TestOfficialTrackingURLUsesOnlyAllowlistedCarrierTrackers verifies known carrier aliases generate escaped official URLs and all other inputs remain non-navigable.
func TestOfficialTrackingURLUsesOnlyAllowlistedCarrierTrackers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		carrier  string
		tracking string
		want     string
	}{
		{name: "UPS", carrier: "United Parcel Service", tracking: "1Z 123/456", want: "https://www.ups.com/track?loc=en_US&tracknum=1Z+123%2F456"},
		{name: "FedEx", carrier: "fedex", tracking: "TRACK-1", want: "https://www.fedex.com/fedextrack/?trknbr=TRACK-1"},
		{name: "USPS", carrier: "USPS", tracking: "9400 1000", want: "https://tools.usps.com/go/TrackConfirmAction?tLabels=9400+1000"},
		{name: "DHL", carrier: "DHL Express", tracking: "JD01", want: "https://www.dhl.com/global-en/home/tracking.html?tracking-id=JD01"},
		{name: "unknown carrier", carrier: "Example Logistics", tracking: "TRACK-1", want: ""},
		{name: "empty tracking", carrier: "UPS", tracking: " \t", want: ""},
		{name: "control characters removed", carrier: "UPS", tracking: "TRACK\x00-1", want: "https://www.ups.com/track?loc=en_US&tracknum=TRACK-1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := officialTrackingURL(test.carrier, test.tracking); got != test.want {
				t.Fatalf("officialTrackingURL(%q, %q) = %q, want %q", test.carrier, test.tracking, got, test.want)
			}
		})
	}
}

// TestPresentDetailHandlesMissingOptionalFieldsAndUnknownStates verifies empty stored snapshots render safe placeholders, including a non-navigable original-order ID.
func TestPresentDetailHandlesMissingOptionalFieldsAndUnknownStates(t *testing.T) {
	unknown := faire.OrderState("ON_HOLD")
	detail := PresentDetail(faire.Order{State: &unknown}, time.Time{})
	if detail.DisplayID != "—" || detail.Status != "On Hold" || detail.OriginalOrderID != "" || detail.OriginalOrderDisplayID != "—" || detail.Customer != "—" || detail.ShippingAddress.Address1 != "—" || detail.SyncedAt != "—" {
		t.Fatalf("detail = %#v", detail)
	}
}

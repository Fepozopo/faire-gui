package orders

import (
	"testing"
	"time"

	"github.com/Fepozopo/faire-gui/faire"
)

// TestPresentDetailMapsApprovedNestedOrderData verifies locally stored detail data is transformed into typed display values.
func TestPresentDetailMapsApprovedNestedOrderData(t *testing.T) {
	orderID := faire.OrderID("order-1")
	originalOrderID := faire.OrderID("original-order-1")
	displayID := "ORDER-1"
	state := faire.OrderStateProcessing
	createdAt := "2026-01-02T03:04:05Z"
	shipAfter := "2026-01-05T00:00:00Z"
	requestedShipDate := "2026-01-06T00:00:00Z"
	expectedShipDate := "2026-01-07T00:00:00Z"
	updatedAt := "2026-01-03T04:05:06Z"
	firstName, lastName := "Ada", "Lovelace"
	name, address1, city := "Ada Lovelace", "1 Computing Lane", "London"
	quantity, commission, payout := int64(2), int64(250), int64(999)
	currency := "USD"
	product, variant, sku := "Widget", "Large", "SKU-1"
	customizationType, customizationValue := "Message", "Hello\x00 world"
	carrier, tracking := "Carrier", "TRACK-1"
	notes := "Leave at desk\x00"
	salesRepName := "Grace Hopper"
	isFreeShipping := true
	freeShippingReason := faire.FreeShippingReasonThreshold
	order := faire.Order{
		ID: &orderID, OriginalOrderID: &originalOrderID, DisplayID: &displayID, State: &state, CreatedAt: &createdAt, ShipAfter: &shipAfter, RequestedShipDate: &requestedShipDate, ExpectedShipDate: &expectedShipDate, UpdatedAt: &updatedAt,
		Customer: &faire.Customer{FirstName: &firstName, LastName: &lastName}, Notes: &notes, SalesRepName: &salesRepName, IsFreeShipping: &isFreeShipping, FreeShippingReason: &freeShippingReason,
		Items:       []faire.OrderItem{{ProductName: &product, VariantName: &variant, SKU: &sku, Quantity: &quantity, Customizations: []faire.Customization{{Type: &customizationType, Value: &customizationValue}}}},
		Shipments:   []faire.Shipment{{Carrier: &carrier, TrackingCode: &tracking}},
		Address:     &faire.Address{Name: &name, Address1: &address1, City: &city},
		PayoutCosts: &faire.PayoutCosts{Commission: &faire.Money{AmountMinor: &commission, Currency: &currency}, TotalPayout: &faire.Money{AmountMinor: &payout, Currency: &currency}},
	}
	detail := PresentDetail(order, time.Date(2026, 1, 4, 5, 6, 0, 0, time.UTC))
	if detail.OrderID != orderID || detail.DisplayID != displayID || detail.Status != "Processing" || detail.Customer != "Ada Lovelace" || detail.Commission != "USD 2.50" || detail.TotalPayout != "USD 9.99" {
		t.Fatalf("detail = %#v", detail)
	}
	if detail.ShippingAddress.Address1 != address1 || len(detail.Items) != 1 || detail.Items[0].Quantity != "2" || detail.Items[0].Customizations[0].Value != "Hello world" || len(detail.Shipments) != 1 || detail.Shipments[0].TrackingCode != tracking {
		t.Fatalf("nested detail = %#v", detail)
	}
	if detail.Notes != "Leave at desk" || detail.OriginalOrderID != "original-order-1" || detail.ShipAfter != "2026-01-05" || detail.RequestedShipDate != "2026-01-06" || detail.ExpectedShipDate != "2026-01-07" || detail.SalesRepName != "Grace Hopper" || detail.IsFreeShipping != "Yes" || detail.FreeShippingReason != "Free Shipping Threshold" || detail.UpdatedAt != "2026-01-03 04:05 UTC" || detail.SyncedAt != "2026-01-04 05:06 UTC" {
		t.Fatalf("freshness or safety fields = %#v", detail)
	}
}

// TestPresentDetailHandlesMissingOptionalFieldsAndUnknownStates verifies empty stored snapshots render safely.
func TestPresentDetailHandlesMissingOptionalFieldsAndUnknownStates(t *testing.T) {
	unknown := faire.OrderState("ON_HOLD")
	detail := PresentDetail(faire.Order{State: &unknown}, time.Time{})
	if detail.DisplayID != "—" || detail.Status != "On Hold" || detail.Customer != "—" || detail.ShippingAddress.Address1 != "—" || detail.SyncedAt != "—" {
		t.Fatalf("detail = %#v", detail)
	}
}

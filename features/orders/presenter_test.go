package orders

import (
	"testing"

	"github.com/Fepozopo/faire-gui/faire"
)

// TestPresentRowFormatsOrdersTableValues verifies the table fields use stable formatting,
// including the delivery address name in the Customer column.
func TestPresentRowFormatsOrdersTableValues(t *testing.T) {
	id := faire.OrderID("bo_123")
	displayID := "ANMQ69YVJB"
	state := faire.OrderStateInTransit
	addressName := "Ada's Antiques"
	firstName := "Ada"
	lastName := "Lovelace"
	createdAt := "2026-01-02T03:04:05Z"
	expectedShipDate := "2026-01-03T04:05:06Z"
	source := "FAIRE_DIRECT"
	quantity := int64(2)
	amount := int64(1234)
	currency := "usd"
	commission := int64(250)
	order := faire.Order{
		ID:               &id,
		DisplayID:        &displayID,
		State:            &state,
		Customer:         &faire.Customer{FirstName: &firstName, LastName: &lastName},
		CreatedAt:        &createdAt,
		ExpectedShipDate: &expectedShipDate,
		Source:           &source,
		Items:            []faire.OrderItem{{Quantity: &quantity, Price: &faire.Money{AmountMinor: &amount, Currency: &currency}}},
		PayoutCosts:      &faire.PayoutCosts{CommissionCents: &commission},
		// Both fields are present to verify the address name takes precedence in the table.
		Address:             &faire.Address{Name: &addressName, PhoneNumber: stringPointer("555-0100")},
		Notes:               stringPointer("Do not expose this"),
		PurchaseOrderNumber: stringPointer("PO-SECRET"),
	}

	row := PresentRow(order)
	want := Row{
		ID:         id,
		DisplayID:  displayID,
		Status:     "In transit",
		Customer:   addressName,
		Total:      "USD 24.68",
		OrderDate:  "2026-01-02",
		ShipDate:   "2026-01-03",
		Commission: "USD 2.50",
		Source:     source,
	}
	if row != want {
		t.Fatalf("PresentRow() = %#v, want %#v", row, want)
	}
}

// TestPresentRowHandlesOptionalData verifies missing optional fields remain safe table placeholders.
func TestPresentRowHandlesOptionalData(t *testing.T) {
	row := PresentRow(faire.Order{})
	want := Row{DisplayID: "—", Status: "—", Customer: "—", Total: "—", OrderDate: "—", ShipDate: "—", Commission: "—", Source: "—"}
	if row != want {
		t.Fatalf("PresentRow() = %#v, want %#v", row, want)
	}
}

// TestPresentRowUsesDateFallbackAndLegacyItemPrice verifies useful legacy API fields are presented consistently.
func TestPresentRowUsesDateFallbackAndLegacyItemPrice(t *testing.T) {
	requestedShipDate := "2026-04-05"
	priceCents := int64(999)
	quantity := int64(3)
	unknownState := faire.OrderState("ON_HOLD")
	row := PresentRow(faire.Order{
		State:             &unknownState,
		RequestedShipDate: &requestedShipDate,
		Items:             []faire.OrderItem{{PriceCents: &priceCents, Quantity: &quantity}},
	})
	if row.Status != "On Hold" {
		t.Fatalf("Status = %q, want %q", row.Status, "On Hold")
	}
	if row.ShipDate != requestedShipDate {
		t.Fatalf("ShipDate = %q, want %q", row.ShipDate, requestedShipDate)
	}
	if row.Total != "USD 29.97" {
		t.Fatalf("Total = %q, want %q", row.Total, "USD 29.97")
	}
}

// TestPresentRowAvoidsMixedCurrencyTotal verifies totals never add incomparable currencies.
func TestPresentRowAvoidsMixedCurrencyTotal(t *testing.T) {
	amount := int64(100)
	usd := "USD"
	eur := "EUR"
	row := PresentRow(faire.Order{Items: []faire.OrderItem{
		{Price: &faire.Money{AmountMinor: &amount, Currency: &usd}},
		{Price: &faire.Money{AmountMinor: &amount, Currency: &eur}},
	}})
	if row.Total != "—" {
		t.Fatalf("Total = %q, want placeholder", row.Total)
	}
}

// stringPointer returns a pointer for optional test-only API fields.
func stringPointer(value string) *string {
	return &value
}

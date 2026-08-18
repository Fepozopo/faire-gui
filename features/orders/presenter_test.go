package orders

import (
	"testing"

	"github.com/Fepozopo/faire-gui/faire"
)

// TestPresentRowFormatsOrdersTableValues verifies the table fields use stable formatting,
// including the delivery business name, Faire-supplied payout, and commission percentage in their respective table columns.
func TestPresentRowFormatsOrdersTableValues(t *testing.T) {
	id := faire.OrderID("bo_123")
	displayID := "ANMQ69YVJB"
	state := faire.OrderStateInTransit
	shippingRecipientName := "Ada Lovelace"
	businessName := "Ada's Antiques"
	firstName := "Ada"
	lastName := "Lovelace"
	createdAt := "2026-01-02T03:04:05Z"
	expectedShipDate := "2026-01-03T04:05:06Z"
	source := "FAIRE_DIRECT"
	quantity := int64(2)
	amount := int64(1234)
	currency := "usd"
	commission := int64(250)
	commissionBPS := int64(1500)
	payout := int64(999)
	order := faire.Order{
		ID:               &id,
		DisplayID:        &displayID,
		State:            &state,
		Customer:         &faire.Customer{FirstName: &firstName, LastName: &lastName},
		CreatedAt:        &createdAt,
		ExpectedShipDate: &expectedShipDate,
		Source:           &source,
		Items:            []faire.OrderItem{{Quantity: &quantity, Price: &faire.Money{AmountMinor: &amount, Currency: &currency}}},
		PayoutCosts:      &faire.PayoutCosts{CommissionBPS: &commissionBPS, CommissionCents: &commission, TotalPayout: &faire.Money{AmountMinor: &payout, Currency: &currency}},
		// Both fields are present to verify the business name takes precedence over the shipping recipient in the table.
		Address:             &faire.Address{Name: &shippingRecipientName, CompanyName: &businessName, PhoneNumber: stringPointer("555-0100")},
		Notes:               stringPointer("Do not expose this"),
		PurchaseOrderNumber: stringPointer("PO-SECRET"),
	}

	row := PresentRow(order)
	want := Row{
		ID:          id,
		DisplayID:   displayID,
		Status:      "In transit",
		Customer:    businessName,
		TotalPayout: "$9.99",
		OrderDate:   "2026-01-02",
		ShipDate:    "2026-01-03",
		Commission:  "15.00%",
		Source:      source,
	}
	if row != want {
		t.Fatalf("PresentRow() = %#v, want %#v", row, want)
	}
}

// TestPresentRowFallsBackToShippingRecipient verifies orders without a business name display their shipping recipient.
func TestPresentRowFallsBackToShippingRecipient(t *testing.T) {
	shippingRecipientName := "Ada Lovelace"
	row := PresentRow(faire.Order{Address: &faire.Address{Name: &shippingRecipientName}})
	if row.Customer != shippingRecipientName {
		t.Fatalf("Customer = %q, want shipping recipient %q", row.Customer, shippingRecipientName)
	}
}

// TestPresentRowHandlesOptionalData verifies missing optional fields remain safe table placeholders.
func TestPresentRowHandlesOptionalData(t *testing.T) {
	row := PresentRow(faire.Order{})
	want := Row{DisplayID: "—", Status: "—", Customer: "—", TotalPayout: "—", OrderDate: "—", ShipDate: "—", Commission: "—", Source: "—"}
	if row != want {
		t.Fatalf("PresentRow() = %#v, want %#v", row, want)
	}
}

// TestPresentRowUsesDateFallbackAndMissingTotalPayout verifies date fallbacks and missing API payout values remain safe.
func TestPresentRowUsesDateFallbackAndMissingTotalPayout(t *testing.T) {
	requestedShipDate := "2026-04-05"
	unknownState := faire.OrderState("ON_HOLD")
	row := PresentRow(faire.Order{
		State:             &unknownState,
		RequestedShipDate: &requestedShipDate,
	})
	if row.Status != "On Hold" {
		t.Fatalf("Status = %q, want %q", row.Status, "On Hold")
	}
	if row.ShipDate != requestedShipDate {
		t.Fatalf("ShipDate = %q, want %q", row.ShipDate, requestedShipDate)
	}
	if row.TotalPayout != "—" {
		t.Fatalf("TotalPayout = %q, want placeholder", row.TotalPayout)
	}
}

// TestFormatTotalUsesDollarForUSDAndCurrencyCodeOtherwise verifies the unambiguous currency formatting policy for displayed monetary values.
func TestFormatTotalUsesDollarForUSDAndCurrencyCodeOtherwise(t *testing.T) {
	amount := int64(1234)
	if value := FormatTotal(&amount, "usd"); value != "$12.34" {
		t.Fatalf("USD total = %q, want $12.34", value)
	}
	if value := FormatTotal(&amount, "EUR"); value != "EUR 12.34" {
		t.Fatalf("EUR total = %q, want EUR 12.34", value)
	}
}

// TestPresentRowUsesAPITotalPayoutDespiteMixedItemCurrencies verifies the table never derives payout from item prices.
func TestPresentRowUsesAPITotalPayoutDespiteMixedItemCurrencies(t *testing.T) {
	amount := int64(100)
	payout := int64(999)
	usd := "USD"
	eur := "EUR"
	row := PresentRow(faire.Order{
		Items: []faire.OrderItem{
			{Price: &faire.Money{AmountMinor: &amount, Currency: &usd}},
			{Price: &faire.Money{AmountMinor: &amount, Currency: &eur}},
		},
		PayoutCosts: &faire.PayoutCosts{TotalPayout: &faire.Money{AmountMinor: &payout, Currency: &usd}},
	})
	if row.TotalPayout != "$9.99" {
		t.Fatalf("TotalPayout = %q, want API payout", row.TotalPayout)
	}
}

// stringPointer returns a pointer for optional test-only API fields.
func stringPointer(value string) *string {
	return &value
}

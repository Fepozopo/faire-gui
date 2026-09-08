package orders

import (
	"testing"
	"time"

	"github.com/Fepozopo/faire-gui/faire"
)

// localDate returns timestamp's calendar date in the operating system timezone for presentation assertions.
func localDate(t *testing.T, timestamp string) string {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		t.Fatalf("parse RFC 3339 timestamp %q: %v", timestamp, err)
	}
	return parsed.In(time.Local).Format("2006-01-02")
}

// TestPresentRowFormatsOrdersTableValues verifies the table fields use stable formatting,
// including the delivery business name, order notes, Faire-supplied payout, commission percentage,
// first-order flat fee, and unformatted purchase order number in their respective table columns.
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
	commissionBPS := int64(1500)
	commissionFlatFee := int64(1000)
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
		PayoutCosts:      &faire.PayoutCosts{CommissionBPS: &commissionBPS, CommissionFlatFee: &faire.Money{AmountMinor: &commissionFlatFee, Currency: &currency}, TotalPayout: &faire.Money{AmountMinor: &payout, Currency: &currency}},
		// Both fields are present to verify the business name takes precedence over the shipping recipient in the table.
		Address:             &faire.Address{Name: &shippingRecipientName, CompanyName: &businessName, PhoneNumber: stringPointer("555-0100")},
		Notes:               stringPointer("Leave at the side entrance"),
		PurchaseOrderNumber: stringPointer("PO-SECRET"),
	}

	row := PresentRow(order)
	want := Row{
		ID:                  id,
		DisplayID:           displayID,
		Status:              "In transit",
		Customer:            businessName,
		Notes:               "Leave at the side entrance",
		TotalPayout:         "$9.99",
		OrderDate:           localDate(t, createdAt),
		ShipDate:            localDate(t, expectedShipDate),
		Commission:          "15.00% + $10.00 | First order",
		Source:              source,
		PurchaseOrderNumber: "PO-SECRET",
	}
	if row != want {
		t.Fatalf("PresentRow() = %#v, want %#v", row, want)
	}
}

// TestPresentRowRetainsPercentageWhenFlatFeeMissing verifies orders without a first-order flat fee retain the percentage-only commission label.
func TestPresentRowRetainsPercentageWhenFlatFeeMissing(t *testing.T) {
	commissionBPS := int64(1500)
	row := PresentRow(faire.Order{PayoutCosts: &faire.PayoutCosts{CommissionBPS: &commissionBPS}})
	if row.Commission != "15.00%" {
		t.Fatalf("Commission = %q, want percentage-only commission", row.Commission)
	}
}

// TestPresentRowRetainsPercentageWhenFlatFeeIsZero verifies a zero-valued flat fee is not labelled as a first order.
func TestPresentRowRetainsPercentageWhenFlatFeeIsZero(t *testing.T) {
	commissionBPS := int64(1500)
	flatFeeAmount := int64(0)
	currency := "USD"
	row := PresentRow(faire.Order{PayoutCosts: &faire.PayoutCosts{
		CommissionBPS:     &commissionBPS,
		CommissionFlatFee: &faire.Money{AmountMinor: &flatFeeAmount, Currency: &currency},
	}})
	if row.Commission != "15.00%" {
		t.Fatalf("Commission = %q, want percentage-only commission", row.Commission)
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
	want := Row{DisplayID: "—", Status: "—", Customer: "—", TotalPayout: "—", OrderDate: "—", ShipDate: "—", Commission: "—", Source: "—", PurchaseOrderNumber: "—", Notes: "—"}
	if row != want {
		t.Fatalf("PresentRow() = %#v, want %#v", row, want)
	}
}

// TestPresentRowHandlesUnknownStateAndMissingTotalPayout verifies unknown API states and missing payout values remain safe.
func TestPresentRowHandlesUnknownStateAndMissingTotalPayout(t *testing.T) {
	unknownState := faire.OrderState("ON_HOLD")
	row := PresentRow(faire.Order{State: &unknownState})
	if row.Status != "On Hold" {
		t.Fatalf("Status = %q, want %q", row.Status, "On Hold")
	}
	if row.TotalPayout != "—" {
		t.Fatalf("TotalPayout = %q, want placeholder", row.TotalPayout)
	}
}

// TestFormatDateInLocationUsesLocalCalendarDay verifies timestamps are rendered as the user's local calendar date.
func TestFormatDateInLocationUsesLocalCalendarDay(t *testing.T) {
	value := "2026-09-15T00:00:00Z"
	location := time.FixedZone("UTC-7", -7*60*60)

	if got := formatDateInLocation(&value, location); got != "2026-09-14" {
		t.Fatalf("formatDateInLocation() = %q, want the local calendar date", got)
	}
}

// TestPresentRowUsesExpectedShipDate verifies every order state displays the expected
// ship date, rather than a requested ship date, and absent expected dates use an em dash.
func TestPresentRowUsesExpectedShipDate(t *testing.T) {
	requestedShipDate := "2026-04-05T00:00:00Z"
	expectedShipDate := "2026-04-06T00:00:00Z"
	newState := faire.OrderStateNew
	processingState := faire.OrderStateProcessing
	tests := []struct {
		name  string
		order faire.Order
		want  string
	}{
		{
			name:  "new order uses expected ship date instead of requested date",
			order: faire.Order{State: &newState, RequestedShipDate: &requestedShipDate, ExpectedShipDate: &expectedShipDate},
			want:  localDate(t, expectedShipDate),
		},
		{
			name:  "new order without expected ship date uses an em dash",
			order: faire.Order{State: &newState, RequestedShipDate: &requestedShipDate},
			want:  "—",
		},
		{
			name:  "non-new order uses expected ship date instead of requested date",
			order: faire.Order{State: &processingState, RequestedShipDate: &requestedShipDate, ExpectedShipDate: &expectedShipDate},
			want:  localDate(t, expectedShipDate),
		},
		{
			name:  "non-new order without expected ship date uses an em dash",
			order: faire.Order{State: &processingState, RequestedShipDate: &requestedShipDate},
			want:  "—",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := PresentRow(test.order).ShipDate; got != test.want {
				t.Fatalf("ShipDate = %q, want %q", got, test.want)
			}
		})
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

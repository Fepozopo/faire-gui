package faire

import "testing"

// TestOrderItemsTotalUsesModernAndLegacyPrices verifies the shared subtotal keeps raw minor units and applies quantity.
func TestOrderItemsTotalUsesModernAndLegacyPrices(t *testing.T) {
	amountMinor, quantity, priceCents := int64(1234), int64(2), int64(99)
	currency := "USD"
	total, totalCurrency := OrderItemsTotal([]OrderItem{
		{Price: &Money{AmountMinor: &amountMinor, Currency: &currency}, Quantity: &quantity},
		{PriceCents: &priceCents},
	})
	if total == nil || *total != 2567 || totalCurrency != "USD" {
		t.Fatalf("OrderItemsTotal() = %v, %q; want 2567, USD", total, totalCurrency)
	}
}

// TestOrderItemsTotalRejectsMixedCurrencies verifies the shared subtotal never combines incomparable currencies.
func TestOrderItemsTotalRejectsMixedCurrencies(t *testing.T) {
	amountMinor := int64(100)
	usd, eur := "USD", "EUR"
	total, currency := OrderItemsTotal([]OrderItem{
		{Price: &Money{AmountMinor: &amountMinor, Currency: &usd}},
		{Price: &Money{AmountMinor: &amountMinor, Currency: &eur}},
	})
	if total != nil || currency != "" {
		t.Fatalf("OrderItemsTotal() = %v, %q; want nil, empty currency", total, currency)
	}
}

package faire

// OrderItemsTotal returns the raw minor-unit subtotal and common currency for items.
// It multiplies each item's unit price by its quantity, uses legacy price_cents as a USD fallback,
// and returns nil with an empty currency when no usable price exists or the items use mixed currencies.
func OrderItemsTotal(items []OrderItem) (*int64, string) {
	var total int64
	currency := ""
	found := false
	for _, item := range items {
		amountMinor, itemCurrency, foundAmount := orderItemAmount(item)
		if !foundAmount {
			continue
		}
		if currency == "" {
			currency = itemCurrency
		}
		// A subtotal across currencies would be misleading without an exchange-rate policy.
		if itemCurrency != currency {
			return nil, ""
		}
		total += amountMinor
		found = true
	}
	if !found {
		return nil, ""
	}
	return &total, currency
}

// orderItemAmount returns item's quantity-adjusted minor-unit amount and currency.
// It returns false when neither the modern Money price nor the legacy price_cents field is usable.
func orderItemAmount(item OrderItem) (int64, string, bool) {
	quantity := int64(1)
	if item.Quantity != nil {
		quantity = *item.Quantity
	}
	if item.Price != nil && item.Price.AmountMinor != nil && item.Price.Currency != nil && *item.Price.Currency != "" {
		return *item.Price.AmountMinor * quantity, *item.Price.Currency, true
	}
	if item.PriceCents != nil {
		return *item.PriceCents * quantity, "USD", true
	}
	return 0, "", false
}

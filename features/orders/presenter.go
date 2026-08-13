package orders

import (
	"fmt"
	"strings"
	"time"

	"github.com/Fepozopo/faire-gui/faire"
)

// Row is the safe, display-ready data for one Orders table row. It intentionally
// excludes personally identifying address data, notes, tracking details, and other
// raw-order fields that are not needed by the list.
type Row struct {
	ID         faire.OrderID
	DisplayID  string
	Status     string
	Customer   string
	Total      string
	OrderDate  string
	ShipDate   string
	Commission string
	Source     string
}

// PresentRows converts orders into stable table rows without retaining raw API
// objects. A new slice is always returned so a caller can safely reuse its source
// response after presentation.
func PresentRows(orders []faire.Order) []Row {
	rows := make([]Row, len(orders))
	for index, order := range orders {
		rows[index] = PresentRow(order)
	}
	return rows
}

// PresentRow converts a Faire order into non-secret table values. Missing optional
// fields use an em dash so table columns remain aligned without exposing Go pointer
// formatting or inventing data.
func PresentRow(order faire.Order) Row {
	return Row{
		ID:         orderID(order.ID),
		DisplayID:  optionalText(order.DisplayID),
		Status:     displayStatus(order.State),
		Customer:   displayCustomer(order.Customer),
		Total:      formatOrderTotal(order.Items),
		OrderDate:  formatDate(order.CreatedAt),
		ShipDate:   formatDate(firstDate(order.ExpectedShipDate, order.RequestedShipDate, order.ShipAfter)),
		Commission: formatCommission(order.PayoutCosts),
		Source:     optionalText(order.Source),
	}
}

// orderID dereferences an optional API ID without allowing a nil value to escape
// into the presentation model.
func orderID(id *faire.OrderID) faire.OrderID {
	if id == nil {
		return ""
	}
	return *id
}

// optionalText returns a display placeholder for absent or whitespace-only values.
func optionalText(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "—"
	}
	return *value
}

// displayStatus turns known API enum values into human-readable table labels while
// still showing a future, unknown API state in a deterministic readable form.
func displayStatus(state *faire.OrderState) string {
	if state == nil {
		return "—"
	}
	switch *state {
	case faire.OrderStateNew:
		return "New"
	case faire.OrderStateProcessing:
		return "Processing"
	case faire.OrderStatePreTransit:
		return "Pre-transit"
	case faire.OrderStateInTransit:
		return "In transit"
	case faire.OrderStateDelivered:
		return "Delivered"
	case faire.OrderStateCanceled:
		return "Canceled"
	case faire.OrderStateBackordered:
		return "Backordered"
	case faire.OrderStatePendingRetailerConfirmation:
		return "Pending retailer confirmation"
	default:
		return titleFromIdentifier(string(*state))
	}
}

// displayCustomer combines the safe name fields available in the API response.
func displayCustomer(customer *faire.Customer) string {
	if customer == nil {
		return "—"
	}
	parts := make([]string, 0, 2)
	if customer.FirstName != nil && strings.TrimSpace(*customer.FirstName) != "" {
		parts = append(parts, strings.TrimSpace(*customer.FirstName))
	}
	if customer.LastName != nil && strings.TrimSpace(*customer.LastName) != "" {
		parts = append(parts, strings.TrimSpace(*customer.LastName))
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " ")
}

// formatOrderTotal totals item price values. Modern Money data takes precedence;
// price_cents is used only as a legacy fallback when Money is unavailable.
func formatOrderTotal(items []faire.OrderItem) string {
	var amountMinor int64
	currency := ""
	foundAmount := false
	for _, item := range items {
		amount, itemCurrency, found := itemAmount(item)
		if !found {
			continue
		}
		if currency == "" {
			currency = itemCurrency
		}
		// Mixed currencies cannot be summed meaningfully, so omit a misleading total.
		if itemCurrency != currency {
			return "—"
		}
		amountMinor += amount
		foundAmount = true
	}
	if !foundAmount {
		return "—"
	}
	return formatMoney(amountMinor, currency)
}

// itemAmount returns a line item's monetary value multiplied by its quantity.
func itemAmount(item faire.OrderItem) (int64, string, bool) {
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

// formatCommission formats the explicit commission Money when present and falls
// back to the legacy cents field for older API responses.
func formatCommission(costs *faire.PayoutCosts) string {
	if costs == nil {
		return "—"
	}
	if costs.Commission != nil && costs.Commission.AmountMinor != nil && costs.Commission.Currency != nil && *costs.Commission.Currency != "" {
		return formatMoney(*costs.Commission.AmountMinor, *costs.Commission.Currency)
	}
	if costs.CommissionCents != nil {
		return formatMoney(*costs.CommissionCents, "USD")
	}
	return "—"
}

// formatMoney produces a locale-independent currency code and two decimal places,
// avoiding host-locale differences in snapshots and table sorting expectations.
func formatMoney(amountMinor int64, currency string) string {
	sign := ""
	if amountMinor < 0 {
		sign = "-"
		amountMinor = -amountMinor
	}
	return fmt.Sprintf("%s%s %d.%02d", sign, strings.ToUpper(currency), amountMinor/100, amountMinor%100)
}

// formatDate converts RFC 3339 timestamps to a stable date-only table label. An
// unparseable API value is retained rather than discarded, preserving debuggable
// information without risking a request or sensitive-data leak.
func formatDate(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "—"
	}
	parsed, err := time.Parse(time.RFC3339, *value)
	if err != nil {
		return *value
	}
	return parsed.Format("2006-01-02")
}

// firstDate returns the first non-empty optional date in priority order.
func firstDate(values ...*string) *string {
	for _, value := range values {
		if value != nil && strings.TrimSpace(*value) != "" {
			return value
		}
	}
	return nil
}

// titleFromIdentifier makes an unfamiliar uppercase underscore API enum readable.
func titleFromIdentifier(value string) string {
	if value == "" {
		return "—"
	}
	words := strings.Fields(strings.ReplaceAll(strings.ToLower(value), "_", " "))
	for index, word := range words {
		words[index] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

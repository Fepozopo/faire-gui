package orders

import (
	"fmt"
	"strings"
	"time"

	"github.com/Fepozopo/faire-gui/faire"
)

// Row is the display-ready data for one Orders table row. It includes the delivery
// address name and the commission percentage, while excluding other address details,
// notes, tracking details, and raw-order fields not needed by the list.
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

// PresentRow converts a Faire order into table values, including the delivery
// address name and Faire's commission percentage. Missing optional fields use an em dash so
// table columns remain aligned without exposing Go pointer formatting or inventing data.
func PresentRow(order faire.Order) Row {
	return Row{
		ID:         orderID(order.ID),
		DisplayID:  optionalText(order.DisplayID),
		Status:     displayStatus(order.State),
		Customer:   displayAddressName(order.Address),
		Total:      formatOrderTotal(order.Items),
		OrderDate:  formatDate(order.CreatedAt),
		ShipDate:   formatDate(firstDate(order.ExpectedShipDate, order.RequestedShipDate, order.ShipAfter)),
		Commission: FormatCommissionPercentage(commissionBPS(order.PayoutCosts)),
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

// displayAddressName returns the delivery address name for the Customer column.
func displayAddressName(address *faire.Address) string {
	// List responses may omit the address, so preserve the table's missing-value convention.
	if address == nil {
		return "—"
	}
	return optionalText(address.Name)
}

// displayCustomer combines the first and last name fields for order detail views.
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

// formatOrderTotal totals item price values and returns the Orders-table total label. Modern Money data takes precedence;
// price_cents is used only as a legacy fallback when Money is unavailable, and mixed currencies return the missing-value placeholder.
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
	return formatTotalAmount(amountMinor, currency)
}

// FormatTotal converts raw total minor units and currency into the Orders-table label.
// It returns the standard missing-value placeholder when either raw value is unavailable.
func FormatTotal(amountMinor *int64, currency string) string {
	if amountMinor == nil || strings.TrimSpace(currency) == "" {
		return "—"
	}
	return formatTotalAmount(*amountMinor, currency)
}

// formatTotalAmount formats amountMinor for a total, using $ for USD and an ISO currency code otherwise.
// It returns the resulting signed total label.
func formatTotalAmount(amountMinor int64, currency string) string {
	sign := ""
	if amountMinor < 0 {
		sign, amountMinor = "-", -amountMinor
	}
	if strings.EqualFold(currency, "USD") {
		return fmt.Sprintf("%s$%d.%02d", sign, amountMinor/100, amountMinor%100)
	}
	return fmt.Sprintf("%s%s %d.%02d", sign, strings.ToUpper(currency), amountMinor/100, amountMinor%100)
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

// FormatCommissionPercentage converts Faire's raw commission BPS to an Orders-table percentage.
// It returns the standard missing-value placeholder when bps is unavailable.
func FormatCommissionPercentage(bps *int64) string {
	if bps == nil {
		return "—"
	}
	return formatPercentageFromBPS(*bps)
}

// commissionBPS returns a copy of the raw commission_bps value in costs for table presentation.
func commissionBPS(costs *faire.PayoutCosts) *int64 {
	if costs == nil || costs.CommissionBPS == nil {
		return nil
	}
	value := *costs.CommissionBPS
	return &value
}

// formatCommissionAmount formats the explicit commission Money in costs for the order detail page.
// It falls back to the legacy cents field and returns the standard missing-value placeholder when neither is present.
func formatCommissionAmount(costs *faire.PayoutCosts) string {
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

// formatPercentageFromBPS converts value basis points to a signed percentage with two decimal places.
// It returns the resulting percentage label.
func formatPercentageFromBPS(value int64) string {
	sign := ""
	if value < 0 {
		sign, value = "-", -value
	}
	return fmt.Sprintf("%s%d.%02d%%", sign, value/100, value%100)
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

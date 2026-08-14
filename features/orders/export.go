package orders

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Fepozopo/faire-gui/faire"
)

// CSVHeader defines the stable column order for every exported order CSV file.
var CSVHeader = []string{
	"id", "display_id", "created_at", "ship_after",
	"address_name", "address_address1", "address_address2", "address_postal_code",
	"address_city", "address_state", "address_state_code", "address_phone_number",
	"address_country", "address_country_code", "address_company_name",
	"is_free_shipping", "brand_discounts_includes_free_shipping", "brand_discounts_discount_percentage",
	"payout_costs_commission_bps", "payout_costs_commission_cents",
	"item_sku", "item_price_cents", "item_quantity", "sale_source", "sales_rep_name", "notes",
}

// WriteCSV writes orders as a CSV with CSVHeader's column order. Each item becomes
// one row so item-specific SKU, price, and quantity values remain associated with
// their order. Orders without items produce one row with blank item fields.
func WriteCSV(writer io.Writer, source []faire.Order) error {
	csvWriter := csv.NewWriter(writer)
	if err := csvWriter.Write(CSVHeader); err != nil {
		return err
	}
	for _, order := range source {
		if len(order.Items) == 0 {
			if err := csvWriter.Write(csvRow(order, nil)); err != nil {
				return err
			}
			continue
		}
		for index := range order.Items {
			if err := csvWriter.Write(csvRow(order, &order.Items[index])); err != nil {
				return err
			}
		}
	}
	csvWriter.Flush()
	return csvWriter.Error()
}

// csvRow returns the CSV values for one order and, when present, one order item.
// It normalizes dates, money, percentages, and source values to the established CSV contract.
func csvRow(order faire.Order, item *faire.OrderItem) []string {
	return []string{
		stringValue(order.ID),
		stringValue(order.DisplayID),
		dateValue(order.CreatedAt),
		dateValue(order.ShipAfter),
		addressValue(order.Address, func(address *faire.Address) *string { return address.Name }),
		addressValue(order.Address, func(address *faire.Address) *string { return address.Address1 }),
		addressValue(order.Address, func(address *faire.Address) *string { return address.Address2 }),
		addressValue(order.Address, func(address *faire.Address) *string { return address.PostalCode }),
		addressValue(order.Address, func(address *faire.Address) *string { return address.City }),
		addressValue(order.Address, func(address *faire.Address) *string { return address.State }),
		addressValue(order.Address, func(address *faire.Address) *string { return address.StateCode }),
		addressValue(order.Address, func(address *faire.Address) *string { return address.PhoneNumber }),
		addressValue(order.Address, func(address *faire.Address) *string { return address.Country }),
		addressValue(order.Address, func(address *faire.Address) *string { return address.CountryCode }),
		addressValue(order.Address, func(address *faire.Address) *string { return address.CompanyName }),
		boolValue(order.IsFreeShipping),
		discountFreeShippingValues(order.BrandDiscounts),
		discountPercentageValues(order.BrandDiscounts),
		payoutCommissionBPS(order.PayoutCosts),
		payoutCommissionCents(order.PayoutCosts),
		itemSKU(item),
		itemPrice(item),
		itemQuantity(item),
		strings.ToUpper(stringValue(order.Source)),
		stringValue(order.SalesRepName),
		stringValue(order.Notes),
	}
}

// addressValue returns a requested address field while keeping missing addresses blank.
func addressValue(address *faire.Address, selectField func(*faire.Address) *string) string {
	if address == nil {
		return ""
	}
	return stringValue(selectField(address))
}

// discountFreeShippingValues joins all present discount values so a multi-discount order loses no data in a single CSV row.
func discountFreeShippingValues(discounts []faire.Discount) string {
	values := make([]string, 0, len(discounts))
	for _, discount := range discounts {
		if discount.IncludesFreeShipping != nil {
			values = append(values, strconv.FormatBool(*discount.IncludesFreeShipping))
		}
	}
	return strings.Join(values, ",")
}

// discountPercentageValues joins all present percentage values so a multi-discount order loses no data in a single CSV row.
func discountPercentageValues(discounts []faire.Discount) string {
	values := make([]string, 0, len(discounts))
	for _, discount := range discounts {
		if discount.DiscountPercentage != nil {
			values = append(values, strconv.FormatFloat(*discount.DiscountPercentage, 'f', -1, 64))
		}
	}
	return strings.Join(values, ",")
}

// payoutCommissionBPS returns Faire's basis-point commission as a percentage with two decimal places.
func payoutCommissionBPS(costs *faire.PayoutCosts) string {
	if costs == nil || costs.CommissionBPS == nil {
		return ""
	}
	return fmt.Sprintf("%.2f", float64(*costs.CommissionBPS)*0.01)
}

// payoutCommissionCents returns Faire's cent-denominated commission as a decimal amount with two decimal places.
func payoutCommissionCents(costs *faire.PayoutCosts) string {
	if costs == nil || costs.CommissionCents == nil {
		return ""
	}
	return fmt.Sprintf("%.2f", float64(*costs.CommissionCents)/100.0)
}

// itemSKU returns an item's SKU or a blank value for an order without items.
func itemSKU(item *faire.OrderItem) string {
	if item == nil {
		return ""
	}
	return stringValue(item.SKU)
}

// itemPrice returns an item's cent-denominated price as a decimal amount with two decimal places.
// It uses Money's minor amount only when the legacy PriceCents field is unavailable.
func itemPrice(item *faire.OrderItem) string {
	if item == nil {
		return ""
	}
	if item.PriceCents != nil {
		return fmt.Sprintf("%.2f", float64(*item.PriceCents)/100.0)
	}
	if item.Price == nil || item.Price.AmountMinor == nil {
		return ""
	}
	return fmt.Sprintf("%.2f", float64(*item.Price.AmountMinor)/100.0)
}

// itemQuantity returns an item's quantity or a blank value for an order without items.
func itemQuantity(item *faire.OrderItem) string {
	if item == nil {
		return ""
	}
	return int64Value(item.Quantity)
}

// dateValue converts Faire's RFC 3339 timestamp or date-only value to the established YYYYMMDD CSV format.
// A malformed API date is blanked rather than exported in an inconsistent format.
func dateValue(value *string) string {
	if value == nil || *value == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		parsed, err := time.Parse(layout, *value)
		if err == nil {
			return parsed.Format("20060102")
		}
	}
	return ""
}

// stringValue dereferences a string-like value or returns a blank CSV field.
func stringValue[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

// boolValue formats an optional boolean or returns a blank CSV field.
func boolValue(value *bool) string {
	if value == nil {
		return ""
	}
	return strconv.FormatBool(*value)
}

// int64Value formats an optional integer or returns a blank CSV field.
func int64Value(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

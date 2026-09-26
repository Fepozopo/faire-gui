package orders

import (
	"bytes"
	"encoding/csv"
	"reflect"
	"testing"

	"github.com/Fepozopo/faire-gui/faire"
)

// TestWriteCSVUsesStableHeaderAndOneRowPerItem verifies exports preserve every item, free-shipping reason, and the specified column order.
func TestWriteCSVUsesStableHeaderAndOneRowPerItem(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	order := faire.Order{
		ID:                 faire.Ptr(faire.OrderID("order-1")),
		DisplayID:          faire.Ptr("ABCD123456"),
		CreatedAt:          faire.Ptr("2026-01-02T03:04:05Z"),
		ShipAfter:          faire.Ptr("2026-01-04T00:00:00Z"),
		Address:            &faire.Address{Name: faire.Ptr("Ada Retailer"), Address1: faire.Ptr("1 Main St"), City: faire.Ptr("London")},
		IsFreeShipping:     faire.Ptr(true),
		FreeShippingReason: faire.Ptr(faire.FreeShippingReasonThreshold),
		BrandDiscounts: []faire.Discount{
			{IncludesFreeShipping: faire.Ptr(true), DiscountPercentage: faire.Ptr(10.5)},
			{IncludesFreeShipping: faire.Ptr(false), DiscountPercentage: faire.Ptr(5.0)},
		},
		PayoutCosts:  &faire.PayoutCosts{CommissionBPS: faire.Ptr(int64(1500)), Commission: &faire.Money{AmountMinor: faire.Ptr(int64(425))}, TotalPayout: &faire.Money{AmountMinor: faire.Ptr(int64(7650))}},
		Source:       faire.Ptr("FAIRE_MARKETPLACE"),
		SalesRepName: faire.Ptr("Sam"),
		Notes:        faire.Ptr("Leave at loading bay"),
		Items: []faire.OrderItem{
			{SKU: faire.Ptr("SKU-1"), Price: &faire.Money{AmountMinor: faire.Ptr(int64(1200))}, Quantity: faire.Ptr(int64(2))},
			{SKU: faire.Ptr("SKU-2"), Price: &faire.Money{AmountMinor: faire.Ptr(int64(3400))}, Quantity: faire.Ptr(int64(1))},
		},
	}
	if err := WriteCSV(&output, SalesSource("ASC"), []faire.Order{order}, true); err != nil {
		t.Fatalf("WriteCSV() error = %v", err)
	}

	rows, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	wantHeader := []string{
		"id", "display_id", "created_at", "ship_after",
		"address_name", "address_address1", "address_address2", "address_postal_code",
		"address_city", "address_state", "address_state_code", "address_phone_number",
		"address_country", "address_country_code", "address_company_name",
		"is_free_shipping", "brand_discounts_includes_free_shipping", "brand_discounts_discount_percentage",
		"payout_costs_commission_bps", "payout_costs_commission",
		"item_sku", "item_price", "item_quantity", "sale_source", "sales_rep_name", "notes", "payout_costs_total_payout", "free_shipping_reason",
	}
	if !reflect.DeepEqual(rows[0], wantHeader) {
		t.Fatalf("header = %#v, want %#v", rows[0], wantHeader)
	}
	if len(rows) != 3 {
		t.Fatalf("row count = %d, want header plus two items", len(rows))
	}
	wantRows := [][]string{
		{"order-1", "ABCD123456", "20260102", "20260104", "Ada Retailer", "1 Main St", "", "", "London", "", "", "", "", "", "", "true", "true,false", "10.5,5", "15.00", "4.25", "SKU-1", "12.00", "2", "ASC", "Sam", "Leave at loading bay", "76.50", "FREE_SHIPPING_THRESHOLD"},
		{"order-1", "ABCD123456", "20260102", "20260104", "Ada Retailer", "1 Main St", "", "", "London", "", "", "", "", "", "", "true", "true,false", "10.5,5", "15.00", "4.25", "SKU-2", "34.00", "1", "ASC", "Sam", "Leave at loading bay", "76.50", "FREE_SHIPPING_THRESHOLD"},
	}
	for index, want := range wantRows {
		if got := rows[index+1]; !reflect.DeepEqual(got, want) {
			t.Fatalf("item row %d = %#v, want %#v", index+1, got, want)
		}
	}
}

// TestWriteCSVOmitsHeaderWhenRequested verifies headerless exports preserve their first order row.
func TestWriteCSVOmitsHeaderWhenRequested(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := WriteCSV(&output, SalesSource("ASC"), []faire.Order{{ID: faire.Ptr(faire.OrderID("order-1"))}}, false); err != nil {
		t.Fatalf("WriteCSV() error = %v", err)
	}
	rows, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(rows) != 1 || rows[0][0] != "order-1" {
		t.Fatalf("rows = %#v, want one headerless order row", rows)
	}
}

// TestSalesSourceForBrandReturnsOnlyConfiguredBrandMappings verifies every supported brand produces its required source.
func TestSalesSourceForBrandReturnsOnlyConfiguredBrandMappings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		brandID faire.BrandID
		want    SalesSource
		found   bool
	}{
		{name: "21", brandID: "b_wpz8vfrdu5", want: "21", found: true},
		{name: "ASC", brandID: "b_56pfaass", want: "ASC", found: true},
		{name: "BJP", brandID: "b_22rl4c1962", want: "BJP", found: true},
		{name: "BSC", brandID: "b_9yilp4yy", want: "BSC", found: true},
		{name: "GTG", brandID: "b_53p4jwgf6g", want: "GTG", found: true},
		{name: "OAT", brandID: "b_amtnu83oc0", want: "OAT", found: true},
		{name: "SM", brandID: "b_ukhf47wscj", want: "SM", found: true},
		{name: "unmapped", brandID: "b_unmapped"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, found := SalesSourceForBrand(test.brandID); found != test.found || got != test.want {
				t.Fatalf("SalesSourceForBrand(%q) = (%q, %t), want (%q, %t)", test.brandID, got, found, test.want, test.found)
			}
		})
	}
}

// TestDateValueFormatsOnlySupportedDates verifies exports never emit raw or malformed API date values.
func TestDateValueFormatsOnlySupportedDates(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		value *string
		want  string
	}{
		{name: "RFC 3339", value: faire.Ptr("2026-01-02T03:04:05Z"), want: "20260102"},
		{name: "date only", value: faire.Ptr("2026-01-02"), want: "20260102"},
		{name: "invalid", value: faire.Ptr("not-a-date"), want: ""},
		{name: "missing", want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := dateValue(test.value); got != test.want {
				t.Fatalf("dateValue(%#v) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

// TestWriteCSVWritesBlankItemFieldsForOrdersWithoutItems verifies incomplete API responses still produce an order export row.
func TestWriteCSVWritesBlankItemFieldsForOrdersWithoutItems(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := WriteCSV(&output, SalesSource("ASC"), []faire.Order{{ID: faire.Ptr(faire.OrderID("order-1"))}}, true); err != nil {
		t.Fatalf("WriteCSV() error = %v", err)
	}

	rows, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("row count = %d, want header plus one order row", len(rows))
	}
	itemFieldIndexes := map[string]int{"item_sku": 20, "item_price": 21, "item_quantity": 22}
	for field, index := range itemFieldIndexes {
		if got := rows[1][index]; got != "" {
			t.Fatalf("%s = %q, want blank for an order without items", field, got)
		}
	}
	if got := rows[1][0]; got != "order-1" {
		t.Fatalf("order ID = %q, want order-1", got)
	}
}

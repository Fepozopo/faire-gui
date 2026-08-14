package orders

import (
	"bytes"
	"encoding/csv"
	"reflect"
	"testing"

	"github.com/Fepozopo/faire-gui/faire"
)

// TestWriteCSVUsesStableHeaderAndOneRowPerItem verifies exports preserve every item and the specified column order.
func TestWriteCSVUsesStableHeaderAndOneRowPerItem(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	order := faire.Order{
		ID:             faire.Ptr(faire.OrderID("order-1")),
		DisplayID:      faire.Ptr("ABCD123456"),
		CreatedAt:      faire.Ptr("2026-01-02T03:04:05Z"),
		ShipAfter:      faire.Ptr("2026-01-04T00:00:00Z"),
		Address:        &faire.Address{Name: faire.Ptr("Ada Retailer"), Address1: faire.Ptr("1 Main St"), City: faire.Ptr("London")},
		IsFreeShipping: faire.Ptr(true),
		BrandDiscounts: []faire.Discount{
			{IncludesFreeShipping: faire.Ptr(true), DiscountPercentage: faire.Ptr(10.5)},
			{IncludesFreeShipping: faire.Ptr(false), DiscountPercentage: faire.Ptr(5.0)},
		},
		PayoutCosts:  &faire.PayoutCosts{CommissionBPS: faire.Ptr(int64(1500)), CommissionCents: faire.Ptr(int64(425))},
		Source:       faire.Ptr("FAIRE_MARKETPLACE"),
		SalesRepName: faire.Ptr("Sam"),
		Notes:        faire.Ptr("Leave at loading bay"),
		Items: []faire.OrderItem{
			{SKU: faire.Ptr("SKU-1"), PriceCents: faire.Ptr(int64(1200)), Quantity: faire.Ptr(int64(2))},
			{SKU: faire.Ptr("SKU-2"), Price: &faire.Money{AmountMinor: faire.Ptr(int64(3400))}, Quantity: faire.Ptr(int64(1))},
		},
	}
	if err := WriteCSV(&output, []faire.Order{order}); err != nil {
		t.Fatalf("WriteCSV() error = %v", err)
	}

	rows, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !reflect.DeepEqual(rows[0], CSVHeader) {
		t.Fatalf("header = %#v, want %#v", rows[0], CSVHeader)
	}
	if len(rows) != 3 {
		t.Fatalf("row count = %d, want header plus two items", len(rows))
	}
	if got, want := rows[1], []string{"order-1", "ABCD123456", "20260102", "20260104", "Ada Retailer", "1 Main St", "", "", "London", "", "", "", "", "", "", "true", "true,false", "10.5,5", "15.00", "4.25", "SKU-1", "12.00", "2", "FAIRE_MARKETPLACE", "Sam", "Leave at loading bay"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first item row = %#v, want %#v", got, want)
	}
	if got := rows[2][21]; got != "34.00" {
		t.Fatalf("second item price = %q, want formatted Money fallback", got)
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
	if err := WriteCSV(&output, []faire.Order{{ID: faire.Ptr(faire.OrderID("order-1"))}}); err != nil {
		t.Fatalf("WriteCSV() error = %v", err)
	}

	rows, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(rows) != 2 || rows[1][0] != "order-1" || rows[1][20] != "" || rows[1][21] != "" || rows[1][22] != "" {
		t.Fatalf("rows = %#v, want an order row with blank item fields", rows)
	}
}

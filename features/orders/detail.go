package orders

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Fepozopo/faire-gui/faire"
)

// Detail is the display-ready, read-only representation of one locally stored Order.
// It intentionally contains approved text fields only and never exposes a raw API object or serialized snapshot to layout code.
type Detail struct {
	OrderID             faire.OrderID
	DisplayID           string
	Status              string
	CreatedAt           string
	UpdatedAt           string
	SyncedAt            string
	Customer            string
	Source              string
	PurchaseOrderNumber string
	Notes               string
	Items               []DetailItem
	Shipments           []DetailShipment
	ShippingAddress     DetailAddress
	Commission          string
	TotalPayout         string
	IsFreeShipping      string
	PendingCancellation string
	FulfilledByFaire    string
}

// DetailItem is the display-ready subset of one ordered product or variant.
type DetailItem struct {
	ProductName    string
	VariantName    string
	SKU            string
	Quantity       string
	Price          string
	Status         string
	Customizations []DetailCustomization
}

// DetailCustomization is one approved retailer-provided item customization value.
type DetailCustomization struct {
	Type  string
	Value string
}

// DetailShipment is the display-ready shipping and tracking information for one order shipment.
type DetailShipment struct {
	Carrier      string
	TrackingCode string
	ShippingType string
	MakerCost    string
	Status       string
}

// DetailAddress is the approved shipping-address representation for the detail screen.
type DetailAddress struct {
	Name        string
	CompanyName string
	Address1    string
	Address2    string
	City        string
	State       string
	PostalCode  string
	Country     string
	PhoneNumber string
}

// PresentDetail converts one typed Order and its local synchronization time into safe display-ready detail values.
func PresentDetail(order faire.Order, syncedAt time.Time) Detail {
	detail := Detail{
		OrderID:             orderID(order.ID),
		DisplayID:           safeDetailText(optionalText(order.DisplayID)),
		Status:              displayStatus(order.State),
		CreatedAt:           formatDate(order.CreatedAt),
		UpdatedAt:           formatDateTime(order.UpdatedAt),
		SyncedAt:            formatSyncedAt(syncedAt),
		Source:              safeDetailText(optionalText(order.Source)),
		PurchaseOrderNumber: safeDetailText(optionalText(order.PurchaseOrderNumber)),
		Notes:               safeMultilineDetailText(optionalText(order.Notes)),
		Items:               presentDetailItems(order.Items),
		Shipments:           presentDetailShipments(order.Shipments),
		ShippingAddress:     presentDetailAddress(order.Address),
		Commission:          formatCommissionAmount(order.PayoutCosts),
		IsFreeShipping:      detailBoolean(order.IsFreeShipping),
		PendingCancellation: detailBoolean(order.HasPendingRetailerCancellationRequest),
		FulfilledByFaire:    detailBoolean(order.IsFulfilledByFaire),
	}
	if order.Customer != nil {
		detail.Customer = safeDetailText(displayCustomer(order.Customer))
	} else {
		detail.Customer = "—"
	}
	if order.PayoutCosts != nil && order.PayoutCosts.TotalPayout != nil && order.PayoutCosts.TotalPayout.AmountMinor != nil && order.PayoutCosts.TotalPayout.Currency != nil {
		detail.TotalPayout = formatMoney(*order.PayoutCosts.TotalPayout.AmountMinor, *order.PayoutCosts.TotalPayout.Currency)
	} else {
		detail.TotalPayout = "—"
	}
	return detail
}

// presentDetailItems maps each stored order item without retaining the raw API item in presentation state.
func presentDetailItems(items []faire.OrderItem) []DetailItem {
	presented := make([]DetailItem, len(items))
	for index, item := range items {
		quantity := "—"
		if item.Quantity != nil {
			quantity = itoaDetail(*item.Quantity)
		}
		price := "—"
		if item.Price != nil && item.Price.AmountMinor != nil && item.Price.Currency != nil {
			price = formatMoney(*item.Price.AmountMinor, *item.Price.Currency)
		} else if item.PriceCents != nil {
			price = formatMoney(*item.PriceCents, "USD")
		}
		presented[index] = DetailItem{
			ProductName:    safeDetailText(optionalText(item.ProductName)),
			VariantName:    safeDetailText(optionalText(item.VariantName)),
			SKU:            safeDetailText(optionalText(item.SKU)),
			Quantity:       quantity,
			Price:          price,
			Status:         displayItemStatus(item.State),
			Customizations: presentDetailCustomizations(item.Customizations),
		}
	}
	return presented
}

// presentDetailCustomizations maps approved customization labels and values while stripping unsafe control characters.
func presentDetailCustomizations(customizations []faire.Customization) []DetailCustomization {
	presented := make([]DetailCustomization, 0, len(customizations))
	for _, customization := range customizations {
		presented = append(presented, DetailCustomization{Type: safeDetailText(optionalText(customization.Type)), Value: safeDetailText(optionalText(customization.Value))})
	}
	return presented
}

// presentDetailShipments maps approved shipment and tracking data from the stored snapshot.
func presentDetailShipments(shipments []faire.Shipment) []DetailShipment {
	presented := make([]DetailShipment, len(shipments))
	for index, shipment := range shipments {
		cost := "—"
		if shipment.MakerCost != nil && shipment.MakerCost.AmountMinor != nil && shipment.MakerCost.Currency != nil {
			cost = formatMoney(*shipment.MakerCost.AmountMinor, *shipment.MakerCost.Currency)
		} else if shipment.MakerCostCents != nil {
			cost = formatMoney(*shipment.MakerCostCents, "USD")
		}
		shippingType := "—"
		if shipment.ShippingType != nil {
			shippingType = titleFromIdentifier(string(*shipment.ShippingType))
		}
		presented[index] = DetailShipment{
			Carrier:      safeDetailText(optionalText(shipment.Carrier)),
			TrackingCode: safeDetailText(optionalText(shipment.TrackingCode)),
			ShippingType: shippingType,
			MakerCost:    cost,
			Status:       formatDate(shipment.UpdatedAt),
		}
	}
	return presented
}

// presentDetailAddress maps the approved stored shipping-address fields with safe placeholders.
func presentDetailAddress(address *faire.Address) DetailAddress {
	if address == nil {
		return DetailAddress{Name: "—", CompanyName: "—", Address1: "—", Address2: "—", City: "—", State: "—", PostalCode: "—", Country: "—", PhoneNumber: "—"}
	}
	return DetailAddress{
		Name:        safeDetailText(optionalText(address.Name)),
		CompanyName: safeDetailText(optionalText(address.CompanyName)),
		Address1:    safeDetailText(optionalText(address.Address1)),
		Address2:    safeDetailText(optionalText(address.Address2)),
		City:        safeDetailText(optionalText(address.City)),
		State:       safeDetailText(optionalText(firstNonEmpty(address.State, address.StateCode))),
		PostalCode:  safeDetailText(optionalText(address.PostalCode)),
		Country:     safeDetailText(optionalText(firstNonEmpty(address.Country, address.CountryCode))),
		PhoneNumber: safeDetailText(optionalText(address.PhoneNumber)),
	}
}

// displayItemStatus converts known and future item states into readable labels.
func displayItemStatus(state *faire.OrderItemState) string {
	if state == nil {
		return "—"
	}
	return titleFromIdentifier(string(*state))
}

// detailBoolean formats optional booleans without making a missing API field look false.
func detailBoolean(value *bool) string {
	if value == nil {
		return "—"
	}
	if *value {
		return "Yes"
	}
	return "No"
}

// formatDateTime converts optional timestamps to a stable UTC freshness label.
func formatDateTime(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "—"
	}
	parsed, err := time.Parse(time.RFC3339Nano, *value)
	if err != nil {
		return "—"
	}
	return parsed.UTC().Format("2006-01-02 15:04 UTC")
}

// formatSyncedAt formats a local synchronization timestamp or returns a placeholder when unavailable.
func formatSyncedAt(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.UTC().Format("2006-01-02 15:04 UTC")
}

// firstNonEmpty returns the first non-blank optional string as a presentation placeholder-compatible value.
func firstNonEmpty(values ...*string) *string {
	for _, value := range values {
		if value != nil && strings.TrimSpace(*value) != "" {
			return value
		}
	}
	return nil
}

// safeDetailText removes control characters that could disrupt a single-line detail label.
func safeDetailText(value string) string {
	if value == "—" {
		return value
	}
	return strings.TrimSpace(strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, value))
}

// safeMultilineDetailText retains line breaks for approved notes while removing other control characters.
func safeMultilineDetailText(value string) string {
	if value == "—" {
		return value
	}
	return strings.TrimSpace(strings.Map(func(character rune) rune {
		if unicode.IsControl(character) && character != '\n' && character != '\t' {
			return -1
		}
		return character
	}, value))
}

// itoaDetail formats a quantity without exposing API numeric pointers to layout.
func itoaDetail(value int64) string {
	return strconv.FormatInt(value, 10)
}

package orders

import (
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Fepozopo/faire-gui/faire"
)

// Detail is the display-ready, read-only representation of one locally stored Order.
// It contains approved text fields, navigation identifiers, and the exact total-payout cents needed to validate a new shipment's label cost without exposing a raw API object or serialized snapshot to layout code.
type Detail struct {
	OrderID                faire.OrderID
	DisplayID              string
	Status                 string
	OriginalOrderID        faire.OrderID
	OriginalOrderDisplayID string
	CreatedAt              string
	ShipAfter              string
	RequestedShipDate      string
	ExpectedShipDate       string
	UpdatedAt              string
	SyncedAt               string
	Customer               string
	Source                 string
	PurchaseOrderNumber    string
	SalesRepName           string
	Notes                  string
	Items                  []DetailItem
	Shipments              []DetailShipment
	ShippingAddress        DetailAddress
	Commission             string
	TotalPayout            string
	TotalPayoutMinor       *int64
	IsFreeShipping         string
	FreeShippingReason     string
	PendingCancellation    string
	FulfilledByFaire       string
}

// DetailItem is the display-ready subset of one ordered product or variant.
// VariantID and OrderedQuantity retain the minimum validated identity and quantity needed to report an item unavailable without exposing a raw API object to layout code.
type DetailItem struct {
	ProductName          string
	VariantName          string
	SKU                  string
	Quantity             string
	Price                string
	VariantID            faire.VariantID
	OrderedQuantity      int64
	AvailabilityEligible bool
	Customizations       []DetailCustomization
}

// DetailCustomization is one approved retailer-provided item customization value.
type DetailCustomization struct {
	Type  string
	Value string
}

// DetailShipment is the display-ready shipping and tracking information for one order shipment.
// TrackingURL is an allowlisted official-carrier destination and is empty when the carrier or tracking code cannot be safely resolved.
type DetailShipment struct {
	Carrier      string
	TrackingCode string
	TrackingURL  string
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

// PresentDetail converts order and its local synchronization time into safe display-ready detail values, including lineage, scheduling, sales-representative, and free-shipping data.
// It returns a Detail containing only approved presentation fields.
func PresentDetail(order faire.Order, syncedAt time.Time) Detail {
	detail := Detail{
		OrderID:                orderID(order.ID),
		DisplayID:              safeDetailText(optionalText(order.DisplayID)),
		Status:                 displayStatus(order.State),
		OriginalOrderID:        orderID(order.OriginalOrderID),
		OriginalOrderDisplayID: detailOrderID(order.OriginalOrderID),
		CreatedAt:              formatDate(order.CreatedAt),
		ShipAfter:              formatDate(order.ShipAfter),
		RequestedShipDate:      formatDate(order.RequestedShipDate),
		ExpectedShipDate:       formatDate(order.ExpectedShipDate),
		UpdatedAt:              formatDateTime(order.UpdatedAt),
		SyncedAt:               formatSyncedAt(syncedAt),
		Source:                 safeDetailText(optionalText(order.Source)),
		PurchaseOrderNumber:    safeDetailText(optionalText(order.PurchaseOrderNumber)),
		SalesRepName:           safeDetailText(optionalText(order.SalesRepName)),
		Notes:                  safeMultilineDetailText(optionalText(order.Notes)),
		Items:                  presentDetailItems(order.Items),
		Shipments:              presentDetailShipments(order.Shipments),
		ShippingAddress:        presentDetailAddress(order.Address),
		Commission:             formatCommissionAmount(order.PayoutCosts),
		IsFreeShipping:         detailBoolean(order.IsFreeShipping),
		FreeShippingReason:     detailFreeShippingReason(order.FreeShippingReason),
		PendingCancellation:    detailBoolean(order.HasPendingRetailerCancellationRequest),
		FulfilledByFaire:       detailBoolean(order.IsFulfilledByFaire),
	}
	if order.Customer != nil {
		detail.Customer = safeDetailText(displayCustomer(order.Customer))
	} else {
		detail.Customer = "—"
	}
	if order.PayoutCosts != nil && order.PayoutCosts.TotalPayout != nil && order.PayoutCosts.TotalPayout.AmountMinor != nil && order.PayoutCosts.TotalPayout.Currency != nil {
		amountMinor := *order.PayoutCosts.TotalPayout.AmountMinor
		detail.TotalPayoutMinor = &amountMinor
		detail.TotalPayout = formatMoney(amountMinor, *order.PayoutCosts.TotalPayout.Currency)
	} else {
		detail.TotalPayout = "—"
	}
	return detail
}

// detailOrderID formats an optional original-order ID for display like an Orders table ID.
// It removes Faire's internal bo_ prefix and uppercases the remaining safe text, returning the missing-value placeholder when the ID is absent or becomes empty after control-character removal.
func detailOrderID(value *faire.OrderID) string {
	if value == nil {
		return "—"
	}
	id := safeDetailText(string(*value))
	if id == "" {
		return "—"
	}
	id, _ = strings.CutPrefix(id, "bo_")
	return strings.ToUpper(id)
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
		}
		variantID := faire.VariantID("")
		if item.VariantID != nil {
			variantID = *item.VariantID
		}
		orderedQuantity := int64(0)
		if item.Quantity != nil {
			orderedQuantity = *item.Quantity
		}
		presented[index] = DetailItem{
			ProductName:          safeDetailText(optionalText(item.ProductName)),
			VariantName:          safeDetailText(optionalText(item.VariantName)),
			SKU:                  safeDetailText(optionalText(item.SKU)),
			Quantity:             quantity,
			Price:                price,
			VariantID:            variantID,
			OrderedQuantity:      orderedQuantity,
			AvailabilityEligible: variantID != "" && orderedQuantity > 0,
			Customizations:       presentDetailCustomizations(item.Customizations),
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
		}
		shippingType := "—"
		if shipment.ShippingType != nil {
			shippingType = titleFromIdentifier(string(*shipment.ShippingType))
		}
		presented[index] = DetailShipment{
			Carrier:      safeDetailText(optionalText(shipment.Carrier)),
			TrackingCode: safeDetailText(optionalText(shipment.TrackingCode)),
			TrackingURL:  officialTrackingURL(optionalTextValue(shipment.Carrier), optionalTextValue(shipment.TrackingCode)),
			ShippingType: shippingType,
			MakerCost:    cost,
			Status:       formatDate(shipment.UpdatedAt),
		}
	}
	return presented
}

// presentDetailAddress maps the approved stored shipping-address fields with safe placeholders.
// officialTrackingURL returns the HTTPS URL for an allowlisted carrier's official tracker.
// It removes control characters and safely escapes the tracking code; unsupported carriers and empty values deliberately return an empty URL so the UI does not forward shipment data to a third party.
func officialTrackingURL(carrier, trackingCode string) string {
	trackingCode = strings.TrimSpace(strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, trackingCode))
	if trackingCode == "" {
		return ""
	}

	carrier = strings.Join(strings.Fields(strings.ToLower(carrier)), " ")
	trackingCode = url.QueryEscape(trackingCode)
	switch carrier {
	case "ups", "united parcel service":
		return "https://www.ups.com/track?loc=en_US&tracknum=" + trackingCode
	case "fedex", "federal express":
		return "https://www.fedex.com/fedextrack/?trknbr=" + trackingCode
	case "usps", "united states postal service":
		return "https://tools.usps.com/go/TrackConfirmAction?tLabels=" + trackingCode
	case "dhl", "dhl express":
		return "https://www.dhl.com/global-en/home/tracking.html?tracking-id=" + trackingCode
	default:
		return ""
	}
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

// detailFreeShippingReason formats an optional Faire free-shipping enum as a readable reason.
// It returns the missing-value placeholder when the API did not provide a reason.
func detailFreeShippingReason(value *faire.FreeShippingReason) string {
	if value == nil {
		return "—"
	}
	return titleFromIdentifier(string(*value))
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

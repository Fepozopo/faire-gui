package application

import (
	"math"
	"strconv"
	"strings"

	"github.com/Fepozopo/faire-gui/faire"
)

const (
	// supportedCarrierCount is the fixed number of carrier choices Faire documents for shipment creation.
	supportedCarrierCount = 18
	// shipmentCurrency is the USD currency code paired with the dollar-denominated label-cost field.
	shipmentCurrency = "USD"
)

// supportedCarrier is one API carrier value paired with its readable menu label.
// Value is sent to Faire while Label is shown in alphabetical dropdown order.
type supportedCarrier struct {
	Value string
	Label string
}

// supportedCarriers contains every documented carrier in alphabetical label order.
// Keeping API values separate from labels preserves Faire's wire format while presenting readable text to users.
var supportedCarriers = [supportedCarrierCount]supportedCarrier{
	{Value: "AUSTRALIA_POST", Label: "Australia Post"},
	{Value: "CANADA_POST", Label: "Canada Post"},
	{Value: "CANPAR", Label: "Canpar"},
	{Value: "DHL_ECOMMERCE", Label: "DHL eCommerce"},
	{Value: "DHL_EXPRESS", Label: "DHL Express"},
	{Value: "DPD", Label: "DPD"},
	{Value: "DPDUK", Label: "DPD UK"},
	{Value: "EVRI", Label: "Evri"},
	{Value: "FEDEX", Label: "FedEx"},
	{Value: "GSO", Label: "GSO"},
	{Value: "INTERLINK_EXPRESS", Label: "Interlink Express"},
	{Value: "LA_POSTE", Label: "La Poste"},
	{Value: "PARCELFORCE", Label: "Parcelforce"},
	{Value: "POSTNL", Label: "PostNL"},
	{Value: "PUROLATOR", Label: "Purolator"},
	{Value: "ROYAL_MAIL", Label: "Royal Mail"},
	{Value: "UPS", Label: "UPS"},
	{Value: "USPS", Label: "USPS"},
}

// shipmentFormIsValid reports whether every visible package has a carrier, tracking number, and non-negative dollar amount.
// Submission deliberately requires all packages to be valid, preventing an added but incomplete package from being silently ignored.
func shipmentFormIsValid(packages []*shipmentFormPackage) bool {
	if len(packages) == 0 {
		return false
	}
	for _, shipment := range packages {
		if shipment == nil || strings.TrimSpace(shipment.carrier) == "" || strings.TrimSpace(shipment.trackingNumber.Text()) == "" {
			return false
		}
		if _, valid := parseDollarAmount(shipment.labelCost.Text()); !valid {
			return false
		}
	}
	return true
}

// shipmentRequestFromForm converts fully validated package controls into Faire's batch shipment request.
// It returns false instead of a partial request when any package is invalid so callers cannot submit only a subset of visible packages.
func shipmentRequestFromForm(packages []*shipmentFormPackage) (faire.AddShipmentsRequest, bool) {
	if !shipmentFormIsValid(packages) {
		return faire.AddShipmentsRequest{}, false
	}

	request := faire.AddShipmentsRequest{Shipments: make([]faire.Shipment, 0, len(packages))}
	for _, shipment := range packages {
		amountMinor, _ := parseDollarAmount(shipment.labelCost.Text())
		carrier := strings.TrimSpace(shipment.carrier)
		trackingNumber := strings.TrimSpace(shipment.trackingNumber.Text())
		currency := shipmentCurrency
		shippingType := faire.ShippingTypeShipOnYourOwn
		request.Shipments = append(request.Shipments, faire.Shipment{
			Carrier:      &carrier,
			TrackingCode: &trackingNumber,
			MakerCost:    &faire.Money{AmountMinor: &amountMinor, Currency: &currency},
			ShippingType: &shippingType,
		})
	}
	return request, true
}

// parseDollarAmount converts a non-negative dollar value with at most two decimal places into integer cents.
// It accepts an optional dollar sign because pasted accounting values commonly include one, and it rejects overflow rather than rounding money.
func parseDollarAmount(value string) (int64, bool) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "$"))
	if value == "" || strings.HasPrefix(value, "-") {
		return 0, false
	}

	whole, fraction, hasFraction := strings.Cut(value, ".")
	if strings.Count(value, ".") > 1 || (whole == "" && !hasFraction) || (whole == "" && fraction == "") || len(fraction) > 2 {
		return 0, false
	}
	if whole == "" {
		whole = "0"
	}
	if fraction == "" {
		fraction = "00"
	} else if len(fraction) == 1 {
		fraction += "0"
	}

	dollars, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || dollars < 0 || dollars > math.MaxInt64/100 {
		return 0, false
	}
	cents, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil || cents < 0 || cents > 99 || dollars == math.MaxInt64/100 && cents > math.MaxInt64%100 {
		return 0, false
	}
	return dollars*100 + cents, true
}

// resetShipmentForm replaces all unsubmitted package controls with one blank UPS package and closes any carrier menu.
func (view *ordersViewState) resetShipmentForm() {
	view.shipmentForm = []*shipmentFormPackage{newShipmentFormPackage()}
	view.carrierMenuPackage = -1
}

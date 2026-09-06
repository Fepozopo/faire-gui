package application

import (
	"math"
	"strconv"
	"strings"
	"unicode"

	"gioui.org/layout"

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

// shipmentFormIsValid reports whether every visible package has a supported valid tracking number and a cost strictly below half the total payout.
// Submission deliberately requires all packages to be valid, preventing an added but incomplete or invalid package from being silently ignored.
func shipmentFormIsValid(packages []*shipmentFormPackage, totalPayoutMinor *int64) bool {
	if len(packages) == 0 || totalPayoutMinor == nil || *totalPayoutMinor <= 0 {
		return false
	}
	for _, shipment := range packages {
		if shipment == nil || strings.TrimSpace(shipment.carrier) == "" || !validTrackingNumber(shipment.carrier, shipment.trackingNumber.Text()) {
			return false
		}
		amountMinor, valid := parseDollarAmount(shipment.labelCost.Text())
		if !valid || !labelCostBelowHalfPayout(amountMinor, *totalPayoutMinor) {
			return false
		}
	}
	return true
}

// shipmentRequestFromForm converts fully validated package controls into Faire's batch shipment request.
// It returns false instead of a partial request when any package is invalid so callers cannot submit only a subset of visible packages.
func shipmentRequestFromForm(packages []*shipmentFormPackage, totalPayoutMinor *int64) (faire.AddShipmentsRequest, bool) {
	if !shipmentFormIsValid(packages, totalPayoutMinor) {
		return faire.AddShipmentsRequest{}, false
	}

	request := faire.AddShipmentsRequest{Shipments: make([]faire.Shipment, 0, len(packages))}
	for _, shipment := range packages {
		amountMinor, _ := parseDollarAmount(shipment.labelCost.Text())
		carrier := strings.TrimSpace(shipment.carrier)
		trackingNumber := normalizedTrackingNumber(carrier, shipment.trackingNumber.Text())
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

// normalizeShipmentFormBlurredFields writes canonical tracking numbers and dollar amounts back to editors when they lose focus.
// This makes the displayed values match the request payload without interrupting a user while they are still typing.
func (view *ordersViewState) normalizeShipmentFormBlurredFields(gtx layout.Context) {
	for _, shipment := range view.shipmentForm {
		if shipment == nil {
			continue
		}
		trackingFocused := gtx.Focused(&shipment.trackingNumber)
		if shipment.trackingFocused && !trackingFocused {
			shipment.trackingNumber.SetText(normalizedTrackingNumber(shipment.carrier, shipment.trackingNumber.Text()))
		}
		shipment.trackingFocused = trackingFocused

		labelCostFocused := gtx.Focused(&shipment.labelCost)
		if shipment.labelCostFocused && !labelCostFocused {
			if amountMinor, valid := parseDollarAmount(shipment.labelCost.Text()); valid {
				shipment.labelCost.SetText(formatDollarAmount(amountMinor))
			}
		}
		shipment.labelCostFocused = labelCostFocused
	}
}

// validTrackingNumber applies the selected carrier's rules after removing all whitespace and dashes.
// Only UPS, FedEx, and USPS have strict documented validation; other carriers remain valid when their normalized tracking number is non-empty.
func validTrackingNumber(carrier, value string) bool {
	value = normalizedTrackingNumber(carrier, value)
	switch carrier {
	case "UPS":
		return len(value) == 18 && strings.HasPrefix(value, "1Z") && asciiAlphanumeric(value)
	case "FEDEX":
		return (len(value) == 12 || len(value) == 14 || len(value) == 15 || len(value) == 20 || len(value) == 22) && asciiDigits(value)
	case "USPS":
		if (len(value) == 20 || len(value) == 22) && asciiDigits(value) {
			return true
		}
		return len(value) == 13 && asciiLetters(value[:2]) && asciiDigits(value[2:11]) && asciiLetters(value[11:])
	default:
		return value != ""
	}
}

// normalizedTrackingNumber removes user-friendly separators and uppercases carriers whose valid tracking numbers may include letters.
// The returned value is both validated and sent to Faire, ensuring API input matches the user-visible canonical form after blur.
func normalizedTrackingNumber(carrier, value string) string {
	value = strings.Map(func(character rune) rune {
		if character == '-' || unicode.IsSpace(character) {
			return -1
		}
		return character
	}, value)
	if carrier == "UPS" || carrier == "USPS" {
		return strings.ToUpper(value)
	}
	return value
}

// asciiAlphanumeric reports whether value contains only ASCII letters and digits.
func asciiAlphanumeric(value string) bool {
	for index := range len(value) {
		character := value[index]
		if !((character >= '0' && character <= '9') || (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z')) {
			return false
		}
	}
	return true
}

// asciiDigits reports whether value contains only ASCII digits.
func asciiDigits(value string) bool {
	for index := range len(value) {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

// asciiLetters reports whether value contains only ASCII letters.
func asciiLetters(value string) bool {
	for index := range len(value) {
		character := value[index]
		if !((character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z')) {
			return false
		}
	}
	return true
}

// labelCostBelowHalfPayout reports whether amountMinor is strictly less than half of the positive payout total.
// Integer arithmetic avoids floating-point rounding and correctly handles odd-cent payouts, such as allowing 499 cents for a 999-cent payout.
func labelCostBelowHalfPayout(amountMinor, totalPayoutMinor int64) bool {
	return totalPayoutMinor > 0 && amountMinor >= 0 && amountMinor <= (totalPayoutMinor-1)/2
}

// shipmentValidationMessage returns compact, field-specific feedback for a package only after the user entered an invalid value.
// Blank untouched fields intentionally produce no message so a new form does not look erroneous before the user begins entering shipment data.
func shipmentValidationMessage(shipment *shipmentFormPackage, totalPayoutMinor *int64) string {
	if shipment == nil {
		return ""
	}
	messages := make([]string, 0, 2)
	if message := trackingValidationMessage(shipment.carrier, shipment.trackingNumber.Text()); message != "" {
		messages = append(messages, message)
	}
	if message := labelCostValidationMessage(shipment.labelCost.Text(), totalPayoutMinor); message != "" {
		messages = append(messages, message)
	}
	return strings.Join(messages, " • ")
}

// trackingValidationMessage describes an invalid typed tracking value for carriers with strict documented rules.
// It returns no message for blank input or carriers that do not yet have a specified validation format.
func trackingValidationMessage(carrier, value string) string {
	if strings.TrimSpace(value) == "" || validTrackingNumber(carrier, value) {
		return ""
	}
	switch carrier {
	case "UPS":
		return "Tracking: use 18 letters/numbers beginning 1Z"
	case "FEDEX":
		return "Tracking: use 12, 14, 15, 20, or 22 digits"
	case "USPS":
		return "Tracking: use 20/22 digits or 2 letters, 9 digits, 2 letters"
	default:
		return ""
	}
}

// labelCostValidationMessage describes a malformed or over-limit typed label cost without warning for a blank untouched field.
func labelCostValidationMessage(value string, totalPayoutMinor *int64) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	amountMinor, valid := parseDollarAmount(value)
	if !valid {
		return "Label cost: enter a valid dollar amount"
	}
	if totalPayoutMinor == nil || *totalPayoutMinor <= 0 {
		return "Label cost: payout unavailable"
	}
	if !labelCostBelowHalfPayout(amountMinor, *totalPayoutMinor) {
		return "Label cost: must be less than 50% of payout"
	}
	return ""
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

// formatDollarAmount converts non-negative cents into a two-decimal dollar string for a blurred label-cost field.
func formatDollarAmount(amountMinor int64) string {
	return strconv.FormatInt(amountMinor/100, 10) + "." + strconv.FormatInt(amountMinor%100+100, 10)[1:]
}

// resetShipmentForm replaces all unsubmitted package controls with one blank UPS package and closes any carrier menu.
func (view *ordersViewState) resetShipmentForm() {
	view.shipmentForm = []*shipmentFormPackage{newShipmentFormPackage()}
	view.carrierMenuPackage = -1
}

package application

import (
	"math"
	"strconv"
	"strings"
	"unicode"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/Fepozopo/faire-gui/faire"
)

const (
	// supportedCarrierCount is the fixed number of carrier choices Faire documents for shipment creation.
	supportedCarrierCount = 3
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
//
// Commented-out carriers are not yet supported by this application,
// but they remain in the source code to preserve the original alphabetical order and to simplify future expansion.
var supportedCarriers = [supportedCarrierCount]supportedCarrier{
	// {Value: "AUSTRALIA_POST", Label: "Australia Post"},
	// {Value: "CANADA_POST", Label: "Canada Post"},
	// {Value: "CANPAR", Label: "Canpar"},
	// {Value: "DHL_ECOMMERCE", Label: "DHL eCommerce"},
	// {Value: "DHL_EXPRESS", Label: "DHL Express"},
	// {Value: "DPD", Label: "DPD"},
	// {Value: "DPDUK", Label: "DPD UK"},
	// {Value: "EVRI", Label: "Evri"},
	{Value: "FEDEX", Label: "FedEx"},
	// {Value: "GSO", Label: "GSO"},
	// {Value: "INTERLINK_EXPRESS", Label: "Interlink Express"},
	// {Value: "LA_POSTE", Label: "La Poste"},
	// {Value: "PARCELFORCE", Label: "Parcelforce"},
	// {Value: "POSTNL", Label: "PostNL"},
	// {Value: "PUROLATOR", Label: "Purolator"},
	// {Value: "ROYAL_MAIL", Label: "Royal Mail"},
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

// shipmentFormReadyForConfirmation reports whether every package has a current successful blur or carrier-change validation result.
// It intentionally reads cached results instead of rechecking editor text during layout, keeping Confirm disabled until edited fields are validated again.
func shipmentFormReadyForConfirmation(packages []*shipmentFormPackage, totalPayoutMinor *int64) bool {
	if len(packages) == 0 || totalPayoutMinor == nil || *totalPayoutMinor <= 0 {
		return false
	}
	for _, shipment := range packages {
		if shipment == nil || strings.TrimSpace(shipment.carrier) == "" || !shipment.trackingValidation.valid || !shipment.labelCostValidation.valid || !shipment.labelCostValidationMatchesPayout(totalPayoutMinor) {
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

// updateShipmentFormValidation records user edits, validates completed fields on blur, and refreshes label-cost results when the payout changes.
// Change events clear stale feedback immediately, while blur and carrier selection are the only normal paths that evaluate typed values.
func (view *ordersViewState) updateShipmentFormValidation(gtx layout.Context, totalPayoutMinor *int64) {
	for _, shipment := range view.shipmentForm {
		if shipment == nil {
			continue
		}

		trackingChanged := shipmentEditorChanged(gtx, &shipment.trackingNumber)
		trackingFocused := gtx.Focused(&shipment.trackingNumber)
		if trackingChanged {
			shipment.clearTrackingValidation()
		}
		if shipment.trackingFocused && !trackingFocused {
			shipment.validateTracking()
		}
		shipment.trackingFocused = trackingFocused

		labelCostChanged := shipmentEditorChanged(gtx, &shipment.labelCost)
		labelCostFocused := gtx.Focused(&shipment.labelCost)
		if labelCostChanged {
			shipment.clearLabelCostValidation()
		}
		if shipment.labelCostFocused && !labelCostFocused {
			shipment.validateLabelCost(totalPayoutMinor)
		} else if !labelCostFocused && shipment.labelCostValidation.completed && !shipment.labelCostValidationMatchesPayout(totalPayoutMinor) {
			shipment.validateLabelCost(totalPayoutMinor)
		}
		shipment.labelCostFocused = labelCostFocused
	}
}

// shipmentEditorChanged drains one editor's Gio events and reports whether the user changed its text.
// Draining events before layout keeps cached validation state synchronized with input while discarding unrelated editor events the shipment form does not act on.
func shipmentEditorChanged(gtx layout.Context, editor *widget.Editor) bool {
	changed := false
	for {
		event, ok := editor.Update(gtx)
		if !ok {
			return changed
		}
		if _, changedEvent := event.(widget.ChangeEvent); changedEvent {
			changed = true
		}
	}
}

// clearTrackingValidation marks tracking as pending after the user edits it and removes any obsolete feedback.
func (shipment *shipmentFormPackage) clearTrackingValidation() {
	shipment.trackingValidation = shipmentFieldValidation{}
}

// clearLabelCostValidation marks label cost as pending after the user edits it and removes any obsolete feedback.
func (shipment *shipmentFormPackage) clearLabelCostValidation() {
	shipment.labelCostValidation = shipmentFieldValidation{}
}

// validateTracking normalizes and validates a nonblank tracking number using the package's currently selected carrier.
// Blank values deliberately remain message-free but invalid, allowing the form to stay quiet until the user supplies required input.
func (shipment *shipmentFormPackage) validateTracking() {
	value := shipment.trackingNumber.Text()
	if strings.TrimSpace(value) == "" {
		shipment.trackingValidation = shipmentFieldValidation{completed: true}
		return
	}

	value = normalizedTrackingNumber(shipment.carrier, value)
	shipment.trackingNumber.SetText(value)
	message := trackingValidationMessage(shipment.carrier, value)
	shipment.trackingValidation = shipmentFieldValidation{completed: true, valid: message == "", message: message}
}

// validateLabelCost formats a valid nonblank amount and evaluates it against the payout snapshot used by this order detail.
// Blank values deliberately remain message-free but invalid, and the recorded payout allows later order-detail refreshes to invalidate stale results.
func (shipment *shipmentFormPackage) validateLabelCost(totalPayoutMinor *int64) {
	shipment.labelCostValidationPayoutAvailable = totalPayoutMinor != nil && *totalPayoutMinor > 0
	if shipment.labelCostValidationPayoutAvailable {
		shipment.labelCostValidationPayoutMinor = *totalPayoutMinor
	}

	value := shipment.labelCost.Text()
	if strings.TrimSpace(value) == "" {
		shipment.labelCostValidation = shipmentFieldValidation{completed: true}
		return
	}

	if amountMinor, valid := parseDollarAmount(value); valid {
		shipment.labelCost.SetText(formatDollarAmount(amountMinor))
	}
	message := labelCostValidationMessage(shipment.labelCost.Text(), totalPayoutMinor)
	shipment.labelCostValidation = shipmentFieldValidation{completed: true, valid: message == "", message: message}
}

// labelCostValidationMatchesPayout reports whether a cached label-cost result was evaluated against the currently displayed payout.
// A mismatched payout cannot enable Confirm because the allowed cost limit is derived from that value.
func (shipment *shipmentFormPackage) labelCostValidationMatchesPayout(totalPayoutMinor *int64) bool {
	payoutAvailable := totalPayoutMinor != nil && *totalPayoutMinor > 0
	return shipment.labelCostValidationPayoutAvailable == payoutAvailable && (!payoutAvailable || shipment.labelCostValidationPayoutMinor == *totalPayoutMinor)
}

// setCarrier changes the package carrier and immediately revalidates a supplied tracking number against the new carrier's rules.
// An empty tracking field stays quiet and invalid, while a nonblank one receives timely carrier-specific feedback without waiting for another blur.
func (shipment *shipmentFormPackage) setCarrier(carrier string) {
	shipment.carrier = carrier
	if strings.TrimSpace(shipment.trackingNumber.Text()) == "" {
		shipment.clearTrackingValidation()
		return
	}
	shipment.validateTracking()
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

// shipmentValidationMessage returns compact feedback from completed field validations for one package.
// It does not inspect editor text, so active edits clear stale feedback without revalidating until blur or an applicable carrier change.
func shipmentValidationMessage(shipment *shipmentFormPackage) string {
	if shipment == nil {
		return ""
	}
	messages := make([]string, 0, 2)
	if message := shipment.trackingValidation.message; message != "" {
		messages = append(messages, message)
	}
	if message := shipment.labelCostValidation.message; message != "" {
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

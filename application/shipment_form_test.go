package application

import (
	"strings"
	"testing"

	"github.com/Fepozopo/faire-gui/faire"
)

// TestParseDollarAmount verifies the label-cost field converts exact dollar amounts to cents without floating-point rounding.
func TestParseDollarAmount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  int64
		valid bool
	}{
		{name: "whole dollars", input: "13", want: 1300, valid: true},
		{name: "two decimal places", input: "13.50", want: 1350, valid: true},
		{name: "one decimal place", input: "13.5", want: 1350, valid: true},
		{name: "pasted dollar sign", input: " $13.50 ", want: 1350, valid: true},
		{name: "zero", input: "0", want: 0, valid: true},
		{name: "missing", input: "", valid: false},
		{name: "negative", input: "-1.00", valid: false},
		{name: "too many decimals", input: "13.501", valid: false},
		{name: "non numeric", input: "thirteen", valid: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, valid := parseDollarAmount(test.input)
			if valid != test.valid || got != test.want {
				t.Fatalf("parseDollarAmount(%q) = (%d, %t), want (%d, %t)", test.input, got, valid, test.want, test.valid)
			}
		})
	}
}

// TestShipmentFormRequiresEveryVisiblePackage verifies an incomplete added package disables confirmation instead of being omitted.
func TestShipmentFormRequiresEveryVisiblePackage(t *testing.T) {
	t.Parallel()

	payoutMinor := int64(10000)
	first := completeShipmentFormPackage("UPS", "1Z999AA10123456784", "13.50")
	second := newShipmentFormPackage()
	packages := []*shipmentFormPackage{first, second}
	if shipmentFormIsValid(packages, &payoutMinor) {
		t.Fatal("shipmentFormIsValid() = true with an incomplete added package, want false")
	}

	second.carrier = "CANADA_POST"
	second.trackingNumber.SetText("9400")
	second.labelCost.SetText("4.25")
	if !shipmentFormIsValid(packages, &payoutMinor) {
		t.Fatal("shipmentFormIsValid() = false with every package complete, want true")
	}
}

// TestShipmentRequestFromFormBuildsBatch verifies a valid form produces Faire's documented carrier values and exact USD money.
func TestShipmentRequestFromFormBuildsBatch(t *testing.T) {
	t.Parallel()

	payoutMinor := int64(10000)
	packages := []*shipmentFormPackage{
		completeShipmentFormPackage("UPS", "1z999-aa10 123456784", "13.50"),
		completeShipmentFormPackage("CANADA_POST", "940011", "4"),
	}
	request, valid := shipmentRequestFromForm(packages, &payoutMinor)
	if !valid {
		t.Fatal("shipmentRequestFromForm() valid = false, want true")
	}
	if len(request.Shipments) != 2 {
		t.Fatalf("shipment count = %d, want 2", len(request.Shipments))
	}
	first, second := request.Shipments[0], request.Shipments[1]
	if first.Carrier == nil || *first.Carrier != "UPS" || first.TrackingCode == nil || *first.TrackingCode != "1Z999AA10123456784" {
		t.Fatalf("first shipment = %#v, want UPS tracking data", first)
	}
	if first.MakerCost == nil || first.MakerCost.AmountMinor == nil || *first.MakerCost.AmountMinor != 1350 || first.MakerCost.Currency == nil || *first.MakerCost.Currency != shipmentCurrency {
		t.Fatalf("first maker cost = %#v, want USD 1350 cents", first.MakerCost)
	}
	if first.ShippingType == nil || *first.ShippingType != faire.ShippingTypeShipOnYourOwn {
		t.Fatalf("first shipping type = %v, want %s", first.ShippingType, faire.ShippingTypeShipOnYourOwn)
	}
	if second.MakerCost == nil || second.MakerCost.AmountMinor == nil || *second.MakerCost.AmountMinor != 400 {
		t.Fatalf("second maker cost = %#v, want 400 cents", second.MakerCost)
	}
}

// TestTrackingValidationAndNormalization verifies each documented strict-carrier rule and the canonical API value.
func TestTrackingValidationAndNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		carrier    string
		input      string
		valid      bool
		normalized string
	}{
		{name: "UPS lower case with separators", carrier: "UPS", input: "1z999-aa10 123456784", valid: true, normalized: "1Z999AA10123456784"},
		{name: "UPS wrong prefix", carrier: "UPS", input: "2Z999AA10123456784", valid: false, normalized: "2Z999AA10123456784"},
		{name: "UPS non alphanumeric", carrier: "UPS", input: "1Z999AA1012345678!", valid: false, normalized: "1Z999AA1012345678!"},
		{name: "FedEx 12 digits", carrier: "FEDEX", input: "1234-5678 9012", valid: true, normalized: "123456789012"},
		{name: "FedEx 14 digits", carrier: "FEDEX", input: "12345678901234", valid: true, normalized: "12345678901234"},
		{name: "FedEx 15 digits", carrier: "FEDEX", input: "123456789012345", valid: true, normalized: "123456789012345"},
		{name: "FedEx 20 digits", carrier: "FEDEX", input: "12345678901234567890", valid: true, normalized: "12345678901234567890"},
		{name: "FedEx 22 digits", carrier: "FEDEX", input: "1234567890123456789012", valid: true, normalized: "1234567890123456789012"},
		{name: "FedEx letters rejected", carrier: "FEDEX", input: "12345678901A", valid: false, normalized: "12345678901A"},
		{name: "USPS domestic 20 digits", carrier: "USPS", input: "1234 5678-9012 3456 7890", valid: true, normalized: "12345678901234567890"},
		{name: "USPS domestic 22 digits", carrier: "USPS", input: "1234567890123456789012", valid: true, normalized: "1234567890123456789012"},
		{name: "USPS international lowercase", carrier: "USPS", input: "cj123456789us", valid: true, normalized: "CJ123456789US"},
		{name: "USPS international invalid middle", carrier: "USPS", input: "CJ12345A789US", valid: false, normalized: "CJ12345A789US"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := validTrackingNumber(test.carrier, test.input); got != test.valid {
				t.Fatalf("validTrackingNumber(%q, %q) = %t, want %t", test.carrier, test.input, got, test.valid)
			}
			if got := normalizedTrackingNumber(test.carrier, test.input); got != test.normalized {
				t.Fatalf("normalizedTrackingNumber(%q, %q) = %q, want %q", test.carrier, test.input, got, test.normalized)
			}
		})
	}
}

// TestLabelCostMustBeBelowHalfPayout verifies the strict half-payout limit and the two-decimal blur format.
func TestLabelCostMustBeBelowHalfPayout(t *testing.T) {
	t.Parallel()

	payoutMinor := int64(10000)
	belowLimit := []*shipmentFormPackage{completeShipmentFormPackage("UPS", "1Z999AA10123456784", "49.99")}
	atLimit := []*shipmentFormPackage{completeShipmentFormPackage("UPS", "1Z999AA10123456784", "50")}
	if !shipmentFormIsValid(belowLimit, &payoutMinor) {
		t.Fatal("shipmentFormIsValid() = false for label cost below half the payout, want true")
	}
	if shipmentFormIsValid(atLimit, &payoutMinor) {
		t.Fatal("shipmentFormIsValid() = true for label cost equal to half the payout, want false")
	}
	if got := formatDollarAmount(1350); got != "13.50" {
		t.Fatalf("formatDollarAmount(1350) = %q, want 13.50", got)
	}
}

// TestShipmentValidationMessages verifies typed invalid values receive concise field-specific feedback while blank untouched fields remain quiet.
func TestShipmentValidationMessages(t *testing.T) {
	t.Parallel()

	payoutMinor := int64(10000)
	tests := []struct {
		name     string
		carrier  string
		tracking string
		cost     string
		want     string
	}{
		{name: "blank untouched package", carrier: "UPS", want: ""},
		{name: "invalid UPS", carrier: "UPS", tracking: "1Z123", want: "Tracking: use 18 letters/numbers beginning 1Z"},
		{name: "invalid FedEx", carrier: "FEDEX", tracking: "ABC", want: "Tracking: use 12, 14, 15, 20, or 22 digits"},
		{name: "invalid USPS", carrier: "USPS", tracking: "ABC", want: "Tracking: use 20/22 digits or 2 letters, 9 digits, 2 letters"},
		{name: "invalid cost", carrier: "UPS", tracking: "1Z999AA10123456784", cost: "abc", want: "Label cost: enter a valid dollar amount"},
		{name: "cost at limit", carrier: "UPS", tracking: "1Z999AA10123456784", cost: "50", want: "Label cost: must be less than 50% of payout"},
		{name: "two invalid fields", carrier: "UPS", tracking: "invalid", cost: "50", want: "Tracking: use 18 letters/numbers beginning 1Z • Label cost: must be less than 50% of payout"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			shipment := completeShipmentFormPackage(test.carrier, test.tracking, test.cost)
			shipment.validateTracking()
			shipment.validateLabelCost(&payoutMinor)
			if got := shipmentValidationMessage(shipment); got != test.want {
				t.Fatalf("shipmentValidationMessage() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestShipmentValidationWaitsForBlur verifies an edit clears stale feedback, disables confirmation, and does not become valid again until a completed validation runs.
func TestShipmentValidationWaitsForBlur(t *testing.T) {
	t.Parallel()

	payoutMinor := int64(10000)
	shipment := completeShipmentFormPackage("UPS", "1Z999AA10123456784", "13.50")
	shipment.validateTracking()
	shipment.validateLabelCost(&payoutMinor)
	packages := []*shipmentFormPackage{shipment}
	if !shipmentFormReadyForConfirmation(packages, &payoutMinor) {
		t.Fatal("shipmentFormReadyForConfirmation() = false after valid completed fields, want true")
	}

	shipment.trackingNumber.SetText("1Z123")
	shipment.clearTrackingValidation()
	if shipment.trackingValidation.valid || shipmentValidationMessage(shipment) != "" {
		t.Fatalf("edited tracking validation = %#v, message = %q, want pending without feedback", shipment.trackingValidation, shipmentValidationMessage(shipment))
	}
	if shipmentFormReadyForConfirmation(packages, &payoutMinor) {
		t.Fatal("shipmentFormReadyForConfirmation() = true while tracking is pending, want false")
	}

	shipment.validateTracking()
	if got := shipmentValidationMessage(shipment); got != "Tracking: use 18 letters/numbers beginning 1Z" {
		t.Fatalf("shipmentValidationMessage() after invalid blur = %q, want tracking feedback", got)
	}

	shipment.trackingNumber.SetText("1Z999AA10123456784")
	shipment.clearTrackingValidation()
	if got := shipmentValidationMessage(shipment); got != "" {
		t.Fatalf("shipmentValidationMessage() while corrected tracking is pending = %q, want empty", got)
	}
	shipment.validateTracking()
	if !shipmentFormReadyForConfirmation(packages, &payoutMinor) {
		t.Fatal("shipmentFormReadyForConfirmation() = false after corrected tracking blur, want true")
	}
}

// TestCarrierChangeRevalidatesNonblankTracking verifies a carrier switch immediately applies the replacement tracking rules without disturbing label-cost validation.
func TestCarrierChangeRevalidatesNonblankTracking(t *testing.T) {
	t.Parallel()

	payoutMinor := int64(10000)
	shipment := completeShipmentFormPackage("UPS", "1Z999AA10123456784", "13.50")
	shipment.validateTracking()
	shipment.validateLabelCost(&payoutMinor)

	shipment.setCarrier("FEDEX")
	if shipment.trackingValidation.valid {
		t.Fatal("tracking validation remains valid after selecting FedEx for a UPS number, want false")
	}
	if got := shipmentValidationMessage(shipment); got != "Tracking: use 12, 14, 15, 20, or 22 digits" {
		t.Fatalf("shipmentValidationMessage() after carrier switch = %q, want FedEx feedback", got)
	}
	if !shipment.labelCostValidation.valid {
		t.Fatal("label-cost validation changed after carrier switch, want preserved valid result")
	}

	shipment.trackingNumber.SetText("")
	shipment.setCarrier("USPS")
	if shipment.trackingValidation.completed || shipmentValidationMessage(shipment) != "" {
		t.Fatalf("blank tracking after carrier switch = %#v, message = %q, want pending without feedback", shipment.trackingValidation, shipmentValidationMessage(shipment))
	}
}

// TestLabelCostValidationTracksPayout verifies a payout change invalidates cached label-cost approval until the amount is checked against the new limit.
func TestLabelCostValidationTracksPayout(t *testing.T) {
	t.Parallel()

	initialPayoutMinor := int64(10000)
	updatedPayoutMinor := int64(9000)
	shipment := completeShipmentFormPackage("UPS", "1Z999AA10123456784", "49.99")
	shipment.validateTracking()
	shipment.validateLabelCost(&initialPayoutMinor)
	packages := []*shipmentFormPackage{shipment}
	if !shipmentFormReadyForConfirmation(packages, &initialPayoutMinor) {
		t.Fatal("shipmentFormReadyForConfirmation() = false for initial valid payout, want true")
	}
	if shipmentFormReadyForConfirmation(packages, &updatedPayoutMinor) {
		t.Fatal("shipmentFormReadyForConfirmation() = true with an unvalidated updated payout, want false")
	}

	shipment.validateLabelCost(&updatedPayoutMinor)
	if got := shipmentValidationMessage(shipment); got != "Label cost: must be less than 50% of payout" {
		t.Fatalf("shipmentValidationMessage() after payout update = %q, want payout feedback", got)
	}
}

// TestSupportedCarriersAreAlphabeticalAndComplete verifies the dropdown exposes the documented carrier values in readable alphabetical order.
func TestSupportedCarriersAreAlphabeticalAndComplete(t *testing.T) {
	t.Parallel()

	want := map[string]struct{}{
		"FEDEX": {}, "UPS": {}, "USPS": {},
	}
	previous := ""
	for _, carrier := range supportedCarriers {
		if strings.ToLower(carrier.Label) < strings.ToLower(previous) {
			t.Fatalf("carrier labels are not alphabetical: %q appears before %q", carrier.Label, previous)
		}
		if _, found := want[carrier.Value]; !found {
			t.Fatalf("unexpected carrier value %q", carrier.Value)
		}
		delete(want, carrier.Value)
		previous = carrier.Label
	}
	if len(want) != 0 {
		t.Fatalf("missing documented carriers: %#v", want)
	}
}

// completeShipmentFormPackage constructs one fully populated package for shipment-form behavior tests.
func completeShipmentFormPackage(carrier, trackingNumber, labelCost string) *shipmentFormPackage {
	shipment := newShipmentFormPackage()
	shipment.carrier = carrier
	shipment.trackingNumber.SetText(trackingNumber)
	shipment.labelCost.SetText(labelCost)
	return shipment
}

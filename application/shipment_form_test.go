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

	first := completeShipmentFormPackage("UPS", "1Z999", "13.50")
	second := newShipmentFormPackage()
	packages := []*shipmentFormPackage{first, second}
	if shipmentFormIsValid(packages) {
		t.Fatal("shipmentFormIsValid() = true with an incomplete added package, want false")
	}

	second.trackingNumber.SetText("9400")
	second.labelCost.SetText("4.25")
	if !shipmentFormIsValid(packages) {
		t.Fatal("shipmentFormIsValid() = false with every package complete, want true")
	}
}

// TestShipmentRequestFromFormBuildsBatch verifies a valid form produces Faire's documented carrier values and exact USD money.
func TestShipmentRequestFromFormBuildsBatch(t *testing.T) {
	t.Parallel()

	packages := []*shipmentFormPackage{
		completeShipmentFormPackage("UPS", "1Z999", "13.50"),
		completeShipmentFormPackage("CANADA_POST", "940011", "4"),
	}
	request, valid := shipmentRequestFromForm(packages)
	if !valid {
		t.Fatal("shipmentRequestFromForm() valid = false, want true")
	}
	if len(request.Shipments) != 2 {
		t.Fatalf("shipment count = %d, want 2", len(request.Shipments))
	}
	first, second := request.Shipments[0], request.Shipments[1]
	if first.Carrier == nil || *first.Carrier != "UPS" || first.TrackingCode == nil || *first.TrackingCode != "1Z999" {
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

// TestSupportedCarriersAreAlphabeticalAndComplete verifies the dropdown exposes the documented carrier values in readable alphabetical order.
func TestSupportedCarriersAreAlphabeticalAndComplete(t *testing.T) {
	t.Parallel()

	want := map[string]struct{}{
		"CANADA_POST": {}, "DHL_ECOMMERCE": {}, "DHL_EXPRESS": {}, "FEDEX": {}, "PUROLATOR": {}, "UPS": {}, "USPS": {}, "POSTNL": {}, "CANPAR": {},
		"INTERLINK_EXPRESS": {}, "GSO": {}, "ROYAL_MAIL": {}, "DPD": {}, "DPDUK": {}, "PARCELFORCE": {}, "AUSTRALIA_POST": {}, "EVRI": {}, "LA_POSTE": {},
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

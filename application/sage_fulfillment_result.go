package application

import (
	"math"
	"strconv"
	"strings"

	"github.com/Fepozopo/faire-gui/faire"
	"github.com/Fepozopo/faire-gui/features/orders"
	sagepolicy "github.com/Fepozopo/faire-gui/sage"
)

// sageExternalShipment is the exact successful external-shipment input retained only until its persisted Faire detail is converted to a Sage result.
type sageExternalShipment struct {
	TrackingNumber string
	CostMinor      int64
}

const (
	// sageSimulatedLabelTrackingNumber is intentionally conspicuous so it cannot be mistaken for a carrier-issued label.
	sageSimulatedLabelTrackingNumber = "TEST-FAIRE-0001"
	// sageSimulatedLabelCostMinor is the fixed $10.00 test freight amount returned to Sage for Cost rules.
	sageSimulatedLabelCostMinor int64 = 1000
)

// sageFulfillmentPolicy combines the selected freight behavior with provider-neutral defaults for an unlisted Sage Ship Via value.
type sageFulfillmentPolicy struct {
	rule sagepolicy.ShipCodeRule
}

// sagePolicyForShipVia resolves an exact configured Ship Via value. Unlisted values intentionally remain completable with UPS Ground preferences, no markup, and actual cost writeback.
func sagePolicyForShipVia(rules sagepolicy.ShipCodeRules, shipVia string) sageFulfillmentPolicy {
	if rule, found := rules[strings.ToUpper(strings.TrimSpace(shipVia))]; found {
		return sageFulfillmentPolicy{rule: rule}
	}
	return sageFulfillmentPolicy{rule: sagepolicy.ShipCodeRule{Carrier: "UPS", Service: "Ground", FreightWriteback: "Cost"}}
}

// sageExternalShipmentsFromRequest extracts normalized tracking and exact entered costs from the immutable Faire request captured before a worker begins.
func sageExternalShipmentsFromRequest(request faire.AddShipmentsRequest) ([]sageExternalShipment, bool) {
	shipments := make([]sageExternalShipment, 0, len(request.Shipments))
	for _, shipment := range request.Shipments {
		if shipment.TrackingCode == nil || shipment.MakerCost == nil || shipment.MakerCost.AmountMinor == nil {
			return nil, false
		}
		tracking := strings.TrimSpace(*shipment.TrackingCode)
		if tracking == "" || *shipment.MakerCost.AmountMinor < 0 {
			return nil, false
		}
		shipments = append(shipments, sageExternalShipment{TrackingNumber: tracking, CostMinor: *shipment.MakerCost.AmountMinor})
	}
	return shipments, len(shipments) > 0
}

// sageSimulatedLabelShipment returns the deterministic test-only one-package label input. It does not represent a Faire API response or carrier purchase.
func sageSimulatedLabelShipment() sageExternalShipment {
	return sageExternalShipment{TrackingNumber: sageSimulatedLabelTrackingNumber, CostMinor: sageSimulatedLabelCostMinor}
}

// sageSimulatedLabelFulfillmentResult builds a completed Sage result without calling Faire so operators can test Sage writeback before label purchase is supported.
func sageSimulatedLabelFulfillmentResult(request sageFulfillmentRequest, shipment sageExternalShipment, policy sageFulfillmentPolicy) sageFulfillmentResult {
	result := sageFulfillmentResult{
		RequestID:    request.RequestID,
		Status:       "COMPLETED",
		SalesOrderNo: request.Document.SalesOrderNo,
		InvoiceNo:    request.Document.InvoiceNo,
		Tracking:     []sageFulfillmentTracking{{PackageNumber: 1, TrackingNumber: shipment.TrackingNumber}},
	}
	for _, line := range request.Lines {
		if line.QuantityShipped > 0 {
			result.PackageItems = append(result.PackageItems, sageFulfillmentPackageItem{PackageNumber: 1, ItemCode: line.ItemCode, ItemType: line.ItemType, Quantity: line.QuantityShipped})
		}
	}
	if policy.rule.FreightWriteback != "Omit" {
		// The simulator's promised $10.00 is a fixed test writeback value, not a carrier cost that should trigger real Ship Via markup.
		freight := shipment.CostMinor
		result.FreightAmountMinor = &freight
	}
	return result
}

// sageCompletedFulfillmentResult constructs the only completed payload Sage may write. Persisted Faire detail supplies the final tracking values, while the captured successful submission supplies authoritative entered carrier cost.
func sageCompletedFulfillmentResult(request sageFulfillmentRequest, detail orders.Detail, submitted []sageExternalShipment, policy sageFulfillmentPolicy) (sageFulfillmentResult, bool) {
	if len(submitted) == 0 || len(detail.Shipments) != len(submitted) {
		return sageFulfillmentResult{}, false
	}
	result := sageFulfillmentResult{
		RequestID:    request.RequestID,
		Status:       "COMPLETED",
		SalesOrderNo: request.Document.SalesOrderNo,
		InvoiceNo:    request.Document.InvoiceNo,
		Tracking:     make([]sageFulfillmentTracking, 0, len(detail.Shipments)),
	}
	costs := make([]int64, len(submitted))
	for index, shipment := range detail.Shipments {
		tracking := strings.TrimSpace(shipment.TrackingCode)
		if tracking == "" || !safeSageResultField(tracking) {
			return sageFulfillmentResult{}, false
		}
		result.Tracking = append(result.Tracking, sageFulfillmentTracking{PackageNumber: index + 1, TrackingNumber: tracking})
		costs[index] = submitted[index].CostMinor
	}
	for _, line := range request.Lines {
		if line.QuantityShipped > 0 {
			if !safeSageResultField(line.ItemCode) || !safeSageResultField(line.ItemType) {
				return sageFulfillmentResult{}, false
			}
			result.PackageItems = append(result.PackageItems, sageFulfillmentPackageItem{PackageNumber: 1, ItemCode: line.ItemCode, ItemType: line.ItemType, Quantity: line.QuantityShipped})
		}
	}
	if len(result.Tracking) == 0 {
		return sageFulfillmentResult{}, false
	}
	result.FreightAmountMinor = sageFreightAmountMinor(policy.rule, costs)
	return result, true
}

// sageFreightAmountMinor returns nil only for the Omit policy. It computes all money in cents and rounds the final total half-up to two decimal places.
func sageFreightAmountMinor(rule sagepolicy.ShipCodeRule, packageCosts []int64) *int64 {
	if rule.FreightWriteback == "Omit" {
		return nil
	}
	var total int64
	for _, cost := range packageCosts {
		total += cost
	}
	amount := float64(total)
	if rule.AdditionalMarkupFixed != nil {
		multiplier := 1
		if rule.ApplyMarkupPerPackage != nil && *rule.ApplyMarkupPerPackage {
			multiplier = len(packageCosts)
		}
		amount += *rule.AdditionalMarkupFixed * 100 * float64(multiplier)
	}
	if rule.AdditionalMarkupPercent != nil {
		if rule.ApplyMarkupPerPackage != nil && *rule.ApplyMarkupPerPackage {
			for _, cost := range packageCosts {
				amount += float64(cost) * *rule.AdditionalMarkupPercent / 100
			}
		} else {
			amount += float64(total) * *rule.AdditionalMarkupPercent / 100
		}
	}
	rounded := int64(math.Floor(amount + 0.5))
	return &rounded
}

// safeSageResultField rejects delimiters and line breaks because the established Sage protocol has no escaping for Tracking or PackageItem components.
func safeSageResultField(value string) bool {
	return strings.TrimSpace(value) != "" && !strings.ContainsAny(value, "|\r\n")
}

// formatSageQuantity serializes a positive Sage line quantity without scientific notation or protocol separators.
func formatSageQuantity(quantity float64) string {
	return strconv.FormatFloat(quantity, 'f', -1, 64)
}

// formatSageRecoveryData creates operator-copyable, line-oriented recovery data without exposing credentials or full Faire order details.
func formatSageRecoveryData(result sageFulfillmentResult) string {
	lines := []string{formatSageRecoveryMetadata(result)}
	for _, tracking := range result.Tracking {
		lines = append(lines, "Package "+itoa(tracking.PackageNumber)+" tracking: "+tracking.TrackingNumber)
	}
	return strings.Join(lines, "\n")
}

// formatSageRecoveryMetadata returns recovery fields other than tracking numbers so the UI can place a paste-ready copy control beside each tracking number.
func formatSageRecoveryMetadata(result sageFulfillmentResult) string {
	lines := []string{"Request ID: " + result.RequestID, "Sales order: " + result.SalesOrderNo, "Invoice: " + result.InvoiceNo}
	if result.FreightAmountMinor != nil {
		lines = append(lines, "Freight amount: "+formatDollarAmount(*result.FreightAmountMinor))
	}
	if result.Error != "" {
		lines = append(lines, "Error: "+result.Error)
	}
	return strings.Join(lines, "\n")
}

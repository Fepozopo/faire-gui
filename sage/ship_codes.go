// Package sage exposes embedded, non-secret Sage fulfillment policy shared by the desktop application.
package sage

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// ShipCodeRule specifies the provider-neutral fulfillment and freight policy for one exact Sage Ship Via value.
// Carrier and Service are current operator-facing preferences; they are not Faire API identifiers until Faire documents them.
type ShipCodeRule struct {
	Carrier                 string   `json:"Carrier"`
	Service                 string   `json:"Service"`
	PaymentParty            string   `json:"PaymentParty"`
	FreightWriteback        string   `json:"FreightWriteback"`
	AdditionalMarkupPercent *float64 `json:"AdditionalMarkupPercent"`
	AdditionalMarkupFixed   *float64 `json:"AdditionalMarkupFixed"`
	ApplyMarkupPerPackage   *bool    `json:"ApplyMarkupPerPackage"`
}

// ShipCodeRules contains validated policy indexed by normalized exact Sage Ship Via text.
type ShipCodeRules map[string]ShipCodeRule

// shipCodesJSON packages the version-controlled Sage policy with the application, preventing a deployment working directory from changing freight behavior.
//
//go:embed ShipCodes.json
var shipCodesJSON []byte

// LoadShipCodeRules decodes and validates the embedded sage/ShipCodes.json policy.
// It returns only normalized, non-secret rules that are safe to retain for the desktop application's lifetime.
func LoadShipCodeRules() (ShipCodeRules, error) {
	var rules ShipCodeRules
	if err := json.Unmarshal(shipCodesJSON, &rules); err != nil {
		return nil, fmt.Errorf("decode Sage ship-code policy: %w", err)
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("Sage ship-code policy has no rules")
	}

	normalized := make(ShipCodeRules, len(rules))
	for shipVia, rule := range rules {
		key := strings.ToUpper(strings.TrimSpace(shipVia))
		if key == "" {
			return nil, fmt.Errorf("Sage ship-code policy contains an empty Ship Via value")
		}
		if _, duplicate := normalized[key]; duplicate {
			return nil, fmt.Errorf("Sage ship-code policy contains duplicate Ship Via value %q", shipVia)
		}
		if strings.TrimSpace(rule.Carrier) == "" || strings.TrimSpace(rule.Service) == "" {
			return nil, fmt.Errorf("Sage ship-code policy %q is missing carrier or service", shipVia)
		}
		switch rule.FreightWriteback {
		case "Cost", "Omit":
		default:
			return nil, fmt.Errorf("Sage ship-code policy %q has unsupported FreightWriteback %q", shipVia, rule.FreightWriteback)
		}
		if rule.AdditionalMarkupFixed != nil && rule.AdditionalMarkupPercent != nil {
			return nil, fmt.Errorf("Sage ship-code policy %q configures both fixed and percentage markup", shipVia)
		}
		for label, value := range map[string]*float64{"AdditionalMarkupFixed": rule.AdditionalMarkupFixed, "AdditionalMarkupPercent": rule.AdditionalMarkupPercent} {
			if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0) {
				return nil, fmt.Errorf("Sage ship-code policy %q has invalid %s", shipVia, label)
			}
		}
		normalized[key] = rule
	}
	return normalized, nil
}

package sage

import "testing"

// TestLoadShipCodeRulesMigratesDisabledToOmit verifies the packaged policy uses the explicit no-writeback term required by the Sage bridge.
func TestLoadShipCodeRulesMigratesDisabledToOmit(t *testing.T) {
	rules, err := LoadShipCodeRules()
	if err != nil {
		t.Fatalf("LoadShipCodeRules() error = %v", err)
	}
	if rule := rules["UPS 3RD PARTY"]; rule.FreightWriteback != "Omit" {
		t.Fatalf("UPS 3RD PARTY FreightWriteback = %q, want Omit", rule.FreightWriteback)
	}
}

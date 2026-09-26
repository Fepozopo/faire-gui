package sage

import "testing"

// TestPackagedShipCodeRulesUseOmitForUPS3rdParty verifies the packaged Sage policy suppresses freight writeback for this carrier.
func TestPackagedShipCodeRulesUseOmitForUPS3rdParty(t *testing.T) {
	rules, err := LoadShipCodeRules()
	if err != nil {
		t.Fatalf("LoadShipCodeRules() error = %v", err)
	}
	rule, found := rules["UPS 3RD PARTY"]
	if !found {
		t.Fatal("UPS 3RD PARTY rule is missing")
	}
	if rule.FreightWriteback != "Omit" {
		t.Fatalf("UPS 3RD PARTY FreightWriteback = %q, want Omit", rule.FreightWriteback)
	}
}

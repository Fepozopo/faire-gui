package application

import (
	"path/filepath"
	"testing"
)

// TestLoadSageFulfillmentSettingsFileDefaultsDisabled verifies a missing settings file never opens the Sage HTTP listener.
func TestLoadSageFulfillmentSettingsFileDefaultsDisabled(t *testing.T) {
	enabled, err := loadSageFulfillmentSettingsFile(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("loadSageFulfillmentSettingsFile() error = %v", err)
	}
	if enabled {
		t.Fatal("enabled = true, want false for a missing settings file")
	}
}

// TestSaveSageFulfillmentSettingsFileRoundTripsEnabledState verifies explicit opt-in and opt-out values both survive application restart.
func TestSaveSageFulfillmentSettingsFileRoundTripsEnabledState(t *testing.T) {
	for _, test := range []struct {
		name    string
		enabled bool
	}{
		{name: "opt in", enabled: true},
		{name: "opt out", enabled: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nested", "sage-fulfillment-settings.json")
			if err := saveSageFulfillmentSettingsFile(path, test.enabled); err != nil {
				t.Fatalf("saveSageFulfillmentSettingsFile() error = %v", err)
			}

			enabled, err := loadSageFulfillmentSettingsFile(path)
			if err != nil {
				t.Fatalf("loadSageFulfillmentSettingsFile() error = %v", err)
			}
			if enabled != test.enabled {
				t.Fatalf("enabled = %t, want persisted %t", enabled, test.enabled)
			}
		})
	}
}

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

// TestSaveSageFulfillmentSettingsFileRoundTripsOptIn verifies the explicit opt-in survives application restart.
func TestSaveSageFulfillmentSettingsFileRoundTripsOptIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "sage-fulfillment-settings.json")
	if err := saveSageFulfillmentSettingsFile(path, true); err != nil {
		t.Fatalf("saveSageFulfillmentSettingsFile() error = %v", err)
	}

	enabled, err := loadSageFulfillmentSettingsFile(path)
	if err != nil {
		t.Fatalf("loadSageFulfillmentSettingsFile() error = %v", err)
	}
	if !enabled {
		t.Fatal("enabled = false, want persisted true opt-in")
	}
}

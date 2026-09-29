package application

import (
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestLoadSageFulfillmentSettingsFileDefaultsDisabled verifies startup creates nothing and ignores the retired Sage settings document.
func TestLoadSageFulfillmentSettingsFileDefaultsDisabled(t *testing.T) {
	for _, test := range []struct {
		name       string
		legacyFile bool
	}{
		{name: "no config directory"},
		{name: "legacy opt-in ignored", legacyFile: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "faire-gui")
			path := filepath.Join(directory, "settings.json")
			if test.legacyFile {
				if err := os.Mkdir(directory, 0o700); err != nil {
					t.Fatalf("create legacy settings directory %q: %v", directory, err)
				}
				if err := os.WriteFile(filepath.Join(directory, "sage-fulfillment-settings.json"), []byte(`{"version":1,"enabled":true}`), 0o600); err != nil {
					t.Fatalf("write legacy settings in %q: %v", directory, err)
				}
			}

			enabled, err := loadSageFulfillmentSettingsFile(path)
			if err != nil || enabled {
				t.Fatalf("loadSageFulfillmentSettingsFile(%q) = (%t, %v), want (false, nil)", path, enabled, err)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("os.Stat(%q) error = %v, want os.ErrNotExist after reading", path, err)
			}
			if !test.legacyFile {
				if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("os.Stat(%q) error = %v, want os.ErrNotExist after reading", directory, err)
				}
			}
		})
	}
}

// TestSaveSageFulfillmentSettingsFileRoundTripsEnabledState verifies both opt-in values and the shared settings document format.
func TestSaveSageFulfillmentSettingsFileRoundTripsEnabledState(t *testing.T) {
	for _, test := range []struct {
		name    string
		enabled bool
	}{
		{name: "opt in", enabled: true},
		{name: "opt out", enabled: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nested", "settings.json")
			if err := saveSageFulfillmentSettingsFile(path, test.enabled); err != nil {
				t.Fatalf("saveSageFulfillmentSettingsFile(%q, %t) error = %v, want nil", path, test.enabled, err)
			}

			enabled, err := loadSageFulfillmentSettingsFile(path)
			if err != nil || enabled != test.enabled {
				t.Fatalf("loadSageFulfillmentSettingsFile(%q) = (%t, %v), want (%t, nil)", path, enabled, err, test.enabled)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("os.ReadFile(%q) error = %v, want nil", path, err)
			}
			var document map[string]any
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatalf("decode settings.json %q error = %v, want nil", path, err)
			}
			want := map[string]any{"version": float64(1), "sageFulfillment": map[string]any{"enabled": test.enabled}}
			if !reflect.DeepEqual(document, want) {
				t.Fatalf("settings.json for enabled=%t = %v, want %v", test.enabled, document, want)
			}
		})
	}
}

// TestSaveSageFulfillmentSettingsFileRejectsInvalidDocument verifies an unreadable preference cannot be replaced by a Sage toggle.
func TestSaveSageFulfillmentSettingsFileRejectsInvalidDocument(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "malformed JSON", content: `{`},
		{name: "unsupported version", content: `{"version":2,"sageFulfillment":{"enabled":false}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatalf("write settings.json %q: %v", path, err)
			}
			if err := saveSageFulfillmentSettingsFile(path, true); err == nil {
				t.Fatalf("saveSageFulfillmentSettingsFile(%q, true) error = nil, want rejection for %s", path, test.name)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != test.content {
				t.Fatalf("settings.json for %s = (%q, %v), want (%q, nil)", test.name, data, err, test.content)
			}
		})
	}
}

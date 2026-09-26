package application

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// sageFulfillmentSettingsFilename is the non-secret, per-user opt-in state for the Sage HTTP listener.
	sageFulfillmentSettingsFilename = "sage-fulfillment-settings.json"
	// sageFulfillmentSettingsVersion permits explicit migration if this small settings document changes.
	sageFulfillmentSettingsVersion = 1
)

// sageFulfillmentSettings stores the persisted opt-in state for the temporary Sage fulfillment endpoint.
type sageFulfillmentSettings struct {
	Version int  `json:"version"`
	Enabled bool `json:"enabled"`
}

// sageFulfillmentSettingsPath returns the owner-specific application settings path without creating it.
func sageFulfillmentSettingsPath() (string, error) {
	configDirectory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user configuration directory: %w", err)
	}
	return filepath.Join(configDirectory, "faire-gui", sageFulfillmentSettingsFilename), nil
}

// loadSageFulfillmentSettings returns false when no user has opted in, keeping the network listener disabled by default.
func loadSageFulfillmentSettings() (bool, error) {
	path, err := sageFulfillmentSettingsPath()
	if err != nil {
		return false, err
	}
	return loadSageFulfillmentSettingsFile(path)
}

// loadSageFulfillmentSettingsFile reads one settings document from path and exists separately for deterministic tests.
func loadSageFulfillmentSettingsFile(path string) (bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open settings: %w", err)
	}
	defer file.Close()

	var settings sageFulfillmentSettings
	if err := json.UnmarshalRead(file, &settings); err != nil {
		return false, fmt.Errorf("decode settings: %w", err)
	}
	if settings.Version != sageFulfillmentSettingsVersion {
		return false, fmt.Errorf("unsupported settings version %d", settings.Version)
	}
	return settings.Enabled, nil
}

// saveSageFulfillmentSettings persists a user's listener choice with owner-only file permissions.
func saveSageFulfillmentSettings(enabled bool) error {
	path, err := sageFulfillmentSettingsPath()
	if err != nil {
		return err
	}
	return saveSageFulfillmentSettingsFile(path, enabled)
}

// saveSageFulfillmentSettingsFile atomically writes one settings document to path and exists separately for deterministic tests.
func saveSageFulfillmentSettingsFile(path string, enabled bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}

	temporaryFile, err := os.CreateTemp(filepath.Dir(path), ".sage-fulfillment-settings-*.json")
	if err != nil {
		return fmt.Errorf("create temporary settings: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	defer os.Remove(temporaryPath)

	if err := temporaryFile.Chmod(0o600); err != nil {
		_ = temporaryFile.Close()
		return fmt.Errorf("secure temporary settings: %w", err)
	}
	if err := json.MarshalWrite(temporaryFile, sageFulfillmentSettings{Version: sageFulfillmentSettingsVersion, Enabled: enabled}); err != nil {
		_ = temporaryFile.Close()
		return fmt.Errorf("encode settings: %w", err)
	}
	if err := temporaryFile.Sync(); err != nil {
		_ = temporaryFile.Close()
		return fmt.Errorf("sync settings: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		return fmt.Errorf("close settings: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace settings: %w", err)
	}
	return nil
}

package application

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// settingsFilename is the per-user, non-secret application settings document.
	settingsFilename = "settings.json"
	// settingsVersion rejects document formats the application cannot safely update.
	settingsVersion = 1
)

// applicationSettings groups persisted preferences so future settings can share the same document.
type applicationSettings struct {
	Version         int                     `json:"version"`
	SageFulfillment sageFulfillmentSettings `json:"sageFulfillment"`
}

// sageFulfillmentSettings stores the user's opt-in choice for the Sage HTTP listener.
type sageFulfillmentSettings struct {
	Enabled bool `json:"enabled"`
}

// settingsPath returns the per-user application settings path without creating a file or directory.
func settingsPath() (string, error) {
	configDirectory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user configuration directory: %w", err)
	}
	return filepath.Join(configDirectory, "faire-gui", settingsFilename), nil
}

// loadSageFulfillmentSettings reads the shared settings document and defaults to disabled when it is absent.
func loadSageFulfillmentSettings() (bool, error) {
	path, err := settingsPath()
	if err != nil {
		return false, err
	}
	return loadSageFulfillmentSettingsFile(path)
}

// loadSageFulfillmentSettingsFile reads the Sage opt-in value from path without creating a missing file.
func loadSageFulfillmentSettingsFile(path string) (bool, error) {
	settings, err := loadSettingsFile(path)
	if err != nil {
		return false, err
	}
	return settings.SageFulfillment.Enabled, nil
}

// loadSettingsFile reads shared preferences, returning disabled defaults when path does not exist.
// Invalid documents fail closed rather than allowing a later save to overwrite other preferences.
func loadSettingsFile(path string) (applicationSettings, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return applicationSettings{Version: settingsVersion}, nil
	}
	if err != nil {
		return applicationSettings{}, fmt.Errorf("open settings: %w", err)
	}
	defer file.Close()

	var settings applicationSettings
	// json.UnmarshalRead decodes the persisted JSON directly from the file reader.
	if err := json.UnmarshalRead(file, &settings); err != nil {
		return applicationSettings{}, fmt.Errorf("decode settings: %w", err)
	}
	if settings.Version != settingsVersion {
		return applicationSettings{}, fmt.Errorf("unsupported settings version %d", settings.Version)
	}
	return settings, nil
}

// saveSageFulfillmentSettings persists a user's listener choice in the shared settings document.
func saveSageFulfillmentSettings(enabled bool) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	return saveSageFulfillmentSettingsFile(path, enabled)
}

// saveSageFulfillmentSettingsFile updates only the Sage preference and atomically replaces path with owner-only permissions.
func saveSageFulfillmentSettingsFile(path string, enabled bool) error {
	settings, err := loadSettingsFile(path)
	if err != nil {
		return err
	}
	settings.SageFulfillment.Enabled = enabled
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}

	// os.CreateTemp keeps the replacement alongside the destination so os.Rename can publish a complete document.
	temporaryFile, err := os.CreateTemp(filepath.Dir(path), ".settings-*.json")
	if err != nil {
		return fmt.Errorf("create temporary settings: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	defer os.Remove(temporaryPath)

	if err := temporaryFile.Chmod(0o600); err != nil {
		_ = temporaryFile.Close()
		return fmt.Errorf("secure temporary settings: %w", err)
	}
	// json.MarshalWrite encodes the versioned document directly to the temporary file.
	if err := json.MarshalWrite(temporaryFile, settings); err != nil {
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

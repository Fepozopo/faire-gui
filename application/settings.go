package application

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Fepozopo/faire-gui/features/payouts"
)

const (
	// settingsFilename is the per-user, non-secret application settings document.
	settingsFilename = "settings.json"
	// settingsVersion rejects document formats the application cannot safely update.
	settingsVersion = 1
)

// settingsMu serializes in-process read/modify/write operations so exports and preference changes preserve each other.
var settingsMu sync.Mutex

// applicationSettings groups persisted preferences and the daily payout checkpoint in one document.
type applicationSettings struct {
	Version         int                     `json:"version"`
	SageFulfillment sageFulfillmentSettings `json:"sageFulfillment"`
	PayoutSequence  payoutSequenceSettings  `json:"payoutSequence,omitzero"`
}

// payoutSequenceSettings stores the full local calendar date and next zero-based deposit suffix offset.
// A missing checkpoint starts at 00; using the year prevents annual day-of-year collisions.
type payoutSequenceSettings struct {
	Date         string `json:"date"`
	NextSequence int    `json:"nextSequence"`
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

// loadSettingsFile reads shared preferences and the payout checkpoint, returning zero defaults for a missing path.
// Invalid documents or checkpoints fail closed rather than being overwritten or silently reusing deposit numbers.
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
	sequence := settings.PayoutSequence
	if sequence.NextSequence < 0 || sequence.NextSequence > payouts.MaxDepositSequences || sequence.Date == "" && sequence.NextSequence != 0 {
		return applicationSettings{}, fmt.Errorf("invalid payout checkpoint: %w", payouts.ErrInvalidDepositSequence)
	}
	if sequence.Date != "" {
		if _, err := time.Parse(time.DateOnly, sequence.Date); err != nil {
			return applicationSettings{}, fmt.Errorf("invalid payout checkpoint date: %w", err)
		}
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

// saveSageFulfillmentSettingsFile updates only the Sage preference, preserving the payout checkpoint.
// It serializes updates and atomically replaces path with owner-only permissions, returning any load or save error.
func saveSageFulfillmentSettingsFile(path string, enabled bool) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	settings, err := loadSettingsFile(path)
	if err != nil {
		return err
	}
	settings.SageFulfillment.Enabled = enabled
	return saveSettingsFile(path, settings)
}

// saveSettingsFile atomically replaces path with settings using owner-only permissions, returning filesystem or encoding errors.
// Callers must hold settingsMu across loading, modifying, and saving to prevent lost in-process updates.
func saveSettingsFile(path string, settings applicationSettings) error {
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

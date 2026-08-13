// Package application coordinates the Faire desktop interface while keeping credentials transient and separate from persisted connection metadata.
package application

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/Fepozopo/faire-gui/connections"
	"github.com/Fepozopo/faire-gui/faire"
)

const (
	windowTitle  = "Faire GUI"
	windowWidth  = 1440
	windowHeight = 820
)

// connectionEditorMode identifies the connection-management form currently visible to the user.
type connectionEditorMode uint8

const (
	// connectionEditorCreate creates a new direct-token connection.
	connectionEditorCreate connectionEditorMode = iota
	// connectionEditorMetadata updates an existing connection's non-secret metadata.
	connectionEditorMetadata
	// connectionEditorCredentials replaces an existing direct-token credential.
	connectionEditorCredentials
	// connectionEditorEnvironmentImport imports one explicitly named direct-token environment variable.
	connectionEditorEnvironmentImport
)

// loadSavedConnections initializes the default connection manager and loads its metadata.
// It returns a nil manager and a credential-safe status message if the credential store or metadata file is unavailable.
func loadSavedConnections(ctx context.Context) (*connections.Manager, []connections.Connection, string) {
	manager, err := connections.NewDefaultManager()
	if err != nil {
		return nil, nil, "Saved connections are unavailable. Check that your system credential store is available, then restart the app."
	}

	savedConnections, err := manager.List(ctx)
	if err != nil {
		return nil, nil, "Saved connection metadata could not be loaded. Check the application configuration and restart the app."
	}

	if len(savedConnections) == 0 {
		return manager, savedConnections, "No saved connections yet. Use the Connections tab to add one."
	}

	return manager, savedConnections, "Select a saved Faire brand connection to load its profile."
}

// explicitEnvironmentToken reads the value of exactly one explicitly named environment variable.
// It returns the non-empty value or an error without scanning, logging, changing, or exposing process environment data.
func explicitEnvironmentToken(environmentName string) (string, error) {
	environmentName = strings.TrimSpace(environmentName)
	if environmentName == "" {
		return "", fmt.Errorf("environment variable name is required")
	}

	accessToken, found := os.LookupEnv(environmentName)
	if !found || accessToken == "" {
		return "", fmt.Errorf("environment variable is missing or empty")
	}
	return accessToken, nil
}

// profileLoadErrorMessage converts an internal profile-loading error into a safe status message.
// It returns actionable HTTP failure guidance without displaying response bodies or credentials.
func profileLoadErrorMessage(err error) string {
	if errors.Is(err, context.Canceled) {
		return "Profile loading was canceled."
	}

	if apiError, ok := errors.AsType[*faire.APIError](err); ok {
		switch apiError.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "Faire rejected this connection's credentials. Update the saved connection or reauthorize it."
		case http.StatusTooManyRequests:
			return "Faire is rate limiting requests. Wait a moment, then try again."
		default:
			return fmt.Sprintf("Faire could not load this brand profile (HTTP %d). Try again later.", apiError.StatusCode)
		}
	}

	return "The saved connection could not be opened. Check the system credential store and try again."
}

// profileSummary formats the selected connection and brand profile for display.
// It returns a non-secret summary that omits absent optional fields.
func profileSummary(connection connections.Connection, profile *faire.BrandProfile) string {
	name := connection.Label
	if profile != nil && profile.Name != nil && *profile.Name != "" {
		name = *profile.Name
	}

	fields := []string{"Connected to " + name}
	if profile != nil && profile.BrandID != nil {
		fields = append(fields, "Brand ID: "+string(*profile.BrandID))
	} else if connection.BrandID != "" {
		fields = append(fields, "Brand ID: "+string(connection.BrandID))
	}
	if profile != nil && profile.Currency != nil && *profile.Currency != "" {
		fields = append(fields, "Currency: "+*profile.Currency)
	}
	if profile != nil && profile.Locale != nil && *profile.Locale != "" {
		fields = append(fields, "Locale: "+*profile.Locale)
	}

	return strings.Join(fields, " • ")
}

// connectionDetails formats non-secret connection metadata for the management screen.
// It returns the authentication mode and optional brand ID without exposing any credential fields.
func connectionDetails(connection connections.Connection) string {
	fields := []string{"Authentication: " + string(connection.AuthenticationMode)}
	if connection.BrandID != "" {
		fields = append(fields, "Brand ID: "+string(connection.BrandID))
	}
	return strings.Join(fields, " • ")
}

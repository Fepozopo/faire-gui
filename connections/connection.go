// Package connections manages non-secret Faire connection metadata and secure credentials.
package connections

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Fepozopo/faire-gui/faire"
)

// Connection identifies a saved Faire brand connection without containing its credentials.
type Connection struct {
	ID                 string                   `json:"id"`
	Label              string                   `json:"label"`
	BrandID            faire.BrandID            `json:"brand_id,omitempty"`
	AuthenticationMode faire.AuthenticationMode `json:"authentication_mode"`
}

// Credentials contains the secrets required to authenticate one Faire connection.
// It must be stored only in a CredentialStore and never in a ConnectionRepository.
type Credentials struct {
	AccessToken      string `json:"access_token,omitempty"`
	AppCredentials   string `json:"app_credentials,omitempty"`
	OAuthAccessToken string `json:"oauth_access_token,omitempty"`
}

// ClientOptions configures HTTP behavior for a client created from a saved connection.
type ClientOptions struct {
	BaseURL    string
	HTTPClient *http.Client
	MaxRetries int
}

// Validate confirms that credentials match the selected Faire authentication mode.
func (c Credentials) Validate(authenticationMode faire.AuthenticationMode) error {
	switch authenticationMode {
	case faire.AuthenticationModeAccessToken:
		if c.AccessToken == "" {
			return fmt.Errorf("connections: direct access token is required")
		}
		if c.AppCredentials != "" || c.OAuthAccessToken != "" {
			return fmt.Errorf("connections: direct-token credentials cannot include OAuth credentials")
		}
	case faire.AuthenticationModeOAuth:
		if c.AccessToken != "" {
			return fmt.Errorf("connections: OAuth credentials cannot include a direct access token")
		}
		if c.AppCredentials == "" || c.OAuthAccessToken == "" {
			return fmt.Errorf("connections: OAuth requires app credentials and an OAuth access token")
		}
	default:
		return fmt.Errorf("connections: unsupported authentication mode %q", authenticationMode)
	}
	return nil
}

// clientConfig returns a Faire client configuration for these credentials and options.
func (c Credentials) clientConfig(options ClientOptions) faire.Config {
	return faire.Config{
		BaseURL:          options.BaseURL,
		HTTPClient:       options.HTTPClient,
		AccessToken:      c.AccessToken,
		AppCredentials:   c.AppCredentials,
		OAuthAccessToken: c.OAuthAccessToken,
		MaxRetries:       options.MaxRetries,
	}
}

// validate confirms that a connection contains stable, non-secret identifying information.
func (c Connection) validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("connections: connection ID is required")
	}
	if strings.TrimSpace(c.Label) == "" {
		return fmt.Errorf("connections: connection label is required")
	}
	switch c.AuthenticationMode {
	case faire.AuthenticationModeAccessToken, faire.AuthenticationModeOAuth:
		return nil
	default:
		return fmt.Errorf("connections: unsupported authentication mode %q", c.AuthenticationMode)
	}
}

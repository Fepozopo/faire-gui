package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"

	keyring "github.com/zalando/go-keyring"
)

// DefaultKeychainService is the macOS Keychain service name used by the application.
const DefaultKeychainService = "github.com/Fepozopo/faire-gui"

// ErrCredentialNotFound indicates that a connection has no secret credential entry.
var ErrCredentialNotFound = errors.New("connection credentials not found")

// CredentialStore securely stores connection credentials outside connection metadata.
type CredentialStore interface {
	Load(context.Context, string) (Credentials, error)
	Save(context.Context, string, Credentials) error
	Delete(context.Context, string) error
}

// KeychainCredentialStore stores one serialized credential bundle per connection in macOS Keychain.
type KeychainCredentialStore struct {
	service string
}

// NewKeychainCredentialStore creates a macOS Keychain-backed credential store using service.
func NewKeychainCredentialStore(service string) (*KeychainCredentialStore, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("connections: macOS Keychain is unavailable on %s", runtime.GOOS)
	}
	if service == "" {
		return nil, fmt.Errorf("connections: Keychain service is required")
	}
	return &KeychainCredentialStore{service: service}, nil
}

// Load retrieves and decodes credentials for one connection from macOS Keychain.
func (s *KeychainCredentialStore) Load(ctx context.Context, connectionID string) (Credentials, error) {
	if err := ctx.Err(); err != nil {
		return Credentials{}, err
	}
	secret, err := keyring.Get(s.service, connectionID)
	if errors.Is(err, keyring.ErrNotFound) {
		return Credentials{}, fmt.Errorf("connections: load credentials for %q: %w", connectionID, ErrCredentialNotFound)
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("connections: load credentials for %q: %w", connectionID, err)
	}

	var credentials Credentials
	if err := json.Unmarshal([]byte(secret), &credentials); err != nil {
		return Credentials{}, fmt.Errorf("connections: decode credentials for %q: %w", connectionID, err)
	}
	return credentials, nil
}

// Save serializes credentials and stores them in macOS Keychain for one connection.
func (s *KeychainCredentialStore) Save(ctx context.Context, connectionID string, credentials Credentials) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, err := json.Marshal(credentials)
	if err != nil {
		return fmt.Errorf("connections: encode credentials for %q: %w", connectionID, err)
	}
	if err := keyring.Set(s.service, connectionID, string(encoded)); err != nil {
		return fmt.Errorf("connections: save credentials for %q: %w", connectionID, err)
	}
	return nil
}

// Delete removes credentials for one connection from macOS Keychain.
func (s *KeychainCredentialStore) Delete(ctx context.Context, connectionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := keyring.Delete(s.service, connectionID); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("connections: delete credentials for %q: %w", connectionID, err)
	}
	return nil
}

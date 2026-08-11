package connections

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Fepozopo/faire-gui/faire"
)

const defaultMetadataFilename = "connections.json"

// Manager coordinates non-secret connection metadata with credentials held in a CredentialStore.
type Manager struct {
	repository      ConnectionRepository
	credentialStore CredentialStore
}

// NewManager creates a connection manager from its metadata repository and secure credential store.
func NewManager(repository ConnectionRepository, credentialStore CredentialStore) (*Manager, error) {
	if repository == nil {
		return nil, fmt.Errorf("connections: repository is required")
	}
	if credentialStore == nil {
		return nil, fmt.Errorf("connections: credential store is required")
	}
	return &Manager{repository: repository, credentialStore: credentialStore}, nil
}

// NewDefaultManager creates a manager using the user's application-config directory and operating system credential store.
func NewDefaultManager() (*Manager, error) {
	path, err := DefaultMetadataPath()
	if err != nil {
		return nil, err
	}
	repository, err := NewFileConnectionRepository(path)
	if err != nil {
		return nil, err
	}
	credentialStore, err := NewSystemCredentialStore(DefaultCredentialService)
	if err != nil {
		return nil, err
	}
	return NewManager(repository, credentialStore)
}

// DefaultMetadataPath returns the owner-only JSON metadata file location for the application.
func DefaultMetadataPath() (string, error) {
	configDirectory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("connections: locate user config directory: %w", err)
	}
	return filepath.Join(configDirectory, "faire-gui", defaultMetadataFilename), nil
}

// List returns all saved connections without exposing their credentials.
func (m *Manager) List(ctx context.Context) ([]Connection, error) {
	return m.repository.List(ctx)
}

// UpdateMetadata updates the non-secret metadata for an existing connection.
// It preserves the existing authentication mode and does not load, return, or modify credentials.
func (m *Manager) UpdateMetadata(ctx context.Context, connection Connection) (Connection, error) {
	existingConnection, err := m.find(ctx, connection.ID)
	if err != nil {
		return Connection{}, err
	}
	if connection.AuthenticationMode != existingConnection.AuthenticationMode {
		return Connection{}, fmt.Errorf("connections: authentication mode cannot change when updating metadata")
	}
	if err := connection.validate(); err != nil {
		return Connection{}, err
	}
	if err := m.repository.Save(ctx, connection); err != nil {
		return Connection{}, err
	}
	return connection, nil
}

// Save creates or updates a connection and stores its credentials in the secure credential store.
func (m *Manager) Save(ctx context.Context, connection Connection, credentials Credentials) (Connection, error) {
	if connection.ID == "" {
		connectionID, err := newConnectionID()
		if err != nil {
			return Connection{}, err
		}
		connection.ID = connectionID
	}
	if err := connection.validate(); err != nil {
		return Connection{}, err
	}
	if err := credentials.Validate(connection.AuthenticationMode); err != nil {
		return Connection{}, err
	}

	previousCredentials, loadErr := m.credentialStore.Load(ctx, connection.ID)
	hadPreviousCredentials := loadErr == nil
	if loadErr != nil && !errors.Is(loadErr, ErrCredentialNotFound) {
		return Connection{}, loadErr
	}
	if err := m.credentialStore.Save(ctx, connection.ID, credentials); err != nil {
		return Connection{}, err
	}
	if err := m.repository.Save(ctx, connection); err != nil {
		if hadPreviousCredentials {
			_ = m.credentialStore.Save(ctx, connection.ID, previousCredentials)
		} else {
			_ = m.credentialStore.Delete(ctx, connection.ID)
		}
		return Connection{}, err
	}
	return connection, nil
}

// Delete removes both metadata and credentials for one connection.
func (m *Manager) Delete(ctx context.Context, connectionID string) error {
	connection, err := m.find(ctx, connectionID)
	if err != nil {
		return err
	}
	credentials, err := m.credentialStore.Load(ctx, connection.ID)
	if err != nil && !errors.Is(err, ErrCredentialNotFound) {
		return err
	}
	if err := m.repository.Delete(ctx, connection.ID); err != nil {
		return err
	}
	if err := m.credentialStore.Delete(ctx, connection.ID); err != nil {
		if credentials != (Credentials{}) {
			_ = m.repository.Save(ctx, connection)
		}
		return err
	}
	return nil
}

// Client creates a new immutable Faire client for a selected saved connection.
func (m *Manager) Client(ctx context.Context, connectionID string, options ClientOptions) (*faire.Client, Connection, error) {
	connection, err := m.find(ctx, connectionID)
	if err != nil {
		return nil, Connection{}, err
	}
	credentials, err := m.credentialStore.Load(ctx, connection.ID)
	if err != nil {
		return nil, Connection{}, err
	}
	if err := credentials.Validate(connection.AuthenticationMode); err != nil {
		return nil, Connection{}, fmt.Errorf("connections: invalid credentials for %q: %w", connection.ID, err)
	}
	client, err := faire.NewClient(credentials.clientConfig(options))
	if err != nil {
		return nil, Connection{}, fmt.Errorf("connections: create Faire client for %q: %w", connection.ID, err)
	}
	return client, connection, nil
}

// find retrieves one saved connection by ID.
func (m *Manager) find(ctx context.Context, connectionID string) (Connection, error) {
	connections, err := m.repository.List(ctx)
	if err != nil {
		return Connection{}, err
	}
	for _, connection := range connections {
		if connection.ID == connectionID {
			return connection, nil
		}
	}
	return Connection{}, fmt.Errorf("connections: find %q: %w", connectionID, ErrConnectionNotFound)
}

// newConnectionID returns a collision-resistant random identifier suitable for Keychain account names.
func newConnectionID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("connections: generate connection ID: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

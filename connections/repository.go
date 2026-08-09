package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const metadataFileVersion = 1

// ErrConnectionNotFound indicates that a requested connection does not exist in the metadata repository.
var ErrConnectionNotFound = errors.New("connection not found")

// ConnectionRepository persists non-secret connection metadata.
type ConnectionRepository interface {
	List(context.Context) ([]Connection, error)
	Save(context.Context, Connection) error
	Delete(context.Context, string) error
}

// FileConnectionRepository stores non-secret connection metadata in a user-readable JSON file.
type FileConnectionRepository struct {
	path string
	mu   sync.Mutex
}

// connectionFile is the on-disk versioned representation of saved connection metadata.
type connectionFile struct {
	Version     int          `json:"version"`
	Connections []Connection `json:"connections"`
}

// NewFileConnectionRepository creates a repository that stores metadata at path.
func NewFileConnectionRepository(path string) (*FileConnectionRepository, error) {
	if path == "" {
		return nil, fmt.Errorf("connections: metadata path is required")
	}
	return &FileConnectionRepository{path: path}, nil
}

// List returns all saved connections without their credentials.
func (r *FileConnectionRepository) List(ctx context.Context) ([]Connection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	metadata, err := r.read()
	if err != nil {
		return nil, err
	}
	return append([]Connection(nil), metadata.Connections...), nil
}

// Save creates or replaces non-secret metadata for one connection.
func (r *FileConnectionRepository) Save(ctx context.Context, connection Connection) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := connection.validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	metadata, err := r.read()
	if err != nil {
		return err
	}
	for index, existing := range metadata.Connections {
		if existing.ID == connection.ID {
			metadata.Connections[index] = connection
			return r.write(metadata)
		}
	}
	metadata.Connections = append(metadata.Connections, connection)
	return r.write(metadata)
}

// Delete removes non-secret metadata for a connection.
func (r *FileConnectionRepository) Delete(ctx context.Context, connectionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	metadata, err := r.read()
	if err != nil {
		return err
	}
	for index, connection := range metadata.Connections {
		if connection.ID == connectionID {
			metadata.Connections = append(metadata.Connections[:index], metadata.Connections[index+1:]...)
			return r.write(metadata)
		}
	}
	return fmt.Errorf("connections: delete %q: %w", connectionID, ErrConnectionNotFound)
}

// read loads metadata from disk, treating a missing file as an empty repository.
func (r *FileConnectionRepository) read() (connectionFile, error) {
	file, err := os.Open(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return connectionFile{Version: metadataFileVersion}, nil
	}
	if err != nil {
		return connectionFile{}, fmt.Errorf("connections: open metadata: %w", err)
	}
	defer func() { _ = file.Close() }()

	var metadata connectionFile
	if err := json.NewDecoder(file).Decode(&metadata); err != nil {
		if errors.Is(err, io.EOF) {
			return connectionFile{Version: metadataFileVersion}, nil
		}
		return connectionFile{}, fmt.Errorf("connections: decode metadata: %w", err)
	}
	if metadata.Version != metadataFileVersion {
		return connectionFile{}, fmt.Errorf("connections: unsupported metadata version %d", metadata.Version)
	}
	for _, connection := range metadata.Connections {
		if err := connection.validate(); err != nil {
			return connectionFile{}, fmt.Errorf("connections: invalid metadata: %w", err)
		}
	}
	return metadata, nil
}

// write atomically replaces the metadata file with owner-only permissions.
func (r *FileConnectionRepository) write(metadata connectionFile) error {
	metadata.Version = metadataFileVersion
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return fmt.Errorf("connections: create metadata directory: %w", err)
	}

	temporaryFile, err := os.CreateTemp(filepath.Dir(r.path), ".connections-*.json")
	if err != nil {
		return fmt.Errorf("connections: create temporary metadata file: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	defer func() { _ = os.Remove(temporaryPath) }()

	if err := temporaryFile.Chmod(0o600); err != nil {
		_ = temporaryFile.Close()
		return fmt.Errorf("connections: secure temporary metadata file: %w", err)
	}
	encoder := json.NewEncoder(temporaryFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metadata); err != nil {
		_ = temporaryFile.Close()
		return fmt.Errorf("connections: encode metadata: %w", err)
	}
	if err := temporaryFile.Sync(); err != nil {
		_ = temporaryFile.Close()
		return fmt.Errorf("connections: sync metadata: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		return fmt.Errorf("connections: close metadata: %w", err)
	}
	if err := os.Rename(temporaryPath, r.path); err != nil {
		return fmt.Errorf("connections: replace metadata: %w", err)
	}
	return nil
}

package ordersstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// SQLiteStore is a process-local SQLite implementation of Store.
type SQLiteStore struct {
	database *sql.DB
	path     string
}

// DefaultPath returns the private default SQLite location for cached Orders data.
func DefaultPath() (string, error) {
	configDirectory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(configDirectory, "faire-gui", "orders.sqlite3"), nil
}

// Open creates or opens a private SQLite Orders database and applies all migrations.
func Open(ctx context.Context, path string) (*SQLiteStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("orders database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create orders directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil && !errors.Is(err, os.ErrPermission) {
		return nil, fmt.Errorf("secure orders directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create orders database: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure orders database: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close orders database: %w", err)
	}

	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open orders database: %w", err)
	}
	database.SetMaxOpenConns(2)
	database.SetMaxIdleConns(2)
	store := &SQLiteStore{database: database, path: path}
	if err := store.configure(ctx); err != nil {
		_ = database.Close()
		return nil, err
	}
	if err := runMigrations(ctx, database); err != nil {
		_ = database.Close()
		return nil, classifyError(err)
	}
	return store, nil
}

// Close releases the SQLite handle without deleting durable local Orders data.
func (s *SQLiteStore) Close() error {
	if s == nil || s.database == nil {
		return nil
	}
	return s.database.Close()
}

// configure enables private-store-safe SQLite settings before migrations and queries execute.
func (s *SQLiteStore) configure(ctx context.Context) error {
	for _, statement := range []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA foreign_keys = ON`,
	} {
		if _, err := s.database.ExecContext(ctx, statement); err != nil {
			return classifyError(fmt.Errorf("configure orders database: %w", err))
		}
	}
	return nil
}

// classifyError maps known SQLite corruption signals to a safe sentinel without exposing SQL details to callers.
func classifyError(err error) error {
	if err == nil {
		return nil
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "database disk image is malformed") || strings.Contains(lower, "database corrupt") || strings.Contains(lower, "file is not a database") {
		return fmt.Errorf("%w: %v", ErrCorruptData, err)
	}
	return err
}

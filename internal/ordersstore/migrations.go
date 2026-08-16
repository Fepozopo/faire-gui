package ordersstore

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const currentSchemaVersion = 1

// migration is one append-only SQLite schema change.
type migration struct {
	version int
	apply   func(context.Context, *sql.Tx) error
}

var migrations = []migration{
	{version: 1, apply: applyMigrationOne},
}

// runMigrations applies each missing append-only migration before Orders data is read.
func runMigrations(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at_utc INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}

	var version int
	if err := database.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version); err != nil {
		return fmt.Errorf("read migration version: %w", err)
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("database version %d: %w", version, ErrUnsupportedSchema)
	}
	for _, migration := range migrations {
		if migration.version <= version {
			continue
		}
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", migration.version, err)
		}
		if err := migration.apply(ctx, tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", migration.version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at_utc) VALUES (?, ?)`, migration.version, time.Now().UTC().UnixMicro()); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", migration.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", migration.version, err)
		}
	}
	return nil
}

// applyMigrationOne creates the initial connection-scoped Orders schema and indexes.
func applyMigrationOne(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE order_sync_state (
			connection_id TEXT PRIMARY KEY,
			bootstrap_created_at_min_utc INTEGER NOT NULL,
			bootstrap_completed_at_utc INTEGER NULL,
			high_watermark_updated_at_utc INTEGER NULL,
			last_successful_sync_at_utc INTEGER NULL,
			last_attempt_at_utc INTEGER NULL,
			last_error_kind TEXT NULL,
			last_error_at_utc INTEGER NULL
		)`,
		`CREATE TABLE orders (
			connection_id TEXT NOT NULL,
			order_id TEXT NOT NULL,
			display_id TEXT NOT NULL,
			state TEXT NULL,
			customer_name TEXT NULL,
			total_display TEXT NULL,
			commission_display TEXT NULL,
			source TEXT NULL,
			created_at_utc INTEGER NULL,
			expected_ship_at_utc INTEGER NULL,
			updated_at_utc INTEGER NOT NULL,
			order_snapshot_json TEXT NOT NULL,
			snapshot_schema_version INTEGER NOT NULL,
			synced_at_utc INTEGER NOT NULL,
			PRIMARY KEY (connection_id, order_id)
		)`,
		`CREATE INDEX orders_by_connection_created ON orders (connection_id, created_at_utc DESC, order_id)`,
		`CREATE INDEX orders_by_connection_state_created ON orders (connection_id, state, created_at_utc DESC, order_id)`,
		`CREATE UNIQUE INDEX orders_by_connection_display_id ON orders (connection_id, display_id)`,
		`CREATE INDEX orders_by_connection_updated ON orders (connection_id, updated_at_utc, order_id)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

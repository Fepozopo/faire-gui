package ordersstore

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const currentSchemaVersion = 9

// migration is one append-only SQLite schema change.
type migration struct {
	version int
	apply   func(context.Context, *sql.Tx) error
}

var migrations = []migration{
	{version: 1, apply: applyMigrationOne},
	{version: 2, apply: applyMigrationTwo},
	{version: 3, apply: applyMigrationThree},
	{version: 4, apply: applyMigrationFour},
	{version: 5, apply: applyMigrationFive},
	{version: 6, apply: applyMigrationSix},
	{version: 7, apply: applyMigrationSeven},
	{version: 8, apply: applyMigrationEight},
	{version: 9, apply: applyMigrationNine},
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

// applyMigrationNine replaces item-derived totals with Faire's explicit total-payout projection and backfills valid cached snapshots.
// It uses ctx and tx for the atomic migration and returns the first SQLite error.
func applyMigrationNine(ctx context.Context, tx *sql.Tx) error {
	for _, statement := range []string{
		`ALTER TABLE orders ADD COLUMN total_payout_amount_minor INTEGER NULL`,
		`ALTER TABLE orders ADD COLUMN total_payout_currency TEXT NULL`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET
		total_payout_amount_minor = CASE WHEN json_valid(order_snapshot_json) THEN json_extract(order_snapshot_json, '$.payout_costs.total_payout.amount_minor') END,
		total_payout_currency = CASE WHEN json_valid(order_snapshot_json) THEN NULLIF(TRIM(json_extract(order_snapshot_json, '$.payout_costs.total_payout.currency')), '') END`); err != nil {
		return err
	}
	for _, statement := range []string{
		`ALTER TABLE orders DROP COLUMN total_amount_minor`,
		`ALTER TABLE orders DROP COLUMN total_currency`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// applyMigrationEight removes the unused customer-name projection because the table uses only the delivery business or recipient label.
// It uses ctx and tx for the atomic migration and returns the first SQLite error.
func applyMigrationEight(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `ALTER TABLE orders DROP COLUMN customer_name`)
	return err
}

// applyMigrationSeven replaces cached shipping-recipient labels with Faire business names when present.
// It uses ctx and tx for the atomic migration and returns the first SQLite error.
func applyMigrationSeven(ctx context.Context, tx *sql.Tx) error {
	// Existing snapshots already retain both address fields, so correct cached labels without another API request.
	_, err := tx.ExecContext(ctx, `UPDATE orders SET address_name = CASE
		WHEN json_valid(order_snapshot_json) THEN COALESCE(
			NULLIF(TRIM(json_extract(order_snapshot_json, '$.address.company_name')), ''),
			NULLIF(TRIM(json_extract(order_snapshot_json, '$.address.name')), '')
		)
		ELSE NULL
	END`)
	return err
}

// applyMigrationSix removes the legacy formatted table projections after every cache has raw replacements.
// It uses ctx and tx for the atomic migration and returns the first SQLite error.
func applyMigrationSix(ctx context.Context, tx *sql.Tx) error {
	for _, statement := range []string{
		`ALTER TABLE orders DROP COLUMN total_display`,
		`ALTER TABLE orders DROP COLUMN commission_display`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// applyMigrationFive adds raw numeric Orders-table projections and backfills them from valid cached snapshots.
// It uses ctx and tx for the atomic migration and returns the first SQLite error.
func applyMigrationFive(ctx context.Context, tx *sql.Tx) error {
	for _, statement := range []string{
		`ALTER TABLE orders ADD COLUMN total_amount_minor INTEGER NULL`,
		`ALTER TABLE orders ADD COLUMN total_currency TEXT NULL`,
		`ALTER TABLE orders ADD COLUMN commission_bps INTEGER NULL`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	// Rebuild typed projections from snapshots so cached rows remain usable before their next remote synchronization.
	_, err := tx.ExecContext(ctx, `UPDATE orders SET
		total_amount_minor = CASE WHEN json_valid(order_snapshot_json) THEN (
			SELECT CASE WHEN COUNT(*) > 0 AND COUNT(DISTINCT currency) = 1 THEN SUM(amount_minor) END
			FROM (
				SELECT (CASE WHEN json_extract(item.value, '$.price.amount_minor') IS NOT NULL AND COALESCE(json_extract(item.value, '$.price.currency'), '') <> '' THEN json_extract(item.value, '$.price.amount_minor') ELSE json_extract(item.value, '$.price_cents') END) * COALESCE(json_extract(item.value, '$.quantity'), 1) AS amount_minor,
					CASE WHEN json_extract(item.value, '$.price.amount_minor') IS NOT NULL AND COALESCE(json_extract(item.value, '$.price.currency'), '') <> '' THEN json_extract(item.value, '$.price.currency') ELSE 'USD' END AS currency
				FROM json_each(orders.order_snapshot_json, '$.items') AS item
				WHERE (json_extract(item.value, '$.price.amount_minor') IS NOT NULL AND COALESCE(json_extract(item.value, '$.price.currency'), '') <> '') OR json_extract(item.value, '$.price_cents') IS NOT NULL
			)
		) END,
		total_currency = CASE WHEN json_valid(order_snapshot_json) THEN (
			SELECT CASE WHEN COUNT(*) > 0 AND COUNT(DISTINCT currency) = 1 THEN MIN(currency) END
			FROM (
				SELECT CASE WHEN json_extract(item.value, '$.price.amount_minor') IS NOT NULL AND COALESCE(json_extract(item.value, '$.price.currency'), '') <> '' THEN json_extract(item.value, '$.price.currency') ELSE 'USD' END AS currency
				FROM json_each(orders.order_snapshot_json, '$.items') AS item
				WHERE (json_extract(item.value, '$.price.amount_minor') IS NOT NULL AND COALESCE(json_extract(item.value, '$.price.currency'), '') <> '') OR json_extract(item.value, '$.price_cents') IS NOT NULL
			)
		) END,
		commission_bps = CASE WHEN json_valid(order_snapshot_json) THEN json_extract(order_snapshot_json, '$.payout_costs.commission_bps') END`)
	return err
}

// applyMigrationFour replaces cached commission amounts with Faire's commission_bps percentage projection.
// It uses ctx and tx for the atomic migration and returns the first SQLite error.
func applyMigrationFour(ctx context.Context, tx *sql.Tx) error {
	// Existing cached snapshots already retain commission_bps, so rewrite the table-only display value without another API request.
	_, err := tx.ExecContext(ctx, `UPDATE orders SET commission_display = CASE
		WHEN json_valid(order_snapshot_json) THEN CASE
			WHEN json_extract(order_snapshot_json, '$.payout_costs.commission_bps') IS NOT NULL THEN printf('%.2f%%', json_extract(order_snapshot_json, '$.payout_costs.commission_bps') * 0.01)
			ELSE NULL
		END
		ELSE NULL
	END`)
	return err
}

// applyMigrationThree adds the delivery-address list projection and restores it from valid private snapshots.
// It uses ctx and tx for the atomic migration and returns the first SQLite error.
func applyMigrationThree(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE orders ADD COLUMN address_name TEXT NULL`); err != nil {
		return err
	}
	// Preserve immediately usable table names for cached orders without trusting malformed historical JSON.
	_, err := tx.ExecContext(ctx, `UPDATE orders SET address_name = CASE
		WHEN json_valid(order_snapshot_json) THEN json_extract(order_snapshot_json, '$.address.name')
		ELSE NULL
	END`)
	return err
}

// applyMigrationTwo adds the updated-at history boundary and requires one safe updated-at re-bootstrap for existing caches.
func applyMigrationTwo(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE order_sync_state ADD COLUMN bootstrap_updated_at_min_utc INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	// Existing rows remain private cached snapshots, but their created-at checkpoint cannot prove an updated-at traversal completed.
	_, err := tx.ExecContext(ctx, `UPDATE order_sync_state SET
		bootstrap_updated_at_min_utc = bootstrap_created_at_min_utc,
		bootstrap_completed_at_utc = NULL,
		high_watermark_updated_at_utc = NULL,
		last_successful_sync_at_utc = NULL,
		last_error_kind = NULL,
		last_error_at_utc = NULL`)
	return err
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

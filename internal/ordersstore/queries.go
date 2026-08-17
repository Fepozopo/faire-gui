package ordersstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// List returns a deterministic local page for query's connection, filters, and selected indexed date column.
// It returns the page and next cursor or a validated storage error.
func (s *SQLiteStore) List(ctx context.Context, query ListQuery) (ListPage, error) {
	if query.ConnectionID == "" {
		return ListPage{}, fmt.Errorf("connection ID: %w", ErrInvalidRecord)
	}
	if query.Limit <= 0 {
		query.Limit = 50
	}
	sortColumn, err := localSortColumn(query.SortColumn)
	if err != nil {
		return ListPage{}, err
	}
	args := []any{query.ConnectionID}
	where := []string{"connection_id = ?"}
	if len(query.States) > 0 {
		placeholders := make([]string, len(query.States))
		for index, state := range query.States {
			placeholders[index] = "?"
			args = append(args, state)
		}
		where = append(where, "state IN ("+strings.Join(placeholders, ",")+")")
	}
	if query.UpdatedAtMin != nil {
		where = append(where, "updated_at_utc >= ?")
		args = append(args, query.UpdatedAtMin.UTC().UnixMicro())
	}
	if query.After != nil {
		if query.After.SortAtUTC == nil {
			where = append(where, "("+sortColumn+" IS NULL AND order_id > ?)")
			args = append(args, query.After.OrderID)
		} else {
			comparison := "<"
			if !query.Descending {
				comparison = ">"
			}
			where = append(where, "("+sortColumn+" "+comparison+" ? OR ("+sortColumn+" = ? AND order_id > ?) OR "+sortColumn+" IS NULL)")
			sortAt := query.After.SortAtUTC.UTC().UnixMicro()
			args = append(args, sortAt, sortAt, query.After.OrderID)
		}
	}
	direction := "ASC"
	if query.Descending {
		direction = "DESC"
	}
	args = append(args, query.Limit+1)
	statement := `SELECT order_id, display_id, state, customer_name, address_name, total_display, commission_display, source, created_at_utc, expected_ship_at_utc, updated_at_utc, synced_at_utc
		FROM orders WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY ` + sortColumn + ` ` + direction + ` NULLS LAST, order_id ASC LIMIT ?`
	rows, err := s.database.QueryContext(ctx, statement, args...)
	if err != nil {
		return ListPage{}, classifyError(err)
	}
	defer func() { _ = rows.Close() }()
	page := ListPage{Rows: make([]LocalRow, 0, query.Limit)}
	for rows.Next() {
		row, err := scanLocalRow(rows)
		if err != nil {
			return ListPage{}, classifyError(err)
		}
		page.Rows = append(page.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return ListPage{}, classifyError(err)
	}
	if len(page.Rows) > query.Limit {
		last := page.Rows[query.Limit-1]
		page.Rows = page.Rows[:query.Limit]
		page.NextCursor = &KeysetCursor{SortAtUTC: rowSortTime(last, query.SortColumn), OrderID: last.OrderID}
	}
	return page, nil
}

// CountByState returns the number of locally stored orders for connectionID whose state exactly matches state.
// It uses the connection-and-state index so tab badges do not require loading snapshots or table pages.
func (s *SQLiteStore) CountByState(ctx context.Context, connectionID, state string) (int, error) {
	if strings.TrimSpace(connectionID) == "" || strings.TrimSpace(state) == "" {
		return 0, fmt.Errorf("connection ID and state: %w", ErrInvalidRecord)
	}
	var count int
	if err := s.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE connection_id = ? AND state = ?`, connectionID, state).Scan(&count); err != nil {
		return 0, classifyError(err)
	}
	return count, nil
}

// localSortColumn maps the closed storage-owned sort set to a trusted SQLite column name.
func localSortColumn(column LocalSortColumn) (string, error) {
	switch column {
	case "", LocalSortCreatedAt:
		return "created_at_utc", nil
	case LocalSortExpectedShipAt:
		return "expected_ship_at_utc", nil
	default:
		return "", fmt.Errorf("local sort column: %w", ErrInvalidRecord)
	}
}

// rowSortTime returns the selected indexed timestamp for a local pagination cursor.
func rowSortTime(row LocalRow, column LocalSortColumn) *time.Time {
	if column == LocalSortExpectedShipAt {
		return row.ExpectedShipAtUTC
	}
	return row.CreatedAtUTC
}

// FindByDisplayID returns one connection-scoped indexed row for connectionID and visible displayID.
// It returns ErrNotFound when the local row does not exist.
func (s *SQLiteStore) FindByDisplayID(ctx context.Context, connectionID, displayID string) (LocalRow, error) {
	row := s.database.QueryRowContext(ctx, `SELECT order_id, display_id, state, customer_name, address_name, total_display, commission_display, source, created_at_utc, expected_ship_at_utc, updated_at_utc, synced_at_utc FROM orders WHERE connection_id = ? AND display_id = ?`, connectionID, displayID)
	value, err := scanLocalRow(row)
	if err == sql.ErrNoRows {
		return LocalRow{}, ErrNotFound
	}
	return value, classifyError(err)
}

// Snapshot returns the complete private snapshot for exactly one connection and order ID.
func (s *SQLiteStore) Snapshot(ctx context.Context, connectionID, orderID string) (Snapshot, error) {
	var value Snapshot
	var updated, synced int64
	err := s.database.QueryRowContext(ctx, `SELECT order_id, order_snapshot_json, snapshot_schema_version, updated_at_utc, synced_at_utc FROM orders WHERE connection_id = ? AND order_id = ?`, connectionID, orderID).Scan(&value.OrderID, &value.SnapshotJSON, &value.SnapshotSchemaVersion, &updated, &synced)
	if err == sql.ErrNoRows {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, classifyError(err)
	}
	value.UpdatedAtUTC = time.UnixMicro(updated).UTC()
	value.SyncedAtUTC = time.UnixMicro(synced).UTC()
	return value, nil
}

// UpsertOrders atomically inserts or replaces every supplied projection, including the delivery address name, and complete snapshot without regressing newer versions.
// It returns a validation or storage error when records cannot be persisted.
func (s *SQLiteStore) UpsertOrders(ctx context.Context, records []OrderRecord) error {
	if len(records) == 0 {
		return nil
	}
	for _, record := range records {
		if err := validateRecord(record); err != nil {
			return err
		}
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return classifyError(err)
	}
	defer func() { _ = tx.Rollback() }()
	statement := `INSERT INTO orders (connection_id, order_id, display_id, state, customer_name, address_name, total_display, commission_display, source, created_at_utc, expected_ship_at_utc, updated_at_utc, order_snapshot_json, snapshot_schema_version, synced_at_utc)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(connection_id, order_id) DO UPDATE SET
		display_id = excluded.display_id, state = excluded.state, customer_name = excluded.customer_name, address_name = excluded.address_name,
		total_display = excluded.total_display, commission_display = excluded.commission_display, source = excluded.source,
		created_at_utc = excluded.created_at_utc, expected_ship_at_utc = excluded.expected_ship_at_utc, updated_at_utc = excluded.updated_at_utc,
		order_snapshot_json = excluded.order_snapshot_json, snapshot_schema_version = excluded.snapshot_schema_version, synced_at_utc = excluded.synced_at_utc
		WHERE excluded.updated_at_utc >= orders.updated_at_utc`
	for _, record := range records {
		if _, err := tx.ExecContext(ctx, statement, record.ConnectionID, record.OrderID, record.DisplayID, nullableText(record.State), nullableText(record.CustomerName), nullableText(record.AddressName), nullableText(record.TotalDisplay), nullableText(record.CommissionDisplay), nullableText(record.Source), nullableTime(record.CreatedAtUTC), nullableTime(record.ExpectedShipAtUTC), record.UpdatedAtUTC.UTC().UnixMicro(), record.SnapshotJSON, record.SnapshotSchemaVersion, record.SyncedAtUTC.UTC().UnixMicro()); err != nil {
			return classifyError(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return classifyError(err)
	}
	return nil
}

// SyncState reads the checkpoint state for one connection and reports whether it exists.
func (s *SQLiteStore) SyncState(ctx context.Context, connectionID string) (SyncState, bool, error) {
	var state SyncState
	var bootstrap, completed, watermark, successful, attempt, errorAt sql.NullInt64
	var errorKind sql.NullString
	err := s.database.QueryRowContext(ctx, `SELECT connection_id, bootstrap_updated_at_min_utc, bootstrap_completed_at_utc, high_watermark_updated_at_utc, last_successful_sync_at_utc, last_attempt_at_utc, last_error_kind, last_error_at_utc FROM order_sync_state WHERE connection_id = ?`, connectionID).Scan(&state.ConnectionID, &bootstrap, &completed, &watermark, &successful, &attempt, &errorKind, &errorAt)
	if err == sql.ErrNoRows {
		return SyncState{}, false, nil
	}
	if err != nil {
		return SyncState{}, false, classifyError(err)
	}
	state.BootstrapUpdatedAtMinUTC = time.UnixMicro(bootstrap.Int64).UTC()
	state.BootstrapCompletedAtUTC = nullableTimeFromInt(completed)
	state.HighWatermarkUpdatedAtUTC = nullableTimeFromInt(watermark)
	state.LastSuccessfulSyncAtUTC = nullableTimeFromInt(successful)
	state.LastAttemptAtUTC = nullableTimeFromInt(attempt)
	state.LastErrorAtUTC = nullableTimeFromInt(errorAt)
	if errorKind.Valid {
		state.LastErrorKind = errorKind.String
	}
	return state, true, nil
}

// BeginBootstrap creates a connection checkpoint or refreshes its attempted-at timestamp while retaining the earliest explicitly requested boundary.
func (s *SQLiteStore) BeginBootstrap(ctx context.Context, connectionID string, boundary, attemptedAt time.Time) error {
	_, err := s.database.ExecContext(ctx, `INSERT INTO order_sync_state(connection_id, bootstrap_created_at_min_utc, bootstrap_updated_at_min_utc, last_attempt_at_utc) VALUES (?, ?, ?, ?)
		ON CONFLICT(connection_id) DO UPDATE SET
		bootstrap_updated_at_min_utc = MIN(order_sync_state.bootstrap_updated_at_min_utc, excluded.bootstrap_updated_at_min_utc),
		last_attempt_at_utc = excluded.last_attempt_at_utc`, connectionID, boundary.UTC().UnixMicro(), boundary.UTC().UnixMicro(), attemptedAt.UTC().UnixMicro())
	return classifyError(err)
}

// RecordAttempt records a new synchronization attempt without changing a completed checkpoint.
func (s *SQLiteStore) RecordAttempt(ctx context.Context, connectionID string, attemptedAt time.Time) error {
	_, err := s.database.ExecContext(ctx, `UPDATE order_sync_state SET last_attempt_at_utc = ? WHERE connection_id = ?`, attemptedAt.UTC().UnixMicro(), connectionID)
	return classifyError(err)
}

// CompleteSync advances the completed checkpoint only after every remote page has been persisted successfully.
func (s *SQLiteStore) CompleteSync(ctx context.Context, connectionID string, bootstrap bool, maximumUpdatedAt *time.Time, completedAt time.Time) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return classifyError(err)
	}
	defer func() { _ = tx.Rollback() }()
	var current sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT high_watermark_updated_at_utc FROM order_sync_state WHERE connection_id = ?`, connectionID).Scan(&current); err != nil {
		return classifyError(err)
	}
	watermark := current
	if maximumUpdatedAt != nil && (!watermark.Valid || maximumUpdatedAt.UTC().UnixMicro() > watermark.Int64) {
		watermark = sql.NullInt64{Int64: maximumUpdatedAt.UTC().UnixMicro(), Valid: true}
	}
	var completedValue any
	if bootstrap {
		completedValue = completedAt.UTC().UnixMicro()
	} else {
		completedValue = nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE order_sync_state SET bootstrap_completed_at_utc = COALESCE(?, bootstrap_completed_at_utc), high_watermark_updated_at_utc = ?, last_successful_sync_at_utc = ?, last_error_kind = NULL, last_error_at_utc = NULL WHERE connection_id = ?`, completedValue, nullableInt(watermark), completedAt.UTC().UnixMicro(), connectionID)
	if err != nil {
		return classifyError(err)
	}
	return classifyError(tx.Commit())
}

// CompleteHistoricalSync records a fully completed older-history expansion and preserves the greatest completed remote watermark.
func (s *SQLiteStore) CompleteHistoricalSync(ctx context.Context, connectionID string, boundary time.Time, maximumUpdatedAt *time.Time, completedAt time.Time) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return classifyError(err)
	}
	defer func() { _ = tx.Rollback() }()
	var current sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT high_watermark_updated_at_utc FROM order_sync_state WHERE connection_id = ?`, connectionID).Scan(&current); err != nil {
		return classifyError(err)
	}
	watermark := current
	if maximumUpdatedAt != nil && (!watermark.Valid || maximumUpdatedAt.UTC().UnixMicro() > watermark.Int64) {
		watermark = sql.NullInt64{Int64: maximumUpdatedAt.UTC().UnixMicro(), Valid: true}
	}
	_, err = tx.ExecContext(ctx, `UPDATE order_sync_state SET
		bootstrap_updated_at_min_utc = MIN(bootstrap_updated_at_min_utc, ?),
		high_watermark_updated_at_utc = ?,
		last_successful_sync_at_utc = ?,
		last_error_kind = NULL,
		last_error_at_utc = NULL
		WHERE connection_id = ?`, boundary.UTC().UnixMicro(), nullableInt(watermark), completedAt.UTC().UnixMicro(), connectionID)
	if err != nil {
		return classifyError(err)
	}
	return classifyError(tx.Commit())
}

// RecordFailure stores only a caller-provided non-sensitive error class when attemptStartedAt still identifies the active synchronization attempt.
// A stale worker therefore cannot overwrite a later successful checkpoint or a newer attempt's error state.
func (s *SQLiteStore) RecordFailure(ctx context.Context, connectionID, kind string, attemptStartedAt, failedAt time.Time) error {
	_, err := s.database.ExecContext(ctx, `UPDATE order_sync_state SET last_error_kind = ?, last_error_at_utc = ?
		WHERE connection_id = ? AND last_attempt_at_utc = ?`, kind, failedAt.UTC().UnixMicro(), connectionID, attemptStartedAt.UTC().UnixMicro())
	return classifyError(err)
}

// DeleteConnectionData transactionally deletes every local Order row and checkpoint belonging to one connection.
func (s *SQLiteStore) DeleteConnectionData(ctx context.Context, connectionID string) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return classifyError(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM orders WHERE connection_id = ?`, connectionID); err != nil {
		return classifyError(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM order_sync_state WHERE connection_id = ?`, connectionID); err != nil {
		return classifyError(err)
	}
	return classifyError(tx.Commit())
}

// scanLocalRow reads scanner's nullable SQLite list projection into a presentation-safe storage record.
// It returns the populated row or the scanner error.
func scanLocalRow(scanner interface{ Scan(...any) error }) (LocalRow, error) {
	var row LocalRow
	var state, customer, address, total, commission, source sql.NullString
	var created, expected sql.NullInt64
	var updated, synced int64
	if err := scanner.Scan(&row.OrderID, &row.DisplayID, &state, &customer, &address, &total, &commission, &source, &created, &expected, &updated, &synced); err != nil {
		return LocalRow{}, err
	}
	row.State, row.CustomerName, row.AddressName, row.TotalDisplay, row.CommissionDisplay, row.Source = state.String, customer.String, address.String, total.String, commission.String, source.String
	row.CreatedAtUTC, row.ExpectedShipAtUTC = nullableTimeFromInt(created), nullableTimeFromInt(expected)
	row.UpdatedAtUTC, row.SyncedAtUTC = time.UnixMicro(updated).UTC(), time.UnixMicro(synced).UTC()
	return row, nil
}

// validateRecord ensures every record can participate in an idempotent connection-scoped upsert.
func validateRecord(record OrderRecord) error {
	if record.ConnectionID == "" || record.OrderID == "" || record.DisplayID == "" || record.UpdatedAtUTC.IsZero() || record.SnapshotJSON == "" || record.SnapshotSchemaVersion <= 0 || record.SyncedAtUTC.IsZero() {
		return fmt.Errorf("missing required order field: %w", ErrInvalidRecord)
	}
	return nil
}

// nullableText converts empty optional display fields to SQL NULL.
func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// nullableTime converts an optional time to integer microseconds for SQLite.
func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().UnixMicro()
}

// nullableTimeFromInt converts a nullable SQLite microsecond timestamp to UTC.
func nullableTimeFromInt(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	converted := time.UnixMicro(value.Int64).UTC()
	return &converted
}

// nullableInt returns nil for an absent SQLite integer value.
func nullableInt(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

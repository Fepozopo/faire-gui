package ordersstore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// TestOpenMigratesAndReopens verifies an empty private database migrates once and retains durable rows.
func TestOpenMigratesAndReopens(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "orders.sqlite3")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	record := testRecord("connection-a", "order-1", "DISPLAY-1", time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC))
	if err := store.UpsertOrders(ctx, []OrderRecord{record}); err != nil {
		t.Fatalf("UpsertOrders() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	page, err := store.List(ctx, ListQuery{ConnectionID: "connection-a", Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(page.Rows) != 1 || page.Rows[0].OrderID != "order-1" || page.Rows[0].AddressName != "Ada's Antiques" {
		t.Fatalf("List() = %#v, want durable order-1 with its delivery address", page.Rows)
	}
}

// TestMigrationsCarryForwardBoundaryAndAddressProjection verifies a v1 cache retains its date while migration 3 restores address names from valid snapshots.
func TestMigrationsCarryForwardBoundaryAndAddressProjection(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "orders.sqlite3")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	boundary := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	for _, statement := range []string{
		`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at_utc INTEGER NOT NULL)`,
		`INSERT INTO schema_migrations(version, applied_at_utc) VALUES (1, 0)`,
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
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			_ = database.Close()
			t.Fatalf("prepare v1 database error = %v", err)
		}
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO order_sync_state(connection_id, bootstrap_created_at_min_utc, bootstrap_completed_at_utc, high_watermark_updated_at_utc, last_successful_sync_at_utc) VALUES (?, ?, ?, ?, ?)`, "connection-a", boundary.UnixMicro(), boundary.UnixMicro(), boundary.UnixMicro(), boundary.UnixMicro()); err != nil {
		_ = database.Close()
		t.Fatalf("insert v1 state error = %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO orders(connection_id, order_id, display_id, updated_at_utc, order_snapshot_json, snapshot_schema_version, synced_at_utc) VALUES (?, ?, ?, ?, ?, ?, ?)`, "connection-a", "order-1", "DISPLAY-1", boundary.UnixMicro(), `{"id":"order-1","address":{"name":"Ada's Antiques"}}`, 1, boundary.UnixMicro()); err != nil {
		_ = database.Close()
		t.Fatalf("insert v1 order error = %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close v1 database error = %v", err)
	}
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	state, found, err := store.SyncState(ctx, "connection-a")
	if err != nil || !found || !state.BootstrapUpdatedAtMinUTC.Equal(boundary) || state.BootstrapCompletedAtUTC != nil || state.HighWatermarkUpdatedAtUTC != nil || state.LastSuccessfulSyncAtUTC != nil {
		t.Fatalf("migrated state = %#v, found=%v, err=%v", state, found, err)
	}
	page, err := store.List(ctx, ListQuery{ConnectionID: "connection-a", Limit: 1})
	if err != nil || len(page.Rows) != 1 || page.Rows[0].AddressName != "Ada's Antiques" {
		t.Fatalf("migrated address projection = %#v, err=%v", page, err)
	}
}

// TestUpsertOrdersKeepsConnectionDataIsolatedAndRejectsOlderVersions verifies storage partitions and conflict safety.
func TestUpsertOrdersKeepsConnectionDataIsolatedAndRejectsOlderVersions(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	newer := time.Date(2026, 2, 2, 12, 0, 0, 0, time.UTC)
	older := newer.Add(-time.Minute)
	if err := store.UpsertOrders(ctx, []OrderRecord{
		testRecord("connection-a", "order-1", "DISPLAY-1", newer),
		testRecord("connection-b", "order-1", "DISPLAY-1", newer),
	}); err != nil {
		t.Fatalf("initial upsert error = %v", err)
	}
	stale := testRecord("connection-a", "order-1", "DISPLAY-1", older)
	stale.SnapshotJSON = `{"version":"older"}`
	stale.TotalDisplay = "USD 1.00"
	if err := store.UpsertOrders(ctx, []OrderRecord{stale}); err != nil {
		t.Fatalf("stale upsert error = %v", err)
	}
	snapshot, err := store.Snapshot(ctx, "connection-a", "order-1")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.SnapshotJSON == stale.SnapshotJSON {
		t.Fatal("older snapshot replaced newer local snapshot")
	}
	if err := store.DeleteConnectionData(ctx, "connection-a"); err != nil {
		t.Fatalf("DeleteConnectionData() error = %v", err)
	}
	if _, err := store.Snapshot(ctx, "connection-a", "order-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted connection snapshot error = %v, want ErrNotFound", err)
	}
	if _, err := store.Snapshot(ctx, "connection-b", "order-1"); err != nil {
		t.Fatalf("other connection snapshot error = %v", err)
	}
}

// TestListUsesStateFilterAndStableKeysetPagination verifies local table reads filter and paginate deterministically.
func TestListUsesStateFilterAndStableKeysetPagination(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	created := time.Date(2026, 2, 3, 12, 0, 0, 0, time.UTC)
	first := testRecord("connection-a", "order-a", "DISPLAY-A", created)
	first.State = "NEW"
	second := testRecord("connection-a", "order-b", "DISPLAY-B", created)
	second.State = "NEW"
	third := testRecord("connection-a", "order-c", "DISPLAY-C", created.Add(-time.Minute))
	third.State = "PROCESSING"
	if err := store.UpsertOrders(ctx, []OrderRecord{first, second, third}); err != nil {
		t.Fatalf("UpsertOrders() error = %v", err)
	}
	page, err := store.List(ctx, ListQuery{ConnectionID: "connection-a", States: []string{"NEW"}, Limit: 1})
	if err != nil {
		t.Fatalf("first List() error = %v", err)
	}
	if len(page.Rows) != 1 || page.Rows[0].OrderID != "order-a" || page.NextCursor == nil {
		t.Fatalf("first page = %#v, want order-a with cursor", page)
	}
	page, err = store.List(ctx, ListQuery{ConnectionID: "connection-a", States: []string{"NEW"}, After: page.NextCursor, Limit: 1})
	if err != nil {
		t.Fatalf("second List() error = %v", err)
	}
	if len(page.Rows) != 1 || page.Rows[0].OrderID != "order-b" || page.NextCursor != nil {
		t.Fatalf("second page = %#v, want final order-b", page)
	}
}

// TestCountByStateScopesNewOrderCountsToTheActiveConnection verifies tab counts include every matching local order without crossing connections.
func TestCountByStateScopesNewOrderCountsToTheActiveConnection(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	updated := time.Date(2026, 2, 3, 12, 0, 0, 0, time.UTC)
	newFirst := testRecord("connection-a", "order-new-1", "DISPLAY-NEW-1", updated)
	newFirst.State = "NEW"
	newSecond := testRecord("connection-a", "order-new-2", "DISPLAY-NEW-2", updated)
	newSecond.State = "NEW"
	processing := testRecord("connection-a", "order-processing", "DISPLAY-PROCESSING", updated)
	processing.State = "PROCESSING"
	otherConnection := testRecord("connection-b", "order-new-other", "DISPLAY-NEW-OTHER", updated)
	otherConnection.State = "NEW"
	if err := store.UpsertOrders(ctx, []OrderRecord{newFirst, newSecond, processing, otherConnection}); err != nil {
		t.Fatalf("UpsertOrders() error = %v", err)
	}

	count, err := store.CountByState(ctx, "connection-a", "NEW")
	if err != nil || count != 2 {
		t.Fatalf("CountByState() = %d, %v; want 2, nil", count, err)
	}
}

// TestListSortsExpectedShipDateInBothDirections verifies local ship-date sorting and keyset pagination remain deterministic.
func TestListSortsExpectedShipDateInBothDirections(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	updated := time.Date(2026, 2, 3, 12, 0, 0, 0, time.UTC)
	earlyShip, lateShip := updated.Add(24*time.Hour), updated.Add(72*time.Hour)
	first := testRecord("connection-a", "order-a", "DISPLAY-A", updated)
	first.ExpectedShipAtUTC = &earlyShip
	second := testRecord("connection-a", "order-b", "DISPLAY-B", updated)
	second.ExpectedShipAtUTC = &lateShip
	third := testRecord("connection-a", "order-c", "DISPLAY-C", updated)
	if err := store.UpsertOrders(ctx, []OrderRecord{first, second, third}); err != nil {
		t.Fatalf("UpsertOrders() error = %v", err)
	}
	descending, err := store.List(ctx, ListQuery{ConnectionID: "connection-a", SortColumn: LocalSortExpectedShipAt, Descending: true, Limit: 1})
	if err != nil || len(descending.Rows) != 1 || descending.Rows[0].OrderID != "order-b" || descending.NextCursor == nil {
		t.Fatalf("descending List() = %#v, err=%v", descending, err)
	}
	descending, err = store.List(ctx, ListQuery{ConnectionID: "connection-a", SortColumn: LocalSortExpectedShipAt, Descending: true, After: descending.NextCursor, Limit: 2})
	if err != nil || len(descending.Rows) != 2 || descending.Rows[0].OrderID != "order-a" || descending.Rows[1].OrderID != "order-c" {
		t.Fatalf("descending next List() = %#v, err=%v", descending, err)
	}
	ascending, err := store.List(ctx, ListQuery{ConnectionID: "connection-a", SortColumn: LocalSortExpectedShipAt, Descending: false, Limit: 3})
	if err != nil || len(ascending.Rows) != 3 || ascending.Rows[0].OrderID != "order-a" || ascending.Rows[1].OrderID != "order-b" || ascending.Rows[2].OrderID != "order-c" {
		t.Fatalf("ascending List() = %#v, err=%v", ascending, err)
	}
}

// TestCompleteSyncOnlyAdvancesCompletedWatermark verifies final checkpoints never move backwards.
func TestCompleteSyncOnlyAdvancesCompletedWatermark(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	boundary := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	first := boundary.Add(2 * time.Hour)
	if err := store.BeginBootstrap(ctx, "connection-a", boundary, boundary); err != nil {
		t.Fatalf("BeginBootstrap() error = %v", err)
	}
	if err := store.CompleteSync(ctx, "connection-a", true, &first, first); err != nil {
		t.Fatalf("CompleteSync() error = %v", err)
	}
	older := first.Add(-time.Hour)
	if err := store.CompleteSync(ctx, "connection-a", false, &older, first.Add(time.Hour)); err != nil {
		t.Fatalf("second CompleteSync() error = %v", err)
	}
	state, found, err := store.SyncState(ctx, "connection-a")
	if err != nil || !found || state.HighWatermarkUpdatedAtUTC == nil || !state.HighWatermarkUpdatedAtUTC.Equal(first) {
		t.Fatalf("SyncState() = %#v, found=%v, err=%v; want original watermark", state, found, err)
	}
}

// TestRecordFailureCannotOverwriteANewerCompletedSync verifies a stale worker cannot reintroduce an invalid-request marker after rebuild or later success.
func TestRecordFailureCannotOverwriteANewerCompletedSync(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	boundary := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	firstAttempt := boundary.Add(time.Hour)
	secondAttempt := firstAttempt.Add(time.Hour)
	watermark := secondAttempt.Add(time.Hour)
	if err := store.BeginBootstrap(ctx, "connection-a", boundary, firstAttempt); err != nil {
		t.Fatalf("first BeginBootstrap() error = %v", err)
	}
	if err := store.BeginBootstrap(ctx, "connection-a", boundary, secondAttempt); err != nil {
		t.Fatalf("second BeginBootstrap() error = %v", err)
	}
	if err := store.CompleteSync(ctx, "connection-a", true, &watermark, watermark); err != nil {
		t.Fatalf("CompleteSync() error = %v", err)
	}
	if err := store.RecordFailure(ctx, "connection-a", "invalid_request", firstAttempt, watermark.Add(time.Minute)); err != nil {
		t.Fatalf("stale RecordFailure() error = %v", err)
	}
	state, found, err := store.SyncState(ctx, "connection-a")
	if err != nil || !found || state.LastSuccessfulSyncAtUTC == nil || state.LastErrorKind != "" || state.LastErrorAtUTC != nil {
		t.Fatalf("state after stale failure = %#v, found=%v, err=%v", state, found, err)
	}
}

// openTestStore opens a temporary migrated SQLite database for one test.
func openTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "orders.sqlite3"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// testRecord returns a valid private snapshot record with stable projection fields.
func testRecord(connectionID, orderID, displayID string, updatedAt time.Time) OrderRecord {
	created := updatedAt.Add(-time.Hour)
	return OrderRecord{
		ConnectionID:          connectionID,
		OrderID:               orderID,
		DisplayID:             displayID,
		State:                 "NEW",
		CustomerName:          "Customer",
		AddressName:           "Ada's Antiques",
		TotalDisplay:          "USD 12.34",
		CommissionDisplay:     "USD 1.23",
		Source:                "FAIRE",
		CreatedAtUTC:          &created,
		UpdatedAtUTC:          updatedAt,
		SnapshotJSON:          `{"id":"order"}`,
		SnapshotSchemaVersion: SnapshotSchemaVersion,
		SyncedAtUTC:           updatedAt,
	}
}

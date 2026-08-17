package orderssync

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Fepozopo/faire-gui/faire"
	"github.com/Fepozopo/faire-gui/internal/ordersstore"
)

// TestSyncBootstrapsAllPagesAndFinalizesCheckpoint verifies a 30-day bootstrap traverses every remote cursor with an exact cursor-only follow-up request.
func TestSyncBootstrapsAllPagesAndFinalizesCheckpoint(t *testing.T) {
	ctx := context.Background()
	store := openSyncStore(t)
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	var options []*faire.OrderListOptions
	source := SourceFunc(func(_ context.Context, received *faire.OrderListOptions) (*faire.OrderPage, error) {
		copied := *received
		options = append(options, &copied)
		if received.Cursor == nil {
			return &faire.OrderPage{Orders: []faire.Order{syncOrder("order-1", now.Add(-time.Hour))}, Cursor: faire.Ptr("cursor-1")}, nil
		}
		return &faire.OrderPage{Orders: []faire.Order{syncOrder("order-2", now)}, Cursor: nil}, nil
	})
	syncer := newTestSyncer(t, store, source, now)
	summary, err := syncer.Sync(ctx, "connection-a")
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if !summary.Bootstrap || summary.Orders != 2 {
		t.Fatalf("Summary = %#v, want bootstrap with two orders", summary)
	}
	page, err := store.List(ctx, ordersstore.ListQuery{ConnectionID: "connection-a", Limit: 2})
	if err != nil || len(page.Rows) != 2 || page.Rows[0].AddressName != "Ada's Antiques" || page.Rows[0].CommissionBPS == nil || *page.Rows[0].CommissionBPS != 1500 {
		t.Fatalf("stored business name and raw commission BPS = %#v, err=%v", page, err)
	}
	if len(options) != 2 || options[0].UpdatedAtMin == nil || options[0].CreatedAtMin != nil || options[0].SortBy == nil || *options[0].SortBy != faire.OrderSortByUpdatedAt || options[0].ExcludedStates != nil || options[1].Cursor == nil || *options[1].Cursor != "cursor-1" || options[1].Limit != nil || options[1].Page != nil || options[1].UpdatedAtMin != nil || options[1].CreatedAtMin != nil || options[1].SortBy != nil || options[1].ExcludedStates != nil || options[1].ShipAfterMax != nil || options[1].OriginalOrderID != nil {
		t.Fatalf("sync options = %#v", options)
	}
	wantBoundary := time.Date(2026, 1, 30, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	if *options[0].UpdatedAtMin != wantBoundary {
		t.Fatalf("UpdatedAtMin = %q, want %q", *options[0].UpdatedAtMin, wantBoundary)
	}
	state, found, err := store.SyncState(ctx, "connection-a")
	if err != nil || !found || state.BootstrapCompletedAtUTC == nil || state.HighWatermarkUpdatedAtUTC == nil || !state.HighWatermarkUpdatedAtUTC.Equal(now) {
		t.Fatalf("SyncState() = %#v, found=%v, err=%v", state, found, err)
	}
}

// TestSyncUsesOverlapForIncrementalRefresh verifies incremental requests replay the configured watermark tail.
func TestSyncUsesOverlapForIncrementalRefresh(t *testing.T) {
	ctx := context.Background()
	store := openSyncStore(t)
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	boundary := now.AddDate(0, 0, -defaultBootstrapLookbackDays)
	watermark := now.Add(-time.Minute)
	if err := store.BeginBootstrap(ctx, "connection-a", boundary, now); err != nil {
		t.Fatalf("BeginBootstrap() error = %v", err)
	}
	if err := store.CompleteSync(ctx, "connection-a", true, &watermark, now); err != nil {
		t.Fatalf("CompleteSync() error = %v", err)
	}
	var requested faire.OrderListOptions
	source := SourceFunc(func(_ context.Context, options *faire.OrderListOptions) (*faire.OrderPage, error) {
		requested = *options
		return &faire.OrderPage{}, nil
	})
	syncer := newTestSyncer(t, store, source, now)
	if _, err := syncer.Sync(ctx, "connection-a"); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	want := watermark.Add(-5 * time.Minute).Format(time.RFC3339Nano)
	if requested.UpdatedAtMin == nil || *requested.UpdatedAtMin != want || requested.CreatedAtMin != nil || requested.SortBy == nil || *requested.SortBy != faire.OrderSortByUpdatedAt || len(requested.ExcludedStates) != 0 {
		t.Fatalf("incremental request = %#v, want updated_at_min %q only", requested, want)
	}
	state, _, err := store.SyncState(ctx, "connection-a")
	if err != nil || state.HighWatermarkUpdatedAtUTC == nil || !state.HighWatermarkUpdatedAtUTC.Equal(watermark) || state.LastSuccessfulSyncAtUTC == nil {
		t.Fatalf("post-empty state = %#v, error = %v", state, err)
	}
}

// TestSyncFromUpdatedAtExpandsHistoryAcrossAllPages verifies an earlier manual update boundary fetches every historical page and persists that boundary.
func TestSyncFromUpdatedAtExpandsHistoryAcrossAllPages(t *testing.T) {
	ctx := context.Background()
	store := openSyncStore(t)
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	initialBoundary := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	historicalBoundary := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	watermark := now.Add(-time.Hour)
	if err := store.BeginBootstrap(ctx, "connection-a", initialBoundary, now); err != nil {
		t.Fatalf("BeginBootstrap() error = %v", err)
	}
	if err := store.CompleteSync(ctx, "connection-a", true, &watermark, now); err != nil {
		t.Fatalf("CompleteSync() error = %v", err)
	}
	requests := 0
	source := SourceFunc(func(_ context.Context, options *faire.OrderListOptions) (*faire.OrderPage, error) {
		requests++
		if requests == 1 {
			if options.UpdatedAtMin == nil || *options.UpdatedAtMin != historicalBoundary.Format(time.RFC3339Nano) || options.CreatedAtMin != nil || options.SortBy == nil || *options.SortBy != faire.OrderSortByUpdatedAt {
				t.Fatalf("initial history request options = %#v", options)
			}
			return &faire.OrderPage{Orders: []faire.Order{syncOrder("older-1", historicalBoundary.Add(time.Hour))}, Cursor: faire.Ptr("history-next")}, nil
		}
		if options.Cursor == nil || *options.Cursor != "history-next" || options.Limit != nil || options.UpdatedAtMin != nil || options.CreatedAtMin != nil || options.SortBy != nil {
			t.Fatalf("history cursor request options = %#v", options)
		}
		return &faire.OrderPage{Orders: []faire.Order{syncOrder("older-2", historicalBoundary.Add(2*time.Hour))}}, nil
	})
	syncer := newTestSyncer(t, store, source, now)
	summary, err := syncer.SyncFromUpdatedAt(ctx, "connection-a", historicalBoundary)
	if err != nil {
		t.Fatalf("SyncFromUpdatedAt() error = %v", err)
	}
	if !summary.HistoryExpanded || summary.Bootstrap || summary.Orders != 2 || requests != 2 {
		t.Fatalf("summary = %#v, requests=%d", summary, requests)
	}
	state, found, err := store.SyncState(ctx, "connection-a")
	if err != nil || !found || !state.BootstrapUpdatedAtMinUTC.Equal(historicalBoundary) || state.HighWatermarkUpdatedAtUTC == nil || !state.HighWatermarkUpdatedAtUTC.Equal(watermark) {
		t.Fatalf("expanded state = %#v, found=%v, err=%v", state, found, err)
	}
}

// TestSyncRetainsCompletedWatermarkAfterPartialFailure verifies replay-safe page commits do not falsely complete a sync.
func TestSyncRetainsCompletedWatermarkAfterPartialFailure(t *testing.T) {
	ctx := context.Background()
	store := openSyncStore(t)
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	boundary := now.AddDate(0, 0, -defaultBootstrapLookbackDays)
	watermark := now.Add(-time.Hour)
	if err := store.BeginBootstrap(ctx, "connection-a", boundary, now); err != nil {
		t.Fatalf("BeginBootstrap() error = %v", err)
	}
	if err := store.CompleteSync(ctx, "connection-a", true, &watermark, now); err != nil {
		t.Fatalf("CompleteSync() error = %v", err)
	}
	calls := 0
	source := SourceFunc(func(_ context.Context, _ *faire.OrderListOptions) (*faire.OrderPage, error) {
		calls++
		if calls == 1 {
			return &faire.OrderPage{Orders: []faire.Order{syncOrder("order-1", now)}, Cursor: faire.Ptr("next")}, nil
		}
		return nil, errors.New("network unavailable")
	})
	syncer := newTestSyncer(t, store, source, now)
	if _, err := syncer.Sync(ctx, "connection-a"); err == nil {
		t.Fatal("Sync() succeeded after page failure")
	}
	state, _, err := store.SyncState(ctx, "connection-a")
	if err != nil || state.HighWatermarkUpdatedAtUTC == nil || !state.HighWatermarkUpdatedAtUTC.Equal(watermark) {
		t.Fatalf("partial failure state = %#v, err = %v", state, err)
	}
	if _, err := store.Snapshot(ctx, "connection-a", "order-1"); err != nil {
		t.Fatalf("successful first-page snapshot was not retained: %v", err)
	}
}

// TestSyncClassifiesBadRequestAndRetainsItsSafePhase verifies an invalid remote request is retained without response content.
func TestSyncClassifiesBadRequestAndRetainsItsSafePhase(t *testing.T) {
	ctx := context.Background()
	store := openSyncStore(t)
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	source := SourceFunc(func(_ context.Context, _ *faire.OrderListOptions) (*faire.OrderPage, error) {
		return nil, &faire.APIError{StatusCode: 400}
	})
	syncer := newTestSyncer(t, store, source, now)
	_, err := syncer.Sync(ctx, "connection-a")
	var listError *ListError
	if !errors.As(err, &listError) || listError.Phase != ListPhaseBootstrap || listError.Cursor {
		t.Fatalf("Sync() error = %#v, want bootstrap ListError", err)
	}
	state, found, stateErr := store.SyncState(ctx, "connection-a")
	if stateErr != nil || !found || state.LastErrorKind != "invalid_request" || state.BootstrapCompletedAtUTC != nil {
		t.Fatalf("state = %#v, found=%v, err=%v", state, found, stateErr)
	}
}

// TestSyncRejectsRepeatedCursorWithoutCheckpointAdvance verifies protocol loops cannot mark unseen data complete.
func TestSyncRejectsRepeatedCursorWithoutCheckpointAdvance(t *testing.T) {
	ctx := context.Background()
	store := openSyncStore(t)
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	source := SourceFunc(func(_ context.Context, _ *faire.OrderListOptions) (*faire.OrderPage, error) {
		return &faire.OrderPage{Cursor: faire.Ptr("again")}, nil
	})
	syncer := newTestSyncer(t, store, source, now)
	_, err := syncer.Sync(ctx, "connection-a")
	if !errors.Is(err, ErrRepeatedCursor) {
		t.Fatalf("Sync() error = %v, want ErrRepeatedCursor", err)
	}
	state, found, stateErr := store.SyncState(ctx, "connection-a")
	if stateErr != nil || !found || state.BootstrapCompletedAtUTC != nil || state.HighWatermarkUpdatedAtUTC != nil {
		t.Fatalf("state after repeated cursor = %#v, found=%v, err=%v", state, found, stateErr)
	}
}

// openSyncStore opens a temporary real SQLite store for sync behavior tests.
func openSyncStore(t *testing.T) *ordersstore.SQLiteStore {
	t.Helper()
	store, err := ordersstore.Open(context.Background(), filepath.Join(t.TempDir(), "orders.sqlite3"))
	if err != nil {
		t.Fatalf("ordersstore.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// newTestSyncer constructs a deterministic coordinator using a fixed clock.
func newTestSyncer(t *testing.T, store ordersstore.Store, source Source, now time.Time) *Syncer {
	t.Helper()
	syncer, err := New(store, source, Config{Now: func() time.Time { return now }, Location: time.UTC})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return syncer
}

// syncOrder returns a minimal valid typed Faire order with a commission percentage for synchronization tests.
func syncOrder(id string, updatedAt time.Time) faire.Order {
	orderID := faire.OrderID(id)
	displayID := "DISPLAY-" + id
	createdAt := updatedAt.Add(-time.Hour).Format(time.RFC3339Nano)
	updated := updatedAt.Format(time.RFC3339Nano)
	commissionBPS := int64(1500)
	shippingRecipientName, businessName := "Ada Lovelace", "Ada's Antiques"
	return faire.Order{ID: &orderID, DisplayID: &displayID, CreatedAt: &createdAt, UpdatedAt: &updated, Address: &faire.Address{Name: &shippingRecipientName, CompanyName: &businessName}, PayoutCosts: &faire.PayoutCosts{CommissionBPS: &commissionBPS}}
}

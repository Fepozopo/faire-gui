package application

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Fepozopo/faire-gui/internal/ordersstore"
)

// TestLoadAndMaybeSyncRefreshPolicy verifies opening the Orders screen reads local data
// only, while selecting a connection begins an immediate synchronization attempt.
func TestLoadAndMaybeSyncRefreshPolicy(t *testing.T) {
	ctx := context.Background()
	store, err := ordersstore.Open(ctx, filepath.Join(t.TempDir(), "orders.sqlite3"))
	if err != nil {
		t.Fatalf("ordersstore.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	tests := []struct {
		name             string
		kind             ordersLoadKind
		wantKeepLoading  bool
		wantFollowUp     bool
		wantFollowUpText string
	}{
		{
			name:            "initial load remains local",
			kind:            ordersLoadInitial,
			wantKeepLoading: false,
		},
		{
			name:             "connection selection refreshes immediately",
			kind:             ordersLoadConnectionRefresh,
			wantKeepLoading:  true,
			wantFollowUp:     true,
			wantFollowUpText: "Showing locally stored orders. Saved connections are unavailable.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controller := newOrdersController(ctx, store, nil, new(sync.WaitGroup), nil)
			controller.loadAndMaybeSync(ordersLoadRequest{
				RequestID:    1,
				ConnectionID: "connection-a",
				State:        controller.view.state,
				Kind:         test.kind,
			})

			first := <-controller.loadResults
			if first.KeepLoading != test.wantKeepLoading {
				t.Fatalf("first KeepLoading = %t, want %t", first.KeepLoading, test.wantKeepLoading)
			}
			if test.wantFollowUp {
				if first.Status != "Checking Faire for updated orders…" {
					t.Fatalf("first Status = %q, want refresh progress", first.Status)
				}
				second := <-controller.loadResults
				if second.Status != test.wantFollowUpText || second.KeepLoading {
					t.Fatalf("follow-up result = %#v, want status %q with loading cleared", second, test.wantFollowUpText)
				}
			}
		})
	}
}

// TestFormatOrdersUpdatedAtUses12HourClock verifies Orders status timestamps retain
// the local date while rendering afternoon times with an AM/PM marker.
func TestFormatOrdersUpdatedAtUses12HourClock(t *testing.T) {
	updatedAt := time.Date(2026, time.January, 2, 15, 4, 0, 0, time.Local)
	if got, want := formatOrdersUpdatedAt(updatedAt), "Jan 2, 3:04 PM"; got != want {
		t.Fatalf("formatOrdersUpdatedAt() = %q, want %q", got, want)
	}
}

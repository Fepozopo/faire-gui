package orderssync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Fepozopo/faire-gui/faire"
	"github.com/Fepozopo/faire-gui/internal/ordersstore"
)

// ErrRepeatedCursor indicates that remote pagination would otherwise loop indefinitely.
var ErrRepeatedCursor = errors.New("orders sync: repeated cursor")

// ErrInvalidRemoteOrder indicates a remote order cannot safely participate in checkpointed synchronization.
var ErrInvalidRemoteOrder = errors.New("orders sync: invalid remote order")

const defaultBootstrapLookbackDays = 30

// ListPhase identifies the non-sensitive synchronization request category that failed.
type ListPhase string

const (
	// ListPhaseBootstrap identifies a first 30-day historical synchronization request.
	ListPhaseBootstrap ListPhase = "bootstrap"
	// ListPhaseHistory identifies an explicit earlier-history expansion request.
	ListPhaseHistory ListPhase = "history"
	// ListPhaseIncremental identifies an overlap-safe updated-orders request.
	ListPhaseIncremental ListPhase = "incremental"
)

// ListError wraps a failed remote list call with safe request-phase metadata.
type ListError struct {
	Phase  ListPhase
	Cursor bool
	Err    error
}

// Error returns a credential-safe synchronization failure description without response content or request data.
func (e *ListError) Error() string {
	return "orders sync list request failed"
}

// Unwrap exposes the underlying error for safe classification by callers.
func (e *ListError) Unwrap() error {
	return e.Err
}

// Config controls bounded and overlap-safe remote synchronization.
type Config struct {
	// PageSize is the maximum number of Orders requested in each remote page.
	PageSize int
	// Overlap is subtracted from a completed watermark before an incremental request.
	Overlap time.Duration
	// Now supplies the current time and makes checkpoint behavior deterministic in tests.
	Now func() time.Time
	// Location defines the start-of-day boundary used for the 30-day initial bootstrap window.
	Location *time.Location
}

// Summary reports safe synchronization facts that application code may publish to the UI.
type Summary struct {
	Bootstrap       bool
	HistoryExpanded bool
	Orders          int
	StartedAt       time.Time
	FinishedAt      time.Time
}

// Syncer coordinates connection-scoped Faire order pages and an Orders store.
type Syncer struct {
	store    ordersstore.Store
	source   Source
	pageSize int
	overlap  time.Duration
	now      func() time.Time
	location *time.Location
}

// New creates a sync coordinator from its storage and remote dependencies.
func New(store ordersstore.Store, source Source, config Config) (*Syncer, error) {
	if store == nil {
		return nil, fmt.Errorf("orders sync store is required")
	}
	if source == nil {
		return nil, fmt.Errorf("orders sync source is required")
	}
	if config.PageSize <= 0 {
		config.PageSize = 50
	}
	if config.Overlap <= 0 {
		config.Overlap = 5 * time.Minute
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Location == nil {
		config.Location = time.Local
	}
	return &Syncer{store: store, source: source, pageSize: config.PageSize, overlap: config.Overlap, now: config.Now, location: config.Location}, nil
}

// Sync performs either an initial 30-day bootstrap or an overlap-safe incremental refresh for connectionID.
func (s *Syncer) Sync(ctx context.Context, connectionID string) (Summary, error) {
	return s.sync(ctx, connectionID, nil)
}

// SyncFromUpdatedAt expands the connection's retained update history when boundary is earlier than the completed bootstrap boundary.
// A later boundary remains a local view filter and follows the normal incremental synchronization path.
func (s *Syncer) SyncFromUpdatedAt(ctx context.Context, connectionID string, boundary time.Time) (Summary, error) {
	if boundary.IsZero() {
		return Summary{}, fmt.Errorf("history boundary: %w", ErrInvalidRemoteOrder)
	}
	boundary = boundary.UTC()
	return s.sync(ctx, connectionID, &boundary)
}

// sync coordinates bootstrap, explicit historical expansion, and ordinary overlap-safe incremental synchronization.
func (s *Syncer) sync(ctx context.Context, connectionID string, requestedBoundary *time.Time) (summary Summary, err error) {
	if strings.TrimSpace(connectionID) == "" {
		return Summary{}, fmt.Errorf("connection ID: %w", ErrInvalidRemoteOrder)
	}
	startedAt := s.now().UTC()
	summary.StartedAt = startedAt
	state, found, err := s.store.SyncState(ctx, connectionID)
	if err != nil {
		return summary, err
	}
	bootstrap := !found || state.BootstrapCompletedAtUTC == nil
	historyExpansion := !bootstrap && requestedBoundary != nil && requestedBoundary.Before(state.BootstrapUpdatedAtMinUTC)
	var options faire.OrderListOptions
	if bootstrap || historyExpansion {
		boundary := bootstrapUpdatedAtBoundary(startedAt, s.location)
		if found {
			boundary = state.BootstrapUpdatedAtMinUTC
		}
		if requestedBoundary != nil && requestedBoundary.Before(boundary) {
			boundary = *requestedBoundary
		}
		if bootstrap {
			if err := s.store.BeginBootstrap(ctx, connectionID, boundary, startedAt); err != nil {
				return summary, err
			}
		} else if err := s.store.RecordAttempt(ctx, connectionID, startedAt); err != nil {
			return summary, err
		}
		formatted := boundary.Format(time.RFC3339Nano)
		options.UpdatedAtMin = &formatted
		state.BootstrapUpdatedAtMinUTC = boundary
	} else {
		if err := s.store.RecordAttempt(ctx, connectionID, startedAt); err != nil {
			return summary, err
		}
		boundary := state.BootstrapUpdatedAtMinUTC
		if state.HighWatermarkUpdatedAtUTC != nil {
			overlapBoundary := state.HighWatermarkUpdatedAtUTC.Add(-s.overlap)
			if overlapBoundary.After(boundary) {
				boundary = overlapBoundary
			}
		}
		formatted := boundary.Format(time.RFC3339Nano)
		options.UpdatedAtMin = &formatted
	}
	summary.Bootstrap = bootstrap
	summary.HistoryExpanded = historyExpansion
	options.Limit = faire.Ptr(int64(s.pageSize))
	// Faire recommends explicit updated-at ordering for predictable polling and cursor traversal.
	sortBy := faire.OrderSortByUpdatedAt
	options.SortBy = &sortBy
	phase := ListPhaseIncremental
	if bootstrap {
		phase = ListPhaseBootstrap
	} else if historyExpansion {
		phase = ListPhaseHistory
	}

	maximumUpdatedAt, ordersCount, syncErr := s.syncPages(ctx, connectionID, options, phase)
	if syncErr != nil {
		// Failure metadata is deliberately a small safe category; the UI never receives the raw failure.
		_ = s.store.RecordFailure(context.Background(), connectionID, errorKind(syncErr), startedAt, s.now().UTC())
		return summary, syncErr
	}
	finishedAt := s.now().UTC()
	if historyExpansion {
		err = s.store.CompleteHistoricalSync(ctx, connectionID, state.BootstrapUpdatedAtMinUTC, maximumUpdatedAt, finishedAt)
	} else {
		err = s.store.CompleteSync(ctx, connectionID, bootstrap, maximumUpdatedAt, finishedAt)
	}
	if err != nil {
		_ = s.store.RecordFailure(context.Background(), connectionID, "storage", startedAt, s.now().UTC())
		return summary, err
	}
	summary.Orders = ordersCount
	summary.FinishedAt = finishedAt
	return summary, nil
}

// bootstrapUpdatedAtBoundary returns the Orders UI's 30-day start-of-day local update boundary in UTC.
func bootstrapUpdatedAtBoundary(now time.Time, location *time.Location) time.Time {
	local := now.In(location).AddDate(0, 0, -defaultBootstrapLookbackDays)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).UTC()
}

// syncPages follows remote cursors, validates every full snapshot, and commits each page before the final checkpoint.
func (s *Syncer) syncPages(ctx context.Context, connectionID string, options faire.OrderListOptions, phase ListPhase) (*time.Time, int, error) {
	seenCursors := make(map[string]struct{})
	var maximumUpdatedAt *time.Time
	ordersCount := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, ordersCount, err
		}
		page, err := s.source.List(ctx, &options)
		if err != nil {
			return nil, ordersCount, &ListError{Phase: phase, Cursor: options.Cursor != nil && strings.TrimSpace(*options.Cursor) != "", Err: err}
		}
		if page == nil {
			return nil, ordersCount, fmt.Errorf("nil order page: %w", ErrInvalidRemoteOrder)
		}
		records := make([]ordersstore.OrderRecord, 0, len(page.Orders))
		for _, order := range page.Orders {
			record, err := RecordFromOrder(connectionID, order, s.now().UTC())
			if err != nil {
				return nil, ordersCount, err
			}
			records = append(records, record)
			if maximumUpdatedAt == nil || record.UpdatedAtUTC.After(*maximumUpdatedAt) {
				updated := record.UpdatedAtUTC
				maximumUpdatedAt = &updated
			}
		}
		if err := s.store.UpsertOrders(ctx, records); err != nil {
			return nil, ordersCount, err
		}
		ordersCount += len(records)
		if page.Cursor == nil || strings.TrimSpace(*page.Cursor) == "" {
			return maximumUpdatedAt, ordersCount, nil
		}
		cursor := strings.TrimSpace(*page.Cursor)
		if _, duplicate := seenCursors[cursor]; duplicate {
			return nil, ordersCount, fmt.Errorf("%w: %q", ErrRepeatedCursor, cursor)
		}
		seenCursors[cursor] = struct{}{}
		options = cursorPageOptions(cursor)
	}
}

// cursorPageOptions builds a follow-up Orders request from exactly the cursor Faire returned.
// Faire embeds the original listing criteria in its opaque cursor, so replaying filters alongside it can make the request invalid.
func cursorPageOptions(cursor string) faire.OrderListOptions {
	return faire.OrderListOptions{Cursor: &cursor}
}

// RecordFromOrder validates order, verifies its JSON round trip, and builds an atomic stored representation for connectionID at syncedAt.
// It preserves every supported typed Order field in the private snapshot and returns the record or a validation error.
// It is used by synchronization and explicit per-order refreshes; callers must upsert the result without advancing a feed checkpoint.
func RecordFromOrder(connectionID string, order faire.Order, syncedAt time.Time) (ordersstore.OrderRecord, error) {
	if order.ID == nil || strings.TrimSpace(string(*order.ID)) == "" || order.UpdatedAt == nil {
		return ordersstore.OrderRecord{}, fmt.Errorf("missing order ID or updated_at: %w", ErrInvalidRemoteOrder)
	}
	updatedAt, err := parseTimestamp(*order.UpdatedAt)
	if err != nil {
		return ordersstore.OrderRecord{}, fmt.Errorf("invalid updated_at: %w", ErrInvalidRemoteOrder)
	}
	snapshot, err := json.Marshal(order)
	if err != nil {
		return ordersstore.OrderRecord{}, fmt.Errorf("serialize order snapshot: %w", ErrInvalidRemoteOrder)
	}
	var verified faire.Order
	if err := json.Unmarshal(snapshot, &verified); err != nil {
		return ordersstore.OrderRecord{}, fmt.Errorf("verify order snapshot: %w", ErrInvalidRemoteOrder)
	}
	if verified.ID == nil || *verified.ID != *order.ID {
		return ordersstore.OrderRecord{}, fmt.Errorf("verify order ID: %w", ErrInvalidRemoteOrder)
	}
	row := projectOrder(order)
	displayID := row.displayID
	if displayID == "" {
		displayID = string(*order.ID)
	}
	return ordersstore.OrderRecord{
		ConnectionID:          connectionID,
		OrderID:               string(*order.ID),
		DisplayID:             displayID,
		State:                 row.state,
		AddressName:           row.addressName,
		TotalAmountMinor:      row.totalAmountMinor,
		TotalCurrency:         row.totalCurrency,
		CommissionBPS:         row.commissionBPS,
		Source:                row.source,
		CreatedAtUTC:          optionalTimestamp(order.CreatedAt),
		ExpectedShipAtUTC:     firstTimestamp(order.ExpectedShipDate, order.RequestedShipDate, order.ShipAfter),
		UpdatedAtUTC:          updatedAt,
		SnapshotJSON:          string(snapshot),
		SnapshotSchemaVersion: ordersstore.SnapshotSchemaVersion,
		SyncedAtUTC:           syncedAt,
	}, nil
}

// projection contains storage-owned raw list values, including total minor units, commission BPS, and the delivery business or recipient name, derived atomically from a remote Order.
type projection struct {
	displayID        string
	state            string
	addressName      string
	totalAmountMinor *int64
	totalCurrency    string
	commissionBPS    *int64
	source           string
}

// projectOrder derives raw list columns, including total minor units, commission BPS, and the delivery business or recipient name, from order without exposing a raw Order outside the worker.
// It returns the storage-owned projection.
func projectOrder(order faire.Order) projection {
	value := projection{}
	if order.DisplayID != nil {
		value.displayID = strings.TrimSpace(*order.DisplayID)
	}
	if order.State != nil {
		value.state = string(*order.State)
	}
	value.addressName = shippingBusinessOrRecipientName(order.Address)
	value.totalAmountMinor, value.totalCurrency = faire.OrderItemsTotal(order.Items)
	value.commissionBPS = commissionBPS(order.PayoutCosts)
	if order.Source != nil {
		value.source = strings.TrimSpace(*order.Source)
	}
	return value
}

// shippingBusinessOrRecipientName returns address's business name, falling back to its shipping recipient name.
// It returns an empty string when Faire did not provide either value.
func shippingBusinessOrRecipientName(address *faire.Address) string {
	if address == nil {
		return ""
	}
	if companyName := optionalString(address.CompanyName); companyName != "" {
		return companyName
	}
	return optionalString(address.Name)
}

// commissionBPS copies Faire's raw commission_bps field from costs for the Orders table projection.
// It returns nil when the API did not provide a commission percentage.
func commissionBPS(costs *faire.PayoutCosts) *int64 {
	if costs == nil || costs.CommissionBPS == nil {
		return nil
	}
	value := *costs.CommissionBPS
	return &value
}

// optionalString returns trimmed optional text for storage projections.
func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// parseTimestamp accepts the documented RFC 3339 timestamp representation used by Faire Orders.
func parseTimestamp(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

// optionalTimestamp parses an optional timestamp and omits malformed display-only values from indexed ordering.
func optionalTimestamp(value *string) *time.Time {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	parsed, err := parseTimestamp(*value)
	if err != nil {
		return nil
	}
	return &parsed
}

// firstTimestamp parses the first valid optional timestamp in priority order.
func firstTimestamp(values ...*string) *time.Time {
	for _, value := range values {
		if parsed := optionalTimestamp(value); parsed != nil {
			return parsed
		}
	}
	return nil
}

// errorKind classifies errors without propagating remote bodies, snapshots, paths, or credentials.
func errorKind(err error) string {
	var apiError *faire.APIError
	if errors.As(err, &apiError) && apiError.StatusCode == http.StatusBadRequest {
		return "invalid_request"
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, ErrInvalidRemoteOrder):
		return "invalid_remote_order"
	case errors.Is(err, ErrRepeatedCursor):
		return "protocol"
	case errors.Is(err, ordersstore.ErrCorruptData):
		return "corrupt_local_data"
	default:
		return "remote_or_storage"
	}
}

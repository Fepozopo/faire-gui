package orderssync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Fepozopo/faire-gui/faire"
	"github.com/Fepozopo/faire-gui/internal/ordersstore"
)

// ErrRepeatedCursor indicates that remote pagination would otherwise loop indefinitely.
var ErrRepeatedCursor = errors.New("orders sync: repeated cursor")

// ErrInvalidRemoteOrder indicates a remote order cannot safely participate in checkpointed synchronization.
var ErrInvalidRemoteOrder = errors.New("orders sync: invalid remote order")

// Config controls bounded and overlap-safe remote synchronization.
type Config struct {
	// PageSize is the maximum number of Orders requested in each remote page.
	PageSize int
	// Overlap is subtracted from a completed watermark before an incremental request.
	Overlap time.Duration
	// Now supplies the current time and makes checkpoint behavior deterministic in tests.
	Now func() time.Time
	// Location defines the start-of-day boundary used for the one-year initial bootstrap window.
	Location *time.Location
}

// Summary reports safe synchronization facts that application code may publish to the UI.
type Summary struct {
	Bootstrap  bool
	Orders     int
	StartedAt  time.Time
	FinishedAt time.Time
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

// Sync performs either an initial one-year bootstrap or an overlap-safe incremental refresh for connectionID.
func (s *Syncer) Sync(ctx context.Context, connectionID string) (summary Summary, err error) {
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
	var options faire.OrderListOptions
	if bootstrap {
		boundary := bootstrapBoundary(startedAt, s.location)
		if found {
			boundary = state.BootstrapCreatedAtMinUTC
		}
		if err := s.store.BeginBootstrap(ctx, connectionID, boundary, startedAt); err != nil {
			return summary, err
		}
		formatted := boundary.Format(time.RFC3339Nano)
		options.CreatedAtMin = &formatted
	} else {
		if err := s.store.RecordAttempt(ctx, connectionID, startedAt); err != nil {
			return summary, err
		}
		boundary := state.BootstrapCreatedAtMinUTC
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
	options.Limit = faire.Ptr(int64(s.pageSize))

	maximumUpdatedAt, ordersCount, syncErr := s.syncPages(ctx, connectionID, options)
	if syncErr != nil {
		// Failure metadata is deliberately a small safe category; the UI never receives the raw failure.
		_ = s.store.RecordFailure(context.Background(), connectionID, errorKind(syncErr), s.now().UTC())
		return summary, syncErr
	}
	finishedAt := s.now().UTC()
	if err := s.store.CompleteSync(ctx, connectionID, bootstrap, maximumUpdatedAt, finishedAt); err != nil {
		_ = s.store.RecordFailure(context.Background(), connectionID, "storage", s.now().UTC())
		return summary, err
	}
	summary.Orders = ordersCount
	summary.FinishedAt = finishedAt
	return summary, nil
}

// bootstrapBoundary returns the existing Orders UI's one-year start-of-day local historical boundary in UTC.
func bootstrapBoundary(now time.Time, location *time.Location) time.Time {
	local := now.In(location).AddDate(-1, 0, 0)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).UTC()
}

// syncPages follows remote cursors, validates every full snapshot, and commits each page before the final checkpoint.
func (s *Syncer) syncPages(ctx context.Context, connectionID string, options faire.OrderListOptions) (*time.Time, int, error) {
	seenCursors := make(map[string]struct{})
	var maximumUpdatedAt *time.Time
	ordersCount := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, ordersCount, err
		}
		page, err := s.source.List(ctx, &options)
		if err != nil {
			return nil, ordersCount, err
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
		options.Cursor = &cursor
	}
}

// RecordFromOrder validates one remote order, verifies its JSON round trip, and builds its atomic stored representation.
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
		CustomerName:          row.customer,
		TotalDisplay:          row.total,
		CommissionDisplay:     row.commission,
		Source:                row.source,
		CreatedAtUTC:          optionalTimestamp(order.CreatedAt),
		ExpectedShipAtUTC:     firstTimestamp(order.ExpectedShipDate, order.RequestedShipDate, order.ShipAfter),
		UpdatedAtUTC:          updatedAt,
		SnapshotJSON:          string(snapshot),
		SnapshotSchemaVersion: ordersstore.SnapshotSchemaVersion,
		SyncedAtUTC:           syncedAt,
	}, nil
}

// projection contains storage-owned list values derived atomically from a remote Order.
type projection struct {
	displayID  string
	state      string
	customer   string
	total      string
	commission string
	source     string
}

// projectOrder derives list columns without exposing a raw Order outside the worker.
func projectOrder(order faire.Order) projection {
	value := projection{}
	if order.DisplayID != nil {
		value.displayID = strings.TrimSpace(*order.DisplayID)
	}
	if order.State != nil {
		value.state = string(*order.State)
	}
	if order.Customer != nil {
		value.customer = strings.TrimSpace(strings.Join([]string{optionalString(order.Customer.FirstName), optionalString(order.Customer.LastName)}, " "))
	}
	value.total = orderTotal(order.Items)
	value.commission = commission(order.PayoutCosts)
	if order.Source != nil {
		value.source = strings.TrimSpace(*order.Source)
	}
	return value
}

// orderTotal produces the stable storage projection used by the existing Orders table.
func orderTotal(items []faire.OrderItem) string {
	var amount int64
	currency := ""
	found := false
	for _, item := range items {
		quantity := int64(1)
		if item.Quantity != nil {
			quantity = *item.Quantity
		}
		var itemAmount int64
		var itemCurrency string
		switch {
		case item.Price != nil && item.Price.AmountMinor != nil && item.Price.Currency != nil && *item.Price.Currency != "":
			itemAmount, itemCurrency = *item.Price.AmountMinor*quantity, *item.Price.Currency
		case item.PriceCents != nil:
			itemAmount, itemCurrency = *item.PriceCents*quantity, "USD"
		default:
			continue
		}
		if currency == "" {
			currency = itemCurrency
		}
		if currency != itemCurrency {
			return ""
		}
		amount += itemAmount
		found = true
	}
	if !found {
		return ""
	}
	return formatMoney(amount, currency)
}

// commission derives the stored commission display from modern or legacy Faire payout fields.
func commission(costs *faire.PayoutCosts) string {
	if costs == nil {
		return ""
	}
	if costs.Commission != nil && costs.Commission.AmountMinor != nil && costs.Commission.Currency != nil && *costs.Commission.Currency != "" {
		return formatMoney(*costs.Commission.AmountMinor, *costs.Commission.Currency)
	}
	if costs.CommissionCents != nil {
		return formatMoney(*costs.CommissionCents, "USD")
	}
	return ""
}

// formatMoney formats a minor-unit amount consistently with the existing Orders list.
func formatMoney(amount int64, currency string) string {
	sign := ""
	if amount < 0 {
		sign, amount = "-", -amount
	}
	return fmt.Sprintf("%s%s %d.%02d", sign, strings.ToUpper(currency), amount/100, amount%100)
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

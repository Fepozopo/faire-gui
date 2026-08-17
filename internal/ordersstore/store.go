package ordersstore

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound indicates that a connection-scoped record does not exist.
var ErrNotFound = errors.New("orders store: record not found")

// ErrInvalidRecord indicates that a caller attempted to persist an incomplete order record.
var ErrInvalidRecord = errors.New("orders store: invalid record")

// ErrUnsupportedSchema indicates that a database was created by a newer application version.
var ErrUnsupportedSchema = errors.New("orders store: unsupported schema version")

// ErrCorruptData indicates that SQLite or a stored snapshot is unreadable and must be rebuilt explicitly.
var ErrCorruptData = errors.New("orders store: corrupt local data")

// SnapshotSchemaVersion identifies the application snapshot mapping currently stored in SQLite.
const SnapshotSchemaVersion = 1

// OrderRecord is one atomic, connection-scoped Orders snapshot and its indexed list projection.
// SnapshotJSON must contain only a serialized typed Order, never credentials or HTTP metadata.
type OrderRecord struct {
	ConnectionID          string
	OrderID               string
	DisplayID             string
	State                 string
	CustomerName          string
	TotalDisplay          string
	CommissionDisplay     string
	Source                string
	CreatedAtUTC          *time.Time
	ExpectedShipAtUTC     *time.Time
	UpdatedAtUTC          time.Time
	SnapshotJSON          string
	SnapshotSchemaVersion int
	SyncedAtUTC           time.Time
}

// LocalRow is the safe indexed projection needed to present one Orders table row.
// It deliberately excludes the complete snapshot and its private nested fields.
type LocalRow struct {
	OrderID           string
	DisplayID         string
	State             string
	CustomerName      string
	TotalDisplay      string
	CommissionDisplay string
	Source            string
	CreatedAtUTC      *time.Time
	ExpectedShipAtUTC *time.Time
	UpdatedAtUTC      time.Time
	SyncedAtUTC       time.Time
}

// LocalSortColumn identifies an indexed date column used for local Orders ordering.
type LocalSortColumn string

const (
	// LocalSortCreatedAt orders rows by the order creation date.
	LocalSortCreatedAt LocalSortColumn = "CREATED_AT"
	// LocalSortExpectedShipAt orders rows by the expected ship date.
	LocalSortExpectedShipAt LocalSortColumn = "EXPECTED_SHIP_AT"
)

// KeysetCursor identifies the final row of a local Orders page ordered by its selected date column then order ID.
type KeysetCursor struct {
	SortAtUTC *time.Time
	OrderID   string
}

// ListQuery specifies a connection-scoped local list query filtered by an optional update-time boundary.
type ListQuery struct {
	ConnectionID string
	States       []string
	UpdatedAtMin *time.Time
	SortColumn   LocalSortColumn
	Descending   bool
	After        *KeysetCursor
	Limit        int
}

// ListPage contains one deterministically ordered local Orders page and an optional next cursor.
type ListPage struct {
	Rows       []LocalRow
	NextCursor *KeysetCursor
}

// Snapshot is the private stored snapshot for exactly one connection and Order ID.
type Snapshot struct {
	OrderID               string
	SnapshotJSON          string
	SnapshotSchemaVersion int
	UpdatedAtUTC          time.Time
	SyncedAtUTC           time.Time
}

// SyncState records the retained update-time boundary and last fully completed synchronization checkpoint for one connection.
type SyncState struct {
	ConnectionID              string
	BootstrapUpdatedAtMinUTC  time.Time
	BootstrapCompletedAtUTC   *time.Time
	HighWatermarkUpdatedAtUTC *time.Time
	LastSuccessfulSyncAtUTC   *time.Time
	LastAttemptAtUTC          *time.Time
	LastErrorKind             string
	LastErrorAtUTC            *time.Time
}

// Store defines persistence operations needed by the Orders synchronization and local-read workflows.
type Store interface {
	Close() error
	List(context.Context, ListQuery) (ListPage, error)
	FindByDisplayID(context.Context, string, string) (LocalRow, error)
	Snapshot(context.Context, string, string) (Snapshot, error)
	UpsertOrders(context.Context, []OrderRecord) error
	SyncState(context.Context, string) (SyncState, bool, error)
	BeginBootstrap(context.Context, string, time.Time, time.Time) error
	RecordAttempt(context.Context, string, time.Time) error
	CompleteSync(context.Context, string, bool, *time.Time, time.Time) error
	CompleteHistoricalSync(context.Context, string, time.Time, *time.Time, time.Time) error
	RecordFailure(context.Context, string, string, time.Time, time.Time) error
	DeleteConnectionData(context.Context, string) error
}

# Persistent Orders Storage and Incremental Sync Plan

## Status

**Planning only — no implementation has been started.**

This document defines the proposed approach for retaining complete Faire order snapshots locally, rendering the Orders table from indexed local data, and refreshing both incrementally from Faire. It establishes the storage foundation for the current read-only Orders list and the next read-only Order detail workflow.

## Problem

The current application calls `GET /orders` whenever an uncached list query is loaded. It has an in-memory cache keyed by connection and selected server filters, but that cache is discarded when the application exits. Consequently, restarting the application requires fetching order pages again before the table can be populated.

Faire's bundled API specification provides the necessary building blocks for incremental synchronization:

- `GET /orders` accepts `updated_at_min`.
- the endpoint is documented as returning orders in ascending `updated_at` order by default;
- responses contain both each order's `updated_at` field and the effective `updated_at_min`; and
- cursor pagination is available.

The current typed client already represents these features through:

- `faire.OrderListOptions.UpdatedAtMin`;
- `faire.OrderSortByUpdatedAt`; and
- `faire.Order.UpdatedAt`.

The goal is therefore not merely to cache pages. It is to maintain a per-connection local order snapshot, index the fields needed for the Orders table, update both transactionally from Faire, and render the list and future detail view without requiring a repeat API fetch.

## Recommendation

Use **SQLite**, embedded in the desktop application, as the local order store.

SQLite is the best fit for the first implementation because it provides all of the following without operating or deploying a separate service:

- durable storage across application restarts;
- atomic transactions, which are essential for safely advancing a synchronization checkpoint;
- indexes for the Orders table's connection, state, date, and display-ID queries;
- schema migrations for future order fields and workflows;
- a single file in the existing per-user application configuration directory; and
- mature, well-tested Go drivers.

Use a pure-Go SQLite driver unless release validation demonstrates a platform-specific reason not to. A pure-Go driver avoids introducing CGO into the current macOS and Windows desktop builds.

### Alternatives considered

| Alternative                                                | Why it is not the primary recommendation                                                                                                                                                                                                                           |
| ---------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Persist the existing `map[string]ordersCacheEntry` as JSON | Easy initially, but it stores query-specific, duplicated pages and cannot efficiently upsert an updated order, query a local table, or guarantee an atomic checkpoint with page writes. Migrations and corruption recovery would also become increasingly fragile. |
| BoltDB, bbolt, or another key-value store                  | Viable for a simple key/value cache, but would require hand-built indexing and query logic for the Orders table. SQLite provides those features directly.                                                                                                          |
| A remote database or sync service                          | Adds credentials, network availability, operational cost, account/multi-device behavior, and security scope that the local desktop application does not currently need. Consider only if multi-device/shared team state becomes a product requirement.             |
| OS keychain / credential manager                           | Correct for API credentials, not tabular application data. It does not offer queryable storage or a transaction model for thousands of order rows.                                                                                                                 |

## Scope and non-goals

### In scope

1. Persist a complete `faire.Order` snapshot per saved connection, including the fields needed for a future offline Order detail view.
2. Persist indexed, table-ready fields derived from that snapshot so the Orders list can filter, sort, and paginate locally.
3. Populate the Orders table and a selected order’s detail view from SQLite immediately, including while offline.
4. Synchronize changed and newly created orders using `updated_at_min`, on user request and automatically once per hour while the application is open.
5. Make snapshot upserts and sync-checkpoint advancement restart-safe and transactional.
6. Preserve stale-result protection and non-blocking Gio frame behavior.
7. Provide a clear user-visible distinction between local data, active synchronization, and a failed refresh.
8. Provide a user action to refresh, rebuild, and delete local order data.

### Explicitly out of scope for the first slice

- Background polling or automatic synchronization while the application is closed.
- Webhooks, push notifications, multi-device synchronization, or a central database.
- Reworking existing CSV export behavior; it remains API-backed until a separate export-freshness and privacy decision is made.
- Implementing order mutations.

The first storage version deliberately retains a full order snapshot, including customer, address, notes, item, shipment, tracking, discount, and payout fields represented by `faire.Order`. This is required for the planned Order detail screen. It expands the local privacy footprint, so its retention, deletion, permissions, and logging rules are first-class requirements in this document.

## Verified current architecture

| Area                 | Current behavior                                                                                                                    | Planned change                                                                                           |
| -------------------- | ----------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| API boundary         | `faire.OrdersService.List` already sends `updated_at_min`, `sort_by`, and cursor query controls.                                    | Add tests for the exact sync request, but keep Faire HTTP concerns in `faire/`.                          |
| Orders feature state | `features/orders.State` holds filters, rendered rows, selection, loading state, and cursor.                                         | Introduce a local-store query/result model outside Gio; keep feature state presentation-oriented.        |
| UI action layer      | `application/orders_actions.go` loads one page directly from Faire and stores safe rows in `DesktopUI.ordersCache`.                 | Replace session-cache reads/writes with an orders store + sync coordinator.                              |
| Saved connections    | `connections/` persists non-secret connection metadata below `os.UserConfigDir()/faire-gui` and secrets in the OS credential store. | Do not move credentials into SQLite. Use only `connections.Connection.ID` as the database partition key. |
| UI concurrency       | Background work publishes credential-safe results and the Gio frame loop applies them.                                              | Preserve this model. Store calls and Faire calls remain outside the frame loop.                          |

## Proposed package boundaries

Create a narrowly focused internal persistence package, for example:

```text
internal/ordersstore/
    doc.go
    store.go            # Store interface and storage-owned domain types
    sqlite.go           # SQLite open, pragma configuration, migrations
    migrations.go       # ordered, versioned schema migrations
    queries.go          # reads, atomic upserts, and sync-state transactions
    *_test.go
internal/orderssync/
    doc.go
    syncer.go           # Faire cursor traversal and checkpoint algorithm
    source.go           # narrow injectable remote-list interface
    *_test.go
```

The intended dependency direction is:

```text
application
  ├── connections.Manager  → credentials and Faire client creation
  ├── faire                → typed HTTP API client
  ├── internal/orderssync  → incremental synchronization orchestration
  │     └── internal/ordersstore → local SQLite snapshots and sync state
  └── features/orders     → Gio-free list/detail presentation models
```

`internal/ordersstore` is a persistence boundary only. It must not import `faire/` or issue HTTP requests. `internal/orderssync` owns traversal of the typed Faire list API, conversion into one atomic stored snapshot/row record, and final checkpoint progression. This prevents the SQLite adapter from becoming an API client and keeps each failure mode independently testable.

Important rules:

- `internal/ordersstore` must not import Gio, `application`, or `faire`; it stores application-owned records and serialized snapshots only.
- `internal/orderssync` may depend on `faire` and `internal/ordersstore`, but must not import Gio or mutate `DesktopUI`.
- Neither internal package may store or return API credentials.
- `faire/` remains a typed API client and must not acquire SQLite knowledge.
- `connections/` remains responsible only for connection metadata and credentials.
- `features/orders` maps typed orders to typed list/detail presentation models; it must not render raw snapshots or import Gio.
- `application/` is responsible for composition, obtaining a selected connection's client, starting local-read/sync work in a goroutine, and applying only safe presentation models or sanitized status text on the frame loop.

Inject narrow store and remote-list interfaces into the sync coordinator and inject the store/coordinator into `DesktopUI` (or an Orders controller owned by it). This allows temporary SQLite and scripted Faire pages in tests without depending on the user's real configuration directory or HTTP service.

## Architecture alignment and prerequisite changes

The persistent-sync and detail design is compatible with `FUTURE_ARCHITECTURE.md` when the following constraints are retained:

1. **Keep the vertical slice coherent.** Implement local persistence, incremental synchronization, local-first Order details, and the detail presentation model as one read-only Orders slice. Do not add mutations, packing slips, or shipment editing as part of this work.
2. **Persist full snapshots without making the UI API-shaped.** SQLite may retain a complete `faire.Order` snapshot, but the Gio frame loop must receive only `features/orders.Row`, a dedicated `features/orders.Detail`, or sanitized status text. The layout must never inspect a raw order or serialized JSON.
3. **Preserve explicit connection ownership.** Every store call and sync request takes the active immutable connection ID. The connection label remains presentation-only and is never used as a database key, directory name, or authorization surrogate.
4. **Keep the current async discipline.** SQLite reads, serialization, Faire calls, and sync writes occur in workers. The existing request-ID/result-channel approach remains sufficient for this slice; do not extract a generic `internal/async` framework before a second completed feature requires it.
5. **Centralize safe error conversion before broad reuse.** Extend the existing Orders-safe error policy for storage and snapshot errors now. Move it to `internal/sanitize` only when Orders and another feature share the same policy; premature extraction would create an empty abstraction.

No broad pre-implementation refactor is warranted. The necessary architectural changes are the two narrow internal packages above, the addition of a Gio-free `features/orders.Detail` model, and application composition/lifecycle ownership for the store. This is the smallest change that preserves the intended dependency direction while avoiding a future persistence or API concern leaking into layout code.

## Storage location and file handling

### Path

Store the database at:

```text
<os.UserConfigDir()>/faire-gui/orders.sqlite3
```

This co-locates local application data with the existing non-secret `connections.json` metadata file, while keeping API credentials exclusively in macOS Keychain or Windows Credential Manager.

Expose a dedicated path helper rather than reusing an unexported connection-package constant. For example, a store-specific `DefaultPath` can call `os.UserConfigDir()` and compose the same application directory.

### File permissions and SQLite artifacts

The database contains a complete cached copy of order data, including customer information, addresses, notes, and shipment/tracking details. It is not a credential database, but it is private and potentially personally identifiable or commercially sensitive local data.

The implementation must:

1. create the parent directory with `0700` permissions;
2. create the primary database file with owner-only permissions (`0600`) where the platform supports it;
3. ensure any temporary, WAL, and shared-memory files inherit or are set to appropriate owner-only access as far as SQLite and the platform permit;
4. never place the database in the repository, Downloads, or a shared temporary directory;
5. never log SQL parameters, serialized order snapshots, or values derived from addresses, notes, tracking details, or customer data; and
6. document that the complete cached order data remains accessible to the OS user who can read the app configuration directory.

Enable WAL mode after testing on both supported platforms. WAL improves read/write coexistence between a list query and a sync transaction, but produces `-wal` and `-shm` companions that must be included in the privacy and deletion behavior.

The approved v1 security posture is owner-only application-data permissions plus the supported device's operating-system account protections and full-disk encryption. Do not add application-level SQLite encryption: it would introduce encryption-key lifecycle, Keychain/Credential Manager, backup, reinstall, and recovery complexity without a stated requirement. This decision must be revisited if the product supports shared accounts, unencrypted devices, centralized backups, or a stronger compliance obligation.

### Database lifecycle

- Open one process-local database handle during application startup.
- Configure connection pooling for a desktop single-process store; begin with a conservative connection count and a busy timeout to avoid `database is locked` failures from concurrent UI operations.
- Run migrations before the UI begins reading Orders data.
- Close the database after the Gio window exits and background work is cancelled.
- A failure to open or migrate the database must not expose SQL internals to the UI. The Orders page should report a safe storage status and still allow an explicitly chosen API-only fallback only if that fallback is deliberately included in the implementation decision.

**Recommended initial behavior:** make persistent storage required for the Orders page once implemented, but preserve the existing safe status/error presentation. Do not silently revert to the old in-memory cache, because that makes storage failures hard to diagnose and produces inconsistent behavior.

## Data model

The first version uses a **hybrid model**: indexed, normalized columns support efficient list queries, while one canonical serialized `faire.Order` snapshot supports offline details without prematurely normalizing every nested Faire object. The snapshot is a JSON serialization of the typed `faire.Order`, not an HTTP response body: it contains no request URL, response headers, credentials, or unrelated response-envelope data.

Database timestamps are stored as normalized RFC 3339 UTC text or as integer Unix microseconds; choose one format consistently. Integer microseconds are preferred for comparisons and indexes, while user-facing formatting must continue to happen in Go.

### `schema_migrations`

| Column                            | Purpose                               |
| --------------------------------- | ------------------------------------- |
| `version INTEGER PRIMARY KEY`     | Applied migration number.             |
| `applied_at_utc INTEGER NOT NULL` | Audit time for migration application. |

Migrations must be append-only. Do not modify an already released migration.

### `order_sync_state`

One row exists for each `connection_id` that has begun synchronization.

| Column                                          | Purpose                                                                                                                                         |
| ----------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `connection_id TEXT PRIMARY KEY`                | Saved connection ID; never an API token.                                                                                                        |
| `bootstrap_created_at_min_utc INTEGER NOT NULL` | Chosen initial historical boundary, retained so rebuild behavior is explicit and testable.                                                      |
| `bootstrap_completed_at_utc INTEGER NULL`       | Null until every initial cursor page commits successfully.                                                                                      |
| `high_watermark_updated_at_utc INTEGER NULL`    | Maximum safe remote `updated_at` observed after a completed sync.                                                                               |
| `last_successful_sync_at_utc INTEGER NULL`      | When a complete sync last committed.                                                                                                            |
| `last_attempt_at_utc INTEGER NULL`              | When sync was most recently started.                                                                                                            |
| `last_error_kind TEXT NULL`                     | A small non-sensitive classification such as `network`, `rate_limited`, `api`, `storage`, or `invalid_remote_timestamp`; never a response body. |
| `last_error_at_utc INTEGER NULL`                | Time of last failed attempt.                                                                                                                    |

Do **not** persist a Faire cursor as the only resume mechanism in v1. A cursor may expire or be meaningful only in the context of its request. Page-level upserts are idempotent, and the old completed watermark remains authoritative until a full synchronization succeeds.

### `orders`

The primary key is `(connection_id, order_id)` so the same Faire order identifier cannot cross-contaminate saved connections.

| Column                                     | Purpose                                                                                 |
| ------------------------------------------ | --------------------------------------------------------------------------------------- |
| `connection_id TEXT NOT NULL`              | Connection partition.                                                                   |
| `order_id TEXT NOT NULL`                   | Faire internal order identifier.                                                        |
| `display_id TEXT NOT NULL`                 | Visible order number.                                                                   |
| `state TEXT NULL`                          | Canonical Faire order state for local state-filter queries.                             |
| `customer_name TEXT NULL`                  | The existing table's customer label; private but required to restore the current table. |
| `total_display TEXT NULL`                  | Existing stable formatted total for the first scope.                                    |
| `commission_display TEXT NULL`             | Existing stable formatted commission for the first scope.                               |
| `source TEXT NULL`                         | Existing source label.                                                                  |
| `created_at_utc INTEGER NULL`              | Canonical creation time for filtering and sort order.                                   |
| `expected_ship_at_utc INTEGER NULL`        | Canonical expected/requested/ship-after time used by the table.                         |
| `updated_at_utc INTEGER NOT NULL`          | Canonical remote update watermark for upsert/conflict handling.                         |
| `order_snapshot_json TEXT NOT NULL`        | Canonical serialized `faire.Order` used to render local detail data.                    |
| `snapshot_schema_version INTEGER NOT NULL` | Version of the application-owned snapshot encoding/mapping contract.                    |
| `synced_at_utc INTEGER NOT NULL`           | Local successful upsert time, useful for diagnostics only.                              |
| `PRIMARY KEY (connection_id, order_id)`    | Guarantees idempotent upsert behavior.                                                  |

Required indexes:

```sql
CREATE INDEX orders_by_connection_created
    ON orders (connection_id, created_at_utc DESC, order_id);

CREATE INDEX orders_by_connection_state_created
    ON orders (connection_id, state, created_at_utc DESC, order_id);

CREATE UNIQUE INDEX orders_by_connection_display_id
    ON orders (connection_id, display_id);

CREATE INDEX orders_by_connection_updated
    ON orders (connection_id, updated_at_utc, order_id);
```

The implementation must define behavior for malformed or missing Faire IDs and timestamps before inserting:

- orders without an ID cannot be safely upserted and must fail the current sync with a safe `invalid remote data` status;
- `updated_at` is required for a checkpointed incremental feed, so an order without a parseable update timestamp must fail the current sync rather than advance a potentially unsafe checkpoint;
- an order snapshot that cannot be serialized to and deserialized from `faire.Order` must fail the current sync before either the indexed fields or snapshot is committed; and
- nullable display fields may be represented as `NULL` and converted to the current em-dash presentation convention at query time.

### Full snapshot semantics and schema evolution

Every successful Faire list, direct lookup, or future mutation response must be converted once into a local record containing both its indexed columns and `order_snapshot_json`. These values must be upserted in the **same SQLite transaction**, so a table row can never refer to a stale or mismatched detail snapshot.

The serialized snapshot allows the initial detail screen to render the full typed order locally: address, customer, notes, items, customizations, shipments, tracking, discounts, payout details, and every other field currently represented by `faire.Order`. It deliberately does not store raw HTTP response bodies, headers, URLs, or credentials.

Schema migrations remain necessary for indexes and application-owned columns. If a change to `faire.Order` or its JSON mapping makes an old snapshot unreadable, the store must detect the `snapshot_schema_version`, clear/rebuild the affected connection's cache, and explain that local data needs to be refreshed. Do not silently render partial or incorrectly mapped details.

## Local query and table behavior

The UI must query SQLite for the current table rather than reading only the most recent sync page.

### Initial connection selection

When the user selects a connection:

1. reset transient UI state and selection as today;
2. read matching rows from the local store immediately;
3. show those rows with a status such as `Showing locally stored orders. Last synced 14:32.`;
4. start a non-blocking incremental sync if a bootstrap exists; otherwise start the initial bootstrap;
5. after a successful sync, re-query the local store using the active table filters and replace the displayed rows; and
6. if sync fails, keep the stored rows visible and show a credential-safe stale-data status.

This gives useful offline behavior and avoids blanking a populated table merely because Faire is unavailable.

### Local filtering, sorting, and pagination

The initial storage slice should apply Orders page status and date filters against local columns. It should use deterministic SQL ordering, ending with `order_id` as a tie-breaker. Cursor pagination should become local keyset pagination rather than reuse Faire cursors.

The table state should distinguish:

- a **remote synchronization cursor**, which is temporary and belongs only to the sync worker; from
- a **local table page cursor**, which represents the final `(sort value, order_id)` key of local query results.

Do not persist either UI cursor in the sync checkpoint.

The existing one-year `created_at_min` default becomes a **local view filter** after the first synchronization. It must not be attached to subsequent `updated_at_min` sync requests, because an older order that changes later must still be received and upserted.

### Direct order-number search

For the first implementation:

1. search the local `(connection_id, display_id)` index first;
2. if a matching local row exists, render it immediately;
3. if it is absent, retain the current authenticated `Orders.Get` fallback; and
4. upsert both the indexed table projection and full local snapshot from a successful direct lookup, provided its `updated_at` is valid.

This makes already-synced searches offline-capable without claiming that a failed local search proves an order does not exist remotely.

### Order details

Selecting an order should be a local-first workflow:

1. read `order_snapshot_json` by `(connection_id, order_id)` and deserialize it into `faire.Order` outside the Gio frame loop;
2. render the detail screen from that snapshot immediately, including offline;
3. show the snapshot's `updated_at` and local `synced_at` as data-freshness information, without implying that it is live data; and
4. provide an explicit future **Refresh order** action that calls `GET /orders/{id}` and transactionally replaces both the snapshot and table projection.

If the selected local row has no readable snapshot, treat it as storage corruption/migration incompatibility: keep the list usable, show a safe detail-load status, and offer a refresh or connection-cache rebuild. Never silently substitute data from a different connection.

### Selection and export

Selection continues to use Faire order IDs. A local refresh may change a displayed order's state or remove it from the active filter; the implementation must retain selected IDs only while that behavior remains intentional and visibly understandable.

CSV export should remain API-backed in the first slice even though full snapshots are stored. This preserves its current freshness and fidelity behavior until a separate decision defines whether stale/offline cached details may be exported.

## Synchronization algorithm

### Design requirements

The algorithm must satisfy these invariants:

1. A completed checkpoint means every page for that sync boundary has been durably upserted.
2. A failed, cancelled, or partially completed sync must never advance the completed high-water mark.
3. Replaying a page is safe and produces the same final local row state.
4. Equal `updated_at` timestamps and delayed API visibility cannot cause a permanently missed order.
5. A connection's data is never read, written, or presented under another connection ID.

### Timestamp overlap window

Use an overlap when building an incremental request:

```text
request_updated_at_min = max(initial_bootstrap_boundary, high_watermark - overlap)
```

Start with a documented, configurable **five-minute overlap** unless Faire's API documentation provides a stronger consistency/precision guarantee. The overlap intentionally re-fetches a small tail of previously seen orders. Idempotent upserts eliminate duplicates and protect against:

- multiple orders sharing the same timestamp;
- timestamp rounding precision differences;
- updates that become visible slightly after a prior page traversal; and
- a process stopping after writing rows but before recording a new checkpoint.

The overlap is a correctness mechanism, not an optimization. It must be unit-tested.

### Initial bootstrap

The current UI defaults to a one-year `created_at_min` lookback. The product decision for the first bootstrap must be explicit:

- **Recommended default:** bootstrap the current one-year historical window, matching current application behavior and avoiding a surprise full-history API pull.
- **Optional user choice:** provide `Sync all available order history` before the initial run or from a data-management screen. This is useful for users who need historical analysis, but can be slow and large.

For the selected bootstrap boundary:

1. create/update `order_sync_state` with the immutable bootstrap boundary and attempt timestamp;
2. request `/orders` with `created_at_min=<bootstrap boundary>`, `limit=<documented safe page size>`, no client-side state exclusion, and default ascending `updated_at` order;
3. follow every Faire cursor, detecting a repeated non-empty cursor as an API/protocol failure;
4. for each page, validate its order IDs and update timestamps, then upsert rows in a SQLite transaction;
5. track the maximum valid `updated_at` observed across all pages in worker memory;
6. only after the final cursor has been consumed, transactionally set `bootstrap_completed_at_utc`, `high_watermark_updated_at_utc`, `last_successful_sync_at_utc`, and clear last-error metadata; and
7. query the local table using the user’s active view filters.

No `excluded_states` filter may be used for the data-sync request. Orders that do not belong in today’s visible tab must still be stored; otherwise a later status-tab switch would incorrectly look like missing data.

### Incremental refresh

For a completed bootstrap:

1. load the connection's sync state;
2. calculate `updated_at_min` using the old completed watermark and overlap;
3. request `/orders` with `updated_at_min`, default ascending `updated_at` order, a stable page limit, and no view-specific state/date filters;
4. follow every cursor and transactionally upsert each validated page;
5. compute the maximum remote `updated_at` observed; and
6. after the last page, write `max(previous_high_watermark, maximum_seen_updated_at)` plus `last_successful_sync_at_utc` in one transaction.

If the incremental response has no orders, set `last_successful_sync_at_utc` but retain the prior high-water mark.

### Upsert conflict rule

Use `INSERT ... ON CONFLICT(connection_id, order_id) DO UPDATE` and only replace an existing row when the incoming `updated_at_utc` is greater than or equal to the stored `updated_at_utc`. The update must replace every indexed column and the complete `order_snapshot_json` atomically.

An equal timestamp must be allowed to replace the row so overlap replays converge if Faire returns a corrected payload with the same timestamp precision. A lower timestamp must not overwrite a newer local table projection or detail snapshot.

### Empty or invalid page safeguards

The worker must fail safely when it encounters:

- a repeated cursor;
- an order missing a stable ID or parseable `updated_at`;
- an impossible/invalid cursor progression according to the client contract;
- a context cancellation;
- an API, credential, rate-limit, or storage error; or
- an error while finalizing the sync state.

On these failures, retain all successfully upserted page data, retain the **previous completed** watermark, record only a safe error classification, and surface safe status text. The next refresh replays from the overlap before that previous watermark, repairing any partial page progress.

### Mutations in future workflows

When the application eventually calls an order mutation endpoint (cancel, move to processing, shipment update, item availability):

1. upsert the returned `faire.Order` projection into SQLite in the same background workflow;
2. do not advance the incremental sync watermark from a mutation result; and
3. still allow the next normal overlap refresh to reconcile the remote source of truth.

This improves UI responsiveness without relying on a locally initiated mutation as proof that no other remote changes occurred.

## Refresh and data-management UX

The existing Refresh control should change from `fetch current page from Faire` to `sync all changes since the local checkpoint`.

Recommended UI states:

| Situation                                 | Example status                                                                           |
| ----------------------------------------- | ---------------------------------------------------------------------------------------- |
| Local rows shown, no active sync          | `Showing locally stored orders. Last synced today at 14:32.`                             |
| First sync with no local rows             | `Downloading initial order history…`                                                     |
| Incremental sync in progress              | `Checking Faire for updated orders…`                                                     |
| Incremental sync succeeds with changes    | `Orders updated from Faire at 14:38.`                                                    |
| Incremental sync succeeds with no changes | `Orders are up to date as of 14:38.`                                                     |
| Sync fails but rows exist                 | `Showing locally stored orders. Last refresh failed; try again when Faire is available.` |
| Sync fails before any data exists         | Existing sanitized load-error message, adapted for synchronization.                      |

Add a small Orders data-management affordance only after the core sync is working:

- **Refresh now** — standard incremental sync.
- **Rebuild local order data** — confirmation dialog, delete only the selected connection's rows and sync state, then run bootstrap again.
- **Delete local order data** — confirmation dialog, delete only the selected connection's rows and sync state; do not delete saved connection metadata or credentials.

A future global preferences/data screen may provide `Delete all locally stored orders` and clearly list the database location. These destructive operations must mention that they remove local copies only, never data at Faire.

Complete order snapshots are retained indefinitely for the selected connection until the user explicitly chooses **Rebuild local order data**, **Delete local order data**, deletes the saved connection, or deletes all local order data. Version 1 does not impose an age- or size-based retention policy.

## Automatic synchronization schedule

The application automatically runs an incremental sync **once per hour while it is running**. It does not schedule background work while the application is closed.

To keep API activity predictable and aligned with the selected-connection model:

1. The automatic scheduler considers only the current active connection; it does not refresh every saved connection in the background.
2. Selecting a connection reads local data immediately and starts a sync when there is no completed bootstrap or its last successful sync is at least one hour old.
3. After a successful automatic or manual sync, the next automatic eligible time is one hour later.
4. A manual **Refresh now** always remains available and may run before that hour elapses.
5. At most one sync may run for a connection at a time. While a sync, explicit detail refresh, or incompatible Orders operation is in progress, the scheduler skips that tick rather than queueing duplicate work.
6. A failed automatic sync leaves cached data and the completed checkpoint intact. The next eligible hourly tick may retry; the user can also retry manually. The application must not introduce a tight automatic retry loop.
7. Changing the active connection cancels or invalidates the prior connection's UI result as today; the new connection receives its own stale check and schedule.

A scheduler ticker belongs in `application/` because it owns the active connection, app lifetime, request IDs, and Gio result publication. The ticker must invoke the same sync coordinator used by manual refresh; it must not duplicate synchronization logic or call SQLite/HTTP on the frame loop.

## Error handling and recovery

Classify errors internally, but do not display raw errors, SQL statements, API response bodies, request URLs, or credentials.

| Failure                                    | Required behavior                                                                                                                                                                                                                                                                                                                                      |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Database open/migration failure            | Prevent normal Orders persistence use, retain a safe status, and offer a documented recovery path such as closing the app and restoring/rebuilding the database.                                                                                                                                                                                       |
| Database corruption                        | Present a safe `Local order data needs to be rebuilt` status and offer only an explicit **Delete and rebuild local order data** action. The action removes the selected connection's local rows and sync state, then performs a new bootstrap. Do not retain, rename, export, or upload a corrupt database, because it may contain private order data. |
| API authentication failure                 | Keep stored rows visible, show the existing credential-safe guidance, and do not advance the checkpoint.                                                                                                                                                                                                                                               |
| Network/server/rate-limit failure          | Keep rows and old checkpoint; let Refresh retry. Respect the client’s existing retry behavior and avoid an automatic retry loop in the UI.                                                                                                                                                                                                             |
| Mid-sync cancellation/application shutdown | Stop work promptly; page transactions already committed are valid, but the completed high-water mark remains unchanged.                                                                                                                                                                                                                                |
| Malformed remote timestamp/ID              | Fail the sync safely without checkpoint advancement. Log only field names/types, never serialized order content.                                                                                                                                                                                                                                       |
| Connection deletion                        | Delete that connection's local rows and sync-state row as best-effort cleanup. A cleanup failure must be reported safely and must not remove another connection's data.                                                                                                                                                                                |

## Implementation phases

### Phase 0 — confirm API and product assumptions

Before implementation, verify against current Faire documentation or a controlled test account:

1. `updated_at_min` inclusion semantics (`>=` versus `>`).
2. timestamp precision and timezone format returned by Faire.
3. whether `created_at_min` and `updated_at_min` can be combined and how they interact.
4. cursor lifetime and whether cursors remain valid when new orders arrive during traversal.
5. the documented maximum page size and practical rate limits.
6. whether an order can be updated with an older/equal timestamp or omitted from an `updated_at_min` response under any known condition.
7. whether one year remains the intended default bootstrap horizon, or users require all historical orders.

Do not base correctness on an assumption that `sort_by=UPDATED_AT` is equivalent to the documented default ordering until the API contract confirms it. The sync request should rely on the documented ascending-by-`updated_at` default unless documentation explicitly says otherwise.

### Phase 1 — introduce and test persistence primitives

1. Select and add the SQLite driver with its license and release-platform implications reviewed.
2. Add the store package, `DefaultPath`, open/close lifecycle, private directory/file handling, and migration runner.
3. Create v1 migrations for `schema_migrations`, `order_sync_state`, `orders`, and the required indexes.
4. Define Go domain types for sync state, local row, local query, keyset cursor, and store errors.
5. Implement migrations and storage methods using parameterized SQL only.
6. Add unit tests using a temporary database and migration tests from an empty database.

**Exit criteria:** a test can open a temporary database, migrate it, upsert rows for two connections without leakage, query deterministic filtered pages, and reopen the database with data preserved.

### Phase 2 — implement local list and detail reads

1. Map a validated `faire.Order` to one atomic local record containing indexed list fields and a canonical serialized snapshot.
2. Convert local rows to `features/orders.Row` without persisting Go-formatted placeholders as the only source of truth.
3. Add local table filters for status, creation date, and deterministic sorting.
4. Add a local detail read that deserializes the matching `faire.Order` snapshot only for the selected connection and order ID.
5. Replace the visible-table load path with a store read on connection selection and filter changes.
6. Replace the current in-memory `ordersCache` type and logic; remove it only after all call sites and teardown tests are migrated.
7. Keep direct API lookup and CSV export behavior unchanged except for optional local-first direct lookup and snapshot upsert after a successful lookup.

**Exit criteria:** an existing populated database renders its list and a selected order's full details after app restart without a network call, and a fresh database renders the appropriate empty/local-loading state.

### Phase 3 — implement bootstrap and incremental sync

1. Create an `internal/orderssync` coordinator that receives a context, connection ID, a narrow Faire-list source, the persistence-only store, and a time source/overlap configuration.
2. Implement page traversal, repeated-cursor detection, snapshot serialization/validation, per-page transactional upserts, and validation.
3. Implement initial bootstrap state creation/finalization.
4. Implement incremental request-boundary calculation and no-results success handling.
5. Implement atomic final checkpoint advancement only after the terminal page.
6. Add a test seam for Faire list responses; do not make synchronization tests depend on a real Faire account.
7. Integrate the coordinator into `application/orders_actions.go` through the existing background-result channel pattern, publishing only safe sync summaries and re-querying local list/detail presentation models afterward.

**Exit criteria:** a simulated crash/error after any page leaves the old checkpoint intact; the next run replays safely and converges to the expected table.

### Phase 4 — integrate list/detail UX and lifecycle

1. Update Refresh labels/statuses and ensure a local list query happens before sync work begins.
2. Refresh the local list query after successful sync, only if its connection/filter request is still current.
3. Add the read-only Order detail route/action, which reads the stored snapshot before any explicit network refresh.
4. Add an explicit per-order refresh action that safely replaces its local snapshot and table projection when `GET /orders/{id}` succeeds.
5. Preserve `ordersRequestID` or replace it with equivalent request tokens so a completion for an old connection, filter, or selected order cannot overwrite the active view.
6. Make connection deletion clear only that connection's local data, including complete order snapshots.
7. Close the store cleanly during shutdown after cancellation; remove only in-memory rows from process memory, not persisted records.
8. Add rebuild/delete-local-data confirmation UI after the base happy path is stable; the confirmation text must state that it removes locally stored order details, including customer and shipping information.

**Exit criteria:** manual use confirms non-blocking interaction, safe stale-data behavior, local list/detail isolation, offline detail rendering, and no saved tokens in the database.

### Phase 5 — release validation

1. Run the entire Go test suite.
2. Test a clean install, migration from a prior schema, and the explicit selected-connection delete-and-rebuild corruption recovery flow.
3. Test offline startup with an existing database.
4. Test a fresh connection, partial sync interruption, restart, and successful convergence.
5. Test a Faire credential failure with existing stored rows.
6. Test the one-hour active-connection schedule, manual refresh bypass, skipped overlapping tick, failure retry on the next tick, and scheduler shutdown.
7. Validate macOS and Windows release builds, including config-directory permissions and SQLite WAL companion files.
8. Inspect a test database to verify that complete intended order snapshots are present, while access tokens, OAuth secrets, authorization headers, request URLs, response headers, and unrelated raw HTTP response bodies are absent.

## Test plan

### Store unit tests

- migration from an empty file and idempotent reopen;
- migration version sequencing and unsupported/future-version failure;
- connection partitioning for every read, upsert, rebuild, and delete operation;
- upsert newer, equal, and older `updated_at` values;
- local filters and deterministic keyset pagination;
- safe handling of nullable presentation fields;
- snapshot serialization/deserialization round trips for nested addresses, notes, items, customizations, shipments, tracking, discounts, and payout data;
- snapshot and indexed-column values are replaced atomically for newer/equal updates;
- selected connection cleanup leaves another connection untouched; and
- file/path failures return classified errors without data leakage.

### Synchronization tests

Use a fake `OrdersService` boundary or an injected list function that records requested options and returns scripted pages.

- first bootstrap requests the configured `created_at_min` and follows every cursor;
- normal incremental sync requests `high_watermark - overlap` with `updated_at_min`;
- sync requests do not include state exclusions or UI-only local filters;
- an update replaces both the stored table row and full detail snapshot, and becomes visible in its new state tab;
- no changed orders updates only the `last_successful_sync_at` time;
- equal timestamps are replayed without duplicates or missed rows;
- repeated cursor produces a failure without checkpoint advancement;
- failure after page N preserves the old checkpoint and retries idempotently;
- cancellation preserves the prior completed checkpoint;
- malformed ID/timestamp fails before advancement;
- an updated old order is received even though its `created_at` precedes the bootstrap boundary; and
- a mutation-result upsert does not change the feed watermark.

### Application tests

- selecting a connection reads local rows before starting the sync;
- offline launch with local rows stays usable;
- stale sync results cannot replace a different active connection/filter;
- Refresh starts incremental sync rather than a page fetch for the active table;
- table rows reload from local storage after successful sync;
- selecting an order reads and renders the matching local snapshot without a network call;
- a local detail snapshot never renders under a different active connection;
- an automatic sync runs only for the active connection after one hour, and a manual refresh uses the same coordinator before the interval elapses;
- an automatic tick neither overlaps an active sync nor starts work after application shutdown;
- local database cleanup occurs on connection deletion;
- shutdown closes/cancels correctly without erasing durable data; and
- user-facing errors remain credential-safe.

## Acceptance criteria

The feature is complete when all of the following are true:

1. On restart, previously synchronized orders render from local SQLite before Faire responds.
2. A completed refresh requests only orders at or after the overlap-adjusted `updated_at` checkpoint, not the full prior history.
3. Updated orders atomically overwrite both indexed table fields and their full local detail snapshot, including changes that affect state tabs and the visible table.
4. A selected order can render its full locally stored `faire.Order` snapshot while offline, without a list or detail API request.
5. An interrupted or failed sync never marks unseen changes as synchronized.
6. Replaying overlapping pages does not create duplicate orders or regress a newer local version.
7. Each saved connection sees only its own cached orders and detail snapshots.
8. API tokens, OAuth secrets, authorization headers, request URLs, response headers, and unrelated raw HTTP response bodies are not stored in the v1 database. Complete `faire.Order` snapshots, including addresses, notes, and tracking details, are intentionally stored as documented private local data.
9. CSV export retains its current full-order API behavior.
10. Users can see whether displayed rows are local, syncing, fresh, or stale after a failed refresh.
11. While the application is open, only the active connection automatically synchronizes at most once per hour; manual refresh uses the same safe incremental path at any time.
12. Complete snapshots persist until the user explicitly deletes/rebuilds the selected connection cache, deletes the saved connection, or deletes all local order data.
13. Automated tests cover migrations, snapshot fidelity, pagination, checkpoint integrity, overlap behavior, connection isolation, automatic-schedule behavior, and stale-result protection.

## Settled implementation decisions

The following product, privacy, and operational decisions are approved for version 1:

1. **Bootstrap horizon:** use the current one-year `created_at_min` default for the initial sync. A later changed order can still be received by the subsequent `updated_at_min` incremental sync, even when it was created before that initial boundary.
2. **Automatic refresh:** while the application is open, automatically synchronize only the active connection at most once per hour. Sync on connection selection only when no bootstrap exists or the prior successful sync is at least one hour old. Manual Refresh remains available at any time.
3. **Local-detail retention:** retain complete snapshots until the user explicitly deletes/rebuilds the selected connection cache, deletes the saved connection, or deletes all local order data. Do not apply age- or size-based eviction in version 1.
4. **Database recovery:** offer only an explicit delete-and-rebuild flow; do not preserve a corrupt database because it may contain private local order details.
5. **Database encryption:** owner-only application files plus supported operating-system account protections and device encryption are sufficient for version 1. Do not add application-level SQLite encryption unless the product's threat model or compliance requirements change.

## References in this repository

- `faire_api.json` — `/orders` documents ascending `updated_at` ordering and the `updated_at_min` parameter.
- `faire/services_orders.go` — existing `/orders` query encoding.
- `faire/types_orders.go` — `Order`, `OrderListOptions`, `OrderPage`, and update timestamp types.
- `application/orders_actions.go` — current direct API loading, stale-result protection, and session-only cache.
- `application/desktop_ui.go` — long-lived UI state and result channels.
- `connections/manager.go` and `connections/repository.go` — established application config-directory and private-file persistence patterns.
- `FUTURE_ARCHITECTURE.md` — package-boundary and credential-safety requirements.

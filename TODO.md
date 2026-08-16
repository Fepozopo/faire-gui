# TODO — Persistent Orders Storage, Incremental Sync, and Local-First Details

## Purpose

Implement durable SQLite-backed Orders storage so the desktop app can:

1. render the Orders table from local data immediately after restart;
2. incrementally synchronize changed Faire orders using `updated_at_min`;
3. retain complete approved `faire.Order` snapshots for a local-first read-only detail view; and
4. protect credentials, private order data, UI responsiveness, and cross-connection isolation.

This document is an implementation handoff. Read it together with:

- `docs/orders-persistent-sync-plan.md` — detailed data, privacy, sync, recovery, and test design;
- `FUTURE_ARCHITECTURE.md` — package boundaries and the intended Orders detail vertical slice;
- `application/orders_actions.go` — current direct API loading, request IDs, and safe background-result pattern;
- `application/desktop_ui.go` and `application/gio_runtime.go` — UI composition, lifecycle, and result draining;
- `faire/services_orders.go` and `faire/types_orders.go` — typed Faire Orders API; and
- `connections/manager.go` and `connections/repository.go` — connection IDs, config directory, and private-file conventions.

## Approved product decisions

These are settled for version 1. Do not reopen or silently change them during implementation.

| Decision | Approved behavior |
| --- | --- |
| Initial sync window | Bootstrap the same one-year `created_at_min` window currently used by the Orders UI. |
| Incremental sync | Use Faire `updated_at_min` with a configurable five-minute overlap and cursor pagination. |
| Automatic refresh | While the app is open, sync only the active connection at most once per hour. No synchronization while the app is closed. |
| Manual refresh | Always available; uses the exact same incremental-sync path as scheduled refresh. |
| Local data | Persist complete `faire.Order` snapshots plus indexed list columns. |
| Detail experience | Open local detail data first; a future explicit per-order refresh calls `GET /orders/{id}`. |
| Retention | Retain snapshots until a user deletes/rebuilds a connection cache, deletes the saved connection, or deletes all local data. No automatic age/size eviction. |
| Corruption recovery | Offer only explicit delete-and-rebuild. Do not retain, rename, upload, export, or back up corrupt private data. |
| Encryption | Owner-only application files and supported OS account/device encryption are sufficient. Do not add application-level SQLite encryption. |
| Exports | Keep CSV exports API-backed in this slice. Do not change export freshness/fidelity behavior. |

## Non-negotiable architectural rules

1. `faire/` remains the typed HTTP/API boundary. **Do not** add SQLite, Gio, UI state, or storage logic there.
2. `connections/` remains responsible only for saved connection metadata and OS credential storage. **Never** place credentials in SQLite.
3. `features/orders/` remains Gio-free. It owns typed list/detail presentation transformations, not raw persistence or widget state.
4. `application/` owns `DesktopUI`, persistent Gio controls, navigation, app lifecycle, background work initiation, result channels, request IDs, and safe UI-state updates.
5. SQLite work, JSON serialization/deserialization, and Faire requests run outside the Gio frame loop.
6. Only safe typed presentation data or sanitized text may cross worker result channels into the Gio frame loop. Never publish `faire.Client`, credentials, raw HTTP responses, serialized snapshots, or a raw `faire.Order` for layout rendering.
7. Every local-data operation must be scoped by immutable `connections.Connection.ID`. Never use the editable label as a database key or directory name.
8. Do not introduce a generic async framework before this slice demonstrates a second real use case. Reuse the existing request-ID/result-channel pattern.

## Target packages

Create these packages as part of this slice:

```text
internal/ordersstore/
    doc.go
    store.go
    sqlite.go
    migrations.go
    queries.go
    *_test.go
internal/orderssync/
    doc.go
    source.go
    syncer.go
    *_test.go
features/orders/
    detail.go
    detail_test.go
```

### `internal/ordersstore/`

This is the persistence boundary only.

- Owns SQLite opening/closing, file path, pragmas, migrations, indexes, transactions, list/detail reads, sync state, atomic order upserts, and cache deletion.
- Must **not** import `faire/`, Gio, or `application/`.
- Store application-owned records, including serialized snapshot bytes/JSON. Do not make the database adapter an HTTP client.
- Return typed storage records/errors; callers map them to `faire.Order` and/or feature presentation models outside the frame loop.

### `internal/orderssync/`

This is the remote-to-local synchronization coordinator.

- May import `faire/` and `internal/ordersstore/`; must not import Gio or `application/`.
- Owns cursor traversal, overlap-boundary calculation, snapshot conversion, per-page persistence calls, repeated-cursor detection, and final checkpoint behavior.
- Depends on a narrow injectable list-source interface so tests use scripted pages rather than real HTTP.
- Does not own scheduling, active-connection selection, widget state, or UI status rendering.

### `features/orders/detail.go`

- Defines a purpose-built `Detail` model and conversion from typed `faire.Order` data.
- The model contains only display fields explicitly approved by the detail screen.
- Even though a complete snapshot is stored privately, no raw snapshot or raw API model may be handed to layout code.
- Test missing optional fields, date/money formatting, unknown states, nested order items, address/shipping information, notes, and safety of rendered strings.

## Storage implementation

### 1. Driver and database lifecycle

1. Choose a maintained pure-Go SQLite driver compatible with the module’s Go version and macOS/Windows targets.
2. Add it to `go.mod` and preserve reproducible builds in `go.sum`.
3. Add `ordersstore.DefaultPath()`:

   ```text
   <os.UserConfigDir()>/faire-gui/orders.sqlite3
   ```

4. Create the parent directory with `0700` and the database file with owner-only permissions (`0600`) where supported.
5. Configure a single process-local database handle at application startup.
6. Use a conservative SQLite pool configuration and busy timeout appropriate for a single-user desktop app.
7. Enable/test WAL mode. Treat `orders.sqlite3-wal` and `orders.sqlite3-shm` as private data and delete them as part of full database deletion if applicable.
8. Run migrations before the Orders page reads local data.
9. Cancel background work and close the store during application shutdown.

### 2. Schema migrations

Use append-only migrations. Never modify a released migration.

Create at least:

#### `schema_migrations`

```text
version INTEGER PRIMARY KEY
applied_at_utc INTEGER NOT NULL
```

#### `order_sync_state`

One row per `connection_id`:

```text
connection_id TEXT PRIMARY KEY
bootstrap_created_at_min_utc INTEGER NOT NULL
bootstrap_completed_at_utc INTEGER NULL
high_watermark_updated_at_utc INTEGER NULL
last_successful_sync_at_utc INTEGER NULL
last_attempt_at_utc INTEGER NULL
last_error_kind TEXT NULL
last_error_at_utc INTEGER NULL
```

Do **not** use a persisted Faire cursor as the only resume/checkpoint mechanism. Cursors may expire; idempotent page upserts plus the last fully completed watermark are the recovery strategy.

#### `orders`

Use `(connection_id, order_id)` as the primary key:

```text
connection_id TEXT NOT NULL
order_id TEXT NOT NULL
display_id TEXT NOT NULL
state TEXT NULL
customer_name TEXT NULL
total_display TEXT NULL
commission_display TEXT NULL
source TEXT NULL
created_at_utc INTEGER NULL
expected_ship_at_utc INTEGER NULL
updated_at_utc INTEGER NOT NULL
order_snapshot_json TEXT NOT NULL
snapshot_schema_version INTEGER NOT NULL
synced_at_utc INTEGER NOT NULL
PRIMARY KEY (connection_id, order_id)
```

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

### 3. Snapshot rules

- `order_snapshot_json` is a serialization of one typed `faire.Order`, not a raw HTTP response body.
- It intentionally contains approved full order data: customer, address, notes, items, customizations, shipments, tracking, discounts, payout data, and other fields represented by `faire.Order`.
- It must never contain API tokens, OAuth secrets, authorization headers, request URLs, response headers, or whole HTTP response envelopes.
- Validate an order ID and parseable `updated_at` before writing any record.
- Serialize and deserialize successfully before committing. A malformed/missing ID, malformed timestamp, or serialization failure fails the current sync safely.
- In one transaction, upsert every indexed list field and the complete snapshot. Never allow table columns and detail snapshot to represent different versions of an order.
- Include a snapshot schema/mapping version. If an old snapshot cannot be mapped after an application change, safely require that connection’s cache to be rebuilt; do not silently render an incorrect partial detail view.

## Local reads and detail behavior

### Orders table

1. On connection selection, reset only transient UI state as needed.
2. Read local table rows first, scoped to the active connection ID.
3. Display cached data immediately with freshness information based on `last_successful_sync_at_utc`.
4. Apply existing state/date filters and deterministic ordering in SQLite.
5. Replace Faire page cursors in the visible table with local keyset pagination; use a stable final tie-breaker such as `order_id`.
6. Keep remote pagination cursors strictly inside the sync worker. Do not persist them as UI or checkpoint state.
7. Treat the existing one-year `created_at_min` as a local view filter after bootstrap. Do not include it in later incremental sync requests, or old orders updated later could be omitted.

### Search and detail

1. Search local `(connection_id, display_id)` first.
2. If found, render the local list row/detail without a network request.
3. If absent, retain the authenticated `Orders.Get` fallback and atomically upsert the result if valid.
4. Opening an order reads the matching local snapshot in a worker, deserializes it to `faire.Order`, maps it to `features/orders.Detail`, and publishes only that detail model or safe status text.
5. Preserve Orders list filters, local pagination position, scroll position, and selection when navigating back from details.
6. Add a dedicated detail navigation action/clickable; do not repurpose row selection used for exports/future bulk actions.
7. Add an explicit **Refresh order** action later in this slice: call `GET /orders/{id}` in a worker, atomically replace the stored row/snapshot, map it to detail, and reject stale results using a detail request ID.

## Synchronization implementation

### Preconditions

Before finalizing the sync algorithm, verify Faire’s current behavior/documentation or a controlled test account for:

- `updated_at_min` inclusion semantics (`>=` versus `>`);
- timestamp precision and timezone format;
- interaction between `created_at_min` and `updated_at_min`;
- cursor lifetime/concurrency behavior;
- maximum practical page size/rate limits; and
- whether the documented default ascending `updated_at` ordering is sufficient without explicitly specifying `sort_by`.

### Bootstrap

1. If a connection has no completed bootstrap, create/update its sync state with the current one-year `created_at_min` boundary and attempt timestamp.
2. Request `/orders` with that `created_at_min`, the approved page limit, no excluded-state filter, and documented ascending `updated_at` ordering.
3. Follow every cursor; reject a repeated non-empty cursor.
4. Validate and atomically upsert each page’s full order snapshots.
5. Track the maximum valid `updated_at` in memory.
6. Only after every page succeeds, transactionally record `bootstrap_completed_at_utc`, `high_watermark_updated_at_utc`, and `last_successful_sync_at_utc`, then clear error metadata.
7. Re-query local table data for the active user filters.

Do not apply status tabs, date filters, or other view-specific exclusions to the storage synchronization request. Store every returned order in the bootstrap window, not only the visible tab.

### Incremental sync

1. Load the last fully completed state for the active connection.
2. Calculate:

   ```text
   updated_at_min = max(bootstrap_created_at_min, high_watermark_updated_at - 5 minutes)
   ```

3. Call `/orders` using `updated_at_min`, default ascending `updated_at` ordering, cursor pagination, and no view-specific filters.
4. Validate and atomically upsert each page.
5. Track the greatest remote `updated_at` seen.
6. After the terminal page only, write:

   ```text
   high_watermark_updated_at_utc = max(previous_watermark, maximum_seen)
   last_successful_sync_at_utc = now
   ```

7. If no orders are returned, update only `last_successful_sync_at_utc`; keep the prior watermark.

### Correctness requirements

- Use an overlap of five minutes. It deliberately re-fetches a small tail to handle equal timestamps, timestamp precision, delayed visibility, and process crashes between page persistence and checkpoint finalization.
- Use idempotent `INSERT ... ON CONFLICT(connection_id, order_id) DO UPDATE` semantics.
- Accept an incoming version with equal `updated_at` so overlap replay can converge corrected content.
- Reject incoming data with older `updated_at` from replacing a newer locally stored snapshot.
- If any page, validation, storage operation, final checkpoint update, API call, or cancellation fails, retain committed valid page data but **do not advance the completed high-water mark**.
- The next sync must replay safely from the prior watermark’s overlap.
- A future locally initiated Faire mutation may upsert its returned order snapshot for responsiveness, but must not advance the remote-feed checkpoint.

## Automatic hourly scheduler

Implement this in `application/`, not in `internal/orderssync/` or the store.

1. Start a one-hour ticker tied to the application context/lifetime.
2. Consider only the active connection.
3. On selection, sync only if bootstrap is absent or the last success is at least one hour old.
4. After manual or automatic success, do not run again until one hour has elapsed.
5. Manual Refresh bypasses the interval but invokes the same coordinator and request/result flow.
6. Ensure one sync per connection at a time. Skip a tick while a sync, detail refresh, or incompatible Orders operation is already running; do not queue duplicates.
7. Failures leave local rows and the checkpoint intact. Let the next eligible hourly tick retry; never create a tight retry loop.
8. On connection change, cancel or invalidate stale results exactly as other Orders operations do.
9. Stop the ticker and prevent new work on shutdown.

## Errors, privacy, and recovery

### Safe UI errors

Never display or log:

- credentials, tokens, OAuth secrets, authorization headers;
- raw API response bodies, request URLs, SQL parameters, SQL statements, or serialized snapshots; or
- raw addresses, notes, tracking data, or customer data inside an error/status message.

Extend existing Orders-safe errors with safe storage/snapshot classes such as unavailable storage, migration failure, corrupt local data, and invalid remote order data. Extract a shared `internal/sanitize` helper only when another completed feature genuinely needs it.

### Corruption recovery

When SQLite open/query/deserialization detects corrupt or unreadable order data:

1. preserve the user’s saved connection and credentials;
2. display a safe message that local order data must be rebuilt;
3. offer explicit **Delete and rebuild local order data** for the selected connection;
4. delete that connection’s `orders` and `order_sync_state` records transactionally; and
5. start bootstrap only after user confirmation.

Do not save a corrupt copy for diagnostics or upload it. It may contain private order details.

### Connection deletion

When a saved connection is deleted, delete only that connection’s order rows and sync-state data as part of its cleanup path. A failure must never delete another connection’s data or expose private SQL details.

## Application integration checklist

- [ ] Add startup store opening/migration in the application composition root.
- [ ] Surface a safe unavailable-storage status if opening/migrating fails.
- [ ] Inject store and sync coordinator dependencies so unit tests use temporary SQLite and fake Faire list pages.
- [ ] Replace `DesktopUI.ordersCache` reads/writes with local-store queries.
- [ ] Preserve/replace `ordersRequestID` stale-result protection for local list reads and sync completion.
- [ ] Add a separate detail request ID/active-order identity so stale snapshot/API responses cannot overwrite a newer order or connection.
- [ ] Update `drainOrderResults` or add focused result types so only safe list/detail models update Gio state.
- [ ] Update shutdown to cancel scheduler/work, close the database, and release in-memory UI state without deleting durable data.
- [ ] Update connection-deletion cleanup to remove the selected connection’s local cache.
- [ ] Add data-management controls after the core path: Refresh now, Rebuild local order data, Delete local order data.
- [ ] Ensure destructive confirmation copy says local data includes stored customer and shipping/order details and never deletes data at Faire.

## Test checklist

### Store

- [ ] Empty database migration and idempotent reopen.
- [ ] Migration sequencing and unsupported future-version failure.
- [ ] Durable reopen preserves data.
- [ ] Strict connection isolation for reads, upserts, deletion, and rebuild.
- [ ] Newer/equal/older `updated_at` conflict handling.
- [ ] Deterministic local filters, ordering, and keyset pagination.
- [ ] Snapshot round trip for nested addresses, notes, items, customizations, shipments, tracking, discounts, and payout data.
- [ ] Atomic indexed-column + snapshot replacement.
- [ ] Missing/malformed ID, timestamp, or snapshot serialization failure.
- [ ] Private path/file failure classification without data leaks.

### Synchronization

- [ ] Bootstrap sends the one-year `created_at_min` and follows every cursor.
- [ ] Incremental sync sends `high_watermark - overlap` via `updated_at_min`.
- [ ] Sync omits UI-only state/date filters and excluded states.
- [ ] Equal-timestamp replay does not duplicate or miss orders.
- [ ] An updated old order is received after bootstrap even when its `created_at` is before the bootstrap boundary.
- [ ] Empty response updates only the successful-sync time.
- [ ] Repeated cursor fails without checkpoint advancement.
- [ ] Failure/cancellation after any page retains the old completed watermark and converges on retry.
- [ ] Full snapshot and table projection update together.
- [ ] A mutation-result upsert does not advance the feed watermark.

### Application/UI

- [ ] Existing local rows render before a network response on startup/connection selection.
- [ ] Offline startup with local rows remains usable.
- [ ] Local detail opens without an API call.
- [ ] Explicit detail refresh replaces only the matching connection/order snapshot.
- [ ] Stale list/detail/sync results cannot overwrite a new filter, selection, or active connection.
- [ ] Manual Refresh invokes the same sync coordinator as automatic refresh.
- [ ] Automatic sync runs only for the active connection after one hour.
- [ ] Automatic ticks skip overlapping work and do not run after shutdown.
- [ ] Rebuild/delete cache is scoped to the selected connection.
- [ ] User-visible statuses remain credential-safe.

### Release/manual validation

- [ ] Run `go test ./...`.
- [ ] Run `go test -race ./...`.
- [ ] Validate a clean install and private config-directory/database/WAL behavior on macOS and Windows.
- [ ] Validate interruption/restart recovery, offline startup, credential failure with cached rows, and delete-and-rebuild recovery.
- [ ] Inspect a test database: intended order snapshots exist; API tokens, OAuth secrets, headers, request URLs, and HTTP response envelopes do not.

## Do not implement

- Do not persist credentials, tokens, headers, raw HTTP responses, or HTTP URLs.
- Do not use connection labels as storage identities.
- Do not sync all saved connections every hour.
- Do not poll while the application is closed.
- Do not add a tight failure retry loop.
- Do not advance a checkpoint after partial sync failure.
- Do not let raw order snapshots reach Gio layout code.
- Do not add order mutations, shipment actions, packing-slip work, or product/inventory storage in this slice.
- Do not alter CSV export to use cached snapshots without a separate explicit decision.
- Do not add application-level database encryption unless the approved threat model changes.
- Do not retain or export corrupt local data for support.

## Completion criteria

The work is complete only when:

1. local orders and full approved detail snapshots survive restart;
2. the table and detail view are usable offline from valid cached data;
3. incremental sync correctly converges using overlap-safe `updated_at_min` checkpoints;
4. active-connection hourly refresh and manual refresh share one safe sync path;
5. private order snapshots remain connection-isolated and credentials never reach SQLite;
6. a user can explicitly delete/rebuild local data safely; and
7. the automated and platform validation listed above passes.

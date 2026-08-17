# Architecture

## Purpose

Faire GUI is a Gio desktop client for selected Faire brand workflows. Its current completed vertical slice is a local-first, read-only Orders workflow with durable per-connection storage, incremental synchronization, local detail views, and API-backed CSV export.

This document records the current package boundaries and the constraints future work must preserve. It is not an implementation plan for the shipped Orders persistence slice.

## Current Orders workflow

The Orders feature provides:

- a connection-scoped SQLite cache at `<os.UserConfigDir()>/faire-gui/orders.sqlite3`;
- complete serialized `faire.Order` snapshots plus indexed table projections;
- initial one-year history synchronization using `updated_at_min`;
- cursor traversal of every remote page, with cursor-only follow-up requests;
- five-minute-overlap incremental polling based on the last completed `updated_at` watermark;
- hourly synchronization for the active connection while the application is open;
- local SQLite filtering, keyset pagination, and date-column sorting;
- local-first order detail views with an explicit remote detail refresh;
- state tabs that filter only local data and never limit the stored sync dataset;
- API-backed CSV export for New, Backordered, and selected orders; and
- connection-scoped rebuild/delete actions on each Brand Profile card.

The Orders table displays **Order date** from `created_at` and **Ship date** from expected, requested, or ship-after data. Its default local sort is Order date descending. The synchronization boundary and checkpoint remain `updated_at` values; they must not be substituted into the displayed Order date.

## Privacy and data boundaries

The database intentionally stores private order snapshots, including customer, address, item, shipment, tracking, notes, and payout data represented by `faire.Order`. It must never store or expose:

- API credentials, OAuth secrets, app credentials, or authorization headers;
- request URLs, response headers, raw response envelopes, or raw response bodies; or
- data from a different saved connection.

The database directory and file are owner-only where supported. SQLite WAL and shared-memory artifacts are private application data as well.

## Package boundaries

```text
cmd/faire-gui
      |
      v
application  ------> features/orders
      |                    |
      |                    v
      |                  faire
      |
      +--------------> internal/orderssync -----> internal/ordersstore
      |                    |
      +--------------> connections -------------> OS credential store
```

### `application/`

Owns Gio rendering, persistent widget state, navigation, worker initiation, result channels, request IDs, and safe status presentation. SQLite, JSON, credential, and HTTP operations run outside the frame loop.

Key Orders responsibilities include:

- immediately loading connection-scoped local rows before an eligible synchronization attempt;
- starting manual, automatic, history-expansion, direct-lookup, and detail-refresh workers;
- applying only `features/orders` presentation models or safe status strings on the Gio frame loop;
- retaining stale-result protection when filters, selected order, or active connection change; and
- presenting confirmation-backed local-data actions for the exact Brand Profile connection that initiated them.

### `features/orders/`

Contains Gio-free Orders state, date normalization, local table-sort state, selection behavior, list/detail presentation models, direct lookup normalization, and CSV formatting. It may use `faire` types but must not import Gio or persistence packages.

### `internal/ordersstore/`

Owns SQLite lifecycle, append-only migrations, connection-scoped snapshots and projections, local query/keyset pagination, sync-state persistence, atomic upserts, and scoped cache deletion.

It must not import `faire`, Gio, or `application`. Its storage key is always immutable `connections.Connection.ID`, never an editable label.

### `internal/orderssync/`

Owns typed remote page traversal, snapshot projection, all-pages synchronization, checkpoint correctness, and safe error classification. It may import `faire` and `internal/ordersstore`, but never Gio or `application`.

A completed checkpoint is written only after every page succeeds. A partial failure may retain safely committed page upserts, but it cannot advance the completed high-watermark. The next refresh replays from the prior watermark with the overlap window.

### `faire/`

Remains the typed Faire External API v2 boundary. It owns request construction, typed API models, and HTTP errors. It must not know about SQLite, Gio state, or rendering.

### `connections/`

Owns non-secret saved connection metadata and operating-system credential storage. It creates authenticated Faire clients for immutable connection IDs. Credentials must never move into SQLite or UI presentation state.

## Synchronization contract

### Bootstrap and history expansion

A connection without a completed bootstrap synchronizes every cursor page beginning at the one-year `updated_at_min` boundary. A user can enter an earlier **Updated At Minimum** date and refresh to expand retained history; the earlier boundary is saved for future sessions.

No status-tab or other local table filter is sent to the synchronization request. Every state returned by Faire is eligible for storage.

### Incremental synchronization

After a completed bootstrap, ordinary manual and hourly refreshes request:

```text
updated_at_min = max(retained history boundary, completed watermark - 5 minutes)
sort_by = UPDATED_AT
```

The overlap deliberately re-fetches a small tail. Atomic upserts prevent duplicates and allow an equal `updated_at` snapshot to replace a prior version. This protects against equal timestamps, delayed remote visibility, and an interruption between a page write and final checkpoint completion.

The initial request carries the update boundary, sort choice, and page size. Follow-up requests carry only the exact opaque cursor returned by Faire. Reapplying initial query controls to a cursor continuation can produce an invalid request.

### Failure behavior

`400 Bad Request` is classified as an invalid request and is not retried automatically. Safe UI statuses identify whether the rejected request was bootstrap, history expansion, incremental sync, or a cursor follow-up page without exposing request URLs, response content, or credentials.

Existing local rows remain visible on synchronization failure. A later successful refresh clears the failure state only when its attempt is newer than the failed worker, preventing stale worker results from overwriting a newer checkpoint.

## User interface behavior

- Synchronization and status messages are displayed above the Orders title.
- Date headers use vertical sort indicators: `↓` means newest first; `↑` means oldest first.
- Local data management lives on each Brand Profile card, alongside **Load brand profile**:
  - **Rebuild local data** deletes that card's local cache and starts a fresh all-pages bootstrap.
  - **Delete local data** deletes only that card's cache and never deletes Faire data or saved credentials.
- Confirming either data action returns the Brand Profile list to its status area. For the active connection, Orders-worker progress is mirrored there; non-active cards report independently without replacing the active Orders table.
- While a local-data action runs, the connection picker refuses active-connection changes. A user may still open Orders for the already active connection, where the same in-progress banner remains visible. This prevents a rebuild from racing a second sync for its connection.

## Future work

Future work should add one useful vertical slice at a time:

1. Product and inventory read-only workflows.
2. Additional local storage only after a clear product need and with equivalent privacy boundaries.
3. Mutating workflows—such as inventory updates, shipment handling, or product editing—only after confirming Faire API behavior, validation, authorization, confirmation, refresh, and test requirements.
4. OAuth Authorization Code Grant and reauthorization support.
5. Shared Gio or asynchronous helpers only after multiple completed features demonstrate a real common abstraction.
6. CI, release packaging, signing, and documented macOS/Windows validation.

Do not add packing slips, shipment mutations, generic async frameworks, or cached CSV export merely because related API endpoints exist. Each requires a deliberate product and privacy decision.

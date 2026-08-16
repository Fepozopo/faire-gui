# Future Architecture Plan

## Purpose

This document proposes a maintainable path from the current Gio desktop application toward progressively recreating Faire brand workflows.

The initial connection-management application now has a read-only Orders vertical slice. It provides:

- persistent sidebar navigation for Orders plus an expandable Settings submenu containing Brand profile, Connections, and manual update checking;
- an Orders startup page with no active saved Faire connection selected until the user explicitly chooses one;
- asynchronous, paginated, cached Orders loading;
- status tabs, a state-filter dialog, supported date filters, direct order-number lookup, and creation-time sorting;
- selectable table rows and CSV exports for New, Backordered, or selected orders; and
- contextual loading, empty, and credential-safe error feedback.

The attached Orders-page reference still suggests later capabilities:

- navigation for additional product workflows; and
- bulk actions such as packing slips and shipment updates.

This plan deliberately preserves two current boundaries:

- `faire/` remains the typed Faire External API v2 client; and
- `connections/` remains responsible for metadata persistence and operating-system credential storage.

The desktop UI must never persist, display, or log API tokens, OAuth tokens, app credentials, authorization headers, or raw API response bodies.

---

## Architectural principles

1. **Keep API, credential storage, application state, and rendering separate.**
   - `faire/` knows HTTP and API types.
   - `connections/` knows saved connection metadata and secrets.
   - feature packages know feature-specific state transformations and presentation models.
   - `application/` owns the Gio window, persistent widget state, navigation, and user actions.
   - `internal/ui/` provides reusable, non-business-specific Gio components.

2. **Treat Gio widgets as persistent state.**
   `widget.Editor`, `widget.Clickable`, `widget.List`, table scroll positions, sort controls, and row selection controls must belong to long-lived UI state. Do not recreate them during every frame.

3. **Background work returns safe values only.**
   HTTP, Keychain, and Credential Manager work runs outside the Gio frame loop. A worker publishes a typed, credential-safe result to a channel and calls `window.Invalidate()`. Only the frame loop updates rendered state.

4. **Use selected connection IDs, not saved credential values.**
   Store an active `connections.Connection.ID` as UI state. When a feature needs a client, call `connections.Manager.Client(ctx, connectionID, options)` in the background. Do not cache credentials in presentation state.

5. **Build vertical slices.**
   Add one useful, read-only workflow at a time before adding mutations. For example: Orders list → order details → packing slips → shipment actions.

6. **Prefer typed feature state over API-shaped UI state.**
   The UI should consume purpose-built rows such as `orders.Row`, not directly manipulate large Faire API response objects in layout functions.

---

## Proposed directory structure

The following is the target structure as the application grows. Existing files can move gradually; a large all-at-once refactor is not required.

```text
.
├── cmd/
│   └── faire-gui/
│       └── main.go
├── application/
│   ├── application.go
│   ├── desktop_ui.go
│   ├── gio_runtime.go
│   ├── gio_layout.go
│   ├── navigation.go
│   ├── update.go
│   ├── connection_actions.go
│   ├── connection_page.go
│   ├── brands_page.go
│   ├── orders_page.go
│   ├── orders_actions.go
│   ├── async.go                   # future, only after repeated async patterns emerge
│   ├── products_page.go
│   ├── inventory_page.go
│   ├── customers_page.go
│   ├── settings_page.go
│   └── *_test.go
├── features/
│   ├── orders/
│   │   ├── doc.go
│   │   ├── state.go
│   │   ├── query.go
│   │   ├── lookup.go
│   │   ├── presenter.go
│   │   ├── selection.go
│   │   ├── detail.go
│   │   ├── export.go
│   │   └── *_test.go
│   ├── products/
│   │   ├── state.go
│   │   ├── query.go
│   │   ├── presenter.go
│   │   └── *_test.go
│   ├── inventory/
│   │   ├── state.go
│   │   ├── presenter.go
│   │   └── *_test.go
│   ├── customers/
│   │   ├── state.go
│   │   ├── presenter.go
│   │   └── *_test.go
│   └── shipments/
│       ├── state.go
│       ├── validation.go
│       └── *_test.go
├── internal/
│   ├── ui/
│   │   ├── theme.go
│   │   ├── shell.go
│   │   ├── navigation.go
│   │   ├── card.go
│   │   ├── form.go
│   │   ├── modal.go
│   │   ├── status.go
│   │   ├── table.go
│   │   ├── empty_state.go
│   │   └── *_test.go
│   ├── async/
│   │   ├── result.go
│   │   ├── request.go
│   │   └── *_test.go
│   ├── sanitize/
│   │   ├── status.go
│   │   └── status_test.go
│   ├── ordersstore/
│   │   ├── store.go
│   │   ├── sqlite.go
│   │   ├── migrations.go
│   │   ├── queries.go
│   │   └── *_test.go
│   └── orderssync/
│       ├── source.go
│       ├── syncer.go
│       └── *_test.go
├── connections/
│   ├── connection.go
│   ├── manager.go
│   ├── repository.go
│   ├── system_credential_store.go
│   └── *_test.go
├── faire/
│   ├── client.go
│   ├── auth_error.go
│   ├── services_orders.go
│   ├── services_products.go
│   ├── services_inventory.go
│   ├── services_retailers.go
│   ├── types_*.go
│   └── *_test.go
├── docs/
│   ├── architecture.md
│   ├── gio-development.md
│   ├── orders-workflow.md
│   └── release-validation.md
├── README.md
├── FUTURE_ARCHITECTURE.md
├── go.mod
└── go.sum
```

Additional feature and internal packages should be introduced only when a completed feature makes them useful. Until then, keeping a small amount of feature-specific code in `application/` is preferable to creating empty abstractions.

---

## Folder and file responsibilities

### `cmd/faire-gui/`

| File      | Responsibility                                                                                                                                                                                   |
| --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `main.go` | Minimal executable entry point. Calls `application.Run()` on the process main goroutine, which Gio requires on macOS. No feature logic, rendering, credential access, or API calls belongs here. |

### `application/`

This package is the desktop composition root. It wires together the manager, API client creation, feature state, reusable Gio controls, and screen layout.

| File                    | Responsibility                                                                                                                                                                                                                                                          |
| ----------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `application.go`        | Presentation-independent helpers shared by desktop actions: startup loading, safe error-message conversion, profile summaries, and explicitly named environment-token lookup.                                                                                           |
| `desktop_ui.go`         | Defines `DesktopUI`, long-lived Gio widget and presentation state, startup construction, the top-level layout, and route selection.                                                                                                                                     |
| `gio_runtime.go`        | Runs the Gio window lifecycle: `Run`, frame-event setup, safe result draining, frame submission, shutdown, and cancellation.                                                                                                                                            |
| `gio_layout.go`         | Shared Gio layout primitives, fields, status treatment, panels, buttons, and drawing helpers. It contains no feature-page rendering or feature-specific API calls.                                                                                                      |
| `navigation.go`         | Implements the sidebar, expandable Settings submenu, session-only active-connection picker, Orders state-filter dialog, and route selection. Future product routes remain non-interactive until their workflows exist.                                                  |
| `update.go`             | Checks GitHub Releases at startup and on demand, presents safe update status, and coordinates user-approved installation.                                                                                                                                               |
| `async.go`              | Optional future home for reusable cancellable-work helpers when more than one page needs the same request lifecycle. Results must contain only safe typed data, user-safe status text, or sanitized errors.                                                             |
| `connection_actions.go` | Loads Brand profiles and creates, updates, imports, replaces, deletes, refreshes, and selects saved connections without exposing credentials.                                                                                                                           |
| `connection_page.go`    | Renders the saved-connection management forms, rows, and deletion confirmation dialog.                                                                                                                                                                                  |
| `brands_page.go`        | Renders the Brand profile verification screen and active-connection summary.                                                                                                                                                                                            |
| `orders_page.go`        | Renders the implemented read-only Orders route: status tabs, direct lookup and supported filters, row selection, CSV-export menu, table/list rows, empty/loading/error states, and pagination controls. It delegates query and presentation logic to `features/orders`. |
| `orders_actions.go`     | Loads Orders asynchronously through `Manager.Client` for the active connection. It provides stale-result protection, an in-memory safe-row cache, pagination, direct lookup, full-order CSV exports, and sanitized error results. It does not implement mutations.      |
| `products_page.go`      | Future product catalog page rendering: search, filters, product table/grid, details and editing entry points.                                                                                                                                                           |
| `inventory_page.go`     | Future inventory page rendering: stock table, search/filter state, and inventory-update forms.                                                                                                                                                                          |
| `customers_page.go`     | Future retailer/customer rendering: list, profiles, search, and detail screen entry points.                                                                                                                                                                             |
| `settings_page.go`      | Application preferences and non-secret display settings. Connection credential management remains in the Connections page.                                                                                                                                              |

### `features/`

A feature package contains **UI-framework-independent feature logic**. It may import `faire/` types but must not import `gioui.org/app`, `widget`, or `material`.

#### `features/orders/`

| File           | Responsibility                                                                                                                                                                                                                                               |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `doc.go`       | Documents the package boundary: Gio-free Orders state, query construction, lookup normalization, presentation, and selection behavior.                                                                                                                       |
| `state.go`     | Defines `State`, the supported server query, included state filters, selected order IDs, cursor, loading/status state, cache metadata, and safe row data.                                                                                                    |
| `query.go`     | Converts selected state/date filters, creation-time or update-time sorting, and a cursor into supported Faire order-list request options.                                                                                                                    |
| `lookup.go`    | Normalizes an entered display ID and converts it to Faire's internal `OrderID` format before a direct lookup.                                                                                                                                                |
| `presenter.go` | Converts typed Faire order responses into UI-ready, non-secret `Row` values: order number, status, customer label, totals, dates, commission label, and source. Formats dates/currency consistently.                                                         |
| `selection.go` | Adds/removes selected order IDs and selects or clears the visible rows. Keeps bulk-action selection behavior unit-testable without Gio.                                                                                                                      |
| `detail.go`    | Converts a locally stored or freshly retrieved typed Faire order into a purpose-built, credential-safe detail model. It exposes only fields deliberately approved for the detail screen and never gives layout code a raw API object or serialized snapshot. |
| `export.go`    | Defines the stable order CSV header and writes full typed orders as one row per item without retaining raw API data in UI state.                                                                                                                             |
| `*_test.go`    | Covers query construction, lookup normalization, list/detail presentation, CSV output, optional API fields, date/currency formatting, and selection behavior without Gio.                                                                                    |

#### `features/products/`

| File           | Responsibility                                                              |
| -------------- | --------------------------------------------------------------------------- |
| `state.go`     | Product search, filters, pagination, selected product, loading/error state. |
| `query.go`     | Product query construction from feature state.                              |
| `presenter.go` | Converts product and variant data into safe rows/cards for rendering.       |
| `*_test.go`    | Tests query and presentation behavior without Gio.                          |

#### `features/inventory/`

| File           | Responsibility                                                                             |
| -------------- | ------------------------------------------------------------------------------------------ |
| `state.go`     | Inventory list state, filtering, selected variants, pending updates, and validation state. |
| `presenter.go` | Safe stock-level and variant presentation formatting.                                      |
| `*_test.go`    | Tests validation and state transitions.                                                    |

#### `features/customers/`

| File           | Responsibility                                                                       |
| -------------- | ------------------------------------------------------------------------------------ |
| `state.go`     | Retailer list/detail view state, search state, pagination, and loading/error status. |
| `presenter.go` | Converts typed retailer responses into compact, display-ready values.                |
| `*_test.go`    | Tests UI-independent customer/retailer state behavior.                               |

#### `features/shipments/`

| File            | Responsibility                                                                                   |
| --------------- | ------------------------------------------------------------------------------------------------ |
| `state.go`      | Shipment-edit form state for selected orders.                                                    |
| `validation.go` | Validates shipping dates, tracking data, and other required inputs before calling the Faire API. |
| `*_test.go`     | Covers field validation and safe error messages.                                                 |

### `internal/ui/`

This package contains reusable Gio presentation primitives. It has no knowledge of Faire orders, products, credentials, or `connections.Manager`.

| File             | Responsibility                                                                                                                                                                |
| ---------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `theme.go`       | Defines the shared material theme, application palette, typography, spacing, and reusable sizes.                                                                              |
| `shell.go`       | Generic two-pane or navigation-rail shell used by future feature pages.                                                                                                       |
| `navigation.go`  | Reusable sidebar/navigation item layout with persistent button state supplied by the caller.                                                                                  |
| `card.go`        | Rounded panel/card rendering helpers. This can absorb the current `card`, `roundedPanel`, and `fill` helpers.                                                                 |
| `form.go`        | Input field, label, validation message, and action-row presentation helpers. It never reads editor values or handles credentials itself.                                      |
| `modal.go`       | Reusable blocking modal/scrim layout. The caller supplies title, body, and persistent confirm/cancel controls.                                                                |
| `status.go`      | Safe status, loading, warning, and error presentation. It should accept already-sanitized text only.                                                                          |
| `table.go`       | Reusable table header, row, column sizing, horizontal-scroll, and selection presentation helpers. It should support the orders table without embedding order-specific labels. |
| `empty_state.go` | Empty-list and error-state layouts with optional caller-supplied retry buttons.                                                                                               |

### `internal/async/`

This is optional until multiple pages need similar request handling.

| File         | Responsibility                                                                                                                                      |
| ------------ | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `result.go`  | Defines generic, non-secret result metadata such as request IDs, result kind, and completion time. Feature result payloads remain feature-specific. |
| `request.go` | Helpers for cancellation, superseding stale list requests, and non-blocking channel publication. It must never own Gio widget state.                |
| `*_test.go`  | Tests cancellation and stale-result behavior.                                                                                                       |

### `internal/sanitize/`

| File             | Responsibility                                                                                                                                                                                          |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `status.go`      | Shared policy helpers for converting known request, credential-store, and API errors into user-safe text. Never include raw bodies, headers, URLs containing credentials, or serialized request values. |
| `status_test.go` | Redaction and safe-message tests.                                                                                                                                                                       |

### `internal/ordersstore/`

This package is the persistence boundary for locally cached Orders data. It is introduced when the persistent Orders sync slice is implemented.

| File group                   | Responsibility                                                                                                                                                                                                                                                               |
| ---------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `store.go`, `queries.go`     | Defines storage-owned records and a narrow store interface for per-connection list reads, full snapshot reads, atomic upserts, sync checkpoints, and cache deletion. It must not import Gio or `faire/`, and must never persist credentials.                                 |
| `sqlite.go`, `migrations.go` | Owns SQLite opening/closing, private file/WAL handling, append-only schema migrations, transactions, indexes, and storage-error classification. It stores complete approved order snapshots but never HTTP headers, request URLs, response envelopes, or authorization data. |
| `*_test.go`                  | Covers migrations, connection isolation, snapshot round trips, atomic upserts, filtering/pagination, checkpoint integrity, and cache deletion.                                                                                                                               |

### `internal/orderssync/`

This package coordinates incremental remote-to-local synchronization without owning UI state or SQLite implementation details.

| File group               | Responsibility                                                                                                                                                                                                                                                                                                                                                  |
| ------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `source.go`, `syncer.go` | Defines a narrow injectable Faire-list source and follows `updated_at_min`/cursor pages with overlap protection. It converts typed Faire orders into atomic storage records, finalizes checkpoints only after a complete traversal, and returns safe sync summaries. It may depend on `faire/` and `internal/ordersstore/`, but never on Gio or `application/`. |
| `*_test.go`              | Covers bootstrap, overlap, repeated cursors, partial failure/cancellation recovery, and snapshot/store convergence without real HTTP requests.                                                                                                                                                                                                                  |

### `connections/`

Keep this package focused exactly as it is today.

| File                         | Responsibility                                                                          |
| ---------------------------- | --------------------------------------------------------------------------------------- |
| `connection.go`              | Non-secret saved connection metadata, credential bundle types, and validation.          |
| `manager.go`                 | Coordinates repository and credential store; creates clients for a selected connection. |
| `repository.go`              | Owner-only atomic metadata-file persistence.                                            |
| `system_credential_store.go` | macOS Keychain and Windows Credential Manager integration.                              |

No Gio imports should be added to this package.

### `faire/`

Keep this package as the typed API boundary.

| File group                                                               | Responsibility                                              |
| ------------------------------------------------------------------------ | ----------------------------------------------------------- |
| `client.go`, `auth_error.go`                                             | Auth configuration, HTTP transport, retries, typed errors.  |
| `services_orders.go`                                                     | Typed Orders endpoints used by the future Orders feature.   |
| `services_products.go`, `services_inventory.go`, `services_retailers.go` | Typed endpoints for their future feature packages.          |
| `types_*.go`                                                             | Generated/hand-maintained typed API models and identifiers. |

No Gio imports, widget state, route logic, or status-message rendering belongs here.

### `docs/`

As the project grows, keep long-lived engineering documentation separate from the README.

| File                    | Responsibility                                                                                              |
| ----------------------- | ----------------------------------------------------------------------------------------------------------- |
| `architecture.md`       | Maintained version of this high-level package dependency map.                                               |
| `gio-development.md`    | Gio frame-loop, persistent widget-state, modal-input-ordering, and background-work rules.                   |
| `orders-workflow.md`    | Product behavior for Orders: status definitions, filters, bulk action requirements, and acceptance checks.  |
| `release-validation.md` | macOS/Windows build, signing, native-window, Keychain/Credential Manager, and manual acceptance validation. |

---

## Target dependency direction

```text
cmd/faire-gui
      |
      v
application  ------------------>  internal/ui
      |                                  |
      +-------------------+--------------+-----+
      |                   |              |
      v                   v              v
features/orders, ...  internal/orderssync  internal/async and internal/sanitize
      |                   |
      |                   +---------> internal/ordersstore
      v
    faire
      |
      +-------------> connections -------------> operating-system credential store
```

Important restrictions:

- `faire/` must not depend on `application/`, `features/`, or Gio.
- `connections/` must not depend on `application/`, `features/`, or Gio.
- `features/` must not depend on Gio.
- `internal/ui/` must not know API request types, credentials, or connection IDs beyond generic caller-provided values.
- `internal/ordersstore/` must not depend on Gio, `application/`, or `faire/`; `internal/orderssync/` may depend only on `faire/` and `internal/ordersstore/` among application packages.
- only `application/` should bridge persistent Gio widget state to feature state, store/sync operations, and manager/API operations.

---

## Orders page: implemented first feature shape

The Orders route is now a **read-only, paginated list**, inspired by the attached reference and constrained to the Faire API capabilities represented in `faire/services_orders.go`.

### Current page state

`features/orders.State` now keeps the Gio-free state required by the list:

```go
// State is the Gio-free state for the Orders list.
type State struct {
    StatusTab      StatusTab
    IncludedStates map[faire.OrderState]struct{}
    Rows           []Row
    SelectedIDs    map[faire.OrderID]struct{}
    Query          ServerQuery
    Loading        bool
    Status         string
    Cursor         string
    Loaded         bool
    CacheKey       string
}
```

The implemented behavior includes:

- a New-and-Processing default state selection, creation-time ordering, and a server-side exclusion mapping for selected states;
- high-level All, New, Processing, Fulfilled, and Canceled tabs plus a state-filter dialog for all supported states;
- direct lookup by normalized display ID;
- supported creation-date and ship-date filters;
- safe, typed order rows and selectable IDs for CSV export or future bulk actions;
- loading, empty, credential-safe error, pagination, and cache-status feedback;
- in-memory safe-row caching per connection and query; and
- request IDs that prevent a stale list or lookup result from replacing newer UI state.

### Current layout components

`application/orders_page.go` renders:

1. page title and active-connection label;
2. refresh control and safe loading/error/empty status;
3. status tabs;
4. direct order-number lookup and supported date/state filters;
5. a scrollable desktop table with stable column widths;
6. whole-row and header-checkbox selection controls, plus a CSV export menu for New, Backordered, and selected orders; and
7. a Load more control when Faire returns a cursor.

No mutation is implemented. Do not add “Create order,” shipment edits, packing-slip generation, or bulk operations merely because they appear in the visual reference. Each requires a confirmed Faire API operation, validation rules, authorization behavior, confirmation and refresh behavior, and dedicated tests.

---

## Logical next step

### Add a read-only Order detail vertical slice

The Gio composition layer has been split into dedicated state, runtime, connection-action, connection-page, and Brand-profile files. The Orders list has reached the right stopping point for a first workflow, so the logical next step is a focused, read-only persistent sync and detail slice—not mutations or another broad navigation area:

> Synchronize complete approved order snapshots for the active connection → render the Orders list from local indexed data → select an order → present a typed local detail model → explicitly refresh from Faire only when requested.

This adds restart-safe local-first behavior and turns the current list into a useful read-only detail workflow before shipment or packing-slip mutations are considered. Complete cached order snapshots are private local data; they must be deleted with the selected connection's cache and must never be passed raw to the Gio frame loop.

### Scope

1. Add persistent storage and synchronization boundaries.
   - Add `internal/ordersstore/` for SQLite snapshots, local list/detail reads, migrations, checkpoint state, and per-connection cache deletion.
   - Add `internal/orderssync/` for typed Faire pagination, overlap-safe `updated_at_min` synchronization, and final checkpoint progression.
   - Keep `faire/` as the HTTP/API boundary and `connections/` as the metadata/credential boundary.

2. Add Orders detail presentation models in `features/orders/`.
   - Add a clearly named `detail.go` rather than expanding the list-row model.
   - Convert a typed locally stored or freshly retrieved `faire.Order` into only the display values deliberately approved for the detail screen.
   - Do not let raw snapshots, credentials, authorization data, HTTP data, or unapproved fields reach layout code.
   - Unit test missing optional fields, output safety, and snapshot-to-detail mapping.

3. Add local-first list/detail actions, route, and persistent controls in `application/`.
   - Read local list rows before starting a background sync and preserve the existing list's filters, local cursor, scroll position, and selection when navigating back.
   - Use a dedicated `widget.Clickable` per visible order or an explicit detail action; do not overload selection, which remains reserved for future bulk actions.
   - Read the selected order snapshot in a worker, convert it to the detail model, and provide a clear Back-to-Orders action.
   - Add an explicit detail refresh that obtains the client through `Manager.Client`, calls `client.Orders.Get`, updates the snapshot in the background, and republishes a safe detail model.
   - Use separate list/detail request IDs (or equivalent) so stale results cannot overwrite a newer selection, filter, or active connection.

4. Render `application/order_detail_page.go`.
   - Show the order number, status, dates, item summary, monetary summary, and the customer/shipping/order information deliberately approved for this read-only screen.
   - Include local-data freshness, loading, empty/not-found, and actionable credential-safe error states.
   - Keep shipment, packing-slip, cancellation, and other mutations absent.

5. Test the vertical slice.
   - selecting a row reads the matching local detail snapshot without a network request;
   - an explicit refresh updates only that connection/order's local snapshot;
   - no active connection or missing order ID produces an actionable message without an API request;
   - a changed connection or later selection rejects a stale result;
   - list state is preserved when returning from detail; and
   - visible detail statuses never include credentials, raw API response bodies, or serialized order data.

### Acceptance criteria

- A user can open a selected order's locally stored detail view from the current Orders list and return without losing the list context.
- Local reads, synchronization, and explicit detail refreshes run asynchronously and the window remains interactive while work is in progress.
- The detail screen presents only the explicitly approved, typed display model even though full snapshots are retained privately in SQLite.
- Missing optional API fields, unknown states, unreadable local snapshots, and API failures render safely and actionably.
- The existing Orders list, local pagination, filters, and selection controls continue to work unchanged.
- Targeted feature/application tests pass, followed by `go test ./...` and `go test -race ./...`.
- The macOS and Windows detail navigation workflow is manually verified before release.

---

## Suggested delivery sequence

1. Active connection state, route navigation, and the read-only Orders list — **completed**.
2. Status tabs, supported filters, direct lookup, pagination, caching, and row selection — **completed**.
3. Split the Gio composition layer into desktop state, runtime, connection actions, connection page, and Brand profile page files — **completed**.
4. Persistent Orders sync and a read-only local-first order detail page, preserving the Orders-list context.
5. Product and inventory read-only pages.
6. Mutating workflows with validation, confirmation, refresh behavior, and tests: inventory updates, shipment processing, and product editing.
7. OAuth Authorization Code Grant and reauthorization workflow.
8. Extract shared Gio shell and asynchronous helpers only when multiple completed features justify them.
9. Platform build/release validation, CI, and packaging.

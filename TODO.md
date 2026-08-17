# TODO — Orders release validation

## Status

The SQLite-backed, local-first Orders implementation is complete in code and covered by the automated Go test suite. The former implementation checklist and detailed persistent-sync plan have been retired.

The remaining work is release/manual validation that requires supported platforms and a controlled Faire connection. Do not mark these items complete without performing and recording the check.

## Completed implementation

- [x] Per-connection pure-Go SQLite storage, migrations, owner-only application-data handling, WAL, busy timeout, and scoped cache deletion.
- [x] Complete serialized `faire.Order` snapshots and indexed local projections with atomic, timestamp-aware upserts.
- [x] Local Orders filtering, keyset pagination, date sorting, search-first lookup, local-first details, and explicit detail refresh.
- [x] One-year `updated_at_min` bootstrap, earlier-history expansion, all-pages cursor traversal, and cursor-only continuation requests.
- [x] Five-minute-overlap incremental synchronization, terminal-page checkpoint advancement, partial-failure recovery, and invalid-request classification.
- [x] Active-connection hourly synchronization and a shared manual refresh path.
- [x] Credential-safe worker/result-channel integration, stale-result protection, and local status feedback.
- [x] Brand Profile per-connection rebuild/delete controls with confirmation, visible progress feedback, and duplicate-sync protection.
- [x] Automated validation:

  ```sh
  go test ./...
  go test -race ./...
  ```

## Remaining manual release validation

### macOS and Windows storage behavior

- [ ] Start from a clean installation and verify the database, WAL, and shared-memory files are created under the application config directory with the expected private access behavior.
- [ ] Open an existing version-1 Orders database and verify migration triggers one updated-at bootstrap while retaining readable cached snapshots.
- [ ] Verify shutdown cancels active work, closes the database, and preserves durable data.

### Faire synchronization behavior

- [ ] Use a controlled connection with more than one Orders page to verify initial bootstrap, history expansion, and incremental synchronization each retrieve every page.
- [ ] Verify a normal manual and hourly refresh request only changes since the completed watermark, plus the five-minute replay overlap.
- [ ] Verify an earlier **Updated At Minimum** expands retained history and remains restored after restart.
- [ ] Verify all Faire order states are stored even when the displayed table is filtered to one state.
- [ ] Verify a rejected cursor follow-up or other `400` reports a safe phase-specific status, retains the old completed checkpoint, and does not enter an automatic retry loop.

### Local-first and recovery behavior

- [ ] Verify offline startup shows already cached Orders and local details without a Faire request.
- [ ] Verify direct lookup falls back to Faire only when the local display-ID index has no matching order.
- [ ] Verify rebuild/delete actions affect only the clicked Brand Profile card's connection and never delete credentials or Faire data.
- [ ] While a rebuild is in progress, verify Orders can be opened for the already active connection without starting a duplicate sync; verify active-connection changes are deferred until completion.
- [ ] Verify rebuild/delete confirmation returns the Brand Profile list to the top and displays progress/completion status.
- [ ] Inspect a test database to confirm approved order snapshots are present while API tokens, OAuth secrets, authorization headers, request URLs, response headers, and raw HTTP envelopes are absent.

## Future work

Broader architecture and future feature direction are maintained in [`FUTURE_ARCHITECTURE.md`](FUTURE_ARCHITECTURE.md).

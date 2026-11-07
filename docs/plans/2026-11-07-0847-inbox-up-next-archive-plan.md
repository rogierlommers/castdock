---
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
status: implemented
---

# Inbox, Up Next, and Archive Plan

## Goal

Give every episode one server-owned lifecycle state so new releases can be processed from an inbox, intentionally ordered for listening, or retained in a browsable archive.

## Product Contract

- Existing episodes and the history fetched during a feed's first successful refresh are archived.
- Episodes first discovered by later refreshes enter the inbox, newest first.
- Inbox episodes can be archived or appended to Up Next.
- Archived episodes remain browsable globally and per podcast and can be appended to Up Next.
- Up Next has explicit ordering with move-up, move-down, and remove controls. Removal archives the episode.
- Completing a queued episode archives it, compacts the queue, and advances the web player to the next queued episode.
- Queue and lifecycle state are canonical backend data for both web and future iOS clients.

## Implementation Units

1. Add an immutable migration for episode lifecycle state and unique queue positions, backfilling existing rows to archived.
2. Preserve state on feed metadata refreshes; classify only newly inserted episodes according to initial versus subsequent refresh.
3. Add transactional state transitions, queue compaction/reordering, completion handling, and paginated state queries.
4. Add Inbox, Up Next, and Archive routes, navigation, templates, controls, search, and player advancement.
5. Add migration, repository, handler, refresh, and browser-script regression coverage; update durable architecture documentation.

## Acceptance Evidence

- Initial feed history is archived and a later new episode enters Inbox.
- Queue append, reorder, remove, and completion leave contiguous unique positions.
- Completed queued playback redirects to the next queued episode when one exists.
- Archive search and state pages are bounded and paginated.
- Formatting, vet, tests, race tests, and Compose validation pass.

## Validation Result

- Migration tests verify existing episodes become archived and migrations remain idempotent.
- Repository tests verify initial-versus-later classification, archive search, queue append/reorder/removal, contiguous positions, and completion transitions.
- HTTP integration tests verify Inbox, Archive search, and Up Next state transitions with real SQLite storage.
- Handler tests verify safe redirects and next-episode responses; `node --check` validates the player script.
- Formatting, `go vet ./...`, `go test ./...`, `go test -race ./...`, and Compose configuration validation pass.
- Browser automation remains unavailable because this environment has no browser driver; server-rendered workflows are covered by handler and HTTP integration tests.

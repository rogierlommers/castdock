---
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
status: implemented
---

# CastDock Go App Implementation Plan

## Goal

Deliver a runnable single-user Go podcast server that imports individual feeds and OPML files, refreshes feeds on demand and on a schedule, renders a usable HTML library and player, and persists playback state every five seconds and on player lifecycle events.

## Product Contract

- Runtime configuration is exclusively environment-variable based, validated at startup, and documented in `.env.example`.
- SQLite stores subscriptions, episodes, and per-episode playback state.
- OPML import recursively discovers `xmlUrl` outlines, skips duplicate subscriptions, and reports imported, skipped, and failed feeds.
- Audio streams directly from publisher enclosure URLs; CastDock does not proxy or download media.
- The web player resumes persisted position and saves while playing every five seconds plus immediately on pause, seek, completion, page hide, and episode change.
- Feed refresh is both scheduled and manually triggerable.
- Feed URLs and redirects are constrained to HTTP(S), resolved through an SSRF-aware client, and bounded by timeout and response size. Descriptions render as text, not trusted HTML.
- This release has no login and is intended only for a trusted private network. Docker Compose binds the published port to loopback by default.
- SwiftUI and downloaded/offline media are outside this milestone.

## Implementation Units

1. Add Go module, embedded migrations, environment configuration, SQLite connection, and graceful process lifecycle.
2. Implement domain records and SQLite repositories with transactional feed/episode upserts and playback upserts.
3. Implement safe feed retrieval, RSS/Atom parsing, OPML parsing, duplicate handling, and scheduled/manual refresh orchestration.
4. Implement server-rendered library, podcast, import, and player pages plus state-changing HTTP endpoints and same-origin protection.
5. Implement browser audio controls and frequent playback synchronization with retry-safe last-write-wins semantics.
6. Add Docker packaging, environment reference, operations documentation, and exact commands to `AGENTS.md`.
7. Verify configuration, migrations, parser/import behavior, SSRF policy, repositories, handlers, browser assets, race safety, and container configuration.

## Acceptance Evidence

- `go test ./...` and `go test -race ./...` pass.
- `go vet ./...` passes.
- A fresh database migrates automatically; restart preserves subscriptions, episodes, and playback.
- Focused tests cover OPML recursion/deduplication, feed parsing, unsafe feed URLs, feed upserts, and playback updates.
- `docker compose -f deploy/compose/compose.yaml config` succeeds when Docker is available.

## Validation Result

- Formatting, `go vet ./...`, `go test ./...`, and `go test -race ./...` pass.
- `govulncheck` reports no known vulnerabilities reachable from the application.
- Compose configuration resolves successfully with `.env.example`.
- Container image construction remains unverified because the local Docker/Colima daemon is not running.
- Browser automation remains unverified because this environment has no host browser driver and `agent-browser` is not installed; HTTP routes and templates are covered by handler tests.

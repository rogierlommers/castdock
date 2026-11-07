# CastDock Agent Guide

## Current State

- The Go web app is runnable. iOS, Android, and Linux/macOS CLI clients remain scaffolds under `clients/`.
- The initial product is a single-user, self-hosted podcast library: it ingests external RSS feeds, tracks playback, serves a Go-rendered web UI, and exposes contracts for future native and CLI clients.
- The deployment baseline is one Go service with SQLite under Docker Compose. Treat changes to those choices as architecture decisions, not incidental implementation details.

## Commands

- Format: `gofmt -w cmd internal migrations web/embed.go`
- Static checks: `go vet ./...`
- Tests: `go test ./...`; race checks: `go test -race ./...`
- Focused package/test: `go test ./internal/syndication` or `go test ./internal/syndication -run '^TestName$'`
- Run locally with `.env`: `./run.sh`; run directly after exporting the environment with `go run ./cmd/castdock`
- Validate Compose: `docker compose --env-file .env -f deploy/compose/compose.yaml config`
- Keep this order for final verification: format, vet, tests, race tests, then Compose validation when Docker is available.

## Boundaries

- `cmd/castdock/` is the composition root. Keep it to startup, configuration, dependency wiring, and lifecycle management.
- `internal/podcast/` and `internal/playback/` own domain rules. They must not import HTTP, SQLite, RSS parser, Docker, or Apple-framework concerns.
- `internal/syndication/` owns external feed retrieval, parsing, normalization, and refresh scheduling. It may depend on domain contracts; domain packages must not depend on it.
- `internal/storage/` implements persistence and owns SQLite-specific code. Do not expose SQL rows or migration details to domain or client code.
- `internal/server/` owns HTTP transport, API handlers, middleware, and web delivery. Keep business rules out of handlers.
- `web/` belongs to the Go service and is shipped with it; it is not a separate Node application.
- Everything under `clients/` communicates through the service API. Clients must not depend on the SQLite schema or duplicate server-owned feed, playback, inbox, archive, or queue rules.
- `migrations/` is the ordered history of persistent-schema changes. Never edit an applied migration; add a new one.
- Runtime configuration is environment-only. Add new settings to `internal/config`, `.env.example`, `README.md`, and Compose together; do not introduce runtime config files or CLI flags.
- `api/openapi.yaml` is the published HTTP contract. Update it with every documented route, request, response, or compatibility change; `/swagger/` must remain self-hosted.
- Access logs are opt-in through `CASTDOCK_ACCESS_LOG_ENABLED`; do not put per-request noise back under the general debug level. Migration progress and summaries are ordinary startup logs, with already-applied files visible at debug level.
- The System page uses an explicit configuration allow-list. Never enumerate environment variables or reflect configuration generically; future settings may contain secrets.

## Safety and Operations

- Feed URLs, redirects, XML, episode descriptions, artwork, and enclosure URLs are untrusted input. Before adding fetching or rendering, define limits, timeouts, redirect checks, private-network policy, and HTML sanitization.
- Single-user does not mean safely unauthenticated on a public network. Keep the service private by default until the exposure and authentication model is explicit.
- Keep runtime data outside the container and preserve the documented SQLite backup/restore path in `deploy/compose/`; this release stores no downloaded media.
- Never commit credentials or filled `.env` files; document required variables in an example file when configuration is introduced.

## Non-obvious Behavior

- Playback updates are last-write-wins by client timestamp. The browser saves every five seconds while playing and on play, pause, seek, completion, and page hide; preserve out-of-order-write protection.
- Episode lifecycle is server-owned: initial feed history and existing migrated rows are `archived`, episodes discovered on later refreshes are `inbox`, and user-selected episodes are `queued`. Feed upserts must preserve an existing episode's state.
- Queue positions are unique and contiguous. Append, reorder, removal, and completed-playback transitions must remain transactional; completing a queued episode archives it and advances the web player.
- Marking an episode as played writes completed playback state and archives it in the same transaction; queued episodes must also compact the remaining queue.
- Up Next is managed on the landing page; `/up-next` is only a compatibility redirect. Drag-and-drop persists through `POST /queue/reorder`, while up/down forms remain the accessible touch and keyboard fallback.
- Full-library refreshes are application-owned background work, never request-owned. Manual and scheduled runs share a single-flight guard, shutdown waits for cancellation to settle, and `context.Canceled` must not be persisted as a feed error.
- Audio comes directly from publisher enclosure URLs. Do not add proxying or downloads without revisiting storage, range requests, SSRF, and backup behavior.
- Feed requests reject private, loopback, link-local, and carrier-grade NAT destinations by default, including after DNS resolution and redirects. `CASTDOCK_ALLOW_PRIVATE_FEEDS=true` is an explicit opt-out.
- Episode show notes keep a sanitized HTML representation plus plain text for search/fallback. Sanitize publisher markup during ingestion and again at the rendering boundary; never cast raw or merely persisted markup to trusted template HTML.

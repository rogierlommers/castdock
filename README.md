# CastDock

CastDock is a self-hosted, single-user podcast library and web player. The Go service imports RSS/Atom feeds and OPML subscription lists, stores the library and playback state in SQLite, refreshes feeds on a schedule, and serves an HTML interface with frequent playback synchronization.

Audio streams directly from each podcast publisher. CastDock stores metadata and playback state, not media files.

> [!WARNING]
> CastDock does not yet include authentication. Keep it on a trusted private network; the supplied Docker Compose configuration publishes only to loopback by default.

## Run locally

Requirements: Go 1.27 or newer.

```sh
cp .env.example .env
mkdir -p data
./run.sh
```

Open <http://127.0.0.1:8080>. Runtime configuration is environment-variable-only; `.env` is a local shell convenience and is ignored by Git.

## Run with Docker Compose

```sh
cp .env.example .env
docker compose --env-file .env -f deploy/compose/compose.yaml up --build
```

See [`deploy/compose/README.md`](deploy/compose/README.md) for exposure, persistence, backup, and restore guidance.

## Development checks

```sh
gofmt -w cmd internal migrations web/embed.go
go vet ./...
go test ./...
go test -race ./...
```

Run one package or one test with:

```sh
go test ./internal/syndication
go test ./internal/syndication -run '^TestParseOPMLNestedAndDeduplicated$'
```

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `CASTDOCK_LISTEN_ADDR` | `127.0.0.1:8080` | HTTP listen address |
| `CASTDOCK_DATABASE_PATH` | `./data/castdock.db` | SQLite database path |
| `CASTDOCK_REFRESH_INTERVAL` | `1h` | Scheduled refresh cadence; `0` disables it |
| `CASTDOCK_REFRESH_CONCURRENCY` | `4` | Concurrent feed refreshes, from 1 to 32 |
| `CASTDOCK_HTTP_TIMEOUT` | `20s` | Outbound feed request timeout |
| `CASTDOCK_MAX_FEED_BYTES` | `33554432` | Maximum feed response size (32 MiB) |
| `CASTDOCK_MAX_OPML_BYTES` | `5242880` | Maximum OPML upload size |
| `CASTDOCK_MAX_OPML_FEEDS` | `500` | Maximum subscriptions accepted from one OPML file |
| `CASTDOCK_ALLOW_PRIVATE_FEEDS` | `false` | Allow feed URLs resolving to private addresses |
| `CASTDOCK_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |

Every runtime setting is read from the environment and validated during startup. There are no application config files or runtime CLI flags.

Logs are written to stdout as human-readable `key=value` text. Set `CASTDOCK_LOG_LEVEL=debug` to log request timing, playback-position saves, and feed-refresh start/completion details. Logs intentionally identify feeds by database ID and title rather than URL because private feed URLs may contain subscription tokens.

## Repository layout

| Path | Ownership |
| --- | --- |
| `cmd/castdock/` | Process entrypoint, dependency wiring, scheduling, and shutdown |
| `internal/podcast/` | Podcast, feed, subscription, and episode domain records |
| `internal/playback/` | Playback-state validation |
| `internal/syndication/` | Safe RSS retrieval, parsing, OPML import, and refresh orchestration |
| `internal/storage/` | SQLite migrations and persistence |
| `internal/server/` | HTTP routes, middleware, API handlers, and template rendering |
| `web/` | Embedded templates and browser assets |
| `migrations/` | Embedded, ordered SQLite migrations |
| `deploy/compose/` | Container image and Compose deployment |
| `ios/CastDock/` | Reserved for the future native SwiftUI client |

# CastDock

[![Test and publish container](https://github.com/rogierlommers/castdock/actions/workflows/ci-docker.yml/badge.svg?branch=main)](https://github.com/rogierlommers/castdock/actions/workflows/ci-docker.yml)

CastDock is a self-hosted, single-user podcast library and web player. The Go service imports RSS/Atom feeds and OPML subscription lists, stores the library and playback state in SQLite, refreshes feeds on a schedule, and serves an HTML interface with frequent playback synchronization.

## Opinionated by design

CastDock is deliberately focused on personal self-hosting, not multi-tenant SaaS or broad podcast-platform compatibility. It assumes one trusted user, one service, SQLite, direct publisher audio streaming, and the Inbox → Up Next → Archive workflow described below. Defaults favor a small private deployment, understandable operations, and portable data over cloud scale or extensive customization. If those constraints match how you want to listen, CastDock aims to provide a coherent experience rather than a collection of interchangeable modes.

Audio streams directly from each podcast publisher. CastDock stores metadata and playback state, not media files.

Each subscription can define its own playback speed from 0.5× to 3× on the podcast page. The preference is stored by the backend and applied automatically whenever an episode from that podcast is played.

New episodes discovered after a podcast's first refresh arrive in **Inbox**, shown as the primary landing-page section above **Up Next**. From there they can be archived, marked as played, or appended to the ordered queue. Up Next is managed entirely on the landing page with drag-and-drop ordering plus keyboard- and touch-friendly move controls. Subscription management, OPML import, and OPML export live together on the dedicated **Library** page. Existing feed history starts in the searchable **Archive**, and completing or marking a queued episode as played archives it and compacts the queue.

**Play History** appears directly after Home and shows the 20 most recently played episodes with a human-friendly playback timestamp. The dedicated **Recent Published** page lists the complete library newest-first in bounded pages, including episode artwork, formatted show notes, played status, lifecycle state, and queue/archive actions. Publisher show-note HTML is sanitized during ingestion and again before rendering.

The read-only **System** page shows runtime and build information, library and database statistics, the 50 most recent client network events for the current process, and an explicit allow-list of effective configuration values. Network events include only the client label, request method and path, response status, and server timestamp; they are cleared on restart. The page never dumps the process environment, so future secret settings are not exposed automatically.

## Native iOS client

The first native SwiftUI client lives in [`clients/ios`](clients/ios). It connects to the same JSON API as the web player and supports Inbox, Up Next, lifecycle actions, feed playback speed, ±15-second seeking, five-second playback synchronization, background audio and lock-screen controls. Open `clients/ios/CastDock.xcodeproj` in Xcode; setup and scope details are in the [iOS README](clients/ios/CastDock/README.md).

**Refresh all** starts one application-owned background refresh and returns to the Library immediately. Manual and scheduled full-library refreshes share a single-flight guard, so they cannot overlap; an in-progress refresh is canceled cleanly during application shutdown rather than being recorded as a feed failure.

> [!WARNING]
> Authentication is optional and disabled when credentials are empty. Keep unauthenticated deployments on a trusted private network. Even with login enabled, prefer HTTPS through a trusted reverse proxy or private overlay network so credentials and session cookies are not sent over plaintext HTTP.

## Run with docker

If you want a quick check how it looks, use this:

```
docker run --rm --name castdock --publish 8080:8080 rogierlommers/castdock:latest
```

And open http://localhost:8080 to see the GUI.

## Run with docker-compose

To run the app using docker-compose, use the following:

```
  castdock:
    container_name: castdock
    image: rogierlommers/castdock:latest
    user: "1000:1000"
    ports:
      - "8080:8080"
    volumes:
      - /your_data/castdock:/data
    environment:
      CASTDOCK_LISTEN_ADDR: ":8080"
      CASTDOCK_DATABASE_PATH: "/data/castdock.db"
      CASTDOCK_REFRESH_INTERVAL: "1h"
      CASTDOCK_REFRESH_CONCURRENCY: "4"
      CASTDOCK_HTTP_TIMEOUT: "20s"
      CASTDOCK_MAX_FEED_BYTES: "33554432"
      CASTDOCK_MAX_OPML_BYTES: "5242880"
      CASTDOCK_MAX_OPML_FEEDS: "500"
      CASTDOCK_ALLOW_PRIVATE_FEEDS: "false"
      CASTDOCK_LOG_LEVEL: "debug"
      CASTDOCK_ACCESS_LOG_ENABLED: "false"
    restart: unless-stopped
```

## Run locally

Requirements: Go 1.27 or newer.

```sh
cp .env.example .env
mkdir -p data
./run.sh
```

Open <http://localhost:8080>. Runtime configuration is environment-variable-only; `.env` is a local shell convenience and is ignored by Git.

## Run with Docker Compose

```sh
cp .env.example .env
docker compose --env-file .env -f deploy/compose/compose.yaml up --build
```

See [`deploy/compose/README.md`](deploy/compose/README.md) for exposure, persistence, backup, and restore guidance.

## API documentation

With CastDock running, open the self-hosted Swagger UI at <http://localhost:8080/swagger/index.html>. The embedded OpenAPI 3.0 document is available directly at <http://localhost:8080/api/openapi.yaml> and lives in [`api/openapi.yaml`](api/openapi.yaml).

The contract currently covers health checks, playback synchronization, queue reordering, and OPML import/export. CastDock has no application-level authentication yet, so API writes must remain on a trusted network and are protected against cross-origin browser requests.

## Development checks

```sh
gofmt -w cmd internal migrations api web/embed.go
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
| `CASTDOCK_LISTEN_ADDR` | `0.0.0.0:8080` | HTTP listen address |
| `CASTDOCK_DATABASE_PATH` | `./data/castdock.db` | SQLite database path |
| `CASTDOCK_REFRESH_INTERVAL` | `1h` | Scheduled refresh cadence; `0` disables it |
| `CASTDOCK_REFRESH_CONCURRENCY` | `4` | Concurrent feed refreshes, from 1 to 32 |
| `CASTDOCK_HTTP_TIMEOUT` | `20s` | Outbound feed request timeout |
| `CASTDOCK_MAX_FEED_BYTES` | `33554432` | Maximum feed response size (32 MiB) |
| `CASTDOCK_MAX_OPML_BYTES` | `5242880` | Maximum OPML upload size |
| `CASTDOCK_MAX_OPML_FEEDS` | `500` | Maximum subscriptions accepted from one OPML file |
| `CASTDOCK_ALLOW_PRIVATE_FEEDS` | `false` | Allow feed URLs resolving to private addresses |
| `CASTDOCK_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `CASTDOCK_ACCESS_LOG_ENABLED` | `false` | Emit one access log for each HTTP request |
| `CASTDOCK_AUTH_USERNAME` | empty | Single-user login name; both credentials must be set to enable authentication |
| `CASTDOCK_AUTH_PASSWORD` | empty | Single-user password; keep it secret and never commit a populated `.env` |
| `CASTDOCK_SESSION_TTL` | `720h` | Login session lifetime; sessions are invalidated when CastDock restarts |

Every runtime setting is read from the environment and validated during startup. There are no application config files or runtime CLI flags.

When authentication is enabled, CastDock protects the web UI, Swagger, OPML operations, and JSON API with an opaque, in-memory session. `/login`, static assets, and `/healthz` remain public. The System page reports only whether authentication is enabled and the session lifetime; it never displays the username or password.

Logs are written to stdout as human-readable `key=value` text. Set `CASTDOCK_LOG_LEVEL=debug` for playback-position saves and one detailed completion event per feed refresh. Set `CASTDOCK_ACCESS_LOG_ENABLED=true` independently to emit request method, path, status, response size, and duration at `info`. Logs intentionally identify feeds by database ID, title, and hostname rather than full URL because private feed URLs may contain subscription tokens.

At startup, CastDock logs the database migration check, every migration applied during that run, its duration, and a completion summary containing the current schema version and applied count. With debug logging enabled, already-applied migration files are listed as skipped.

## GitHub CI and Docker Hub publishing

`.github/workflows/ci-docker.yml` runs formatting, static analysis, and race-enabled tests before building a `linux/amd64` and `linux/arm64` container. Pull requests build without publishing. Pushes to `main`, `v*` tags, and manually dispatched runs publish to [`rlommers/castdock`](https://hub.docker.com/r/rlommers/castdock).

Configure these GitHub repository secrets before the first publishing run:

- `DOCKERHUB_USERNAME` — Docker Hub account name with write access to `rlommers/castdock`
- `DOCKERHUB_TOKEN` — Docker Hub access token; do not use an account password

`main` publishes `latest` and a commit-SHA tag. Version tags such as `v1.2.3` additionally publish `1.2.3` and `1.2`. Dependabot checks Go modules, the Docker base image, and GitHub Actions every Monday. Updates are grouped per ecosystem, producing at most one normal Go PR, one Docker PR, and one GitHub Actions PR per run instead of one PR per dependency.

## Repository layout

| Path | Ownership |
| --- | --- |
| `api/` | Embedded OpenAPI contract served by Swagger UI |
| `cmd/castdock/` | Process entrypoint, dependency wiring, scheduling, and shutdown |
| `internal/podcast/` | Podcast, feed, subscription, and episode domain records |
| `internal/playback/` | Playback-state validation |
| `internal/syndication/` | Safe RSS retrieval, parsing, OPML import, and refresh orchestration |
| `internal/storage/` | SQLite migrations and persistence |
| `internal/server/` | HTTP routes, middleware, API handlers, and template rendering |
| `web/` | Embedded templates and browser assets |
| `migrations/` | Embedded, ordered SQLite migrations |
| `deploy/compose/` | Container image and Compose deployment |
| `.github/` | CI, Docker Hub publishing, and Dependabot configuration |
| `clients/` | iOS, Android, and Linux/macOS CLI clients that consume the HTTP API |

## License

CastDock is open-source software licensed under the [MIT License](LICENSE).

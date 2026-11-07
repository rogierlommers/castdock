# Docker Compose deployment

From the repository root:

```sh
cp .env.example .env
docker compose --env-file .env -f deploy/compose/compose.yaml up --build
```

The service binds to all interfaces and is available locally at `http://localhost:8080`. The default Compose port publication also exposes CastDock to the LAN. This release has no authentication, so restrict the host binding to `127.0.0.1` or protect it with trusted network controls or an authenticated reverse proxy when the host is not private.

Runtime data is bind-mounted from `./data` by default. Override the host path with `CASTDOCK_DATA_DIR`; the application database path inside the container remains controlled by `CASTDOCK_DATABASE_PATH`.

Compose intentionally sets the in-container application values `CASTDOCK_LISTEN_ADDR=:8080` and `CASTDOCK_DATABASE_PATH=/data/castdock.db`; the loopback-only host port and data bind mount provide the deployment boundary. The local-development values for those variables in `.env` are therefore not passed into the container.

## Backup and restore

Stop CastDock before copying the SQLite files so the database, WAL, and shared-memory files remain consistent:

```sh
docker compose --env-file .env -f deploy/compose/compose.yaml down
cp -a data "data.backup-$(date +%Y%m%d-%H%M%S)"
docker compose --env-file .env -f deploy/compose/compose.yaml up -d
```

To restore, stop CastDock, move the current `data` directory aside, copy the selected backup to `data`, and start the service. CastDock does not download media in this release, so the SQLite directory is the complete application state.

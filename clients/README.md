# CastDock clients

This directory groups clients that consume CastDock's HTTP API. Clients must treat [`../api/openapi.yaml`](../api/openapi.yaml) as the transport contract and must not depend on the server's SQLite schema or reimplement server-owned feed, playback, inbox, archive, or queue rules.

- `ios/` — native Apple client
- `android/` — native Android client
- `cli/` — Linux and macOS command-line client

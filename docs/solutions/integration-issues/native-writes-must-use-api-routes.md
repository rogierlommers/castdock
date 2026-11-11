---
title: Native writes must use API routes
date: 2026-11-11
category: integration-issues
module: Native client HTTP integration
problem_type: integration_issue
component: api_layer
symptoms:
  - An authenticated iOS write returns HTTP 403 before its handler runs
  - The same operation succeeds from a same-origin browser form
root_cause: wrong_api
resolution_type: code_fix
severity: medium
tags: [ios, api, csrf, native-client, http-403]
---

# Native writes must use API routes

## Problem

The iOS client attempted to persist playback speed through the browser form
route. The request included `X-CastDock-Client: ios` but no browser Origin
metadata, so write-request protection rejected it with HTTP 403 before the
playback-options handler ran.

## Symptoms

- The speed changed in the active player but failed to persist.
- The server returned `cross-origin request rejected` for the native request.

## What Didn't Work

Calling a web form route from a native client does not make the route a native
API. CastDock intentionally honors the native-client header only under `/api/`;
broadening that exception to all routes would weaken the separation between
browser form writes and native API writes.

## Solution

Provide a dedicated JSON endpoint under `/api/` and keep the browser route
unchanged:

```text
POST /api/feeds/{id}/playback-options
Content-Type: application/json
X-CastDock-Client: ios

{"playbackSpeed": 1.5}
```

The endpoint validates the payload and the existing 0.5–3.0 speed range, uses
the same repository operation as the browser flow, and returns HTTP 204. The
iOS client calls this endpoint instead of `/feeds/{id}/playback-options`.

## Why This Works

The route now matches the middleware's established native-write boundary. It
keeps authentication and input validation in place while avoiding an Origin
requirement that only browsers can satisfy naturally. Browser forms continue
to require exact same-origin evidence.

## Prevention

- Add every native write under `/api/` and document it in `api/openapi.yaml`.
- Test native writes with `X-CastDock-Client` and no Origin header.
- Do not fix native 403 responses by widening the client-header exception to
  non-API routes.

## Related Issues

- `internal/server/server.go`
- `internal/server/server_test.go`
- `clients/ios/CastDock/APIClient.swift`
- `api/openapi.yaml`

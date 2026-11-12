---
title: Bounded client network history for operator diagnostics
date: 2026-11-07
last_updated: 2026-11-12
category: design-patterns
module: HTTP server diagnostics
problem_type: design_pattern
component: observability
severity: low
applies_when:
  - Operators need recent client activity without enabling full access logs
  - High-frequency client writes must remain visible without persistent audit storage
tags: [network-events, diagnostics, bounded-memory, sensitive-data]
---

# Bounded client network history for operator diagnostics

## Context

CastDock clients save playback every five seconds while audio is playing. Operators need to see those saves and other client requests on the System page, but full request capture would create an unbounded data set and could retain credentials, tokens, private URLs, or request bodies.

## Guidance

Capture diagnostic events once in HTTP middleware after the response completes rather than adding event writes to each handler. This records reads, writes, validation failures, and authorization failures consistently and makes the delivered HTTP status available.

Keep the event deliberately smaller than an access log:

- retain only server time, a length-limited client label, method, URL path without its query, response status, and a server-owned description;
- never retain request or response bodies, cookies, authorization headers, remote addresses, user agents, or query strings;
- keep a fixed-size in-memory history and clear it on process restart;
- copy the history under a read lock before rendering so templates do not observe concurrent mutation;
- render through `html/template` so the client-provided label and request path remain escaped.

For live delivery, publish a coalesced non-blocking signal when the store changes
rather than sending event payloads through subscriber channels. Each subscriber
reads a fresh bounded snapshot after receiving the signal, so a slow browser
cannot block request logging or accumulate an unbounded queue. Send an initial
snapshot immediately, keep the Server-Sent Events connection alive through
idle periods, and unsubscribe when the request context is canceled.

Keep the server-rendered list as a no-JavaScript fallback. The browser should
construct refreshed rows with DOM `textContent`, not `innerHTML`, because client
labels and request paths remain untrusted display values. Authentication stays
on the same-origin stream route through the existing session middleware.

Client-request detection should include both API routes and requests carrying the native client header. The latter matters for native health checks outside the `/api/` namespace. Exclude documentation endpoints that happen to live under `/api/` but are not application-client traffic.

## Why This Matters

Handler-level instrumentation is easy to omit when endpoints are added and usually misses requests rejected by middleware. A bounded middleware-level record gives useful troubleshooting evidence without turning the System page into durable audit storage or duplicating the optional access log.

## When to Apply

- Use this pattern for short-lived, operator-facing diagnostics in the single-process deployment.
- Use persistent, structured audit storage instead if events must survive restart, support compliance, or be complete across multiple service replicas.
- Reassess the stored fields before adding any request metadata; operational convenience does not justify retaining secrets or private user data.

## Examples

For `POST /api/episodes/42/playback`, retain a description such as `Playback timestamp saved`, the method and path, client label, HTTP status, and server timestamp. Do not retain the JSON playback payload or any session cookie.

## Related

- `internal/server/network_events.go`
- `internal/server/server.go`
- `web/templates/system.html`
- `web/static/system-events.js`

---
title: SQLite queue compaction can violate uniqueness transiently
date: 2026-11-07
category: database-issues
module: Episode queue persistence
problem_type: database_issue
component: database
symptoms:
  - Marking a queued episode as played fails with a UNIQUE constraint error
  - Queue compaction succeeds for some episode orders but fails for others
root_cause: data_integrity
resolution_type: code_fix
severity: high
tags: [sqlite, queue, unique-index, transactions, row-order]
---

# SQLite queue compaction can violate uniqueness transiently

## Problem

Removing an episode from the ordered queue could fail while decrementing later
positions, leaving the episode unplayed after the transaction rolled back. The
failure depended on the relationship between episode IDs and queue positions,
so small or insertion-ordered test queues did not expose it.

## Symptoms

- `compact played queue: constraint failed: UNIQUE constraint failed: episodes.queue_position (2067)`
- The same compaction statement could also affect lifecycle changes and
  playback-completion transitions.

## What Didn't Work

- A single `UPDATE episodes SET queue_position = queue_position - 1 ...` is not
  safe merely because its final set of values would be unique. SQLite enforces
  the unique index as rows are updated, and row traversal is not guaranteed to
  follow queue order.
- Tests with one remaining queue item performed no multi-row compaction. Tests
  whose row IDs followed queue order also hid the unsafe intermediate state.

## Solution

Move affected positions into a disjoint temporary range before calculating
their final values. CastDock uses negative positions inside the same
transaction:

```go
UPDATE episodes
SET queue_position = -queue_position
WHERE queue_position > ?;

UPDATE episodes
SET queue_position = -queue_position - 1
WHERE queue_position < ?;
```

The second parameter is the negative of the removed position. All queue-removal
paths call the shared `compactQueueAfter` helper in
`internal/storage/store.go`.

## Why This Works

Normal queue positions are positive. Negating every affected position first
makes the temporary values disjoint from both unaffected positions and final
positive positions. The second update can therefore restore each row to its
decremented position without depending on SQLite's row traversal order. Both
steps remain in the caller's transaction, so an error rolls back the archive,
playback, and queue changes together.

## Prevention

- Exercise ordered-table mutations with record IDs deliberately different from
  logical order; do not rely only on insertion-ordered fixtures.
- When changing several uniquely indexed values in SQLite, reserve a disjoint
  temporary value range before assigning final values.
- Keep queue compaction behind one helper so state changes, playback completion,
  and explicit mark-as-played operations cannot diverge.

## Related Issues

- `internal/storage/store.go`
- `internal/storage/store_test.go`
- `migrations/002_episode_lifecycle.sql`

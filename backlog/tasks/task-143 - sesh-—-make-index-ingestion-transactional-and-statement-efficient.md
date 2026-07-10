---
id: TASK-143
title: sesh — make index ingestion transactional and statement-efficient
status: Done
assignee: []
created_date: '2026-07-10 01:39'
updated_date: '2026-07-10 02:02'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 143000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Evidence and full context: backlog doc-001 (sesh shipper and ingest efficiency findings).

A two-second append batch can contain hundreds of JSONL rows. The indexer currently issues per-row dedup queries and inserts plus graph/unification statements without an explicit event transaction, causing repeated statement preparation, WAL commits, and fsyncs. Measured: SQLite commit/statement churn dominates store CPU under batched appends; a rough scratch transaction cut a 320-row batch from ~61–99ms to ~16.6ms median (conservative production target: at least 2x, expected ~4x).

Settled decisions:
- Start with one transaction per append event; only batch several events together if a bounded consumer drain preserves FIFO order and dirty recovery.
- Do not weaken mirror fsync-before-ACK or make indexing synchronous with ACK.
- Keep SQLite and the re-derivable index model.
- Measure transaction and prepared-statement changes separately before adding schema constraints that could interact with logical-session unification.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Each append event is parsed and applied atomically in an explicit SQLite transaction; a failed event leaves dirty_for_reindex set and no partially visible index state
- [x] #2 Dedup and insert work uses prepared or set-based statements and eliminates the separate per-row existence round trip where correctness permits
- [x] #3 Append-event ordering, logical-session unification, overlap dedup, quarantine, complete offsets, and deterministic reindex output remain row-for-row equivalent
- [x] #4 Durable mirror ACK remains independent of indexing; index failure never rolls back or blocks already durable mirror bytes
- [x] #5 A 320-row/eight-event benchmark reduces index CPU/wall time by at least 50% and SQLite commit/fsync count by at least 75% without increasing append-event loss risk
- [x] #6 Existing index, replay, backfill, resume, quarantine, and surface gates remain green
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Fixed on branch sesh-store-efficiency (b22b049, store lane worker, orchestrator-verified). One explicit SQLite transaction per append event spanning complete-offset read, parse/apply, prepared row writes, unification/dedup, offset advance, and dirty clear; rollback + dirty_for_reindex marking outside the failed tx preserves recovery evidence. Per-row existence query replaced with prepared INSERT..SELECT..WHERE NOT EXISTS (same predicate; in-tx visibility preserves batch-internal dedup semantics); quarantine message + ledger inserts prepared and in-tx. No schema constraints added (settled decision honored). Regression test proves zero partial rows + dirty=1 on mid-event failure, then clean retry. Measured: 320-row/8-event batch median 93.1ms -> 22.3ms (76% reduction, 4.17x); traced fsync 407 -> 84 (79.4%). Orchestrator re-ran pinned gate uncached from the lane worktree: all packages + check scripts green; worker also ran go test -race on internal/index green. Mirror fsync-before-ACK and consumer ordering untouched. Merge pending lane review + hera handoff.
<!-- SECTION:NOTES:END -->

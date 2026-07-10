---
id: TASK-143
title: sesh — make index ingestion transactional and statement-efficient
status: To Do
assignee: []
created_date: '2026-07-10 01:39'
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
- [ ] #1 Each append event is parsed and applied atomically in an explicit SQLite transaction; a failed event leaves dirty_for_reindex set and no partially visible index state
- [ ] #2 Dedup and insert work uses prepared or set-based statements and eliminates the separate per-row existence round trip where correctness permits
- [ ] #3 Append-event ordering, logical-session unification, overlap dedup, quarantine, complete offsets, and deterministic reindex output remain row-for-row equivalent
- [ ] #4 Durable mirror ACK remains independent of indexing; index failure never rolls back or blocks already durable mirror bytes
- [ ] #5 A 320-row/eight-event benchmark reduces index CPU/wall time by at least 50% and SQLite commit/fsync count by at least 75% without increasing append-event loss risk
- [ ] #6 Existing index, replay, backfill, resume, quarantine, and surface gates remain green
<!-- AC:END -->

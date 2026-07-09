---
id: TASK-131
title: >-
  sesh — incremental logical-session unification + Sessions query fold
  (run-sesh-107 unit A)
status: To Do
assignee: []
created_date: '2026-07-09 23:12'
labels:
  - run-sesh-107
dependencies: []
priority: high
ordinal: 131000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Parent: TASK-107 (AC#3 + the U7 residual). Lead unit of run-sesh-107.

Problem (field-verified 2026-07-09, see TASK-107 comment): ProcessAppend (tools/sesh/internal/index/index.go:138) runs unifyLogicalSessions after EVERY append PUT that inserts rows. That function loads every message UUID of every file into memory (fileSummaries -> overlapPairs, one query per file), does O(files^2) pairwise overlap comparison, rewrites logical_session_id + file_ordinal across the whole messages table, then runs dedupeAll (windowed DELETE over the entire messages table). The shipper chunks large files, so one big session file triggers this global sweep dozens of times; per-PUT cost grows with total index size. Observed on a single node backfilling one user (343 claude + 378 codex files, 443MB mirror, 39MB index): store pegged at 100% CPU for hours, ingest ~600KB/min, surface pages 20s+.

Scope IN: make the append path incremental — unification, file_ordinal maintenance, and dedup triggered by one append must be scoped to the logical-session group(s) connected to the touched file (its wire session, plus files overlapping it by >=2 message UUIDs, transitively as needed for correctness). Also fold SQLStore.Sessions (tools/sesh/internal/surface/sqlstore.go) per-logical-session maxTimestamp query into a bounded number of queries.

Scope OUT: shipper chunking, wire types (docs/specs/sesh-wire.md is FROZEN), the files registry table, Reindex's right to do global work, TASK-107 AC#1/2/4/5 (separate unit).

Settled decisions — do not re-litigate; if one seems wrong, STOP and report on your unit thread, never substitute your own design:
- Global unify/ordinals/dedupe remain in Reindex; only the per-append path becomes incremental.
- Dedup semantics are ratified: same partition key (tool, logical_session_id, entry_type, message_uuid) and same preference order as dedupeAll today. The incremental path must preserve them exactly.
- The sqlite index tables are disposable (Reindex rebuilds from mirror): adding index-owned columns/indices/caches is allowed IF Reindex rebuilds them from scratch. The files table and wire schema must not change.
- Equivalence is the acceptance bar, not approximate parity: incremental-ingest state must equal post-Reindex state (Indexer.Checksum already exists for this).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Per-append work is scoped: an append to file F touches only F's connected logical-session group; no full-table UPDATE/DELETE and no per-file overlap loading of unrelated files on the append path
- [ ] #2 Equivalence test (new, automated): ingest the fixture corpus incrementally, snapshot Indexer.Checksum, run Reindex on the same store, checksums match
- [ ] #3 Perf evidence: automated benchmark or timed harness showing per-append cost does not grow with the number of unrelated files; before/after numbers reported on the unit thread
- [ ] #4 SQLStore.Sessions no longer issues one maxTimestamp query per logical session
- [ ] #5 All existing gates green uncached: go build ./..., go vet ./..., go test ./..., tests/check-*.sh
<!-- AC:END -->

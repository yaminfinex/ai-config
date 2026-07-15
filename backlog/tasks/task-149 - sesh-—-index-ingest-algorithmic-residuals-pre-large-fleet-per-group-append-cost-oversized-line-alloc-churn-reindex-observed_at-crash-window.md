---
id: TASK-149
title: >-
  sesh — index ingest algorithmic residuals (pre-large-fleet): per-group append
  cost, oversized-line alloc churn, reindex observed_at crash window
status: Done
updated_date: '2026-07-15'
assignee: []
created_date: '2026-07-10 10:13'
labels:
  - sesh
dependencies: []
priority: low
ordinal: 149000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement (small batch). Three surviving residuals from the index-scalability and efficiency work, none urgent at current scale — schedule before onboarding a large fleet. Evidence context: backlog doc-001 and the incremental-unification design in internal/index/index.go.

(1) Per-append cost within one logical group: every append re-runs full-file file_ordinal UPDATEs for each group member plus a windowed dedupe over the group partition even when nothing changed — O(session rows) per shipped chunk. Transactional ingest cut the constant ~4x but the shape remains; consider dirty-tracking within the group or ordinal-stable inserts.

(2) readCompleteLine pre-allocates the full 8MiB max-line cap for any line over 64KiB; peak is bounded but TotalAlloc churns on transcript files with large base64 lines. Size to len+fragment and grow instead.

(3) Reindex is non-transactional across its ledger DELETE + rebuild: a crash in between loses quarantine observed_at history (the snapshot is in-memory only). Persist the snapshot or wrap the ledger swap in its own transaction.

Settled decisions:
- The disposable-index model, deterministic reindex-equivalence bar (Indexer.Checksum), and one-transaction-per-append-event boundary all stay.
- No schema constraints that interact with logical-session unification without separate measurement (same rule as the transactional-ingest work).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Appending to one file of a settled logical group no longer rewrites unchanged group members' ordinals or re-dedupes an unchanged partition (or measured negligible at representative group sizes)
- [ ] #2 Lines over 64KiB allocate proportional to actual line size; TotalAlloc on a large-base64 fixture drops measurably with peak still bounded
- [ ] #3 A crash injected between reindex ledger delete and rebuild loses no observed_at history
- [ ] #4 Reindex-equivalence checksum and full pinned gate remain green
<!-- AC:END -->

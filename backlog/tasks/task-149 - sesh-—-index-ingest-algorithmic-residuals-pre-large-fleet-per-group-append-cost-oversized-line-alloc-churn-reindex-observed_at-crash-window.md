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
- [x] #1 Appending to one file of a settled logical group no longer rewrites unchanged group members' ordinals or re-dedupes an unchanged partition (or measured negligible at representative group sizes)
- [x] #2 Lines over 64KiB allocate proportional to actual line size; TotalAlloc on a large-base64 fixture drops measurably with peak still bounded
- [x] #3 A crash injected between reindex ledger delete and rebuild loses no observed_at history
- [x] #4 Reindex-equivalence checksum and full pinned gate remain green
<!-- AC:END -->

## Evidence (Done, 2026-07-15)

Lane: branch task-149-ingest-residuals (builder-gana, codex
gpt-5.6-sol; sole substance reviewer reviewer-mevu, codex; hera merge
gate). internal/index + tests + design note
(docs/design/2026-07-15-index-ingest-residuals.md), 5 files.

- AC1 measured FIRST, not negligible: settled 10-file/10k-row group,
  one append — main 278-281ms (dedupe examined 10,001 rows despite
  maint_rows=0); fixed 5.8-6.0ms (~50x, reviewer-reproduced 332ms ->
  6.0-6.6ms). Fast path inherits placement, seeks appended keys via
  pinned overlap-index queries; full connected-component path retained
  on cross-logical linkage (reviewer adversarially verified both
  orders). Structural gate pins the targeted plan, rejects
  whole-partition regression.
- AC2: x8 growth cliff found in review (P1: 1MiB+1 regressed to
  9.6MB/op, worse than main) — fixed to x2 bounded growth; final:
  1MiB ~2.03MB/op, 1MiB+1/+64KiB ~4.13MB/op, 8MiB cap ~16.7MB/op
  cumulative, retained capacity <=2x; boundary benchmarks committed.
- AC3: red/green crash injection — old non-transactional DELETE lost
  observed_at (red on main); fixed swap stages in memory and runs
  DELETE+INSERTs in one transaction; injection rolls back preserving
  exact observed_at, retry succeeds. No DDL.
- Review finding 2 (P1): vanished-member rejoin divergence — CONFIRMED
  PRE-EXISTING on main, ruled out of lane; proven NOT widened
  (byte-identical incremental+Reindex checksums vs main behavior, both
  orders); design note documents the hole; filed as TASK-220 (edb6e11).
- TASK-136 equivalence/ordinal properties green; empty-uuid
  non-participation held; maint_rows truthful.
- Verdict PASS #77353; merge 4127430 --no-ff; post-merge house battery
  59/59; pushed. Battery era note: lane gated under Go 1.26.5
  (TASK-217 unification), 59/59 uncached on frozen 0b92f33.
- Deploy: tag sesh-v0.1.14 exact on merge; store live "sesh-v0.1.14";
  release published clean sesh-v0.1.14; this node updated
  v0.1.13-3-gc76d270 -> v0.1.14, shipping healthy (ack 13s), nodes
  page 200/0.36s.

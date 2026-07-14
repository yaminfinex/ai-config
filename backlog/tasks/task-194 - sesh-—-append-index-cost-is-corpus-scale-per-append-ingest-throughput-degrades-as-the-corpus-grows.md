---
id: TASK-194
title: >-
  sesh — append-index cost is corpus-scale per append; ingest throughput
  degrades as the corpus grows
status: Done
assignee:
  - mika
created_date: '2026-07-13 20:22'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 193000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Upstream finding from the store read/write-split work (evidence: tools/sesh/docs/design/2026-07-13-sesh-store-read-write-split.md): each index append pays logical-session maintenance that scales with corpus/session size, not append size — measured at a 1.3GB/~500k-row replay corpus: a 241-byte append holds the write connection ~0.45-0.63s (exclusive phases: dedupe ~0.30s, inherit ~0.07s, unify ~0.07s), ~2.1s on the slower store VM. Two shippers already saturate the single write connection; the read/write split protects readers, but ingest itself will degrade as the fleet corpus grows — at 10x corpus this is seconds per append and shipper backpressure. SESH_DEBUG=1 shows the phase split on the live node.

Candidate angles from the finder: dedupeLogical/inheritFileLogicalSession query plans (write side has no INDEXED BY pins), incremental rather than whole-group dedupe, skip unify when the append introduces no new logical linkage. Constraint: frozen index schema and wire v1; the fix space is algorithmic/incremental, or a sanctioned schema-forward migration proposed via design note first.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Append cost measured and characterized against corpus size on live data (SESH_DEBUG evidence recorded)
- [x] #2 Append-time work made bounded or sub-linear in corpus size without violating frozen wire/index contracts, or a design note proposing the sanctioned migration if impossible
- [x] #3 Regression gate pins the append-cost property to the extent testable
- [x] #4 Docs current per decision-001
<!-- AC:END -->

## Evidence (Done, 2026-07-14)

Merged to main at 96ef5e9 (--no-ff, linear 6e5e0f9 -> de820bc -> 1b592e4
-> 0d46251, 6 files), pushed; deployed live as sesh-v0.1.8.

Fix (inside frozen contracts, no DDL, no new index): INDEXED BY pins on
the three prefix-walk maintenance queries (inherit lookup,
sameLogicalFiles, dedupe window) + no-op-free relabel/file-ordinal
UPDATEs. Fixture (1x/2x/4x = 2.5k/5k/10k sessions): tail append
61/114/222ms corpus-linear -> ~3.5ms flat (~63x at 4x, zero corpus
scaling). Contract as narrowed in review: steady-state maintenance writes
bounded by appended rows; a linkage append additionally rewrites the
touched connected component once (never corpus-scale); maintenance reads
bounded by the touched component.

AC1 live evidence: BEFORE on record in the read/write-split design note
(~2.1s per 241-byte append on the store VM, 2026-07-13, SESH_DEBUG).
AFTER captured live 2026-07-14 on the v0.1.8 store (SESH_DEBUG enabled
via /etc/sesh/serve.env for the capture window, then removed): steady
appends 138-151ms total (parse ~3ms, inherit ~6ms, insert ~1-14ms, unify
~97ms, dedupe ~27ms, commit ~4ms), maint_rows=0 on all captured
non-linkage appends — zero maintenance writes, ~15x live. The residual
unify/dedupe cost is the touched-component READ on a very large live
session, exactly the documented bound. One 1.6s outlier during a load
spike. Journal line verified identifier-free (tool/sizes/timings only).

Review (sole reviewer, full-scope after a scope correction): 5 findings —
4xP1 (universal write-bound claim false for linkage appends -> contract
narrowed; AC1 live-evidence gap -> held open until this capture; wrong
gate denominator (post-dedupe survivors) -> pre-maintenance touched
cardinality; non-discriminating fixture -> bridge-merge M*U shape
asserting corrected bound passes AND old denominator would fail) + 1xP3
(NOT NULL premise stated beside the <> predicates). Reviewer
independently verified: oracle SQL byte-matches pre-change history,
differential mutations trip in every direction, reindex and
restore-then-reindex agree, stamp consumers never depended on no-op
UPDATE side effects, bench shape reproduced. Final verdict READY TO
MERGE; merge-gate battery 58/58 green post-merge.

---
id: TASK-194
title: >-
  sesh — append-index cost is corpus-scale per append; ingest throughput
  degrades as the corpus grows
status: To Do
assignee: []
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
- [ ] #1 Append cost measured and characterized against corpus size on live data (SESH_DEBUG evidence recorded)
- [ ] #2 Append-time work made bounded or sub-linear in corpus size without violating frozen wire/index contracts, or a design note proposing the sanctioned migration if impossible
- [ ] #3 Regression gate pins the append-cost property to the extent testable
- [ ] #4 Docs current per decision-001
<!-- AC:END -->

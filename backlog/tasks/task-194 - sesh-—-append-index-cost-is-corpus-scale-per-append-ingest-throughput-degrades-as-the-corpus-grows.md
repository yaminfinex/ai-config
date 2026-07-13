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
Upstream finding from the store read/write-split work (evidence: tools/sesh/docs/design/2026-07-13-sesh-store-read-write-split.md): each index append pays unify/dedupe/inherit work that scales with the corpus, not the append (~0.5-0.6s per small append at a 1.3GB corpus on the prod VM; dedupe dominates at ~0.3s). The read/write split stopped this from blocking readers, but the write path itself will slow ingest as the fleet corpus grows — at 10x corpus this is seconds per append and shipper backpressure. SESH_DEBUG per-phase timing on the live node now shows the phase split for measurement. Constraint: frozen index schema and wire v1; the fix space is algorithmic/incremental (bounded dedupe windows, incremental unify, precomputed inheritance) or a sanctioned schema-forward migration proposed via design note first.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Append cost measured and characterized against corpus size on live data (SESH_DEBUG evidence recorded)
- [ ] #2 Append-time work made bounded or sub-linear in corpus size without violating frozen wire/index contracts, or a design note proposing the sanctioned migration if impossible
- [ ] #3 Regression gate pins the append-cost property to the extent testable
- [ ] #4 Docs current per decision-001
<!-- AC:END -->

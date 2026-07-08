---
id: TASK-066
title: >-
  namespace_id consumer resolution: seat.namespace stays a raw path until
  consumers resolve ids
status: To Do
assignee:
  - hera
created_date: '2026-07-08 08:48'
updated_date: '2026-07-08 23:42'
labels: []
dependencies: []
ordinal: 66000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
A4 mints namespace records for legacy hcom_dir paths, but seated v2 snapshots RETAIN seat.namespace as a raw path, because command consumers derive HCOM_DIR directly from that field. Making seat.namespace a pure namespace_id everywhere requires teaching legacy-view/command consumers to resolve namespace_id -> namespace path first. Deferred out of A4 deliberately; the A4 adversarial reviewer asked for verification that the interim path/record coexistence cannot disagree. Natural home: wave-B/C consumer work.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
From wave-a4 worker BACKLOG (#9803): A4 mints namespace records for legacy hcom_dir paths, but seated v2 snapshots RETAIN seat.namespace as a raw path because current command consumers derive HCOM_DIR directly from that field. Making seat.namespace a pure namespace_id everywhere requires teaching legacy-view/command consumers to resolve namespace_id -> namespace path first. Deferred out of A4 deliberately; adversarial review (review-a4-mori) asked to verify the interim path/record coexistence cannot disagree. Natural home: wave-B/C consumer work.
<!-- SECTION:NOTES:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Inventory of every consumer deriving HCOM_DIR (or any path) from seat.namespace, with resolution strategy per consumer
- [ ] #2 seat.namespace either becomes a pure namespace_id with consumers resolving via namespace records, or an explicit keep-the-path decision is recorded with rationale
- [ ] #3 The reviewer question is answered with a test: interim path/record coexistence cannot disagree (or the disagreement mode is pinned and refused)
<!-- AC:END -->

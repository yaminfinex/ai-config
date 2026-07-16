---
id: TASK-253
title: Raise wire carries thread-to-task linkage so desk anchors can inherit
status: To Do
assignee: []
created_date: '2026-07-16 00:51'
updated_date: '2026-07-16 02:03'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 252500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
From the ratified synthesis audit (owner via design-seat, mc lane): earlier desk rulings silently assumed the raise wire carries which working thread and board task an ask belongs to; the audit makes it an honest ask. Shape: the raise metadata carries thread and task references so the owner-desk anchor (and the ask entity, see the dyadic-raise task) can inherit linkage without the owner reconstructing context. Rides naturally with the dyadic-raise reopening but is separable.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Raise wire metadata carries working-thread and task references; absent references = absent lines (omission valid)
- [ ] #2 Desk/ask-entity rendering inherits the linkage without manual reconstruction
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
SUBSTRATE LANDED 2026-07-16 (peer inform): mish gained a first-class asks board (mish asks — create/reply/settle/close/withdraw-with-citation/link/widen-membership, JSON-first, mish.ask/v1 frozen, per-mission asks/ dir). Adversarially reviewed and merged on the mission-control branch; shared binary reinstalled with existing surface verified bit-identical. Design doc: missions/2026-07-15-mission-control/artifacts/asks-board-design.md. This lane's design must consume that entity/wire rather than inventing a parallel one; re-validate scope against it at dispatch (worth-it re-check).
<!-- SECTION:NOTES:END -->

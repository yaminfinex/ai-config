---
id: TASK-254
title: >-
  Substrate-side hygiene-failure detection: lost state, unfired continuations,
  wedged sessions as readable signals
status: To Do
assignee: []
created_date: '2026-07-16 00:51'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 253500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
From the ratified synthesis audit (owner via design-seat, mc lane): mission-control renders failures only — DETECTION is substrate-side (herder/hcom lane). Classes named by the audit: lost state, unfired continuations (compact --then failures are currently invisible — see the existing persist-lifecycle task for that specific class), wedged sessions. Shape (design-first): each class gets a checkable signal a renderer can read, not inference from idle time. Overlaps: the compact-then continuation-lifecycle task covers one class; the observer daemon covers liveness. This capture is the umbrella — dedupe at design checkpoint before implementing.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Inventory of hygiene-failure classes with existing-coverage map (dedupe vs continuation-lifecycle + observer scopes)
- [ ] #2 Reviewed design: signal shape per class, readable by renderers, declared/checkable not inferred
<!-- AC:END -->

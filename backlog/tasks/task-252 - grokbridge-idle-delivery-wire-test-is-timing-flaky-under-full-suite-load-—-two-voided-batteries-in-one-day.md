---
id: TASK-252
title: >-
  grokbridge idle-delivery wire test is timing-flaky under full-suite load — two
  voided batteries in one day
status: To Do
assignee: []
created_date: '2026-07-16 00:22'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 251500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The grokbridge mock-wire test asserting idle delivery through tap/fetch/ack fails intermittently ONLY under full-module-suite load (package runtime ~15s), with wake="HCOM_RECOVER pending=1" observed where the test expects the normal delivery wake. Passes 3x consecutively in isolation (-count=3, ~0.01s). Two independent full batteries on the same day were voided by it — orchestrator gate and builder gate, on a diff touching zero grok files — costing a full battery re-run each time (house rule: any battery failure voids the run).

Fix directions: make the wake-source assertion robust to scheduler-delayed recovery nudges (a recovery wake with pending>0 may be a legitimate interleaving, not a delivery failure), or serialize/deflake the timing dependency. Not license to weaken the delivery contract — the test must still fail on real lost delivery.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Root cause identified: why full-suite load produces the recovery wake interleaving
- [ ] #2 Test deterministic under load (repeated full-module runs green) without weakening the lost-delivery detection
<!-- AC:END -->

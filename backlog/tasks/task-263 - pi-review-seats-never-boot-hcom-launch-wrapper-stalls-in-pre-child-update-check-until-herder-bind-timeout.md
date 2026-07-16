---
id: TASK-263
title: >-
  pi review seats never boot: hcom launch wrapper stalls in pre-child update
  check until herder bind timeout
status: To Do
assignee: []
created_date: '2026-07-16 11:35'
labels:
  - herder
  - pi
dependencies: []
priority: medium
ordinal: 262500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Every pi review-seat spawn this run has failed to boot — 0/6 lifetime across four rounds, identical signature each time: `herder spawn --agent pi` times out at the 60s bind predicate (tool=pi, hooks bound, nonempty session UUID) with the diagnosis "the hcom launch wrapper may still be in its pre-child update check (an empty roster confirms that Pi has not connected)"; the pane is cleaned up automatically. The pi calibration protocol (parallel pi seat on every behavior-diff review, same model as the incumbent) has produced zero harness-comparison data because the seat never comes up.

Investigate and fix the boot path: (a) confirm the wrapper's pre-child update check is the actual stall (network reachability to its update endpoint, check duration, whether it blocks child exec); (b) make the check non-blocking or bounded for spawned seats (env/flag to skip or timeout the update check at launch), or extend/condition herder's bind window for the pi agent kind if the check is legitimate but slow; (c) one verified live pi seat boot end-to-end (spawn → bind → brief delivery → a trivial tool action) as acceptance.

Calibration context lives in the run's pi review ledger; this task is only the boot defect, not the calibration protocol itself.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Root cause of the pre-child stall confirmed with evidence (not the diagnosis string alone)
- [ ] #2 Spawned pi seats boot within the bind window (update check skipped, bounded, or window conditioned per agent kind)
- [ ] #3 Live acceptance: one pi seat spawn binds, receives a brief over the bus, and performs a tool action
<!-- AC:END -->

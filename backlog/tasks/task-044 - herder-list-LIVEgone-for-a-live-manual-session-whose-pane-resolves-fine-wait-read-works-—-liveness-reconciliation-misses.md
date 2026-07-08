---
id: TASK-044
title: >-
  herder list: LIVE=gone for a live manual session whose pane resolves fine
  (wait --read works) — liveness reconciliation misses
status: To Do
assignee: []
created_date: '2026-07-08 04:45'
labels: []
dependencies: []
priority: low
ordinal: 44000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Observed (hera restart, 2026-07-08): after a correct enroll (guid 404a13df, label hera, pane w6554208c1918a12-1), herder list shows LIVE=gone even though the session is alive in exactly that pane and herder wait hera --read resolves the pane and reads the screen successfully. The pre-restart @dora row showed the same. So wait's pane resolution and list's liveness reconciliation disagree — list likely matches live herdr agent entries on different keys (hcom name / session id / legacy pane handle) and misses manual/adopted sessions. Cosmetic-plus: orchestrators reading herder list will wrongly triage live agents as dead. Related: TASK-035, TASK-041, and the enroll-stale-bus-name task filed alongside this one.
<!-- SECTION:DESCRIPTION:END -->

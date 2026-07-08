---
id: TASK-044
title: >-
  herder list: LIVE=gone for a live manual session whose pane resolves fine
  (wait --read works) — liveness reconciliation misses
status: To Do
assignee: []
created_date: '2026-07-08 04:45'
updated_date: '2026-07-08 05:13'
labels: []
dependencies: []
priority: low
ordinal: 44000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Observed (hera restart, 2026-07-08): after a correct enroll (guid 404a13df, label hera, pane w6554208c1918a12-1), herder list shows LIVE=gone even though the session is alive in exactly that pane and herder wait hera --read resolves the pane and reads the screen successfully. The pre-restart @dora row showed the same. So wait's pane resolution and list's liveness reconciliation disagree — list likely matches live herdr agent entries on different keys (hcom name / session id / legacy pane handle) and misses manual/adopted sessions. Cosmetic-plus: orchestrators reading herder list will wrongly triage live agents as dead. Related: TASK-035, TASK-041, and the enroll-stale-bus-name task filed alongside this one.
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 05:04
---
vibe (herdr-0.7.3 audit, bus #5629, applied by hera): Possibly subsumed by TASK-046 (status parsing broke fleet-wide post-handoff; this pre-handoff miss may share the matcher, as 046 already notes). Re-test after 046's parser fix lands — folded into TASK-050 (NEW-4).
---

created: 2026-07-08 05:13
---
hera x-ref (vibe #5729): mechanism likely identified via TASK-046 — pre-handoff-process detection loss + coordinate epoch mismatch, not a matcher key herder omits. The 046 tri-state fix ('undetected' vs 'gone') should cover this row's symptom; re-verify under TASK-050 after 046 lands.
---
<!-- COMMENTS:END -->

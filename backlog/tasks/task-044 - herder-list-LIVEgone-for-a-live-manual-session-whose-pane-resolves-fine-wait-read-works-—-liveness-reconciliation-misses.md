---
id: TASK-044
title: >-
  herder list: LIVE=gone for a live manual session whose pane resolves fine
  (wait --read works) — liveness reconciliation misses
status: Done
assignee: []
created_date: '2026-07-08 04:45'
updated_date: '2026-07-08 10:20'
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

created: 2026-07-08 06:31
---
RESOLVED BY TASK-046 (production-verified, vibe #6881): post-merge herder list from main shows true statuses fleet-wide (2 done/3 idle/1 working/1 undetected/26 genuinely gone); the pre-handoff manual-row miss was the process-epoch detection-loss mechanism, now honestly reported as undetected. No separate fix. TASK-050s 044 leg closes with this.
---

created: 2026-07-08 10:20
---
Second repro, post-046 (TASK-050 controlled restart, vibe verification #10964): the replacement hera session (guid bbbc84c2) shows live_status=undetected with seat.terminal_id matching the pane's actual terminal EXACTLY (term_65612408bc9034, vibe diffed herdr pane get vs seat). The 046 tri-state is honest as designed, but vibe isolated the mechanism: herdr pane get reports NO agent field / agent_status unknown for a claude relaunched by shell command in an existing pane — herdr's agent tracker only adopts agents it started via agent start, so herder list's liveness join has no input. Stays Done here; the adoption gap is filed as TASK-070.
---
<!-- COMMENTS:END -->

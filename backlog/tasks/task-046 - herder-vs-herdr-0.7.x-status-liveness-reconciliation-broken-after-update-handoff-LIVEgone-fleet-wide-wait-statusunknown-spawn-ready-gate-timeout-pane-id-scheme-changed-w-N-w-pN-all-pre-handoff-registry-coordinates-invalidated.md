---
id: TASK-046
title: >-
  herder vs herdr 0.7.x: status/liveness reconciliation broken after update
  --handoff (LIVE=gone fleet-wide, wait status=unknown, spawn ready-gate
  timeout); pane-id scheme changed w-N -> w:pN; all pre-handoff registry
  coordinates invalidated
status: To Do
assignee: []
created_date: '2026-07-08 04:56'
updated_date: '2026-07-08 05:08'
labels: []
dependencies: []
priority: high
ordinal: 46000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Live gate results (hera, 2026-07-08, herdr 0.6.10 -> 0.7.3 via update --handoff): (1) WORKS: herder spawn creates panes (new-format pane_id w6554208c1918a12:p4), bash prompt typed+submitted+executed (HANDOFF_GATE_OK), wait --read resolves and reads panes, cull ok. (2) BROKEN: status detection everywhere — herder list shows LIVE=gone for every row including live sessions and a freshly re-enrolled row with new-format coordinates; herder wait times out with status=unknown (never sees idle); spawn's ready-gate reports timeout(status=unknown) even though delivery verifies. Likely herder parses herdr agent-list/status output whose shape changed in 0.7.x. (3) MIGRATION: the handoff issued new terminal ids and a new pane-id scheme (w...-N -> w...:pN), so every pre-handoff registry row has dead coordinates; hera's own row went unresolvable (wait: 'terminal ... not live anywhere') until re-enrolled via the TASK-042 adoption dance. Any still-live pre-handoff spawned agent needs re-enroll or a registry migration pass. Fix directions: (a) update herder's herdr status parsing for 0.7.x (and version-gate it); (b) post-handoff reconcile/migrate command or automatic coordinate refresh from live herdr state; (c) x-ref TASK-044 (liveness miss predates handoff — may share the matcher), TASK-041/035 (coordinate drift family), TASK-045 (duplicate pane-id registration on failed codex binds).
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 05:04
---
vibe (herdr-0.7.3 audit, bus #5629, applied by hera): Upstream substrate for fix direction (a): 0.7.2 added session.snapshot (full client runtime state in ONE socket response — better liveness source than stitching agent-list/status calls), layout.updated + pane.scroll_changed events, and `herdr api schema --json` (bundled JSON Schema of the socket API — contract tests can pin response shapes mechanically and detect upstream drift instead of golden-string parsing). Protocol is now v14. For migration direction (b): note terminal_ids were ALSO reissued at handoff, so a reconcile pass cannot key on terminal_id alone — needs label/cwd/agent-kind matching against live herdr state, or the TASK-042 adoption dance per row. Audit x-ref: vibe, herdr-0.7.3 delta review 2026-07-08.
---

created: 2026-07-08 05:08
---
hera diagnostic datapoint (2026-07-08): status reconciliation under 0.7.3 is NOT uniformly broken — a freshly SPAWNED row (ff71e7f3, new tab, claude) shows LIVE=working correctly, while hera's own re-ENROLLED row (404a13df, new-format coordinates) still shows LIVE=gone. So the matcher works for spawn-minted seat records and misses enroll-minted ones — look for a field spawn records but enroll omits (tab_id? started_by_pane? live-detection key). Narrows the parser-fix search considerably. Also x-ref new TASK-051: fork's native launch path died under 0.7.3 while spawn's survived.
---
<!-- COMMENTS:END -->

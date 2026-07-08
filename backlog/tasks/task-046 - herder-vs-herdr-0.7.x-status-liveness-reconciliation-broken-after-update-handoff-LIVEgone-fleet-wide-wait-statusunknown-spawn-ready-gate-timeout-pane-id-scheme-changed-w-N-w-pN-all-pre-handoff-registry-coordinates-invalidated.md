---
id: TASK-046
title: >-
  herder vs herdr 0.7.x: status/liveness reconciliation broken after update
  --handoff (LIVE=gone fleet-wide, wait status=unknown, spawn ready-gate
  timeout); pane-id scheme changed w-N -> w:pN; all pre-handoff registry
  coordinates invalidated
status: In Progress
assignee:
  - vibe
created_date: '2026-07-08 04:56'
updated_date: '2026-07-08 05:24'
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

created: 2026-07-08 05:13
---
vibe TASK-046 diagnosis (bus #5729, applied by hera) — parse-shape hypothesis FALSIFIED: ParseAgentList's envelope (.result.agents[]/terminal_id/agent_status) matches 0.7.3 exactly; post-handoff spawn-minted rows reconcile fine. Two confirmed mechanisms:

(1) COORDINATE EPOCH INVALIDATION (fix direction b): handoff reissued terminal ids in a NEW SCHEME — old term_+16hex vs new term_+13hex — alongside the pane-id change. Pre-handoff rows are dead-keyed, so reconcile misses agents that are alive and detected in `herdr agent list` (dd58bd50, 275a4ac2, 8beda4c3 live+named, all LIVE=gone). Discovery: 0.7.x agent list exposes a `name` field equal to our undecorated label — ready-made fallback key.

(2) UPSTREAM DETECTION LOSS for pre-handoff PROCESSES: hera's row (404a13df) has CORRECT new-epoch coordinates but the pane shows agent_status=unknown / no agent field and is absent from agent list while demonstrably mid-conversation — pre-handoff processes' hook reports do not reach the new server (same shape as the hcom stale-PATH gotcha 3d71d34; restart is the recovery). Fully explains wait: herder wait delegates to herdr wait agent-status, which can never leave unknown for a detection-lost pane -> timeout(status=unknown). REFINES the earlier spawn-vs-enroll datapoint: the split is PROCESS-EPOCH (post-handoff processes detect; pre-handoff ones do not, however fresh their row).

AGREED FIX (hera ack on the ticket): (a) reconcile fallback chain in list — terminal_id primary, then exact new-format pane_id, then agent-list name==label; emit matched_by. (b) liveness tri-state: agent-list miss + pane-list hit -> live_status 'undetected' (reserved 'gone' for no-pane) — fixes hera's row; likely the TASK-044 mechanism; feeds TASK-050. (c) wait: on timeout with pane present + status unknown, emit detection-lost guidance instead of bare timeout. (d) DECISION (hera): coordinate self-heal is an EXPLICIT `herder reconcile` command, not auto-heal during list — list stays read-only; one auditable migration command; matches herder-spec section 8.3 doctrine (reconciliation is triggered, never assumed) so wave-F subsumes it cleanly. Unambiguous match = name+cwd+agent-kind. (e) upstream gap (server-side re-adoption of surviving processes after update --handoff, not covered by #684) -> logged on TASK-029.
---

created: 2026-07-08 05:18
---
Dispatch state (vibe #5776): claude worker @task046-demo (guid 47f2c45b) spawned into worktree /home/grace/Coding/ai-config-task046, branch task-046-liveness; brief at TASK-046-BRIEF.md (scope a-d, explicit reconcile with dry-run default + --apply, re-confirm/re-bind/unseat vocabulary, refuse-on-ambiguity, never-steal-terminals, hermetic tests). CAVEAT for the merge gate: fixes (a)+(b) (list fallback chain + tri-state) were committed by vibe as WIP BEFORE the no-direct-changes instruction arrived — worker instructed to review-or-improve them; gate + adversarial review must cover that WIP commit like everything else. vibe is review-only from here: worker DONE -> vibe review -> hera gate re-run + adversarial review -> merge.
---

created: 2026-07-08 05:24
---
Policy enforcement (vibe #5926, owner policy: codex implements, opus reviews, Fable never implements): the claude worker 47f2c45b was Fable — culled mid-work; one uncommitted wait.go edit discarded, no commits of its own, branch task-046-liveness still carries only vibe's reviewed WIP. Re-dispatched a CODEX worker into the same worktree with HERDER_SPAWN_BIND_MS=480000 to ride TASK-045 bind latency; spawn in flight. hera gate note: adversarial review on hand-back uses claude opus (--model claude-opus-4-8).
---
<!-- COMMENTS:END -->

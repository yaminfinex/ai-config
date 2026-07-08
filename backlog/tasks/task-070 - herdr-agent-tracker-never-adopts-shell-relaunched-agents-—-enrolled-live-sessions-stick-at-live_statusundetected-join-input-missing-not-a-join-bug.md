---
id: TASK-070
title: >-
  herdr agent tracker never adopts shell-relaunched agents — enrolled live
  sessions stick at live_status=undetected (join input missing, not a join bug)
status: To Do
assignee: []
created_date: '2026-07-08 10:20'
updated_date: '2026-07-08 23:42'
labels: []
dependencies: []
priority: medium
ordinal: 70000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
herdr agent tracker only adopts agents it started: a shell-relaunched agent in an existing pane reports agent_status=unknown with no agent field, so its enrolled registry row shows live_status=undetected forever even when seat.terminal_id exactly matches a live terminal (verified by diff: term_65612408bc9034, row bbbc84c2). Post-046 tri-state is honest, but operators cannot distinguish live-shell-relaunched from dead via herder list.

RE-GROUND FIRST (the seat observer shipped after this was filed — TASK-080, merge 7012f9e): the observer confirms seats from bus evidence and snapshot evidence and its advice now annotates herder list. Verify against a live shell-relaunched session (the orchestrator pane is a standing specimen) whether observer confirmation already closes this gap in practice — note the observer itself cannot see herdr agent-tracker status for unadopted agents either, since herdr never detects them. NOTE: do this re-ground after TASK-081 lands (observer snapshot parsing is broken until then; herdr-side evidence is currently empty).

Remaining candidate fixes if the gap survives re-grounding: (a) upstream — herdr agent adopt / tracker pickup for foreground agents in existing panes; ledger entry on TASK-029 at closeout per protocol; (b) herder-side — list upgrades undetected to a distinct "seated (unverified live)" presentation when seat.terminal_id matches a live pane exactly, or observer confirmation timestamps surface in list. Live test subjects preserved in comments (row 275a4ac2: live agent, row regressed to unseated).
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 12:41
---
Fresh data point (TASK-072 bulk-retire sweep, 2026-07-08): row 275a4ac2 (comments-ux, quick-sites lane) shows latest state=unseated while the agent is LIVE (live_status=idle, pane w655a9196cb2ef2a:p1) — a live agent whose row regressed to unseated, the inverse presentation of this task's original shape but the same observer-gap class: nothing re-seats or corrects rows for sessions herder did not spawn. Excluded from the bulk-retire on live-status grounds. TASK-073 (universal seat observer) is the structural fix; this row is a good live test subject when it dispatches.
---
<!-- COMMENTS:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Re-grounded post-observer (and post-TASK-081): live shell-relaunched session checked against observer sweep + herder list; verdict recorded — gap closed / narrowed / unchanged
- [ ] #2 If a gap remains: herder-side presentation fix implemented (undetected-with-matching-live-terminal rendered distinctly), or an explicit do-not-build verdict with reasons
- [ ] #3 Upstream candidate (herdr tracker adoption of foreground agents) appended to the TASK-029 ledger with evidence
<!-- AC:END -->

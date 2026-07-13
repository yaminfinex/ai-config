---
id: TASK-070
title: >-
  herdr agent tracker never adopts shell-relaunched agents — enrolled live
  sessions stick at live_status=undetected (join input missing, not a join bug)
status: To Do
assignee: []
created_date: '2026-07-08 10:20'
updated_date: '2026-07-13 01:05'
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
RE-GROUND COMPLETE (2026-07-12, hera, read-only): gap SURVIVES the observer. Live specimen: the orchestrator row (shell-relaunched claude, enrolled not spawned) is observer-CONFIRMED with a fresh sweep timestamp in observer.status.json, yet herder list shows LIVE=unknown and has NO advice/confirmation surface (checked --help and output — zero annotation). The 070 description said "observer advice now annotates herder list" — it does not, or not for this class. VERDICT: fix direction (b) — surface observer confirmations in herder list (distinct presentation for observer-confirmed-but-tracker-undetected), fold into the identity-cluster unit. Upstream candidate (a) (herdr agent adopt / tracker pickup of foreground agents) goes to the TASK-029 ledger at cluster closeout.

A1 merge (a1c5acd) note: rows now carry hcom_verified (additive *bool) and all carry paths re-verify or mark — the presentation gap this task tracks (observer-confirmed liveness not surfaced in herder list, no advice surface) REMAINS open; A1 shipped the identity-integrity substrate, not the list/advice presentation. herdr-adopt upstream candidate still queued for the TASK-029 ledger.

2026-07-13 staleness audit: AMEND — split scopes. Upstream tracker-adoption ask stays on the TASK-029 ledger (candidate recorded). LOCAL residue is real and stays here: observer records per-guid Confirmed timestamps (observerstatus/status.go:12-24; observercmd/observer.go:218-221) but list renders only Flags, never Confirmed (listcmd/list.go:483-512, hera spot-verified) and unmatched live panes stay undetected (378-386). Rescope this task to: surface observer confirmation in list output.
<!-- SECTION:NOTES:END -->

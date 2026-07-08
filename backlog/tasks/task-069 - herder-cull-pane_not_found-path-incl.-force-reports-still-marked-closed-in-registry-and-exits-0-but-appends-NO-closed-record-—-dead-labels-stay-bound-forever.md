---
id: TASK-069
title: >-
  herder cull: pane_not_found path (incl. --force) reports 'still marked closed
  in registry' and exits 0 but appends NO closed record — dead labels stay bound
  forever
status: Done
assignee:
  - '@hera'
created_date: '2026-07-08 10:19'
updated_date: '2026-07-08 11:30'
labels: []
dependencies: []
priority: high
ordinal: 69000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Live hit (TASK-050 controlled restart, 2026-07-08): guid 404a13df (label hera, state=unseated, live_status=gone, seat=null — a dead migrated_v1 row with no pane) could not be closed. Both `herder cull --guid 404a13df...` and the same with --force printed: cull errored hera (404a13df-...) pane= -> pane_not_found (still marked closed in registry) and exited 0 — but herder list --all --json shows the guid's latest record is STILL event=migrated_v1/state=unseated; no closed record was appended. The message and exit code are lies on this path. --force's documented purpose is exactly this case (skip terminal_id verification, just mark the registry row closed) and it does not do it. Consequence chain: enroll label-uniqueness treats the dead row as active -> rename refuses for the same reason -> retire does not exist -> cull is the only escape hatch and it silently no-ops => a dead agent's label is permanently unreclaimable (the standing orchestrator now runs as hera-restart-050b because label hera is stuck on the corpse). Fix: the pane_not_found/error branch must still append the closed record (at minimum under --force), and the label-uniqueness predicate should not count unseated+gone rows as active holders. Blocks the TASK-042 composite. Evidence: napkins/run-herder-dx/restart-050-brief.md post-restart appendix.
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 10:55
---
Dispatched: codex worker task069-66b53edf (bus @task069-melo), worktree task-069-cull-closed-record off main 3a92aaa (branch base includes 067 merge). Brief: napkins/run-herder-dx/brief-069.md — scope: confirmed-write closed record on pane-less cull paths, honest nonzero exit on write failure, label freed once closed (minimal uniqueness-predicate fix in scope; unseated-not-closed stays on TASK-042). Orchestrator performs live 404a13df reclamation as post-merge validation. Opus adversarial review to follow DONE.
---

created: 2026-07-08 11:17
---
Round 1: worker DONE 2a0adbf; my gate green 28/28 from worker worktree. Opus review (review069-sumi #11628): REQUEST-CHANGES — P1 CONFIRMED on brief attack-angle-1: appendClosed wrote state=retired on EVERY cull path (incl normal live-seated cull) = wave-C retire semantic smuggled under cull; spec is cull->unseated/resumable (:67,:119-121,:397), AC-11 latent breakage once resume enforces refuses-retired, default-list disappearance. P2: check-cull-busdrop.sh contract assertion flipped to bless the violation. LOW: pane-less no-force path can bus-drop a live-but-undetected agent. Verified good: exit-code honesty, confirmed-write guard, snapshot carry-forward, real-trap seeding in the reclaim check. Fix round dispatched (#11639): revert to unseated close records keeping all confirmed-write machinery, restore busdrop contract, replace reclaim check with spec-truth check (label STAYS held post-cull until retire ships), guard the bus-drop. Spec ruling requested from spec-ravu (#11640): pull retire forward (rec A) vs cull --retire variant (rec against); note systemic-review memo finding 3 (enforcement ahead of escape verbs) recommends shipping retire + --take-from as one composite unit. NOTE: systemic reviewer and opus reviewer DIVERGED on retired-on-forced-cull — resolved in favor of spec text pending ruling.
---

created: 2026-07-08 11:30
---
Round 2: fix c975c9c — my regate green 28/28; opus delta APPROVE (#11825: P1 unseated restored on all paths with confirmed-write machinery kept, label correctly stays held per AC-18; busdrop contract restored; bus-drop guard verified against live hcom list exit contract, no --gone strand; both flagged attacks verified not landing). Merged main 38bf281 (no-ff); post-merge gate on main from repo root: herder+bottle OK, 28/28 suites. OPEN RESIDUAL (P2 non-blocking, review-recommended): repeat cull of already-unseated row appends a row per call (§5.2 idempotency deviation, --gone sweep churn until rotation) — confirmed-no-op shape proposed to spec-ravu (#11836); follow-up task on concurrence. Worker task069-66b53edf + reviewer review069-d0b1383e culled; worktree/branch removed. NOTE: 404a13df label reclamation deferred to TASK-071 (C0) post-merge validation per ruling.
---
<!-- COMMENTS:END -->

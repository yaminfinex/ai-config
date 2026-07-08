---
id: TASK-069
title: >-
  herder cull: pane_not_found path (incl. --force) reports 'still marked closed
  in registry' and exits 0 but appends NO closed record — dead labels stay bound
  forever
status: In Progress
assignee:
  - '@hera'
created_date: '2026-07-08 10:19'
updated_date: '2026-07-08 10:55'
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
<!-- COMMENTS:END -->

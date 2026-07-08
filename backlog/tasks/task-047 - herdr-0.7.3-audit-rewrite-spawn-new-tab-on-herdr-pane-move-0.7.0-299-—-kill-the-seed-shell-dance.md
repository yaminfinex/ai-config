---
id: TASK-047
title: >-
  herdr-0.7.3 audit: rewrite spawn --new-tab on herdr pane move (0.7.0 #299) —
  kill the seed-shell dance
status: To Do
assignee: []
created_date: '2026-07-08 05:04'
updated_date: '2026-07-08 05:08'
labels: []
dependencies: []
priority: medium
ordinal: 47000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Filed by hera on behalf of vibe (parallel orchestrator, herdr-0.7.3 upgrade audit, bus #5629) — applied verbatim per single-writer protocol.

pane move relocates a running pane into another tab/new tab/new workspace without restarting its process (verified in 0.7.3: `herdr pane move <pane> --new-tab [--workspace] [--label]`). Replaces the current --new-tab choreography (tab create -> seed shell -> agent start -> pane get verify -> close seed) documented in docs/spawn-patterns.md:79-83, removing the close-wrong-pane hazard and the agent+spare-shell failure shape. Plan: spawn via the normal split path in the current tab, then pane move --new-tab; keep label handling. NOTE (per hera/lale isolation on TASK-045): this does NOT fix TASK-045 — the live codex defect is shim-path bind latency, independent of tab mechanics. This is pane-hygiene + simplification; x-ref TASK-036 (codex bind window) only insofar as fewer moving parts during boot. Blocker: land after TASK-046 (status parsing) so ready-gate verification is trustworthy when testing the rewrite.
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 05:08
---
vibe (herdr-0.7.3 audit, bus #5689, applied by hera): pane move verified live (--new-workspace): running codex TUI survived relocation intact. CRITICAL nuance for the rewrite: pane_id/tab_id/workspace_id are REASSIGNED on move (w6554208c1918a12:pA -> w2:p1) — 'stable ids' means never-recycled, NOT immutable across moves; terminal_id is what persists. So the --new-tab rewrite must refresh registry coordinates after the move (or lean on terminal_id resolution, which worked: cull retargeted a post-move stale row correctly). Also observed: pane ordinals are hex (...p9->pA). Bonus: culling the workspace's last pane cleans up the whole workspace — no seed-shell-style residue. (Probe policy per bigboss: future probes go in a separate workspace — pane move --new-workspace used for exactly that.)
---
<!-- COMMENTS:END -->

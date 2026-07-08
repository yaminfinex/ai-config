---
id: TASK-047
title: >-
  herdr-0.7.3 audit: rewrite spawn --new-tab on herdr pane move (0.7.0 #299) —
  kill the seed-shell dance
status: Done
assignee: []
created_date: '2026-07-08 05:04'
updated_date: '2026-07-08 09:21'
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

created: 2026-07-08 05:20
---
spec-ravu independent confirmation (#5816): herdr pane move --new-tab --workspace live-moved their own RUNNING claude pane (w6554208c1918a12:pC -> w3:p2); process and terminal_id survived, pane_id RE-KEYS across workspace moves too. Registry rows need re-enroll (or the TASK-046 reconcile) after any move — the rewrite must do the coordinate refresh unconditionally.
---

created: 2026-07-08 05:22
---
Refinement (spec-ravu #5865): pane_id re-keying on move is CROSS-WORKSPACE only — same-workspace --new-tab moves keep pane_id unchanged (verified live: two panes moved to new tabs within w3, both pane_ids survived; earlier re-key was the w6554208c1918a12 -> w3 crossing). terminal_id persists in both cases. Consequence for the rewrite: same-workspace tab moves need NO coordinate refresh; cross-workspace moves need re-enroll or herder reconcile.
---

created: 2026-07-08 09:09
---
[hera 2026-07-08] Vibe hand-back (#10058): worker task047-zamo, 1 commit 2aaad7a, fence held (spawncmd+herdrcli+docs/goldens/mocks), vibe live-validated (real spawn moved, coordinates re-fetched, cull clean, no seed shell). HERA GATE GREEN from worktree: vet/test both modules (spawncmd+herdrcli -count=1 fresh), 23/23 suites. Adversarial review dispatched: review-047-bozo (guid 5ad8a38c, own tab), brief napkins/run-herder-dx/brief-review-047.md — angles: move-before-gates cached-state audit, re-fetch failure interleavings, --new-tab --workspace combo, fail-soft JSON encoding, worktree seed-close guard, 052 label-shape consumers, dead code, pane hygiene on move_failed. NOTE: A4 merged after this gate — 047 is now second lander; integration (merge main in-branch) routes with any fix round after the verdict.
---

created: 2026-07-08 09:14
---
[hera 2026-07-08] MERGED to main (78edf09, no-ff) after review-047-bozo CLEAN verdict (#10145: all 8 angles probed — launchPaneID pre-move capture is CORRECT by design (matches child launch_context), post-refetch paneID feeds reads, no stale tab-scoped state in cull/send/wait, JSON-escaped fail-soft reason, new-tab can never reach the seed-close block, 052 label shape has no dependent consumers, dead code gone, move_failed leaves a labelled cullable pane). Second-lander integration auto-merged clean; post-merge gate on main GREEN (fresh -count=1, 24/24 suites). Reviewer static-only — execution covered by hera gate + vibe live validation. Residuals filed to TASK-051 polish bucket: LOW latent (re-fetch uses pre-move pane id with no terminal_id fallback — safe while new-tab moves stay same-workspace, breaks if doctrine ever changes), NIT compactMessage passes non-whitespace control/ANSI bytes to the human stderr line, NIT dead write opts.Tab. Credits: task047-zamo (2aaad7a), vibe (dispatch, review, live validation), review-047-bozo. TASK-052 fold delivered: --label-prefix replaces role prefix.
---

created: 2026-07-08 09:21
---
[hera 2026-07-08] PRODUCTION-VERIFIED: post-merge spawns now report 'agent pane moved, no seed shell' in the new-tab line (first seen on review-a5 spawn) — the rewrite is live in the orchestration loop.
---
<!-- COMMENTS:END -->

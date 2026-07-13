---
id: TASK-175
title: >-
  statusline: identity segment + bus snapshot survive hcom rename (launch-frozen
  HCOM_INSTANCE_NAME)
status: In Progress
assignee: []
created_date: '2026-07-13 00:51'
updated_date: '2026-07-13 01:31'
labels: []
dependencies: []
priority: low
ordinal: 174000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Observed live on the orchestrator session: pane statusline shows @mono while the live registry label and bus binding are hera. Root cause: ~/.claude/statusline.sh renders the @name segment from launch-frozen HCOM_INSTANCE_NAME/HCOM_NAME env (deliberately env-only, no live call) — after an identity repair/rename the env diverges from the live bus name forever (same launch-frozen-vs-live class as the TASK-029 candidate-13 pane_id entry). Functional side effect beyond cosmetics: the renderer computes its bus-snapshot state file from the frozen name (statusline/mono.env) while the herder sidecar writes under the live name (statusline/hera.env), so the unread/last-activity segment silently never renders and the renderer's context snapshot writes go to a file nothing reads. Fix directions to evaluate: key the sidecar snapshot by GUID (stable) rather than bus name, and/or have the renderer fall back to resolving via HERDER_GUID when its env-derived state file does not exist (one cheap stat, still no live call on the hot path).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Snapshot file keyed by a rename-stable key (session GUID) written by the sidecar; renderer locates it via HERDER_GUID env — after an hcom rename the bus segment (unread/last) keeps rendering, proven by a test that renames mid-flight
- [ ] #2 Renderer @name shows the LIVE bus name when it diverges from launch-frozen env, with NO new live process call on the render hot path (live name carried inside the snapshot file is the suggested mechanism)
- [ ] #3 Sessions without HERDER_GUID (manual/non-herder) keep current behavior; no orphaned snapshot files accumulate from the migration
- [ ] #4 Full pinned battery green from the worktree; statusline.sh passes bash -n; snapshot writer changes covered by unit tests
- [ ] #5 AMENDED (round 1): snapshot keyed by the stable per-session PROCESS ID — renderer via its own HCOM_PROCESS_ID env with name-keyed fallback when absent or file missing; sidecar via the correlated row's launch_context.process_id through the vetted correlator (paneCorrelated required) — covering hcom-launched AND herder-spawned sessions; the motivating renamed live session renders @<live name> with an intact bus segment post-merge
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
2026-07-13: DONE received (4d411df) — GUID-keyed snapshots, HCOM_LIVE_NAME field for @name, one-time name-file cleanup on stable write, fallback preserved; worker battery 53/53. Harvest: independent gate running; opus reviewer dispatched (lenses incl. deletion-safety race on recycled names).

2026-07-13 review round 1 (nave, opus; 6 executable adversarial proofs, all read-only): 2 P1 — fix missed the motivating session (hcom-launched, no HERDER_GUID; remedy = process-id keying, AC1 amended accordingly); GUID-keyed segment silently vanishes whenever the keyed file is absent (release path deletes on 5 transient list misses — single-writer SPOF replaced the old redundant fan-out). 2 P2 — unscoped delete-by-name (recycled-name live-session deletion horn + orphan-never-cleaned horn; shipped test injected a mock-only env var that hid both); correlation re-implemented instead of reusing the vetted correlator (fork guard + pane-first precedence dropped). 1 P3 — permanent create/delete churn vs legacy sidecars. Passing lenses: hot path zero-spawn, fallback byte-identical, GUID rename tracking, hygiene. Fix round 1 sent to raza; nave holds for delta.

2026-07-13 fix round 1 delta (raza, 3af1471): renderer keys by HCOM_PROCESS_ID w/ name-keyed fallback; sidecar keys ONLY by correlated-row launch_context.process_id through findRowCorrelated (correlated required, fork exclusion + pane-first pinned); transient missing>=5 preserves snapshots (release(false)) vs genuine ppid death removes writer-owned file only; one-shot transition cleanup w/ roster-ownership refusal; live-env-shaped tests incl. recycled-name + post-rename orphan scenarios; churn pinned one-shot. Worker battery 53/53. Independent re-gate running; nave delta requested.

2026-07-13 delta (nave): P1-1/P1-2/P2-4/P3-5 VERIFIED FIXED (side-by-side render proof on the motivating hcom-launched shape: @mono/wrong-file on main -> @hera/correlated-file on fix; fallback retains segment). Residual P2: cleanup keyed by row.Name but legacy writer keys by BaseName -> no-op for every tagged session, orphan reachable via boot-window fallback, one-shot burned on the no-op; hand-crafted fixture (not writeRows) hid it. Residual P3: correlation derived twice per tick (double /proc scan when uncorrelated). Fix round 2 to raza.
<!-- SECTION:NOTES:END -->

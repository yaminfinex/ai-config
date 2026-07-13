---
id: TASK-175
title: >-
  statusline: identity segment + bus snapshot survive hcom rename (launch-frozen
  HCOM_INSTANCE_NAME)
status: In Progress
assignee: []
created_date: '2026-07-13 00:51'
updated_date: '2026-07-13 00:57'
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
<!-- AC:END -->

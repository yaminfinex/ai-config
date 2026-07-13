---
id: TASK-175
title: >-
  statusline: identity segment + bus snapshot survive hcom rename (launch-frozen
  HCOM_INSTANCE_NAME)
status: To Do
assignee: []
created_date: '2026-07-13 00:51'
labels: []
dependencies: []
priority: low
ordinal: 174000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Observed live on the orchestrator session: pane statusline shows @mono while the live registry label and bus binding are hera. Root cause: ~/.claude/statusline.sh renders the @name segment from launch-frozen HCOM_INSTANCE_NAME/HCOM_NAME env (deliberately env-only, no live call) — after an identity repair/rename the env diverges from the live bus name forever (same launch-frozen-vs-live class as the TASK-029 candidate-13 pane_id entry). Functional side effect beyond cosmetics: the renderer computes its bus-snapshot state file from the frozen name (statusline/mono.env) while the herder sidecar writes under the live name (statusline/hera.env), so the unread/last-activity segment silently never renders and the renderer's context snapshot writes go to a file nothing reads. Fix directions to evaluate: key the sidecar snapshot by GUID (stable) rather than bus name, and/or have the renderer fall back to resolving via HERDER_GUID when its env-derived state file does not exist (one cheap stat, still no live call on the hot path).
<!-- SECTION:DESCRIPTION:END -->

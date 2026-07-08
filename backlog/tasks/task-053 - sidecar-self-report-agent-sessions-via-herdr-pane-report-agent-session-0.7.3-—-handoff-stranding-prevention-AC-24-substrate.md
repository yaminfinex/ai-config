---
id: TASK-053
title: >-
  sidecar: self-report agent sessions via herdr pane report-agent-session
  (0.7.3) — handoff-stranding prevention + AC-24 substrate
status: To Do
assignee: []
created_date: '2026-07-08 05:25'
labels: []
dependencies: []
priority: high
ordinal: 53000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Filed by hera on behalf of vibe + spec-ravu coordination (bus #5982; ravu memo: napkins/herder-spec/memo-sid-exposure.md in the herder-spec worktree).

Sidecar self-reports agent sessions via `herdr pane report-agent-session` (new 0.7.3 CLI). sidecar.go:438 already holds the SessionID from hcom list but never tells herdr; ONE extra call makes herder self-sufficient (no third-party integration installs needed for sid exposure), and reported sids ride PaneAgentSessionSnapshot in the HandoffManifest — meaning the NEXT `update --handoff` stops stranding registry rows entirely. This is the PREVENTION half of the handoff problem; `herder reconcile` (TASK-046, in flight) is the one-time migration half. It also makes the herder-spec AC-24 probe substrate real (per-pane sid exposure becomes herder-fed rather than assumed — spec amendment in flight on the herder-spec branch). Ordering note for TASK-046: sid-based matching stays OUT of the current reconcile fallback ladder because sids are empty by default until this lands. Implementation: codex worker per owner model policy. x-ref TASK-046, herder-spec AC-24.
<!-- SECTION:DESCRIPTION:END -->

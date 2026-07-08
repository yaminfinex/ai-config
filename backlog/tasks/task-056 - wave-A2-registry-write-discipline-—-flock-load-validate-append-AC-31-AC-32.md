---
id: TASK-056
title: >-
  wave A2: registry write discipline — flock + load-validate-append (AC-31,
  AC-32)
status: To Do
assignee: []
created_date: '2026-07-08 05:55'
labels: []
dependencies: []
priority: high
ordinal: 56000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Plan unit A2 (spec-plan-wave-a.md). Single write path: exclusive flock on live file; load -> validate (label uniqueness over full projection; owned-fields-only appends; idempotent no-ops) -> append -> fsync. Refuse-unlocked where flock unavailable. Reroute every existing writer (spawn/enroll/cull/sidecar) through it — mechanical, behaviour frozen by A1 projection tests. Tests: concurrent label claims (two processes one winner), stale-enrichment-cannot-revert-rename, heartbeat-cannot-resurrect-culled. Depends: A1 (TASK-055). Adversarial review mandatory (locking = engine risk).
<!-- SECTION:DESCRIPTION:END -->

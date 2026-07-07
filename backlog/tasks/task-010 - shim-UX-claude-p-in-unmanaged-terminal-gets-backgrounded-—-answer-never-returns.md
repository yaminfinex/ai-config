---
id: TASK-010
title: >-
  shim UX: claude -p in unmanaged terminal gets backgrounded — answer never
  returns
status: To Do
assignee: []
created_date: '2026-07-07 06:33'
labels: []
dependencies: []
priority: medium
ordinal: 10000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-001 finding: with machine-wide shims, hand-run 'claude -p ...' in an unmanaged terminal is routed through herder launch -> hcom, which backgrounds the session; the -p answer never returns to the caller. Decide intended UX: bypass the shim for -p/non-interactive invocations, or make hcom stream the result back.
<!-- SECTION:DESCRIPTION:END -->

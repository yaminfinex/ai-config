---
id: TASK-043
title: >-
  herder enroll: records hcom bus name from stale HCOM_INSTANCE_NAME env instead
  of live hcom identity — silently misroutes send after hcom start --as
status: To Do
assignee: []
created_date: '2026-07-08 04:45'
labels: []
dependencies: []
priority: medium
ordinal: 43000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Live hit (hera restart, 2026-07-08): after reclaiming the bus name with hcom start --as hera, herder enroll wrote the new registry row with hcom_name=dora — it trusts the HCOM_INSTANCE_NAME env var, which is frozen at process launch and goes stale the moment the session reclaims a different identity. Consequence: herder send to that row would target a stopped bus name (@dora) and fail or misroute, silently. Workaround used: re-enroll with HCOM_INSTANCE_NAME=hera overridden on the command line. Fix: enroll (and any row-writing path) should resolve the LIVE bus identity from hcom (e.g. hcom list --json for the current session/process id) and prefer it over env, or at least cross-check env vs live and warn on mismatch. Same disease class as TASK-035/041: registry trusting launch-time coordinates that drift.
<!-- SECTION:DESCRIPTION:END -->

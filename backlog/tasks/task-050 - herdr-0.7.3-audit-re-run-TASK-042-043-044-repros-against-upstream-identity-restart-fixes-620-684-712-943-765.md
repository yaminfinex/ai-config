---
id: TASK-050
title: >-
  herdr-0.7.3 audit: re-run TASK-042/043/044 repros against upstream
  identity/restart fixes (#620/#684/#712/#943/#765)
status: To Do
assignee: []
created_date: '2026-07-08 05:04'
labels: []
dependencies: []
priority: high
ordinal: 50000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Filed by hera on behalf of vibe (herdr-0.7.3 upgrade audit, bus #5629) — applied verbatim per single-writer protocol.

0.7.x shipped exactly-on-target fixes: Claude Code session restore now accepts real /clear, /resume, and compacted identity changes (#620); hook sequence guards re-anchor after fresh session refs / proven process exits (#684); root-agent restore ignores child-process session overwrites (#712); foreground session reports can replace stale saved references (#943); official hook integrations scope lifecycle reports to the intended root process (#765). Re-run each hera-restart repro on 0.7.3 BEFORE building local machinery: TASK-042 (identity adoption) may shrink to a registry-side affordance; TASK-043 (stale HCOM_INSTANCE_NAME env) is hcom-side and likely UNAFFECTED — re-verify only; TASK-044 (list LIVE=gone for live manual session) is possibly subsumed by TASK-046's parser fix — re-test after 046 lands. Outcome per task: close, re-scope, or confirm-still-broken with fresh evidence. Blocker: TASK-046 for the 044 leg only.
<!-- SECTION:DESCRIPTION:END -->

---
id: TASK-042
title: >-
  herder: no first-class identity-adoption path for a restarted session in the
  same pane (adopt/reenroll-as)
status: To Do
assignee: []
created_date: '2026-07-08 04:45'
labels: []
dependencies: []
priority: medium
ordinal: 42000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Live hit (hera restart, 2026-07-08): the orchestrator session died (post-upgrade dead hooks) and a fresh claude session started in the SAME pane under a new bus name (dora). Assuming the retired identity took a three-step off-label dance: (1) hcom start --as hera to reclaim the bus name (only hcom can do this; herder rename only relabels a row + pane title, and cannot re-point a dead row at a live session); (2) herder enroll with manually-set HERDER_GUID=<old full guid> HERDER_LABEL/HERDER_ROLE env to adopt the old guid — the env-input path is designed for spawn bootstraps, not interactive adoption, and requires digging the full guid out of herder list --all --json (JSONL); (3) a SECOND enroll to fix the bus coordinate (see companion task on stale HCOM_INSTANCE_NAME). Proposal: herder adopt <target> — run from inside a pane; reclaims the target guid+label+role for the current pane/session, reclaims the hcom bus name (hcom start --as) or verifies it matches, retires stale rows via the TASK-035 guards, appends the row with LIVE coordinates. Retire-on-reenroll itself worked perfectly in production (retired @dora/@vore/@zero rows in one shot).
<!-- SECTION:DESCRIPTION:END -->

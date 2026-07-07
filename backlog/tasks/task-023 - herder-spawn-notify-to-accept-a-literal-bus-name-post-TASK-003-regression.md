---
id: TASK-023
title: 'herder spawn --notify-to: accept a literal bus name (post-TASK-003 regression)'
status: In Progress
assignee:
  - unit-h-risa
created_date: '2026-07-07 08:31'
updated_date: '2026-07-07 08:38'
labels:
  - run-herder-dx
dependencies: []
priority: high
ordinal: 23000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Found live by orchestrator dispatching wave 3: herder spawn --notify-to hera now hard-errors ('spawner does not resolve to a bus-bound agent... tried --notify-to "hera"') — the post-TASK-003 resolution treats --notify-to purely as a registry hint (guid/label/terminal/pane -> row -> hcom_name) and never considers that the value may BE a bus name. Also visible: spawner detection from a Claude Code Bash-tool env yields guid 'user', raw pane p_744, empty terminal — so self-resolution cannot rescue it. Fix: resolve --notify-to against registry hcom_name (active rows) and/or fall back to literal bus name validated against hcom list, consistent with send's HERDER_BUS=hcom literal affordance. Keep the bus-less hard error for genuinely unresolvable targets.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 spawn --notify-to <bus-name> works from a non-bus-env shell (live smoke: notify lands as hcom message)
- [ ] #2 unresolvable --notify-to still hard-errors before pane creation; suite golden updated
<!-- AC:END -->

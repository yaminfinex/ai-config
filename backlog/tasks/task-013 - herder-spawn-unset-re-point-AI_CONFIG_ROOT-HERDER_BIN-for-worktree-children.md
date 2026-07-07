---
id: TASK-013
title: 'herder spawn: unset/re-point AI_CONFIG_ROOT + HERDER_BIN for worktree children'
status: Done
assignee: []
created_date: '2026-07-07 06:40'
updated_date: '2026-07-07 07:40'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 13000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-002 finding (run-herder-bootstrap): children spawned --cwd into a worktree inherit the spawner's AI_CONFIG_ROOT and HERDER_BIN pointing at the MAIN checkout, so wrappers/tests silently build and exercise the wrong tree (bit the check-hook-bootstrap suite live). spawn should re-point these at the child cwd's checkout or unset them.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit 353325b (unit-b-spawn-hygiene, merged d424cab). Children spawned --cwd into a DIFFERENT ai-config checkout get AI_CONFIG_ROOT + HERDER_BIN re-pointed at that checkout (inherited env was beating the wrappers' own location -> silent wrong-tree builds; bit check-hook-bootstrap and the orchestrator's own verification). Launch itself stays on the spawner's proven-buildable bin/herder. Sliding door: outside any checkout, inherited values left alone (unset would break legit out-of-tree use). Suite-side sweep for remaining check scripts → TASK-019.
<!-- SECTION:NOTES:END -->

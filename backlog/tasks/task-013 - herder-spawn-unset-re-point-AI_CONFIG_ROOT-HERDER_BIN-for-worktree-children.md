---
id: TASK-013
title: 'herder spawn: unset/re-point AI_CONFIG_ROOT + HERDER_BIN for worktree children'
status: In Progress
assignee: []
created_date: '2026-07-07 06:40'
updated_date: '2026-07-07 06:51'
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

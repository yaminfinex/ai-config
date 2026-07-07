---
id: TASK-009
title: 'herder docs: spawn from a worktree prepends THAT worktree''s shims'
status: To Do
assignee: []
created_date: '2026-07-07 06:33'
updated_date: '2026-07-07 06:49'
labels:
  - run-herder-dx
dependencies: []
priority: low
ordinal: 9000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-001 finding: herder spawn prepends the spawning checkout's tools/herder/shims to the child PATH, so spawning from a worktree uses that worktree's shims, not main's. Safe after the sibling-shim marker fix (e88f859) but surprising; document in tools/herder README/help.
<!-- SECTION:DESCRIPTION:END -->

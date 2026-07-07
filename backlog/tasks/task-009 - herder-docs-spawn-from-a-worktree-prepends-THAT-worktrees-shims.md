---
id: TASK-009
title: 'herder docs: spawn from a worktree prepends THAT worktree''s shims'
status: Done
assignee: []
created_date: '2026-07-07 06:33'
updated_date: '2026-07-07 07:40'
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit 353325b (unit-b-spawn-hygiene, merged d424cab). README 'Spawn Environment' section + spawn --help Behavior paragraph: spawning-checkout shims prepend, mise PATH re-pin, checkout re-point (TASK-013), notify resolution (TASK-005).
<!-- SECTION:NOTES:END -->

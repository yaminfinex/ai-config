---
id: TASK-006
title: 'herder spawn: one-shot worktree mode (--worktree BRANCH [--base REF])'
status: To Do
assignee: []
created_date: '2026-07-07 05:57'
updated_date: '2026-07-07 08:30'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 6000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Observed during run-herder-bootstrap: spawning a worker into a fresh worktree takes two CLIs and manual plumbing — herdr worktree create --json, extract workspace_id + checkout path, then herder spawn --workspace ... --cwd ... --new-tab. A herder spawn --worktree <branch> [--base REF] flag could drive herdr worktree create itself and spawn into the resulting workspace in one verified step.
<!-- SECTION:DESCRIPTION:END -->

---
id: TASK-026
title: >-
  herder cull: worktree-aware cleanup guidance (and optional worktree flags on
  spawn)
status: To Do
assignee: []
created_date: '2026-07-07 09:02'
labels:
  - run-herder-dx
dependencies: []
priority: low
ordinal: 26000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-006 nice-to-haves (Unit H): (1) culling the last agent in a --worktree-spawned workspace auto-closes the workspace, so the spawn summary's 'herdr worktree remove --workspace' advice goes stale post-cull — cleanup falls to raw 'git worktree remove' + 'git branch -D'; the orchestrator hit exactly this cleaning its verification smoke. Either cull learns an opt-in worktree-cleanup flag (report-only by default, degrade-safe doctrine) or the spawn summary/docs get a post-cull breadcrumb naming the git commands. (2) --worktree with an existing branch could fall back to 'herdr worktree open'. (3) workspace label override flag. Design which of the three are worth it as one small unit.
<!-- SECTION:DESCRIPTION:END -->

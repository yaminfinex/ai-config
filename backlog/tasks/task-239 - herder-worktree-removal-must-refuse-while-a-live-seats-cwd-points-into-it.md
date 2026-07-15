---
id: TASK-239
title: 'herder: worktree removal must refuse while a live seat''s cwd points into it'
status: To Do
assignee: []
created_date: '2026-07-15 08:54'
labels:
  - herder
dependencies: []
priority: high
ordinal: 238500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field incident: the orchestrator removed a task worktree while a live session's registry-recorded cwd still pointed into it; the resident pty died minutes later (cwd gone under a running codex session). A napkin rule is not enforcement — make it mechanical. Fix: a herder-owned worktree-cleanup verb (or a guard herder exposes that cleanup flows use) that refuses removal when any non-retired registry row records a cwd inside the target path, naming the resident seat(s) + remedy (cull/move them first, or --force). Also evaluate guarding at cull-time cleanup. AC: removal attempt with a live resident refuses naming the seat; after the resident is culled/moved it proceeds; red-first test.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Removal with live resident refuses, names seat, gives remedy; proceeds after resident gone; red-first
<!-- AC:END -->

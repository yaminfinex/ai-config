---
id: TASK-012
title: >-
  bin/herder: build-cache thrash across checkouts — rm -f herder-* wipes sibling
  binaries
status: To Do
assignee: []
created_date: '2026-07-07 06:40'
labels: []
dependencies: []
priority: medium
ordinal: 12000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-002 finding (run-herder-bootstrap): bin/herder's cache prune (rm -f herder-*) deletes OTHER checkouts' cached binaries, and failed builds wipe BEFORE building — a live session and a worktree rebuild ping-pong each other. Keep per-hash binaries and prune by age instead. Related: TASK-008 (toolchain pick).
<!-- SECTION:DESCRIPTION:END -->

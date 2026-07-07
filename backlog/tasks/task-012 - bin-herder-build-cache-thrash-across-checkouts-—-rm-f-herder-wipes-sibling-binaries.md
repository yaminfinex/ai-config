---
id: TASK-012
title: >-
  bin/herder: build-cache thrash across checkouts — rm -f herder-* wipes sibling
  binaries
status: Done
assignee: []
created_date: '2026-07-07 06:40'
updated_date: '2026-07-07 07:40'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 12000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-002 finding (run-herder-bootstrap): bin/herder's cache prune (rm -f herder-*) deletes OTHER checkouts' cached binaries, and failed builds wipe BEFORE building — a live session and a worktree rebuild ping-pong each other. Keep per-hash binaries and prune by age instead. Related: TASK-008 (toolchain pick).
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit f1dd2cc (unit-a-bin-herder, merged 4468676). Pre-build rm -f herder-* wipe REMOVED; per-hash binaries kept; reuse scans all cache candidates (XDG/HOME/shared tmp) + touch-on-use; successful build seeds a UID-scoped ${TMPDIR}/herder-cache-$UID so fake-HOME suite callers reuse instead of rebuilding per case; prune only AFTER successful build, only by age (14d bins / 1d stale .tmp), never the current hash, ownership-checked. Locale-sensitive hash key remains → TASK-018.
<!-- SECTION:NOTES:END -->

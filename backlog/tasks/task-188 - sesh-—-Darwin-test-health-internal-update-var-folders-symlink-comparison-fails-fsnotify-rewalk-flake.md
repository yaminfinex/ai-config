---
id: TASK-188
title: >-
  sesh — Darwin test health: internal/update /var/folders symlink comparison
  fails; fsnotify rewalk flake
status: To Do
assignee: []
created_date: '2026-07-13 07:49'
labels:
  - sesh
dependencies: []
priority: low
ordinal: 187000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Observed on real Darwin during the wedge investigation, pre-existing on clean main: internal/update tests fail on Darwin because /var/folders temp paths are symlinks and the test compares unresolved paths; TestPeriodicWatchRewalkRegistersNestedDirectory flakes (fsnotify timing). Neither runs in CI today — no Darwin gate exists. Fix both tests; consider what a minimal recurring Darwin check looks like (owner Mac, manual just test invocation documented, or nothing — decide and record).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Both tests pass on Darwin; path comparisons EvalSymlinks-normalized; flake root-caused or deflaked with a real sync point
<!-- AC:END -->

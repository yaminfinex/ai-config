---
id: TASK-187
title: >-
  sesh — first recovery pass: bounded-concurrency GETs + launchd fd soft limits
  + Darwin status config resolution
status: To Do
assignee: []
created_date: '2026-07-13 07:49'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 186000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Follow-ups from the Mac wedge investigation (branch mac-ship-wedge-fix): (1) first recovery pass is sequential — 3,253 files at ~310ms-1s each is 17-70 min; bound-concurrency the recovery GETs; (2) under launchd the 256-fd soft limit starves kqueue-fsnotify over a large corpus (4,389 fds observed foreground) — non-causal today (rescan fallback covers) but plist should set SoftResourceLimits; (3) sesh status run interactively on Darwin reports 'store: not configured' because the URL lives in the plist that only the service sees — status should resolve config from the installed plist the way sesh update does, so Mac users get honest output.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Recovery pass runs GETs with bounded concurrency; total first-pass time on a 3k-file corpus measured and recorded
- [ ] #2 launchd plist template sets SoftResourceLimits appropriate for kqueue watching; rendered by sesh setup
- [ ] #3 sesh status on Darwin resolves the store URL from the installed plist; tested
<!-- AC:END -->

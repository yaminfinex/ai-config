---
id: TASK-146
title: 'observer: bake review + autostart decision (closure task)'
status: To Do
assignee: []
created_date: '2026-07-10 01:50'
labels: []
dependencies: []
priority: high
ordinal: 146000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The observer daemon has been baking since 2026-07-09T09:42Z (manual instance, pid 2876552) on the post-backoff-fix build; the autostart default stays OFF until the owner reviews the bake. This task is the closure: assemble bake evidence for the watch items (busCorroboratesDead breadth, reconnect/generation behavior across herdr restarts, reconfirmation row volume vs interval, false dormant-live / turnover rates), owner reviews, and the autostart default flips ON or the daemon is parked with a reason. Related: the reconfirm-interval cadence ruling is its own open task; the spec erratum fold-in is separate.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Bake evidence assembled for all four watch items with numbers from the live state dir
- [ ] #2 Owner ruling recorded: autostart ON (with chosen cadence) or parked with reason
- [ ] #3 If ON: autostart default flipped + docs updated; if parked: standing orders updated
<!-- AC:END -->

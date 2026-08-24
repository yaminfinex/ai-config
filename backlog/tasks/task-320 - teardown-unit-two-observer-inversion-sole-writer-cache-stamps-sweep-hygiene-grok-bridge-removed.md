---
id: TASK-320
title: 'teardown unit two: observer inversion — sole-writer cache stamps, sweep hygiene, grok bridge removed'
status: To Do
assignee: []
created_date: '2026-08-24 07:30'
labels:
  - herder
  - teardown
dependencies:
  - TASK-319
ordinal: 318600
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The observer becomes the ledger's only writer; authority-row transitions
collapse to probe-corroborated cache stamps. The two pinned liveness fences
land as sweep hygiene: no second row on an occupied pane (dedupe, keep the
corroborated row), dead rows retired within a sweep plus grace. Stamp the
hcom `blocked` state for the web view. Grok bridge and doctrine-delivery
machinery removed (owner ruling 2026-08-24: grok dropped, revivable from git
history). Backbone: missions repo,
missions/fleet-refit/artifacts/conductor/observer-as-cache-design-note.md.
<!-- SECTION:DESCRIPTION:END -->

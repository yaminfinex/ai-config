---
id: TASK-152
title: 'orchestrate: controlled herdr restart recovery drill'
status: To Do
assignee: []
created_date: '2026-07-10 10:15'
labels: []
dependencies: []
references:
  - docs/herdr-upgrade.md
priority: low
ordinal: 151000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Harvest the remaining operational invariant from the retired herder systemic review. Add a repeatable controlled-restart drill that proves an orchestrated fleet recovers after herdr restart and coordinate re-keying without trusting frozen pane data. Place the durable procedure in the orchestrate references or herdr upgrade runbook, not in a historical review memo.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Document a standalone restart drill with setup, restart, reconciliation, and success criteria
- [ ] #2 The drill covers an enrolled orchestrator and at least one spawned worker
- [ ] #3 Live observations replace frozen coordinates and messages route after recovery
- [ ] #4 A regression gate or recorded manual verification demonstrates the procedure
<!-- AC:END -->

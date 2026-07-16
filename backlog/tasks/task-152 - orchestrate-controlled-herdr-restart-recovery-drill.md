---
id: TASK-152
title: 'orchestrate: controlled herdr restart recovery drill'
status: Done
assignee: []
created_date: '2026-07-10 10:15'
updated_date: '2026-07-16 01:20'
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
- [x] #1 Document a standalone restart drill with setup, restart, reconciliation, and success criteria
- [x] #2 The drill covers an enrolled orchestrator and at least one spawned worker
- [x] #3 Live observations replace frozen coordinates and messages route after recovery
- [x] #4 A regression gate or recorded manual verification demonstrates the procedure
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Drill executed live on the 0.7.3→0.7.4 handoff (owner-run update, orchestrator-run gate): disposable bash worker ticked across the swap with zero gap; pane ids stable, terminal ids reissued; detection-lost rows unseated by reconcile --apply (dormant default) and re-seated via pinned enroll + rename; 3 codex workers auto-re-bound (D12); spawn/read/cull probes green post-handoff; messages routed throughout (bus unaffected). Durable procedure written into docs/herdr-upgrade.md as the 'Controlled restart drill' section with setup/restart/reconciliation/success criteria. This run is the recorded manual verification.
<!-- SECTION:NOTES:END -->

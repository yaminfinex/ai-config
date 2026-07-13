---
id: TASK-154
title: >-
  design herd-server phase: spoke transport, delivery, mission overlays, and
  hot-read gates
status: In Progress
assignee: []
created_date: '2026-07-10 10:15'
updated_date: '2026-07-13 01:02'
labels: []
dependencies: []
references:
  - docs/specs/system-boundaries.md
priority: low
ordinal: 153000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Run a design unit for the remaining cross-component server tier before implementation. Preserve the ratified direction harvested from the retired boundaries and node-daemon documents: phase 1b adds outbound node registration/spoke telemetry, inbound delivery, mission-directory snapshot overlays, and human delegation; phase 2 may add hot herder reads only after legacy-view retirement. The file remains truth, the observer stays disposable, and no write routes through a daemon.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Design compares at least three server/spoke shapes and records a recommendation
- [ ] #2 Pins node registration, reconnect/replay, inbound delivery receipts, mission overlay reconciliation, and delegation semantics
- [ ] #3 Keeps session service and missions independently adoptable and herder-aware only in the allowed direction
- [ ] #4 Phase 2 hot reads are explicitly gated on legacy-view retirement with cold parity
- [ ] #5 Produces proposed spec amendments and filed-ready implementation tasks; no code ships in the design unit
<!-- AC:END -->

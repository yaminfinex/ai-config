---
id: TASK-176
title: 'sesh — check battery vs mise go pin drift (1.26.4 pin, 1.26.5 session GOROOT)'
status: In Progress
assignee: []
created_date: '2026-07-13 01:01'
labels:
  - sesh
dependencies: []
priority: low
ordinal: 175000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Found during TASK-172 gates: the tests/check-*.sh battery only passes when mise pins go 1.26.5, because session GOROOT env points at 1.26.5 and poisons the 1.26.4 pin that lib.sh suggests. Reconcile the pin (bump to 1.26.5 or fix lib.sh GOROOT handling so the pin is authoritative).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Battery green from a clean shell with only the repo's pinned toolchain; no GOROOT leakage
<!-- AC:END -->

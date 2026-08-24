---
id: TASK-238
title: 'check-help-contract: cover reconcile and observer verbs'
status: Done
assignee: []
created_date: '2026-07-15 08:32'
labels:
  - herder
dependencies: []
priority: low
ordinal: 237500
---

## Teardown reconciliation — 2026-08-24

Closed by the command-surface flip. Reconcile and hook retired; the surviving
list/observer help surface is generated from the live command registry and
covered by the slimmed help contract.

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Pre-existing gap found during a merge confirm (never referenced in the suite on any lineage commit): registered user-facing verbs 'reconcile' and 'observer' have --help but are absent from the check-help-contract verb list, so their help surface is untested. 'hook' is marked (internal) — decide deliberately and comment it either way. Consider deriving the list from cli.go registrations instead of hand-maintaining it (kills the silent-drop class the union-resolution lesson exposed).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 reconcile + observer covered (or an explicit commented exclusion); hook exclusion made deliberate
<!-- AC:END -->

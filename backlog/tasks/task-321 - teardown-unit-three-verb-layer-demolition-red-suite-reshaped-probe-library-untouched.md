---
id: TASK-321
title: 'teardown unit three: verb-layer demolition — red suite reshaped, probe library untouched'
status: Done
assignee: []
created_date: '2026-08-24 07:30'
labels:
  - herder
  - teardown
dependencies:
  - TASK-320
ordinal: 318700
---

## Teardown reconciliation — 2026-08-24

Implementation is approved at `3e02b3a` on `impl-verb-demolition`. Status stays
open until the conductor performs the single atomic merge with unit four;
merge is deploy on this box.

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Delete herder's send/compact/cull/spawn gateways, adopt/enroll remnants, the
credential seams that gate only those verbs, and the sidecar +
continuation-state subsystem (replaced by the validated six-line helper).
Occupant probe library untouched. Red suite reshaped per the regression
register in herder-teardown-composition.md: the self-compact fixture becomes
a term-inject + queued-send composition check; fence fixtures retire with
their verbs; the keep-green law transfers to wrapper obligations. Installed
binary is NOT reinstalled in this unit (unit four owns the flip).
<!-- SECTION:DESCRIPTION:END -->

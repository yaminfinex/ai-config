---
id: TASK-161
title: >-
  registry outcome-consumption gate: reject discard shapes (_ = outcomes,
  empty-body range, pass-to-ignoring-func)
status: To Do
assignee: []
created_date: '2026-07-12 08:23'
labels: []
dependencies: []
priority: low
ordinal: 160000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
From the typed-write-outcomes adversarial review (non-blocking note): the outcome-consumption AST gate reuses the error read-detector, so ANY read of the outcomes identifier counts as consumed — blank-discard after bind (_ = outcomes), range-and-discard (for range outcomes {}), and passing outcomes to a function that ignores them all PASS the gate. It does catch the two realistic accidental regressions (dropping outcomes at the call site; bind-but-never-read), and every in-tree consumer genuinely consumes, so this is hardening, not a live defect. WORK: extend the scanner to reject blank-assignment discard and empty-body range over write outcomes, and consider requiring outcomes to reach a branch/return/aggregation; keep the existing positive shapes passing (no false positives on legitimate consumption — verify against all current callers). Extend the negative fixture set for each new rejected shape.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Blank-discard, empty-body-range, and bind-never-read shapes each have a negative fixture the gate rejects
- [ ] #2 All current in-tree consumers still pass the gate (no false positives)
<!-- AC:END -->

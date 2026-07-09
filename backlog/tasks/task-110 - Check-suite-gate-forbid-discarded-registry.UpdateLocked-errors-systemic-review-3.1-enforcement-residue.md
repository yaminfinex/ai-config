---
id: TASK-110
title: >-
  Check-suite gate: forbid discarded registry.UpdateLocked errors
  (systemic-review 3.1 enforcement residue)
status: In Progress
assignee: []
created_date: '2026-07-09 07:05'
updated_date: '2026-07-09 09:44'
labels: []
dependencies: []
priority: high
ordinal: 110000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
UNIT TYPE: implement.

The 2026-07-08 systemic review (docs/design/2026-07-08-herder-systemic-review-memo.md, proposal 3.1) was marked shipped, but its enforcement element never landed: a check-suite gate asserting no Go code discards the error from registry.UpdateLocked (pattern class: `_, err :=` assigned then ignored, or `_, _ =`, or bare call). Discarded write errors are the claimed-success-without-confirmed-effect cluster (memo cluster B) applied to the registry — the exact bug class behind the 2026-07-09 write-freeze incident, where writes silently no-opped.

SCOPE: add the gate to the existing check suite under tools/herder/tests/ following the house pattern (see check-observer-contract.sh T-9 for a prior grep-gate with a negative self-test: the gate must include a demo that a synthetic violation IS caught, so the gate is provably failing-capable, not aspirational). Sweep current code first: if any real discards exist today, fix them in the same change or list them as explicit findings.

SETTLED DECISION: the gate lives in the bare check-*.sh suite (runs in the standard gate sequence), not in a linter config that workers might not run.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A check-*.sh gate exists that scans tools/herder for discarded UpdateLocked errors and fails on violation
- [ ] #2 The gate includes a negative self-test: a synthetic violation is demonstrably caught (T-9 pattern)
- [ ] #3 Current tree passes the gate, with any pre-existing discards either fixed or explicitly listed in the task
- [ ] #4 Full check suite still ALL GREEN bare from repo root
<!-- AC:END -->

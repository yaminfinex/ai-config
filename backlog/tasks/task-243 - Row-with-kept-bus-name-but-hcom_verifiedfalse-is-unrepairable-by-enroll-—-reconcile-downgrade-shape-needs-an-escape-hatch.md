---
id: TASK-243
title: >-
  Row with kept bus name but hcom_verified=false is unrepairable by enroll —
  reconcile-downgrade shape needs an escape hatch
status: To Do
assignee: []
created_date: '2026-07-15 11:25'
labels: []
dependencies: []
ordinal: 242500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Found during adversarial review of the enroll corroboration matrix (separate-filing note, not a formula violation). Shape: reconcile can downgrade a row to seat.hcom_verified=false while KEEPING the stored hcom_name (the self-bound-unverified shape). The enroll guid-reuse formula requires the verified live bus name to EQUAL the stored name (strict branch — the stored name is present, so the bootstrap exception does not apply), but the live roster may never again produce that name as verified, so B can never be satisfied and the row is permanently unrepairable by enroll. The refusal remedy says 'restore or join the stored bus name' without pointing at the actual escape hatches (reconcile re-verification, adopt).

Fix directions to evaluate (design checkpoint first): (a) treat stored-but-unverified names as bootstrap-eligible when the stored seat says hcom_verified=false (captures the new verified name; still requires S || (T && L)); (b) keep the strict branch but make the refusal remedy name the real escape hatches; (c) reconcile stops keeping names it cannot verify. Guard rail: whatever ships must not weaken the strict branch for verified stored names — a different live identity must still refuse. AC sketch: red-first fixture of the downgraded shape; repair path proven; strict-branch refusal for verified stored names unchanged (mutation-armed).
<!-- SECTION:DESCRIPTION:END -->

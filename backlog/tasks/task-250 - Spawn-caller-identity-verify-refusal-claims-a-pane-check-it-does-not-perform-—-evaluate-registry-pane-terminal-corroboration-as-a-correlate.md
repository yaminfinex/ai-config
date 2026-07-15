---
id: TASK-250
title: >-
  Spawn caller-identity verify: refusal claims a pane check it does not perform
  — evaluate registry pane/terminal corroboration as a correlate
status: To Do
assignee: []
created_date: '2026-07-15 20:29'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 249500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
From the same live incident as the TASK-243 duplicate-mint evidence (2026-07-15, seated orchestrator row with a registry-recorded pane). The fail-closed caller-identity check on spawn (shipped with the sender-identity unit) refused a session whose row IS in the registry with matching pane+terminal+hcom_name+sid — and the refusal text claims a pane check that evidently does not match a pane the registry can already see. Only HCOM_SESSION_ID=<sid> in the caller env unblocks it (the standing ambient-env workaround). Post-enroll (even after a verified duplicate row existed) bare spawn STILL refused — so the check does not consult the registry corroboration it claims.

TWO PARTS:
1. HONESTY (small, ship first if split): the refusal text must describe only checks actually performed, with cause+remedy naming the env correlate that unblocks.
2. DESIGN QUESTION (checkpoint before code): should the verify path accept registry corroboration it can already see (caller pane/terminal matches a seated row with a verified hcom_name) as the identity correlate when the env correlate is absent? Guard rail: must NOT weaken fail-closed — a caller whose pane matches nothing, or matches a row with a different identity, still refuses; ambient identity env passthrough hazards (see the HERDER_* passthrough task) mean pane corroboration must be proven not spoofable from a child shell before it is accepted.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Refusal text describes only checks actually performed; cause+remedy names the env correlate
- [ ] #2 Design checkpoint ruling on registry pane/terminal corroboration as correlate (accept or refuse-with-rationale), reviewed before code
- [ ] #3 Fail-closed unchanged for non-matching or mismatched-identity callers (red-first fixture)
<!-- AC:END -->

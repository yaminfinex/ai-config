---
id: TASK-243
title: >-
  Row with kept bus name but hcom_verified=false is unrepairable by enroll —
  reconcile-downgrade shape needs an escape hatch
status: In Progress
assignee: []
created_date: '2026-07-15 11:25'
updated_date: '2026-07-15 23:34'
labels: []
dependencies: []
ordinal: 242500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Found during adversarial review of the enroll corroboration matrix (separate-filing note, not a formula violation). Shape: reconcile can downgrade a row to seat.hcom_verified=false while KEEPING the stored hcom_name (the self-bound-unverified shape). The enroll guid-reuse formula requires the verified live bus name to EQUAL the stored name (strict branch — the stored name is present, so the bootstrap exception does not apply), but the live roster may never again produce that name as verified, so B can never be satisfied and the row is permanently unrepairable by enroll. The refusal remedy says 'restore or join the stored bus name' without pointing at the actual escape hatches (reconcile re-verification, adopt).

LIVE INCIDENT (2026-07-15, orchestrator-vile row, forensics in registry): worse than the predicted refusal — running enroll per the spawn-refusal remedy on the downgraded row (guid 7ef0b17d, event=reconciled, seat has NO hcom_verified, stored hcom_name kept) did not refuse OR repair: it MINTED A DUPLICATE seated row (guid 5a663744, role manual, mechanism=enroll) with the SAME terminal_id, SAME pane, SAME hcom_name, SAME sid (source harvest), hcom_verified=true. Two seated rows now claim one live pane. Consequences: (a) label-targeted cull of the duplicate passes terminal verification and would close the victim's LIVE pane; (b) no cleanup verb exists for this shape — retire requires unseated, cull closes the pane, there is no unseat-without-close; (c) the enroll did not even unblock the caller (spawn still refused without HCOM_SESSION_ID in env). ADDED SCOPE: enroll must never mint a second seated row when an existing row matches the same terminal+pane+hcom_name(+sid) — repair/re-bind that row (or refuse with the real escape hatch), never duplicate. The duplicate-row cleanup path (unseat-without-pane-close or equivalent) is part of the fix's proof.

Fix directions to evaluate (design checkpoint first): (a) treat stored-but-unverified names as bootstrap-eligible when the stored seat says hcom_verified=false (captures the new verified name; still requires S || (T && L)); (b) keep the strict branch but make the refusal remedy name the real escape hatches; (c) reconcile stops keeping names it cannot verify. Guard rail: whatever ships must not weaken the strict branch for verified stored names — a different live identity must still refuse. AC sketch: red-first fixture of the downgraded shape; repair path proven; strict-branch refusal for verified stored names unchanged (mutation-armed); red-first fixture of the duplicate-mint shape (enroll on matching terminal+pane+name repairs or refuses, never mints); the live duplicate specimen (5a663744 vs 7ef0b17d) is cleaned as part of proving the repair/cleanup path.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Red-first fixture: downgraded shape (stored name kept, hcom_verified=false) — repair path proven; strict-branch refusal for verified stored names unchanged (mutation-armed)
- [ ] #2 Red-first fixture: enroll on a matching terminal+pane+hcom_name repairs or refuses with real escape hatch — never mints a duplicate seated row
- [ ] #3 A cleanup path exists for a duplicate seated row on a live pane (unseat-without-close or equivalent); the live specimen pair is cleaned with it — ORDERING: the duplicate is load-bearing for the victim's bare identity-correlated verbs (pane match), so repair + re-verify the ORIGINAL row first, then clean the duplicate; cleaning first re-strands the victim
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
CLEANUP ORDERING UPDATE (2026-07-15, wire-proven): the duplicate specimen row is now LOAD-BEARING for the victim — their bare identity-correlated verbs (compact) succeed via the duplicate row pane match while the original downgraded row still refuses. Cleanup must therefore repair and re-verify the ORIGINAL row first (bare-verb path proven), and only then clean the duplicate — cleaning first re-strands the victim. AC #3 is to be read with this ordering.
<!-- SECTION:NOTES:END -->

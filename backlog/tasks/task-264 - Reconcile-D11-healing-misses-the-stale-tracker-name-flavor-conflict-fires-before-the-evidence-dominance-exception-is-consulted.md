---
id: TASK-264
title: >-
  Reconcile D11 healing misses the stale-tracker-name flavor: conflict fires
  before the evidence-dominance exception is consulted
status: To Do
assignee: []
created_date: '2026-07-16 13:05'
updated_date: '2026-07-17 01:04'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 263500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Residual flavor from the launch-context repair unit's live field acceptance. The merged evidence-dominance exception healed stale stored-bus-name rows fleet-wide (multiple rows reported "re-confirm — terminal live; launch context backfill pending from exact live terminal+pane and unique joined bus; written and confirmed"). But the named field row itself still conflicts:

    conflict — stored terminal is live as name="<fork-label>"; D11 refuses to unseat, use manual adoption/enroll

The herdr TRACKER row for the terminal carries the fork-era label while the registry label was later transferred (rename --take-from), and the D11 tracker-name comparison diverts the row to conflict BEFORE the new exception is consulted. Suspected second divergence: the exception requires the recorded SID to resolve the unique joined bus row, and a bus row reclaimed via `hcom start --as` may carry a session binding that does not match the registry-recorded SID.

Also fold in (acceptance-evidence hygiene from the same field run): bare spawn from the field row PASSED post-merge even though its stored bus name was still stale — which exceeds the fallback's documented preconditions (stored bus name must match the unique joined row). Determine which evidence actually admitted the sender (session-binding match healed by hook traffic? bus-row backfill via another path?) and pin the true admitting path with a fixture; an unexplained pass on an identity fence is a gap in the matrix even when the outcome is desired.

Scope: extend the dominance exception (or the tracker-name comparison ordering) so a stale tracker label with otherwise-exact seat evidence heals, with the same fail-closed negatives; plus the admitting-path investigation above. The field row remains live in the fleet as the acceptance case; its seat is fully operational (spawn works bare), so this is repair-verb completeness, not an outage.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Stale-tracker-name flavor with exact seat evidence heals under reconcile --apply (red-first fixture reproducing the field conflict), negatives remain conflict
- [ ] #2 The post-merge spawn admitting path for the field row is identified and pinned by a fixture (no unexplained passes on the sender fence)
- [ ] #3 Live field row heals end-to-end (stored bus name corrected, launch context backfilled, herder send by guid resolves the live bus)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Correction from the TASK-266 independent investigation (verified against HEAD by hera): the description's ordering hypothesis is wrong in detail — the D11 evidence-dominance exception IS consulted on tracker-name conflicts (reconcilecmd/reconcile.go, reconcileBusIdentity trackerConflict branch). The field row fails the exception's PREDICATE: ResolveExactSessionPane requires recorded-SID equality AND bus-row launch-frozen pane equality, both unsatisfiable for reclaimed (start --as) / empty-launch-context shapes; and the launch-context backfill only arms on a re-confirm outcome, which the standing conflict blocks. The description's 'suspected second divergence' (SID leg) is correct. Fix scope = the predicate's admissible evidence classes, not consultation order. AC#1 wording should be read accordingly.
<!-- SECTION:NOTES:END -->

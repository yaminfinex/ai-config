---
id: TASK-305
title: >-
  slim-down: reconcile the identity-migration program tasks (267/272 line)
  against the deletion-first charter
status: Done
assignee: []
created_date: '2026-08-21 04:40'
updated_date: '2026-08-21 11:32'
labels:
  - herder
  - slimdown
dependencies:
  - TASK-301
references:
  - napkins/herder-slimdown-charter.md
  - docs/specs/herder-spec.md
priority: medium
ordinal: 304500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Charter decision 4 (napkins/herder-slimdown-charter.md): TASK-267/272's identity migration program is parked/superseded by ground-truth resolution — check each remaining migration-stage task (U1 canonical rebirth, U2 break-glass repair, U4 observer liveness consolidation, U5 epochs; find their task ids around 269-276) against the charter and close or re-scope with an explicit note. TASK-268 and TASK-041 resolve by deletion (pass 3 fixtures prove it). Also draft the spec amendments the charter requires: re-seat-in-place removal and pane-id demotion in docs/specs/herder-spec.md — drafts only, owner ratifies. Credentials (U3/TASK-272) shipped and STAY — they are orthogonal to coordinate copies.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every open identity-migration task dispositioned: closed-superseded or re-scoped, each with a charter citation in its notes
- [x] #2 Spec amendment drafts exist for re-seat-in-place removal and pane-id demotion, marked awaiting owner ratification
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
CLOSED 2026-08-21: owner ratified the spec amendment drafts via fleet-refit mission TASK-3 (relayed by reza) — groups A/B/C as drafted; X as modified (auto-heal only on positive transcript-match; verb-time only in v1, observer sweep fast-follow per U5 ruling). Applied to docs/specs/herder-spec.md in commit 84e84eb: re-seat-in-place removed, coordinates → observation provenance + invariant 15, §8.3 → occupant resolution, AC-40 rewritten, AC-22/23/24 re-evidenced, epochs demoted (D6), D11 amended, D13 added. Full sweep: no live reconcile references remain except persisted event vocabulary (reconciled rows = append-only history, correctly kept) and amendment notes. Task-line dispositions were already complete (274 archived, 268/041 re-scoped, 275 survives). Decision sheet updated: D-2 and D-9 marked RULED.
<!-- SECTION:NOTES:END -->

---
id: TASK-266
title: >-
  Research: root-cause the registration/assignment brittleness class —
  independent architecture review of the identity layer
status: Done
assignee: []
created_date: '2026-07-17 00:15'
updated_date: '2026-07-17 01:04'
labels:
  - herder
  - research
dependencies: []
priority: high
ordinal: 265500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner-ordered (2026-07-17): an independent Fable-lane investigation of the season's identity/registration/assignment failure class — seats going spawn-dead, repair verbs refusing to repair, rows born incomplete, takeover windows, liveness janitors firing on the wrong side of both failure modes. The investigator forms their own view from primary sources (board task files with field evidence, the three code stores and their verbs, the hazard docs, the run journal) and delivers a root-cause memo: incident taxonomy by mechanism, the architectural properties that keep generating the class, ranked improvement directions with migration honesty, a keep-list of fences that earned their keep, and an ours-vs-upstream split.

Type: research/design. No implementation; read-only on repo and all live state; isolated probes only. Deliverable: memo (run napkins) + bus report; durable docs/design promotion is a separate owner-gated step, as is any implementation chain that follows.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Taxonomy clusters the incident/task set by mechanism with each claim verified against code or recorded evidence (not inherited from summaries)
- [x] #2 Root causes named as architectural properties, each tested against the record; improvement directions ranked with the invariant established, incidents prevented, and blast radius
- [x] #3 Keep-list and ours-vs-upstream split included; memo delivered and reported with top-three findings inline
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Accepted 2026-07-17. Memo at napkins/run-herder-dx/registration-brittleness-memo.md (run-scoped; durable promotion owner-gated). Investigator delivered taxonomy M1-M6, root causes H1-H7 (all six brief hypotheses tested — accepted/sharpened/narrowed, plus one new: coordinates carry no validity epoch), ranked improvements R1-R6, keep-list, ours-vs-upstream split, and a disagreements section. Orchestrator verification: all load-bearing code citations spot-checked at HEAD and CONFIRMED — (a) BuildProvenance unconditionally harvests ambient HCOM_SESSION_ID and the projection stamps Source=harvest + Continuity=confirmed (open contamination hole, TASK-244 second vector, code-verified); (b) enroll repair hard-gates on live.Verified (the recorded field refusal verbatim); (c) reconcile D11 dominance exception IS consulted on tracker conflicts but its predicate (recorded-SID + stored-pane equality) is unsatisfiable for reclaim/empty-context shapes and the backfill only arms on re-confirm — corrects TASK-264's ordering hypothesis; (d) the shared identity proof core matches launch-frozen env against db-fossilized launch_context (fossil-vs-fossil). Top recommendation: R1 canonical rebirth + R2 attested break-glass repair; cheap immediate slice: stop harvesting ambient SID in creator flows. Implementation chain and durable-docs promotion are owner-gated next steps.
<!-- SECTION:NOTES:END -->

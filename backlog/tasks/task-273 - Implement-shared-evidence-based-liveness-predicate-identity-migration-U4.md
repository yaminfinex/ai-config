---
id: TASK-273
title: 'Implement: shared evidence-based liveness predicate (identity migration U4)'
status: In Progress
assignee: []
created_date: '2026-07-17 04:28'
updated_date: '2026-07-17 22:04'
labels:
  - herder
  - identity-migration
dependencies: []
priority: medium
ordinal: 272500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
IMPLEMENT unit, stage 4 of the ratified identity migration. GROUND TRUTH (settled, double-reviewed + owner-ratified — do NOT relitigate; deviations are STOP-AND-REPORT to @hera, read this twice): docs/design/2026-07-17-identity-migration-plan.md §U4 and docs/design/2026-07-17-identity-architecture-target.md (T4). Memo §4 keep-list = HARD constraints. No hard dependency on earlier stages (sequenced after them because they remove generators; verdicts sharpen once rows are complete and callers honest) — confirm sequencing with @hera at pickup if earlier stages have not merged.

Goal: define the liveness predicate ONCE — positive death evidence (occupant exited, pane gone within an unchanged epoch, dead pid behind a stale bus row) vs observation gap (everything else) — in a small package (e.g. internal/liveness/) applied by every observing component: sidecars for occupants, node observer for every seated row, CLI verbs when they observe first. SETTLED (spec-conformance disposition): this is authority consolidation of the RULES not the ACTOR — first-observer-appends and observer-disposability spec semantics are preserved; whichever component first observes positive death appends the unseat through ordinary locked-writer discipline; observer stays the continuous adjudicator and advice surface, NOT a required daemon or sole author; observer liveness is a precondition for nothing; registry facts = sole truth, observer advice = display-tier with observed_via-style provenance. Two first-class behaviors: (a) positive death → unseat by whichever applier saw it; (b) keepalive starvation with a live holder → loud 'holder alive, keepalive failing' advisory BEFORE upstream janitor staleness windows convert config problems into identity loss. Ad-hoc liveness inference elsewhere in herder (traffic history, own-launch records) is DELETED in favor of the shared predicate.

DESIGN CHECKPOINT REQUIRED BEFORE CODE (predicate API, evidence taxonomy, applier conversion list, advisory surface). Adversarial review is orchestrator-dispatched after your DONE; do not arrange reviewers yourself. The orchestrator's bus address is @hera; there is no @orchestrator alias. Commit on your unit branch before DONE. Hygiene: no agent names, task numbers, run identifiers, or SHAs in durable text.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Design checkpoint note approved by orchestrator BEFORE any code
- [ ] #2 Every plan §U4 test scenario implemented and green: starved-keepalive advisory (no unseat, cause class named); dead-pid-behind-listening-row positive unseat with evidence recorded; observer-down first-observer unseat by sidecar/CLI under the shared predicate; foreign-launched pane observed alive via pane/process evidence; observer-down blocks no verb and fabricates no verdict, restart catch-up converges without backdated timestamps; absence-of-evidence surfaces observation gap, never automated unseat
- [ ] #3 Replay fixtures for both season wrong-side failures — reap-the-living (starved keepalive) and spare-the-dead (fossilized listening row) — advisory fires on the first, positive-evidence unseat on the second, from more than one applier
- [ ] #4 All ad-hoc liveness inference sites converted to the shared predicate (enumerate them in the checkpoint); existing observer disposability/catch-up suites green; keep-list re-audit of the final diff
<!-- AC:END -->

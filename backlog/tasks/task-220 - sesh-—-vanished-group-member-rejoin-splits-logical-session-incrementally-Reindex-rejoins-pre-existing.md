---
id: TASK-220
title: >-
  sesh — vanished group member rejoin splits logical session incrementally;
  Reindex rejoins (pre-existing)
status: To Do
assignee: []
created_date: '2026-07-15 09:10'
labels:
  - sesh
dependencies: []
priority: low
ordinal: 219500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Pre-existing incremental-vs-Reindex divergence, CONFIRMED
during TASK-149 substance review (thread task149, findings bus #77056,
reviewer repro on clean main): a group member whose rows are ALL removed
by dedupe loses its inherited logical placement; a later append to that
same file/generation silently splits from its prior logical group, while
Reindex replays the deleted overlap history and rejoins it.

Repro (clean archive, fails identically on main and on the TASK-149
branch): file A has two keys; file B resumes A with exactly those two
keys and is fully deduped; append a unique tail to B in a later pass.
Incremental Checksum 322693.../3 with tail logical ...98001; Reindex
8157a4.../3 with tail logical ...98000. Same defect family as TASK-136
(arrival-order survivor divergence), one layer further: placement
inheritance vs surviving-row-derived membership.

TASK-149 lane obligations (no-widening proof + design-note
qualification of the known hole) document but do not fix this. Fix
directions from the review: persist/recover placement independently of
surviving message rows, or replay enough history before
admitting/processing the rejoin. Constraints as always: index schema
FROZEN (if persistence needs a column, STOP and surface the schema
question to the orchestrator first), bounded append cost
(touched-component discipline), TASK-136 equivalence + ordinal
compaction properties stay green, empty-uuid non-participation
preserved.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Reviewer repro (fully-deduped member, later-pass tail append) yields identical incremental and post-Reindex checksums, BOTH arrival orders; red baseline shown on unfixed main
- [ ] #2 Reindex fixed-point holds; TASK-136 equivalence/ordinal tests untouched and green; empty-uuid rows unaffected
- [ ] #3 Append cost stays bounded (no corpus-scale replay); maint_rows truthful; no DDL
- [ ] #4 Full pinned gate green
<!-- AC:END -->

---
id: TASK-162
title: >-
  continuation surfacing residuals: retired-sibling name-reuse suppresses
  observer row hint; --json empty-registry gap; row-attachment golden
status: In Progress
assignee: []
created_date: '2026-07-12 09:14'
updated_date: '2026-07-12 12:15'
labels: []
dependencies: []
priority: low
ordinal: 161000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Three Low residuals from the failed-continuation-surfacing delta review, none defeating the primary herder list top block. (1) continuationTarget counts ANY projection row whose Seat.HcomName matches regardless of state — a retired/lost sibling carrying the same hcom_name (common after cull + respawn-with-same-name) makes a live target look ambiguous and suppresses its per-row observer flag; skip retired/lost rows or prefer the seated-verified match. (2) unresolved_continuations attaches per reconciled row, so with ZERO rows (empty/all-culled registry) an unresolved failure appears nowhere in --json output though table mode shows it; emit a document-level object or synthetic row so --json is row-count-independent (also dedupes the identical per-row copies). (3) The list-contract continuation scenarios do not exercise the observer-advice ROW path (no observer.status.json fixture), so a reintroduction of the flag-broadcast regression would only be caught at the unit layer; add a scenario with a scoped observer flag pinning row attachment. Also add a one-line unit test for the ambiguous-target-emits-no-flag case (verified live in review, untested).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Retired/lost rows excluded from continuation target resolution; name-reuse scenario attaches the flag to the seated row
- [ ] #2 --json surfaces unresolved failures with zero reconciled rows
- [ ] #3 List-contract scenario pins observer-advice row attachment; ambiguous-target unit test added
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Dispatched to codex 5.6 worker on branch task-162-continuation-residuals; brief napkins/run-herder-dx/task-162-implement-brief.md. AC2 either/or SETTLED pre-dispatch (advisor-concurred): document-level kind-discriminated unresolved_continuation JSONL record; per-row attachment removed (no in-tree reader); no dual emission.
<!-- SECTION:NOTES:END -->

---
id: TASK-162
title: >-
  continuation surfacing residuals: retired-sibling name-reuse suppresses
  observer row hint; --json empty-registry gap; row-attachment golden
status: Done
assignee: []
created_date: '2026-07-12 09:14'
updated_date: '2026-07-12 12:50'
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
- [x] #1 Retired/lost rows excluded from continuation target resolution; name-reuse scenario attaches the flag to the seated row
- [x] #2 --json surfaces unresolved failures with zero reconciled rows
- [x] #3 List-contract scenario pins observer-advice row attachment; ambiguous-target unit test added
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged 7374487 (single amended commit 2fcbd0f). Leg 1 shipped as DEFENSE-IN-DEPTH, not a fix: reviewer proved retired/lost-with-seat is unreachable through the real write path (UpdateLocked normalizer strips seats on every transition out of seated), so the original residual described a phantom state; guard kept (future lost-writers may preserve seats) with an honest comment, real write-path name-reuse test (seat→cull→retire→respawn, WriteApplied asserted per event) pins the writer invariant as a tripwire, writer-impossible rows isolated in a labeled synthetic test. The operator-REACHABLE variant (stale seated row + name reuse → ambiguity → no flag) is out of scope by the settled ambiguity rule and filed as TASK-166. Leg 2 per settled design: one document-level kind=unresolved_continuation JSONL record per failure, row-count-independent (zero-row and missing-registry cases golden-pinned); per-row unresolved_continuations REMOVED (no in-tree reader; commit body documents the contract change); help documents kind==session selection, pinned by a v2-shaped fixture golden; table UNRESOLVED block byte-unchanged; marshal failures warn loud on stderr (injected-failure coverage). Leg 3: observer-advice row attachment pinned at contract level via real observer status fixture; ambiguous-target-no-flag unit test uses the reachable two-seated shape. observercmd touched only within the mid-unit narrow grant (continuationTarget + comment). Opus review APPROVE-with-residuals → 5-item fix round → delta APPROVE (mutation-tested); identifier sweep blocked the merge once (run agent name in new fixture) → neutralized in amended 2fcbd0f, re-sweep clean. Gates: independent 53/53 ×3, post-merge 53/53. Observer-side change takes effect at next observer start (owner-controlled rollout).
<!-- SECTION:NOTES:END -->

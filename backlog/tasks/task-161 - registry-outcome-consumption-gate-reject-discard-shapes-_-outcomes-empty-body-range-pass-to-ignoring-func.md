---
id: TASK-161
title: >-
  registry outcome-consumption gate: reject discard shapes (_ = outcomes,
  empty-body range, pass-to-ignoring-func)
status: Done
assignee: []
created_date: '2026-07-12 08:23'
updated_date: '2026-07-12 13:05'
labels: []
dependencies: []
priority: low
ordinal: 160000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
From the typed-write-outcomes adversarial review (non-blocking note): the outcome-consumption AST gate reuses the error read-detector, so ANY read of the outcomes identifier counts as consumed — blank-discard after bind (_ = outcomes), range-and-discard (for range outcomes {}), and passing outcomes to a function that ignores them all PASS the gate. It does catch the two realistic accidental regressions (dropping outcomes at the call site; bind-but-never-read), and every in-tree consumer genuinely consumes, so this is hardening, not a live defect. WORK: extend the scanner to reject blank-assignment discard and empty-body range over write outcomes, and consider requiring outcomes to reach a branch/return/aggregation; keep the existing positive shapes passing (no false positives on legitimate consumption — verify against all current callers). Extend the negative fixture set for each new rejected shape.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Blank-discard, empty-body-range, and bind-never-read shapes each have a negative fixture the gate rejects
- [x] #2 All current in-tree consumers still pass the gate (no false positives)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged cd4e1ff (bb3d42a + 977d419 + 6300844, single check-script file). The outcome-consumption gate now distinguishes meaningful consumption from discard reads: rejects post-bind blank assignment (including POSITIONAL laundering via same-index LHS/RHS mapping — review-caught), empty-body range, bind-never-read, and locally-provable pass-to-ignoring-function (unnamed/_ parameter). Discard dominance rule (a recognized discard within a binding lifetime beats coexisting meaningful reads) reviewer-ruled KEEP: proven load-bearing (without it, mixed read+discard statements launder) and properly bounded (re-binds, closures, error policy untouched). Diagnostics report at the DISCARD site with cause+remedy; fixtures assert line AND wording. Honest boundary documented in the script header (imported/method/generic callees, multi-result forwarding, function-typed variables, alias-then-discard, non-empty non-inspecting range, write-only sinks, builtin-derived reads assumed meaningful — the last pinned by a positive fixture). Range rule deliberately NOT tightened (false-positive risk on counting loops; settled per brief CONSIDER guidance). Caller enumeration: 43 sites/17 files, reviewer-reconciled to 50 grep hits (7 = benign test-helper substring noise). Scanner parse/compile failure FAILs, proven non-vacuous by reviewer mutation testing. Opus review: 1 P2 + 2 P3 round 1 (zero false positives across 27 reviewer fixtures), semantics-APPROVE + 1 P2 wording round 2, zero-finding final APPROVE with empirical semantics-frozen proof (33 fixtures 1:1 across scanner builds). Gates: independent 53/53 ×2, post-merge 53/53.
<!-- SECTION:NOTES:END -->

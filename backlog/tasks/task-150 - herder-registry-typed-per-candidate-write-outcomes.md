---
id: TASK-150
title: 'herder registry: typed per-candidate write outcomes'
status: Done
assignee: []
created_date: '2026-07-10 10:15'
updated_date: '2026-07-12 08:31'
labels: []
dependencies: []
references:
  - docs/specs/herder-spec.md
modified_files:
  - tools/herder/internal/registry/write.go
priority: high
ordinal: 149000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement the confirmed-write contract already stated in the herder spec. Registry multi-row writes currently return only encoded rows plus a batch error, forcing observer and callers to reconstruct applied/noop and collapsing refusal to the whole batch. Replace that ambiguity with typed per-candidate applied/noop/refused outcomes and migrate every writer to surface or handle them explicitly. This is the unharvested residue from the retired systemic review; it is distinct from the existing discarded-error gate.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Registry write API returns one typed outcome per candidate: applied, noop, or refused with reason
- [x] #2 Observer and all production writers consume outcomes directly; no encoded-row matching reconstructs status
- [x] #3 Multi-row mixed outcome behavior is atomic where required and pinned by tests
- [x] #4 No caller discards write outcomes or errors; repository gate proves it
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
SHIPPED, merged 53a51c6 (branch task-150-write-outcomes, commits e7c1920 + 6875e2f). UpdateLocked returns one positional typed WriteOutcome per callback candidate: applied (encoded Row) / noop / refused (reason + preserved typed cause). Candidate-row batches stay atomic: refusedBatch early-returns before any disk write; the refusing candidate carries its specific reason, blocked candidates atomic-block, unevaluated candidates explicit — all three taxonomy branches pinned by a 3-candidate test with registry-bytes-unchanged assertion (mutation-verified by the reviewer: a taxonomy swap fails the test). All nine production writers migrated (cull, enroll, lifecycle, observer incl. range aggregation for turnover, reconcile with the roster-unavailable guard untouched, rename, retire/reopen, sidecar, spawn registration + enrichment); encoded-row matching helpers deleted; cull's appendClosed requires WriteApplied (the cull-writes-nothing incident class is dead). AST gate extended to UpdateLocked/AppendLegacySessionEvent/spawn wrapper with 25 negative fixtures — reviewer verified all reject and probed evasion shapes; the surviving discard shapes are hardening follow-up TASK-161. Adversarial review (opus, cross-family): APPROVE round 1 with two non-blocking notes, coverage note fixed in a tight test-only round, delta APPROVE with mutation proof. Gates: independent 4-module + 53-script battery green from the worktree; post-merge battery green on main.
<!-- SECTION:NOTES:END -->

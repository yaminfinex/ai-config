---
id: TASK-167
title: >-
  registry check gates: port fail-closed declared-test validation to sibling
  gates
status: Done
assignee: []
created_date: '2026-07-12 13:32'
updated_date: '2026-07-12 14:24'
labels: []
dependencies: []
priority: medium
ordinal: 166000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Adversarial review of the write-discipline gate hardening found the same defect class LIVE in a sibling gate: tools/herder/tests/check-registry-migration.sh declares TestLegacyV1MigrationHandlesMixedFile in its -run alternation, but that test exists nowhere in tools/herder/internal (verified independently) — the gate is green while running 5 of its 6 declared invariants. check-registry-rotation.sh has the same named-alternation shape (all 12 names currently resolve, but nothing prevents future silent shrink). Port the fail-closed idiom shipped in check-registry-write-discipline.sh (declared-name list validated via go test -list, missing-name self-probe, per-name RUN/PASS evidence, declaration-count floor) to both scripts, and repair or replace the ghost migration test name with whatever test now anchors the mixed-file migration invariant (if none does, flag to the orchestrator before inventing coverage). check-registry-v2.sh runs the whole package and is immune; check-statusline-snapshot.sh generates its named test at gate time and is not a ghost.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 check-registry-migration.sh no longer declares any nonexistent test; the mixed-file migration invariant is anchored by a real, executing test or explicitly flagged
- [x] #2 Both migration and rotation gates validate every declared name exists and executes, fail closed on renames/deletions/skips, and include the missing-name self-probe
- [x] #3 Registry gates and full herder go test suite pass
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged c7b170c (worker commit 29f093a). Migration ghost replaced by the two refusal anchors (TestLockedWriteRefusesInjectedLegacyV1RowInBornV2Registry, TestLockedWriteRefusesLegacyV1RowInPlantedMigrationArchive) — reviewer traced the ghost's full lineage (renamed in f6fc2f2 leaving a prefix behind an anchored regex = ghost birth; deleted in 2fce98e) and confirmed the anchors are the ghost's direct descendants with the buggy accept-expectation inverted to refusal; no closer candidate among all 13 migration/legacy tests. Both folded residuals shipped on all three gates: distinct-name floor (duplicate declarations red) and skip-shaped evidence probe (dropping only the PASS grep goes red). Adversarial review (opus): 16/16 mutation matrix red both gates, cache-replay safe, empty -list fails closed, APPROVE zero fix rounds; 2 non-blocking P3 notes (RUN-grep boundary asymmetry - no constructible failure; undeclared refusal sibling TestLockedWriteRefusesLegacyV1AppendToMintedRegistry as optional future declaration; note: floors are equality pins - bump when adding anchors). Gates 53/53 independent + post-merge 53/53. Sweep clean.
<!-- SECTION:NOTES:END -->

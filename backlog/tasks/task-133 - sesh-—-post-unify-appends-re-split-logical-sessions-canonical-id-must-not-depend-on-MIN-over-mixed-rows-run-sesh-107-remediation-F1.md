---
id: TASK-133
title: >-
  sesh — post-unify appends re-split logical sessions: canonical id must not
  depend on MIN over mixed rows (run-sesh-107 remediation F1)
status: Done
assignee:
  - sesh107-remF1-lola
created_date: '2026-07-09 23:53'
updated_date: '2026-07-10 00:06'
labels:
  - run-sesh-107
dependencies: []
priority: high
ordinal: 133000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Born from the run-sesh-107 review tail (finding 1, CONFIRMED by repro; thread sesh107-review #34474). Pre-existing defect — reproduces identically before the incremental-unification change (per-append global unify era) — but it voids TASK-131's equivalence bar and is field-relevant, so it is fixed in-run.

Defect (tools/sesh/internal/index/index.go — unifyConnectedLogicalSessions, fileSummary, insertRows path): an append to a file that was previously unified into another session's canonical logical id inserts its NEW rows under the file's original parsed wire id. Re-convergence then depends on MIN(logical_session_id) over the file's now-mixed rows happening to pick the canonical id, which fails whenever the file's own wire id sorts lexicographically before the canonical id (~half of resumed sessions) — the >=2-message-UUID overlap evidence that originally connected the files was already consumed by dedup, so group discovery finds only the file itself.

Reproduced failure: original session wire id ...900002 (file A); resume session ...900001 (file B) replaying 2 of A's message UUIDs; ship B in two chunks. Chunk 1 unifies correctly (B -> logical 900002, file_ordinal 1, replayed rows deduped). Chunk 2 inserts under 900001; MIN picks 900001; group={B}; B's rows split permanently across two logical sessions and B's file_ordinal flips 1 -> 0 (transcript ordering corrupted). Reindex heals it; the next append re-splits it — active resumed sessions flap forever on the surface.

Fix direction (settled): new rows of a file must join the file's CURRENT logical session — as established by its existing rows' logical_session_id from prior unification — before/instead of trusting the parsed wire id, and group canonical selection must not depend on MIN over mixed rows. An index-owned persistent wire-id -> canonical mapping is acceptable if Reindex rebuilds it from scratch.

Settled decisions — do not re-litigate; tension = STOP and report on your unit thread:
- Global Reindex semantics stay authoritative and unchanged; the incremental path converges to Reindex output, never the other way.
- No wire-schema or files-registry-table changes; index-owned disposable state only.
- file_ordinal must be stable across later appends (no flips once assigned within an unchanged group).
- The equivalence test gap IS the point: tests added here must cover the shapes the shipped test missed — post-unify appends, both adversarial id orderings (resume id sorting before AND after the original), and a three-file transitive chain. Fixture-only coverage that stays blind to these shapes is not acceptance.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Repro from the description (resume id sorting before original, two-chunk append) yields identical incremental and post-Reindex checksums, one logical session, stable file_ordinal
- [x] #2 Equivalence tests extended: post-unify appends, both id orderings, three-file transitive chain — all green
- [x] #3 No regression on the scoped-append perf property (benchmark from TASK-131 still flat vs unrelated-file count)
- [x] #4 Full pinned gate green uncached (build, vet, go test ./..., all tests/check-*.sh)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Landed as 3efd24e (worker sesh107-remF1-lola, verified by mive: independent uncached gate green, benchmark re-run flat, diff reviewed). Append rows now inherit the file generation's existing logical session pre-insert, only when exactly one existing logical id differs from the wire id — resumed files stay unified after dedup consumed overlap evidence; normal/parser-break files unchanged (broad rule was tried and rejected: check-s10 caught it). Adversarial equivalence tests added: post-unify appends both id orderings + transitive chain, all asserting checksum parity with Reindex. Reviewer repro re-verification pending.
<!-- SECTION:NOTES:END -->

---
id: TASK-220
title: >-
  sesh — vanished group member rejoin splits logical session incrementally;
  Reindex rejoins (pre-existing)
status: Done
updated_date: '2026-07-15'
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
- [x] #1 Reviewer repro (fully-deduped member, later-pass tail append) yields identical incremental and post-Reindex checksums, BOTH arrival orders; red baseline shown on unfixed main
- [x] #2 Reindex fixed-point holds; TASK-136 equivalence/ordinal tests untouched and green; empty-uuid rows unaffected
- [x] #3 Append cost stays bounded (no corpus-scale replay); maint_rows truthful; no DDL
- [x] #4 Full pinned gate green
<!-- AC:END -->

## Evidence (Done, 2026-07-15)

Lane: branch task-220-rejoin-divergence (builder-gunu, codex
gpt-5.6-sol; sole substance reviewer reviewer-diru, codex; hera-cleared
gate with merge + post-merge battery + push delegated to mika). 2
commits (25ce7f6 + 0397910), 5 files, internal/index + design note.

- Fix: bounded history replay on the incremental append path. Predicate
  (after review fix): zero surviving NON-QUARANTINE placement-bearing
  rows for the exact file generation (pinned full-key lookups,
  plan-allowlisted); that one file replays from its durable mirror with
  already-indexed byte spans filtered — quarantine messages not
  resurrected, quarantine_ledger + fact_observations untouched
  (INSERT-only contract pinned by regression). Cost: file-history +
  touched component (5,000-row corpus test: 2 history + 1 tail rows).
- Review P1 (FIXED 0397910, independently re-verified): quarantine-only
  survivors suppressed replay — same divergence class persisted
  (reviewer repro checksums 77a40e8f.../4 vs dd1d79f2.../4); fix
  classifies on placement-bearing rows + span-filtered replay.
- Red/green chains on bus: original task repro red on main
  (d0aa0c5d.../3 vs a16c922b.../3) green on branch; quarantine variant
  red at 25ce7f6 green at 0397910; BOTH arrival orders; second-Reindex
  fixed point; false-positive probes (junk-only, quarantined
  generation, pi header-only) all correct; mirror-failure replay rolls
  back transactionally, converges after repair+Reindex.
- TASK-136 equivalence/ordinal, TASK-149 settled fast path + bounded
  gates, pi suite, empty-uuid non-participation: all green.
- Verdict APPROVE #81320; merge d81a290 --no-ff on post-train main;
  post-merge battery BY MIKA per hera convention: first run VOIDED
  (hera board commit landed mid-battery, disclosed; restarted per the
  mechanical mid-gate rule), restart from 38b9b64: 4 module gates +
  60/60 checks (check-toolchain-gate.sh now in the bar); author-check;
  pushed.
- Deploy: tag sesh-v0.1.16 exact; store live "sesh-v0.1.16"; release
  published clean; this node updated v0.1.15 -> v0.1.16, shipping
  healthy, nodes page 200/0.36s.

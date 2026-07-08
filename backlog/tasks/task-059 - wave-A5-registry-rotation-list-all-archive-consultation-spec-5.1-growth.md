---
id: TASK-059
title: 'wave A5: registry rotation + list --all archive consultation (spec 5.1 growth)'
status: In Progress
assignee: []
created_date: '2026-07-08 05:55'
updated_date: '2026-07-08 09:47'
labels: []
dependencies: []
priority: low
ordinal: 59000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Plan unit A5 (spec-plan-wave-a.md). Size-threshold rotation reusing A4 rotate-reseed mechanics; archives read-only beside the log; list --all and lineage resolution consult archives. Smallest unit; may fold into A4 if the worker is ahead. Depends: A4 (TASK-058).
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 08:57
---
[hera 2026-07-08] +A5 obligation from A4 review (mori #9974 LOW): post-migration, retired guids live archive-only — list --all must consult archives and lineage resolution must resolve forked_from pointing at archived guids, else they vanish/dangle silently. Carry the reviewer's evidence into the A5 brief.
---

created: 2026-07-08 09:09
---
[hera 2026-07-08] A4 merged; dispatching A5 (rotation + archive consultation) — final wave-A unit.
---

created: 2026-07-08 09:10
---
[hera 2026-07-08] Dispatched: codex worker wave-a5-lina (guid d3e618cf), worktree wave-a5-rotation (workspace wD), brief napkins/run-herder-dx/brief-wave-a5.md. Scope: size-threshold rotation REUSING A4 mechanics (durable numbered archives, node-guaranteed reseed, same crash guarantees), list --all archive consultation with ARCHIVED marking, lineage resolution across archive boundaries, multi-archive deterministic merge (live beats archives), optional cheap truncation-detection hardening (mori residual — skip-with-reasons valid). Inherited obligation from mori LOW carried in. Adversarial review mandatory. Last unit of wave A.
---

created: 2026-07-08 09:21
---
[hera 2026-07-08] Worker DONE (#10259): single commit 5036236. HERA GATE GREEN from worktree: vet/test both modules (registry -count=1 fresh), 25/25 suites incl new check-registry-rotation.sh. Deviations accepted pending review sanity-check: 8 MiB default (env-overridable), expected-count hardening skipped (format-contract change — reasonable). Adversarial review dispatched: review-a5-vonu (guid 338e9497, own tab), brief napkins/run-herder-dx/brief-review-a5.md — angles: trigger placement inside the SAME lock acquisition, rotation-vs-migration ordering, crash recovery must reseed from NEWEST archive not 0001, A4-blocker-class recurrence via the rotation entry point, per-guid latest-wins across archive sequence, consultation cost bounding + archive-row resurrection risk into WRITE decisions (fork/resume loadRegistry), read-only discipline, pre-A5 binary compat, env parsing. MEDIUM+ blocks merge.
---

created: 2026-07-08 09:31
---
[hera 2026-07-08] Opus adversarial verdict (review-a5-vonu, #10343): BLOCKED — 1 BLOCKER + 2 MEDIUM. BLOCKER: rotation's retired-row pruning makes A4's count-based migrationNeeded heuristic actively dangerous — crash variant recovers from STALE 0001 archive (post-migration sessions lost, retired resurrected); NO-CRASH variant fires on every write once legitimate retirement drift puts live non-retired below N(0001), clobber+resurrect forever; masked by a test whose co-located archive has nonRetired=1. Deviation (b) (skipped expected-count hardening) REJECTED by reviewer — the skip is the root cause; adopted, revisited in fix. MEDIUM-1: sidecar no-resurrect guard reads live-only; rotation-pruned retired guid -> latest==nil -> guard skipped -> stale hook resurrects dead guid as seated (regression vs A4). MEDIUM-2: lifecycle.loadRegistry unconditionally scans ALL archives on every fork/resume — unbounded. LOWs: rotation thrash over-threshold reseed; duplicate archive after partial crash; garbage rotate-bytes env fails all writes (ruling: keep fail-closed, name var+value). NITs: 3+ archive tie untested, refusal texts ungated, cosmetic duplicate node row in return. Probed clean: trigger in-lock pre-append, newest-archive recovery sourcing (when reached), byte-verify, node-guaranteed reseed, latest-wins ordering, ARCHIVED marking, write decisions live-only, read-only discipline, list (no --all) pays nothing. Fix round dispatched to wave-a5-lina (delivered).
---

created: 2026-07-08 09:47
---
[hera 2026-07-08, pre-compact checkpoint] TASK-059: fix round 10b39a3 regated green (25/25, fresh registry/sidecar/lifecycle); delta requested from review-a5-vonu (incl. residual-corner probe: retirement drift with no rotation archive yet). TASK-063: round-2 61ab259 regated green (25/25 incl executable suite); delta requested from review-063-magi (incl. B1 order-guarantee probe). Both workers held (wave-a5-lina, task063-taro via vibe); both reviewers standing by. On CLEAN deltas: merge pipeline as standard (no-ff, post-merge gate from repo root — A5 second-lander note: 063 touches no herder internals so overlap is nil, merge order free). Vibe is compacting too (owner-called, steer preserved). Hera compacting now per owner.
---
<!-- COMMENTS:END -->

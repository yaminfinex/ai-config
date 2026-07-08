---
id: TASK-059
title: 'wave A5: registry rotation + list --all archive consultation (spec 5.1 growth)'
status: In Progress
assignee: []
created_date: '2026-07-08 05:55'
updated_date: '2026-07-08 09:21'
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
<!-- COMMENTS:END -->

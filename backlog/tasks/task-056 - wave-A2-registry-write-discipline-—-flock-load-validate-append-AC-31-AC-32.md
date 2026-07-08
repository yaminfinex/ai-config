---
id: TASK-056
title: >-
  wave A2: registry write discipline — flock + load-validate-append (AC-31,
  AC-32)
status: In Progress
assignee: []
created_date: '2026-07-08 05:55'
updated_date: '2026-07-08 07:03'
labels: []
dependencies: []
priority: high
ordinal: 56000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Plan unit A2 (spec-plan-wave-a.md). Single write path: exclusive flock on live file; load -> validate (label uniqueness over full projection; owned-fields-only appends; idempotent no-ops) -> append -> fsync. Refuse-unlocked where flock unavailable. Reroute every existing writer (spawn/enroll/cull/sidecar) through it — mechanical, behaviour frozen by A1 projection tests. Tests: concurrent label claims (two processes one winner), stale-enrichment-cannot-revert-rename, heartbeat-cannot-resurrect-culled. Depends: A1 (TASK-055). Adversarial review mandatory (locking = engine risk).
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 05:59
---
x-ref from TASK-046 adversarial review F3 (pre-existing, not a regression): registry.Append is O_APPEND with NO flock; reconcile (and spawn/enroll/rename) do non-atomic read-classify-append, so a concurrent full-row writer between Load and Append is silently reverted by latest-wins. This unit (A2 flock + load-validate-append) is the designed fix — make sure reconcile's write path reroutes through it too, not just the original writers.
---

created: 2026-07-08 06:31
---
Dispatching: brief at napkins/run-herder-dx/brief-wave-a2.md carries spec-ravus transition ruling verbatim (writers validate v2-in-flock; read-to-write vs read-to-display split with validator-is-truth arbiter; v2-event rows with legacy-view derivation FIRST — no dual-write, no hybrid rows; pre-A3 rows grandfathered by absent-node rule). Adversarial review mandatory.
---

created: 2026-07-08 07:03
---
[hera 2026-07-08] Worker wave-a2-bumo DONE (#7436): commit e0c3122. HERA GATE GREEN from worktree ~/.herdr/worktrees/ai-config/wave-a2-write-discipline: go vet+test tools/herder AND tools/bottle pass, all 22 check-*.sh PASS sequentially (incl new check-registry-write-discipline.sh). Diff scope matches brief: new registry/write.go locked path, legacy-view derivation in registry.go, every writer rerouted (spawn/enroll/cull/rename/reconcile/fork/resume/sidecar), goldens updated for v2 row shape. Worker deviation note: cull now returns command failure on lock/write refusal instead of silent success (in-scope tightening). Opus adversarial review dispatched: review-a2-kilo (guid e8175423, own tab), brief napkins/run-herder-dx/brief-review-a2.md — angles incl flock per-OFD trap (two-process test requirement), rotation lock identity, bypass-writer grep, no-op appends no row, torn-row-vs-quarantine interaction. Sequencing: F1 (TASK-045) ahead in pipeline; A2 merges second and takes conflict-check + regate (both touch sidecar.go).
---
<!-- COMMENTS:END -->

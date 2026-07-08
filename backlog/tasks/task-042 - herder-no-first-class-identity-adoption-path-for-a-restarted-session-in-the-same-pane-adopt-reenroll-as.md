---
id: TASK-042
title: >-
  identity adoption for a restarted session: respec as enroll (new guid) +
  rename --take-from + retire — guid reuse violates spec D1
status: To Do
assignee: []
created_date: '2026-07-08 04:45'
updated_date: '2026-07-08 05:29'
labels: []
dependencies: []
priority: medium
ordinal: 42000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Live hit (hera restart, 2026-07-08): the orchestrator session died (post-upgrade dead hooks) and a fresh claude session started in the SAME pane under a new bus name (dora). Assuming the retired identity took a three-step off-label dance: (1) hcom start --as hera to reclaim the bus name (only hcom can do this; herder rename only relabels a row + pane title, and cannot re-point a dead row at a live session); (2) herder enroll with manually-set HERDER_GUID=<old full guid> HERDER_LABEL/HERDER_ROLE env to adopt the old guid — the env-input path is designed for spawn bootstraps, not interactive adoption, and requires digging the full guid out of herder list --all --json (JSONL); (3) a SECOND enroll to fix the bus coordinate (see companion task on stale HCOM_INSTANCE_NAME). Proposal: herder adopt <target> — run from inside a pane; reclaims the target guid+label+role for the current pane/session, reclaims the hcom bus name (hcom start --as) or verifies it matches, retires stale rows via the TASK-035 guards, appends the row with LIVE coordinates. Retire-on-reenroll itself worked perfectly in production (retired @dora/@vore/@zero rows in one shot).
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 05:04
---
vibe (herdr-0.7.3 audit, bus #5629, applied by hera): Upstream #620/#684/#943/#712/#765 (see TASK-050) land directly on this task's territory. Re-run the restart repro on 0.7.3 before designing adopt/reenroll-as; the herdr-side identity half may now work, leaving only the herder-registry adoption affordance to build.
---

created: 2026-07-08 05:29
---
RESPEC per spec D1 / §3.1-1 (spec-ravu #6043, hera concurs — flagged in the spec review as flag 2): drop the adopt-same-guid design; a restarted process is a NEW transcript and must get a new guid. The composite affordance is: herder enroll (new guid) -> rename <new> <label> --take-from <old> (explicit lease transfer) -> retire <old>. Today's live runbook (guid 404a13df reused for hera's new transcript) was expedient but spec-illegal; do not repeat post-ratification. Wrapping the composite as a single  convenience remains open — but it composes the three verbs, never re-keys a guid.
---
<!-- COMMENTS:END -->

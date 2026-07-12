---
id: TASK-043
title: >-
  herder enroll: records hcom bus name from stale HCOM_INSTANCE_NAME env instead
  of live hcom identity — silently misroutes send after hcom start --as
status: Done
assignee: []
created_date: '2026-07-08 04:45'
updated_date: '2026-07-12 07:49'
labels: []
dependencies: []
priority: medium
ordinal: 43000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Live hit (hera restart, 2026-07-08): after reclaiming the bus name with hcom start --as hera, herder enroll wrote the new registry row with hcom_name=dora — it trusts the HCOM_INSTANCE_NAME env var, which is frozen at process launch and goes stale the moment the session reclaims a different identity. Consequence: herder send to that row would target a stopped bus name (@dora) and fail or misroute, silently. Workaround used: re-enroll with HCOM_INSTANCE_NAME=hera overridden on the command line. Fix: enroll (and any row-writing path) should resolve the LIVE bus identity from hcom (e.g. hcom list --json for the current session/process id) and prefer it over env, or at least cross-check env vs live and warn on mismatch. Same disease class as TASK-035/041: registry trusting launch-time coordinates that drift.
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 05:04
---
vibe (herdr-0.7.3 audit, bus #5629, applied by hera): no upstream change touches hcom env-var staleness — expect unaffected; TASK-050 (NEW-4) includes a cheap re-verify to confirm.
---

created: 2026-07-08 10:19
---
0.7.3 re-verify complete (TASK-050 controlled restart): CONFIRMED STILL BROKEN, as predicted — no upstream change touches env staleness. Fresh repro: live bus identity hera (after hcom start --as hera), env frozen at HCOM_INSTANCE_NAME=mono; herder enroll wrote row 0c607d43 with hcom_name=mono. Workaround re-verified: HCOM_INSTANCE_NAME=hera herder enroll wrote row bbbc84c2 with hcom_name=hera (bus link confirmed: herder list shows BUS=@hera). New wrinkle for the fix design: the label-uniqueness check runs BEFORE pane-supersession retirement, so the corrective re-enroll cannot reuse the label the broken row holds — it needs a variant label, then rename. An enroll that resolved live hcom identity (or cross-checked env vs live) would have avoided the entire second enroll.
---
<!-- COMMENTS:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 enroll resolves the LIVE hcom bus identity (from hcom, not env) or cross-checks env vs live and refuses/warns on mismatch — repro from comment 2 passes: after hcom start --as X with stale HCOM_INSTANCE_NAME=Y, the row records hcom_name=X
- [x] #2 the corrective path needs no variant-label dance: fixing a wrong-name row does not require enrolling under a throwaway label first (or the constraint is documented in the refusal text)
- [x] #3 suite covers stale-env enroll and the mismatch refusal/warning
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
SHIPPED in identity-integrity unit A1, merged a1c5acd (branch task-a1-identity-integrity, commits 1c7ced2 + f210777). enroll now IGNORES launch-time HCOM_INSTANCE_NAME and resolves the joined live bus row from session/process/pane evidence (new internal/hcomidentity package); when live identity is unprovable it writes EMPTY hcom_name + hcom_verified:false + a repair instruction — never a guess. AC2: the variant-label dance is gone — re-running herder enroll on the EXISTING guid is the repair/rebind affordance (same label reusable), with SID corroboration so an inherited HERDER_GUID in a different session refuses rather than re-keying. Multi-correlate evidence (caller-own HCOM_PROCESS_ID + env HERDR_PANE_ID first + canonical pane second) matches what hcom actually records; conflicting correlates fail closed. AC3: stale-env enroll golden (records bus-live, not stale-launch-name), unprovable-identity golden, re-enroll refusal/repair goldens, hcomidentity unit tests. Adversarial review (opus): round-1 REQUEST-CHANGES F1-F5 all fixed, delta APPROVE; F1 blocker (canonical-vs-env pane false-refusal) was confirmed live on the orchestrator's own session before the fix round. Gates: independent 4-module + 53-script battery green from the worktree; post-merge battery green on main.
<!-- SECTION:NOTES:END -->

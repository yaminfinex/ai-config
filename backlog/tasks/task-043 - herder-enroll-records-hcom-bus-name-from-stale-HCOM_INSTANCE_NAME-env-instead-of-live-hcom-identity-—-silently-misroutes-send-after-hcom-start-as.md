---
id: TASK-043
title: >-
  herder enroll: records hcom bus name from stale HCOM_INSTANCE_NAME env instead
  of live hcom identity — silently misroutes send after hcom start --as
status: To Do
assignee: []
created_date: '2026-07-08 04:45'
updated_date: '2026-07-08 23:41'
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
- [ ] #1 enroll resolves the LIVE hcom bus identity (from hcom, not env) or cross-checks env vs live and refuses/warns on mismatch — repro from comment 2 passes: after hcom start --as X with stale HCOM_INSTANCE_NAME=Y, the row records hcom_name=X
- [ ] #2 the corrective path needs no variant-label dance: fixing a wrong-name row does not require enrolling under a throwaway label first (or the constraint is documented in the refusal text)
- [ ] #3 suite covers stale-env enroll and the mismatch refusal/warning
<!-- AC:END -->

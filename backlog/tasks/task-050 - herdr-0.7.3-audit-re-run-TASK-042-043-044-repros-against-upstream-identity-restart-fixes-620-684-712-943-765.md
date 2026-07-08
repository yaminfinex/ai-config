---
id: TASK-050
title: >-
  herdr-0.7.3 audit: re-run TASK-042/043/044 repros against upstream
  identity/restart fixes (#620/#684/#712/#943/#765)
status: Done
assignee: []
created_date: '2026-07-08 05:04'
updated_date: '2026-07-08 10:19'
labels: []
dependencies: []
priority: high
ordinal: 50000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Filed by hera on behalf of vibe (herdr-0.7.3 upgrade audit, bus #5629) — applied verbatim per single-writer protocol.

0.7.x shipped exactly-on-target fixes: Claude Code session restore now accepts real /clear, /resume, and compacted identity changes (#620); hook sequence guards re-anchor after fresh session refs / proven process exits (#684); root-agent restore ignores child-process session overwrites (#712); foreground session reports can replace stale saved references (#943); official hook integrations scope lifecycle reports to the intended root process (#765). Re-run each hera-restart repro on 0.7.3 BEFORE building local machinery: TASK-042 (identity adoption) may shrink to a registry-side affordance; TASK-043 (stale HCOM_INSTANCE_NAME env) is hcom-side and likely UNAFFECTED — re-verify only; TASK-044 (list LIVE=gone for live manual session) is possibly subsumed by TASK-046's parser fix — re-test after 046 lands. Outcome per task: close, re-scope, or confirm-still-broken with fresh evidence. Blocker: TASK-046 for the 044 leg only.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Controlled restart executing now (wave A + 063 both landed; vibe lane empty, vibe GO on bus #10829). Test subject: hera's own session. Boot brief + repro checklist + pre-restart evidence: napkins/run-herder-dx/restart-050-brief.md. Pre-restart facts: herdr 0.7.3; outgoing session env HCOM_INSTANCE_NAME=dora (stale, itself 043 evidence); row 404a13df unseated/gone (D9 dormant, expected). Legs: 042 composite (enroll new guid -> rename --take-from -> retire; absent verbs recorded as missing affordances) + 043 enroll env-staleness re-verify. Restart mechanics driven by vibe: /exit outgoing claude in pane w6554208c1918a12:p1, fresh claude in same pane pointed at the brief.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 06:31
---
044 leg RESOLVED by TASK-046 (production verification #6881) — no separate fix needed. Remaining legs: 042 (adoption composite affordance) + 043 (enroll env staleness re-verify) need a controlled session-restart repro; heras own session is the natural test subject — coordinate with hera, schedule at the next natural restart or after wave-A settles rather than forcing one mid-wave.
---

created: 2026-07-08 10:19
---
Controlled restart EXECUTED — this comment is written by the replacement session (now @hera, guid bbbc84c2). Full evidence in napkins/run-herder-dx/restart-050-brief.md (post-restart appendix). Per-leg outcomes:

042 leg — RE-SCOPE. Upstream 0.7.x fixed the herdr/hcom half: the pane shim (HERDER_SHIM=1) minted a FRESH identity at launch (HCOM_INSTANCE_NAME=mono, new HCOM_PROCESS_ID d49ed878) — the stale-env-inheritance hypothesis from the accident is FALSIFIED; and hcom 0.7.23 dropped the dead hera bus row entirely (no stale row lingered; #620/#684/#943 behaving as advertised). hcom start --as hera reclaimed the bus name cleanly. But NOTHING auto-adopts registry-side: no row for the new session, manual reclamation required end-to-end, and every composite verb is missing — rename has no --take-from; herder retire does not exist (unknown command); enroll refuses label hera held by DEAD guid 404a13df with text: label "hera" already belongs to active guid 404a13df (an unseated/live_status=gone row counts as "active" for label uniqueness). With the cull bug (TASK-069) the label hera is UNRECLAIMABLE by any current verb. Final identity: guid bbbc84c2, label hera-restart-050b, hcom_name=hera, bus @hera, pane w6554208c1918a12:p1 (pane ids did NOT reshuffle).

043 leg — CONFIRM-STILL-BROKEN, fresh evidence. First enroll (row 0c607d43) recorded hcom_name=mono from the frozen launch env despite live bus identity hera. Workaround re-verified: HCOM_INSTANCE_NAME=hera herder enroll wrote row bbbc84c2 with hcom_name=hera. Wrinkle: same-label re-enroll is refused (label-uniqueness check runs BEFORE pane-supersession retirement), so the workaround needs a variant label when the first enroll already took the intended one.

044 leg — already resolved via TASK-046 (comment 1), not re-run.

NEW defect found during the composite attempt: herder cull pane_not_found path (with and without --force) prints "still marked closed in registry", exits 0, but appends NO closed record — filed as TASK-069. Closing this audit task; residual work lives on 042/043/069.
---
<!-- COMMENTS:END -->

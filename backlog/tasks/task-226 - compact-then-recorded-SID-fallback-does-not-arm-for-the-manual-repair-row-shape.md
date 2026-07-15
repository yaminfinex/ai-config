---
id: TASK-226
title: >-
  compact --then: recorded-SID fallback does not arm for the manual-repair row
  shape
status: In Progress
assignee: []
created_date: '2026-07-15 04:48'
updated_date: '2026-07-15 04:49'
labels: []
dependencies: []
priority: medium
ordinal: 225500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
FIELD DEFECT in the adoption-binding fix, live specimen available. A hand-recovered session whose registry row was repaired by MANUAL pinned-env re-enroll (on the pre-fix binary) still gets the bus-identity refusal from compact --then when HCOM_SESSION_ID is absent from the calling env — the recorded-SID verification path that the committed golden proves (then_dryrun_repaired exits 0) does not arm for this row shape.

EVIDENCE (reporter re-verified on the current binary, canonical checkout level with origin/main past the fix train): env-absent 'herder compact --dry-run --then probe' exits 2 with byte-exact refusal: '--then bus identity mismatch: no joined bus row matches the calling session, process, or pane. Rerun herder enroll from this session to repair its bus binding, then retry.' Live specimen row guid 7ef0b17d: sids=[{sid present, source: harvest}], provenance={mechanism: enroll, tool_session_id present}, seat={kind herdr, pane_id present, hcom_name present, confirmed_at recent}, NO launch_context in projection, state seated, continuity confirmed. With HCOM_SESSION_ID set in env, the same invocation SUCCEEDS — so verification is env-driven and the row fallback never fires.

INVESTIGATE (hypotheses, verify do not assume): (a) the arming conjunction — recorded-SID injection requires resolveSelfRow proof AND seat.hcom_verified==true AND ambient SessionID empty; determine which conjunct fails for this row (hcom_verified flag state as written by the OLD enroll binary is the prime suspect; sids[].source filtering and the resolveSelfRow env/pane requirements are the others). (b) Whether the passing golden fixture differs from the field shape in exactly that conjunct — if so the fixture is proving a narrower claim than the DONE report stated.

FIX SCOPE: (1) make the recorded-SID fallback arm for legitimately repaired rows regardless of WHICH binary performed the repair (never weaken identity resolution — the Resolve proof classes and the fail-closed discipline stand; the recorded row binding must still be proof-backed); (2) refusal cause-split: distinguish row-unbound (remedy: re-enroll) from row-bound-but-not-arming (name the actual missing piece) — the current text prescribes re-enroll to an already-repaired row, which misdirects the operator. (3) Regression: committed test whose fixture matches the FIELD shape (harvest-sourced SID, no launch_context, old-repair flag state), red before fix.

CONSTRAINTS: live row 7ef0b17d is READ-ONLY evidence (probes use isolated state; the reporter offers re-runs on request through the orchestrator). Workaround exists (HCOM_SESSION_ID in env), so no fleet emergency.
<!-- SECTION:DESCRIPTION:END -->

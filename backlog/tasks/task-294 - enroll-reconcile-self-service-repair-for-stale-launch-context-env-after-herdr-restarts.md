---
id: TASK-294
title: >-
  enroll/reconcile: self-service repair for stale launch-context env after herdr
  restarts
status: Done
assignee: []
created_date: '2026-07-20 05:19'
updated_date: '2026-07-24 22:24'
labels: []
dependencies: []
ordinal: 293500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field-report (second deployment): long-lived sessions keep HERDR_PANE_ID/HERDER_GUID from a dead herdr epoch. spawn's refusal suggests the inline-override recovery (good), but enroll's launch_context_pane_conflict has NO self-service repair — operators had to bus stop+start (which RENAMES the agent each time, churning identity) and hand-supply HCOM_SESSION_ID, because enroll's evidence is env-only and interactive sessions do not carry those vars into shell subprocesses. Fixes per report: (a) enroll --session-id / --hcom-name explicit-evidence flags; (b) a reconcile path that repairs a stale-but-nonempty launch context against unambiguous live evidence; (c) UPSTREAM (bus) candidate: rebind-without-rename. Design relevance: this is field evidence for the credential/identity acquisition ruling in the API decision sheet — env-only evidence dies on long-lived sessions; explicit-evidence recovery must exist.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Done 2026-07-24: asks (a) and (b) both closed on main (see notes above — 2dd6659 repair spine, 7c5e692 explicit-evidence flags, both gated green on the implementing box). Ask (c) rebind-without-rename is an UPSTREAM candidate tracked on the TASK-029 ledger, not this task's deliverable; the enroll fix removes the day-to-day need for it. Rolled out here at pull 28ea318; solo-box mode ruled same day, no local re-gate (origin box battery green per commit records).
<!-- SECTION:NOTES:END -->

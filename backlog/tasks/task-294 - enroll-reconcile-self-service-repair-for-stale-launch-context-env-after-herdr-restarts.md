---
id: TASK-294
title: >-
  enroll/reconcile: self-service repair for stale launch-context env after herdr
  restarts
status: To Do
assignee: []
created_date: '2026-07-20 05:19'
updated_date: '2026-07-22 01:05'
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
Partial closure 2026-07-22: ask (b) — repair of a stale-but-nonempty launch context against unambiguous live evidence — is CLOSED by main 2dd6659 (RepairLaunchContext consults a live herdr pane snapshot; a recorded pane provably absent from a readable snapshot is repaired to the verified live pane and the process binding is re-derived; still-live pane stays a refused collision; unreadable snapshot stays strict). Rides the shared seat-completion spine, so enroll and reconcile both benefit. Remaining scope: (a) enroll --session-id/--hcom-name explicit-evidence flags (env-only evidence still dies on long-lived interactive sessions); (c) rebind-without-rename stays on the TASK-029 upstream ledger. Gate: 63/63 battery green on main at 2dd6659.

Closure 2026-07-24: ask (a) — enroll --session-id/--hcom-name explicit-evidence flags — is CLOSED. When the flags are supplied, enroll drops the frozen ambient HCOM_SESSION_ID/HCOM_PROCESS_ID (dead launch epoch on a resumed/long-lived session) and feeds the explicit name/session into the SAME hcomidentity.Resolve engine as evidence, corroborated against the live roster; a stale/wrong assertion fails closed with an actionable message rather than silently degrading or re-seating onto another live agent. This re-seats the SAME guid in place, so it repairs a wedged pane without the bus stop+start that renames the agent (the field-report churn). The corroborated evidence also flows into the credential-path VerifySelectedBus. Three new enrollcmd tests (resume-wedge refusal without flags → verified with flags; fail-closed on an unjoined --hcom-name; token validation); full herder battery green. Only remaining scope is (c), the upstream rebind-without-rename on the TASK-029 ledger — this local change removes the day-to-day need for it.
<!-- SECTION:NOTES:END -->

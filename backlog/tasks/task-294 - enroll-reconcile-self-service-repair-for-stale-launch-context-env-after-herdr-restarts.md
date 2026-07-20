---
id: TASK-294
title: >-
  enroll/reconcile: self-service repair for stale launch-context env after herdr
  restarts
status: To Do
assignee: []
created_date: '2026-07-20 05:19'
labels: []
dependencies: []
ordinal: 293500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field-report (second deployment): long-lived sessions keep HERDR_PANE_ID/HERDER_GUID from a dead herdr epoch. spawn's refusal suggests the inline-override recovery (good), but enroll's launch_context_pane_conflict has NO self-service repair — operators had to bus stop+start (which RENAMES the agent each time, churning identity) and hand-supply HCOM_SESSION_ID, because enroll's evidence is env-only and interactive sessions do not carry those vars into shell subprocesses. Fixes per report: (a) enroll --session-id / --hcom-name explicit-evidence flags; (b) a reconcile path that repairs a stale-but-nonempty launch context against unambiguous live evidence; (c) UPSTREAM (bus) candidate: rebind-without-rename. Design relevance: this is field evidence for the credential/identity acquisition ruling in the API decision sheet — env-only evidence dies on long-lived sessions; explicit-evidence recovery must exist.
<!-- SECTION:DESCRIPTION:END -->

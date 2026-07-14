---
id: TASK-199
title: >-
  Characterize hcom-native pi integration before pi U1 — native-vs-custom
  worth-it re-check
status: In Progress
assignee: []
created_date: '2026-07-14 00:44'
updated_date: '2026-07-14 00:45'
labels: []
dependencies: []
ordinal: 198000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
BLOCKING pi implementation (U1 does not file until this lands). hcom 0.7.23 advertises pi as an AUTOMATIC integration (hcom pi, resume, fork; hooks-based: mid-turn injection + idle wake) — neither the pi demo nor the pi first-class design evaluated it; the design rebuilt inbound delivery custom (extension driver + journal + lease + spool) inheriting the grok pattern, but grok is ABSENT from hcom tool list while pi is PRESENT. Unit: research/characterization. In FULL isolation (isolated HCOM_DIR + isolated pi home per the demo isolation pattern; NEVER live ~/.hcom or live homes): empirically characterize hcom pi against pi 0.80.6 — launch mechanism (what hcom installs/wraps for pi), inbound delivery truth (mid-turn injection, idle wake, ordering, duplicate behavior), identity/status/transcript fidelity, crash/restart behavior, version compat (hcom 0.7.23 vintage vs current pi), credential/env hygiene of the native path. OUTPUT: filled evaluation per docs/new-harness-onboarding.md new SECTION 0, and a native-vs-custom decision record: either the pi design DR-2 inbound machine collapses to native-under-herder-launch-contract (design amendment follows), or the native path is documented inadequate (specific gaps vs the design requirements: durable exactly-once-ish delivery, provider credential scoping, lifecycle authority) and the design stands WITH the evaluation recorded. Everything outside DR-2 (launch contract, managed home, provider pinning, pinned install, operator capability lane) is unaffected either way.
<!-- SECTION:DESCRIPTION:END -->

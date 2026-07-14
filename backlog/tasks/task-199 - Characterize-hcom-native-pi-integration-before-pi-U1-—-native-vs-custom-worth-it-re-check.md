---
id: TASK-199
title: >-
  Characterize hcom-native pi integration before pi U1 — native-vs-custom
  worth-it re-check
status: Done
assignee: []
created_date: '2026-07-14 00:44'
updated_date: '2026-07-14 01:18'
labels: []
dependencies: []
ordinal: 198000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
BLOCKING pi implementation (U1 does not file until this lands). hcom 0.7.23 advertises pi as an AUTOMATIC integration (hcom pi, resume, fork; hooks-based: mid-turn injection + idle wake) — neither the pi demo nor the pi first-class design evaluated it; the design rebuilt inbound delivery custom (extension driver + journal + lease + spool) inheriting the grok pattern, but grok is ABSENT from hcom tool list while pi is PRESENT. Unit: research/characterization. In FULL isolation (isolated HCOM_DIR + isolated pi home per the demo isolation pattern; NEVER live ~/.hcom or live homes): empirically characterize hcom pi against pi 0.80.6 — launch mechanism (what hcom installs/wraps for pi), inbound delivery truth (mid-turn injection, idle wake, ordering, duplicate behavior), identity/status/transcript fidelity, crash/restart behavior, version compat (hcom 0.7.23 vintage vs current pi), credential/env hygiene of the native path. OUTPUT: filled evaluation per docs/new-harness-onboarding.md new SECTION 0, and a native-vs-custom decision record: either the pi design DR-2 inbound machine collapses to native-under-herder-launch-contract (design amendment follows), or the native path is documented inadequate (specific gaps vs the design requirements: durable exactly-once-ish delivery, provider credential scoping, lifecycle authority) and the design stands WITH the evaluation recorded. Everything outside DR-2 (launch contract, managed home, provider pinning, pinned install, operator capability lane) is unaffected either way.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
MERGED a1e8c49 (head 0f4fbcd, 2 commits, docs-only +307). Decision: keep DR-2 custom delivery — native hcom-pi (0.7.23 x pi 0.80.6) verified working for idle wake/ordering/identity/transcript/resume but acks at injection; crash-after-ack = falsely-complete receipt, no replay (orchestrator-verified: source 4cef94de + probe2 artifacts). Fix round 1 re-grounded the decision per incumbent P1: settlement-ack fork disclosed and defeated (multi-turn correlation needs DR-2's journal), decision re-anchored on native-absent herder-specific gaps (epoch fencing, progress lease, capability lanes, lifecycle authority); provider scope moved to launch contract; hygiene applied. Dual APPROVE at 0f4fbcd (incumbent zunu + calibration rafo). pi U1 now gates ONLY on owner sign-off (items 7+8). Upstream candidate: hcom pi extension acks at injection not settlement.
<!-- SECTION:NOTES:END -->

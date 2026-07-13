---
id: TASK-185
title: 'sesh: grok session support (interacts with the managed GROK_HOME decision)'
status: To Do
assignee: []
created_date: '2026-07-13 06:21'
labels: []
dependencies: []
priority: medium
ordinal: 184000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner ask (2026-07-13): sesh should support grok sessions (store/resume), alongside claude/codex. KNOWN TENSION to resolve in design: grok is a FULLY HERDER-MANAGED family — sessions live under the herder state root (<state>/grok-home), NOT live ~/.grok, and the manual-CLI vs herder-seat homes differ by design (three drifts: home, pinned binary, auth source). sesh must decide which home(s) it indexes, whether manual ~/.grok sessions and herder-seat sessions are one namespace or two, and how resume TARGETS a home (a resumed session must re-enter the SAME home/contract it was recorded under — resuming a herder-seat session into live ~/.grok would break the quarantine). Also feeds the owner's multi-account direction: different state roots per account with sesh resume bridging session fungibility across accounts. Coordinate with TASK-170 U3 (lifecycle/resume seams, in flight) and the sesh lane owner.
<!-- SECTION:DESCRIPTION:END -->

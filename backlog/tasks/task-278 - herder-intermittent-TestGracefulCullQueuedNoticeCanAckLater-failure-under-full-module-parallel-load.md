---
id: TASK-278
title: >-
  herder: intermittent TestGracefulCullQueuedNoticeCanAckLater failure under
  full-module parallel load
status: To Do
assignee: []
created_date: '2026-07-17 09:07'
labels:
  - herder
  - flake
dependencies: []
priority: low
ordinal: 277500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Observed once by an adversarial reviewer running full-module go test ./... on an isolated ca2aa6a copy: internal/cullcmd TestGracefulCullQueuedNoticeCanAckLater FAILed once. Ruled NOT attributable to the break-glass delta on evidence: passes 3/3 in isolation (0.097s), ca2aa6a full-module 2/3 subsequent runs green, 67e71e7 full-module 3/3 green; cullcmd does not link repaircmd (the only new production code) and the delta's registry.go change moved cullcmd's linked surface toward main (byte-identical). Signature resembles the TASK-223 TempDir/parallelism family (load-sensitive, passes in isolation) but it is a DIFFERENT test in a different package — deliberately NOT folded into TASK-223. Failure body was not captured (grep-filtered). Reproduce under parallel full-module load, capture the assertion, and fix the race or isolate the test.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Failure reproduced under full-module parallel load with the assertion body captured
- [ ] #2 Root cause identified (shared temp/bus state, timing, or ack-window race) and fixed or the test made isolation-robust
<!-- AC:END -->

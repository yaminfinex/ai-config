---
id: TASK-172
title: 'sesh — T-0: naming + capability rename (sesh, tag:sesh, infinex.xyz/cap/sesh)'
status: Done
assignee: []
created_date: '2026-07-13 00:49'
updated_date: '2026-07-13 01:01'
labels:
  - sesh
dependencies: []
priority: high
ordinal: 171000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Per ratified design docs/design/2026-07-12-sesh-store-served-distribution.md §1 (DP-1). Must land before any tsnet rollout; no deployed grant exists, so no migration path needed.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Constant in internal/store/listen_tsnet.go, help text in internal/cli/root.go, tests/check-deploy-artifacts.sh assertions, README (9 refs), etc/install-ship.sh usage renamed
- [x] #2 grep for sesh-store and sesh.dev/cap returns only historical backlog/design docs
- [x] #3 go test ./... and check scripts green
<!-- AC:END -->

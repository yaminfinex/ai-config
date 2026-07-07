---
id: TASK-008
title: >-
  bin/herder: prefer a Go toolchain satisfying go.mod (fixes 9 red contract
  suites)
status: In Progress
assignee: []
created_date: '2026-07-07 06:33'
updated_date: '2026-07-07 06:51'
labels:
  - run-herder-dx
dependencies: []
priority: high
ordinal: 8000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
9 contract suites (enroll/fork/help/launch/list/rename/resume/spawn/wait) are red on main: their hermetic env -i PATH only reaches /usr/bin/go 1.22, fake HOME defeats bin/herder's build cache, and go1.22 cannot fetch the go1.26 toolchain, so bin/herder exits 1 inside every case. Accepted as known-red baseline for run-herder-bootstrap (verified identical on main and unit branches). Fix: bin/herder wrapper prefers a go satisfying go.mod (e.g. mise x go), which also un-blinds the suites.
<!-- SECTION:DESCRIPTION:END -->

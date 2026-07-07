---
id: TASK-008
title: >-
  bin/herder: prefer a Go toolchain satisfying go.mod (fixes 9 red contract
  suites)
status: Done
assignee: []
created_date: '2026-07-07 06:33'
updated_date: '2026-07-07 07:40'
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit f1dd2cc (unit-a-bin-herder, merged 4468676). bin/herder version-checks command -v go against go.mod (toolchain directive wins, else go line); on mismatch probes mise where go (5s timeout) + scans mise install roots incl. one derived from the mise binary prefix (that last un-blinds env -i fake-HOME suites). Builds pin GOTOOLCHAIN=local — no build can stall on a toolchain fetch; no satisfying toolchain = fast explicit error + remedy. RESULT: the 9 environmentally-red contract suites (enroll/fork/help/launch/list/rename/resume/spawn/wait) all green on this box; battery 16/16 after wave-1 integration. Sliding door: auto-download path removed in favour of no-hang doctrine. Twin fix for bin/bottle → TASK-015.
<!-- SECTION:NOTES:END -->

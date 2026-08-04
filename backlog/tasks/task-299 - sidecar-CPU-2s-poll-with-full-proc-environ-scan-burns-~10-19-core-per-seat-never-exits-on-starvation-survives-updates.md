---
id: TASK-299
title: >-
  sidecar CPU: 2s poll with full /proc environ scan burns ~10-19% core per seat,
  never exits on starvation, survives updates
status: To Do
assignee: []
created_date: '2026-08-04 02:08'
labels:
  - herder
  - performance
dependencies: []
priority: high
ordinal: 298500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Measured 2026-08-03: ~17 sidecars (12 ubuntu + 5 grace) each burning 10-19% of a core continuously — 2+ cores total, for weeks. Mechanism per 2s tick (sidecarcmd/sidecar.go:174): fork 'hcom list --json' (~80ms CPU), and on pane-correlation miss walk ALL of /proc reading comm+cmdline for ~800 processes plus full environ for every claude/codex-matching process (~108 on this box) via scanProcessEnvirons, plus statusline snapshot writes. Compounding design properties: (a) observeLiveness treats starvation as advisory-only — a sidecar whose seat is observation_gap polls the expensive miss-path forever, exiting only when its launch-wrapper parent dies; (b) herder update never reaps running sidecars — 54 content-hashed old binaries in ~/.cache/herder, sidecars from 4 builds running since Jul 19-25 observed. Fix directions: back the tick off when row is missing/starved (2s -> 30s+), exit after sustained starvation, share one /proc scanner across seats or go event-driven off hcom, make herder update reap/restart old-binary sidecars. Related cleanup done 2026-08-04: dead-seat sidecars (vito, kana) and an 8-day orphaned recursive correlation grep (1462 CPU-min) killed manually.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 steady-state sidecar CPU for an uncorrelated seat is <1% of a core
- [ ] #2 a sidecar whose seat stays starved beyond a bounded window exits and records why
- [ ] #3 herder update leaves no sidecars running from superseded binaries
<!-- AC:END -->

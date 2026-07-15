---
id: TASK-229
title: 'herder spawn --mission <slug>: spawn and join in one step'
status: Done
assignee: []
created_date: '2026-07-15 05:02'
updated_date: '2026-07-15 10:22'
labels:
  - herder
dependencies:
  - TASK-228
priority: high
ordinal: 228500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner directive 2026-07-15 (SUPER HIGH, third of three): sugar over the join verb — 'herder spawn --mission <slug>' spawns the agent and records explicit mission membership in one step. Depends on the join/leave membership mechanics (TASK-228); same row/field shape, no second representation.

Settled: membership recorded via the same mechanism join uses (explicit membership, wins over marker inference); spawn refusal behavior for a bad/unknown slug mirrors join's typed refusal; no membership side effects on spawn failure (failed spawn leaves no membership residue).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 herder spawn --mission <slug> results in a running agent whose herder list --json row carries the same explicit membership shape as a post-hoc join
- [x] #2 Failed spawn leaves no membership residue
- [x] #3 Bad slug refuses with the same typed cause+remedy class as join
- [x] #4 Unit tests cover spawn-with-mission happy path, failure hygiene, and refusal
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged 8a63e71 (--no-ff, head f426135); post-merge battery 60/60 green on main, pushed. Design checkpoint approved pre-code (explicit resolution before any pane/workspace side effect; membership on the initial registered row through central normalization; no join-event append; registration refusal tears down the launched pane confirmed). AC1: initial-row membership {slug,source:explicit} identical to post-hoc join, pinned in list projection AND exact spawn --json wire shape (mutation-proven pin). AC2: no residue on pre-launch refusal or registration refusal; adjudicated boundary — post-registration enrichment failure retains truthful seated membership (nonzero exit, no cleanup append), pinned by regression test. AC3: all four refusal classes byte-identical to join including empty slug (untyped special-case deleted in fix round). AC4: happy path, failure hygiene, refusal table (4 rows), rotation survival, inferred-source refusal via the new call site. Review: incumbent opus fix-list(4) then delta APPROVE, all fixes mutation-verified; calibration seat found 2/4 pre-incumbent. Independent gate + re-gate 60/60.
<!-- SECTION:NOTES:END -->

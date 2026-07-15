---
id: TASK-229
title: 'herder spawn --mission <slug>: spawn and join in one step'
status: In Progress
assignee: []
created_date: '2026-07-15 05:02'
updated_date: '2026-07-15 08:06'
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
- [ ] #1 herder spawn --mission <slug> results in a running agent whose herder list --json row carries the same explicit membership shape as a post-hoc join
- [ ] #2 Failed spawn leaves no membership residue
- [ ] #3 Bad slug refuses with the same typed cause+remedy class as join
- [ ] #4 Unit tests cover spawn-with-mission happy path, failure hygiene, and refusal
<!-- AC:END -->

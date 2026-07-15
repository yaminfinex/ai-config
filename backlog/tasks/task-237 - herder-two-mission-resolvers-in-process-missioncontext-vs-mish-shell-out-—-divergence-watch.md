---
id: TASK-237
title: >-
  herder: two mission resolvers (in-process missioncontext vs mish shell-out) —
  divergence watch
status: To Do
assignee: []
created_date: '2026-07-15 08:16'
updated_date: '2026-07-15 09:58'
labels:
  - herder
dependencies: []
priority: low
ordinal: 236500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Reviewer observation from the raise unit: herder now resolves missions two ways — in-process missioncontext (join/leave/list/registry, deliberately semantics-compatible with mish because mish internals are not importable across modules) and a shell-out to mish resolve (raise; blessed by mish help as the intended machine-consumer path). Both are correct today (line-by-line compatibility was verified in the membership review). This task is the WATCH: when mish resolution semantics change, both herder surfaces must move together. Options to evaluate: extract a shared library module both import; make missioncontext shell out too (cost: mish binary becomes a hard dep of list); or a cross-module contract test that runs both resolvers over the same fixture tree and diffs (cheapest, catches drift mechanically). Recommend the contract test.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A mechanism exists that fails loudly when the two resolvers diverge on the same cwd/fixture tree
<!-- AC:END -->

## Comments

<!-- COMMENTS:BEGIN -->
author: hera
created: 2026-07-15 09:58
---
Concrete instance from the spawn-mission review (incumbent observation, separate-filing per shared-code rule): herder raise --mission resolves through the mish CLI shell-out into a payload string, while spawn --mission and join resolve through in-process missioncontext into registry membership — one flag name, two resolvers, two meanings across verbs. Any behavioral divergence between the two resolution paths shows up as verb-dependent mission semantics. Fix direction when staffed: single resolution authority (or a pinned equivalence contract test across all --mission-bearing verbs).
---
<!-- COMMENTS:END -->

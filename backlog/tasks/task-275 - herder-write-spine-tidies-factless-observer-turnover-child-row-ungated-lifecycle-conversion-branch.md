---
id: TASK-275
title: >-
  herder write-spine tidies: factless observer turnover child row; ungated
  lifecycle conversion branch
status: To Do
assignee: []
created_date: '2026-07-17 07:19'
labels:
  - herder
  - identity-migration
dependencies: []
priority: low
ordinal: 274500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Two non-blocking advisories from the canonical-seat-completion unit's final review, both verified in code by the reviewer with merge authority. (1) The observer turnover CHILD row is minted seated with hcom_verified=true and ZERO binding facts: completion only mints facts for the one completed row, and the legacy exemption in the registry normalizer (empty Bindings + no current row admits) lets the child through factless. Self-heals on the guid's next completion, so harm is bounded — but it is a factless live-verified claim window. Consider minting the child's facts in the turnover batch or narrowing the legacy exemption. (2) lifecyclecmd: the SessionEventFromJSON failure branch after a successful UpdateRawObject encode goes straight to pane teardown without the occupant gate — defensively unreachable (re-parse of just-encoded JSON), but it is the lone unconverted sibling of the completion-failure handler; route it through the same helper (pre-completion, registryApplied=false) for uniformity.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Turnover child row either carries binding facts at mint or the legacy exemption is narrowed, with a pin proving a factless seated live-verified row can no longer be minted on the turnover path
- [ ] #2 Lifecycle conversion-failure branch routed through the occupant-gated completion-failure handler; existing lifecycle preservation pins stay green
<!-- AC:END -->

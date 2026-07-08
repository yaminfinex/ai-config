---
id: TASK-035
title: >-
  herder registry: reused panes accumulate stale 'working' manual rows; pane-id
  resolution picks the wrong bus name
status: To Do
assignee: []
created_date: '2026-07-08 01:20'
updated_date: '2026-07-08 01:20'
labels: []
dependencies: []
priority: medium
ordinal: 35000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Live failure (TASK-034 experiment #2, 2026-07-08): pane w6554208c1918a12-1 has three manual-enroll rows — @hera (live session), @vore, @zero (both from earlier sessions in the same pane, still marked LIVE=working). `herder send <pane-id>` resolved to stale @zero and errored 'not found on bus', when @hera was live and deliverable. Two defects: (1) liveness for manual rows appears pane-based, so a dead session's row stays 'working' forever once the pane is reused; (2) pane-id resolution doesn't disambiguate multiple matching rows — no prefer-bus-live, no newest-first guarantee, no ambiguity error. Fix directions: mark superseded manual rows gone on re-enroll of the same pane; resolution should prefer the row whose bus name is currently joined (hcom list), and error loudly on ambiguity instead of silently picking. Acceptance: reused-pane re-enroll retires prior rows; `herder send <pane-id>` with one live + N stale rows delivers to the live one; ambiguous-with-multiple-live errors with the candidate list; golden/suite coverage for the reused-pane case.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Re-enrolling a reused pane retires (marks gone) prior manual rows for that pane
- [ ] #2 herder send <pane-id> with one bus-live + N stale rows delivers to the live one
- [ ] #3 Multiple bus-live candidates for one pane-id errors loudly with the candidate list (no silent pick)
- [ ] #4 Suite/golden coverage for the reused-pane resolution case
- [ ] #5 Docs/help text updated if resolution semantics are user-visible
<!-- AC:END -->

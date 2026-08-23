---
id: TASK-314
title: >-
  slim-down: liveness plane occupant-first (death rules, targeting, observer
  sweep deletion)
status: To Do
assignee: []
created_date: '2026-08-23 11:53'
labels:
  - herder
  - slimdown
dependencies: []
ordinal: 313500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Unit 7. Depends on unit 2 + owner decisions D-1 and D-4. Scope: IMPL-7. missions/fleet-refit/artifacts (missions repo): conductor playbook herder-slimdown-run-playbook.md governs; scope source: wayfinder herder-slimdown-readiness-package.md
<!-- SECTION:DESCRIPTION:END -->

## Pinned obligations from the adopt/enroll deletion review (2026-08-23)

Two deletion-map REPLACE rows had their old fences deleted in the
adopt/enroll unit with replacement deferred to THIS unit's occupant-based
liveness plane. Neither is keep-list sacred; interim protection is
verb-time resolution + multi-match fail-closed. They must re-emerge here:

- [ ] Occupied-pane mint fence ("one live session per pane"): a fresh
      enroll on an occupied pane currently mints a second row silently.
- [ ] Stale-row hygiene ("dead session never lingers as LIVE"): dead rows
      currently linger until this liveness plane retires them.

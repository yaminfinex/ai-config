---
id: TASK-313
title: 'slim-down: seat-completion ladder deletion + end herder writes into hcom DB'
status: Done
assignee: []
created_date: '2026-08-23 11:53'
labels:
  - herder
  - slimdown
dependencies: []
ordinal: 312500
---

## Teardown reconciliation — 2026-08-24

Done by teardown unit three. Seatcompletion and the herder-to-hcom DB write
path were deleted wholesale.

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Unit 6. Depends on unit 2 + owner decision D-6 (credential call-sites). Scope: IMPL-6. missions/fleet-refit/artifacts (missions repo): conductor playbook herder-slimdown-run-playbook.md governs; scope source: wayfinder herder-slimdown-readiness-package.md
<!-- SECTION:DESCRIPTION:END -->

## Pinned handoffs from the reconcile/repair deletion review (2026-08-23)

- [ ] Delete seatcompletion NarrowFallback: reconcile was its sole
      production setter and is now gone; the field is dead code whose
      removal is mapped to this unit.
- [ ] Opportunistic: hcomidentity.ResolveLiveContext is pre-existing
      zero-caller dead code — delete if this unit touches that file.

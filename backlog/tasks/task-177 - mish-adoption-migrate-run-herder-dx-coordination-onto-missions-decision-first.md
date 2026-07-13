---
id: TASK-177
title: >-
  mish adoption: migrate run-herder-dx coordination onto missions
  (decision-first)
status: Done
assignee: []
created_date: '2026-07-13 01:02'
updated_date: '2026-07-13 01:33'
labels: []
dependencies: []
priority: high
ordinal: 176000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
mish shipped complete (its build run closed with all eleven units merged; binary + skills symlinked; its 8 check scripts run in the house battery). Owner direction: get mish out and migrate this run over to it. DECISION unit first, then a separate migration unit. The decision must rule: (a) what of the live run's coordination substrate migrates into a mission per the ratified mission spec — playbook, standing-orders digest, run journal, per-unit briefs — and what stays where it is (backlog/ board custody in particular: mish has its own backlog-floor gate; double-custody is forbidden); (b) adopt semantics (spec: adopt MOVES, never copies) applied to a LIVE run without disrupting in-flight lanes; (c) slug + mission scaffold shape via the mish CLI; (d) whether napkins/-gitignored artifacts enter mission custody (they become tracked — single-copy risk resolves, but bus/task identifiers in them are run-scoped by doctrine). Decision unit output: a one-page ruling with the migration unit's capture, ACs, and territory fence, owner-confirmable. Constraint: the run stays operational throughout; hera remains the coordination writer during and after migration.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
DECISION DELIVERED AND MERGED (951e1e9, docs/design/mish-run-migration-decision.md, docs-only). Four review rounds (kune, codex-high), final delta APPROVE. The migration unit is CAPTURED IN THE DOC and BLOCKED on owner rulings: (1) identifier boundary — mission-spec amendment exempting opaque artifacts/ contents (recommended) vs redaction manifest; (2) missions repo creation/hosting (dedicated repo recommended); (3) MISSIONS_REPO provisioning; (4) hera push authority on the missions repo; (5) slug confirm (herder-dx recommended); (6) mish binary + skills provisioning (owner precondition, unit installs nothing); (7) sibling-lane coordination. On rulings: file the migration task from the doc's capture (11 ACs incl. typed-baseline continuity gate + cold-resume drill + remote-custody pipeline).
<!-- SECTION:NOTES:END -->

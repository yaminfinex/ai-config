---
id: TASK-021
title: docs/skills hygiene sweep for wave 0/1 changes
status: To Do
assignee: []
created_date: '2026-07-07 07:41'
updated_date: '2026-07-07 07:41'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 21000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
User directive (2026-07-07): docs and skills hygiene is part of definition of done. Retroactive sweep for the wave 0/1 merges: verify tools/herder README + --help text reflect the machine-wide shim takeover (TASK-001), SUBAGENTS/codex addendum behavior (TASK-002/014), fork provenance semantics (TASK-004), bin/herder toolchain+cache behavior (TASK-008/012 — README or header comment), and check skills/orchestrate + any repo skills referencing herder for drift (e.g. gate guidance should mention env -u HERDER_BIN until TASK-019). EXCLUDE tools/herder/docs/delivery-drivers.md (TASK-003 territory, in flight).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every wave-0/1 behavior change is reflected (or verified already correct) in README/--help/docs; list checked surfaces in notes
- [ ] #2 No stale references to pre-TASK-008 wrapper behavior or removed shim semantics remain (grep evidence)
<!-- AC:END -->

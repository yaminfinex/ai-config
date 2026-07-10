---
id: TASK-151
title: >-
  herder spec conformance: resolve, process seats, LOST transition, and send
  receipts
status: To Do
assignee: []
created_date: '2026-07-10 10:15'
labels: []
dependencies: []
references:
  - docs/specs/herder-spec.md
priority: medium
ordinal: 150000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Reconcile standing herder-spec promises that have no complete implementation or task. Decide and implement, or explicitly amend out, the resolve command; end-to-end process/headless seats; the verified-transcript-gone transition to LOST; and send results that identify the guid and report unseating/continuity facts. Existing cull, rename, namespace, and legacy-view gaps remain owned by their current tasks and are out of this capture.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every scoped spec promise is either implemented with contract tests or removed through an explicit spec amendment
- [ ] #2 herder resolve surface and output are pinned, or the spec no longer claims the command
- [ ] #3 Process seats can be created, waited on, sent to, and culled without herdr, or are removed from the v1 contract
- [ ] #4 A verified missing transcript can produce LOST, with lineage/refusal behavior tested, or LOST is removed from the v1 contract
- [ ] #5 Send output reports the resolved guid and specific unseated/continuity state required by the final contract
<!-- AC:END -->

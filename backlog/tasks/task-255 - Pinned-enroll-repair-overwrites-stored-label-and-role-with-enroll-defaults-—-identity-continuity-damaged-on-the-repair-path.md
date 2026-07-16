---
id: TASK-255
title: >-
  Pinned enroll repair overwrites stored label and role with enroll defaults —
  identity continuity damaged on the repair path
status: To Do
assignee: []
created_date: '2026-07-16 00:58'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 254500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Found on the first live run of the repair path (worked otherwise: guid, pane, bus name, SIDs all preserved; atomic repair-first ordering held). The repaired row came back labeled <role-default>-<shortguid> with role=manual, replacing the stored label and role. Label was restored via herder rename (which syncs herdr too); the ROLE remains overwritten and rename does not touch it. Fix: the same-guid repair path must preserve the stored label and role when the caller requests none (explicit --label/--role still win). Red-first fixture: repair a row with a distinctive stored label+role, assert both survive. Check the adoption and core-key rebind paths for the same class while in there.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Same-guid repair preserves stored label and role absent explicit flags (red-first)
- [ ] #2 Core-key rebind and adoption paths audited for the same overwrite class
<!-- AC:END -->

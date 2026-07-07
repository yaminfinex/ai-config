---
id: TASK-016
title: >-
  herder spawn/resume: BuildProvenance spawned_by is the grandparent when
  spawner was itself spawned
status: To Do
assignee: []
created_date: '2026-07-07 07:29'
updated_date: '2026-07-07 08:30'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 16000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Unit D finding (run-herder-dx, TASK-004 fixed fork only): spawncmd's BuildProvenance prefers ambient HERDER_SPAWNED_BY, which in a spawned session names that session's OWN spawner — so a spawn by agent A records A's spawner (grandparent) in the child row, while the env exported to the child (HERDER_SPAWNED_BY=A) is correct: row and env disagree. Candidate fix: BuildProvenance takes spawnedBy explicitly — creator flows (spawn/fork) pass HERDER_GUID, enroll/sidecar keep HERDER_SPAWNED_BY. resume's no-prior-provenance fallback has the same issue (cosmetic — resume preserves existing provenance).
<!-- SECTION:DESCRIPTION:END -->

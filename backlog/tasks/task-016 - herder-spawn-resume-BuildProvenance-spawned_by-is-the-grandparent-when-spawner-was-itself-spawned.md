---
id: TASK-016
title: >-
  herder spawn/resume: BuildProvenance spawned_by is the grandparent when
  spawner was itself spawned
status: In Progress
assignee:
  - unit-h-risa
created_date: '2026-07-07 07:29'
updated_date: '2026-07-07 08:38'
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

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 registry.BuildProvenance takes spawnedBy explicitly; creator flows (spawn, fork, resume no-prior-provenance fallback) pass the session that performed the action (HERDER_GUID, else "user"); enroll/sidecar keep the ambient HERDER_SPAWNED_BY chain byte-identically
- [ ] #2 suite locks row/env agreement: a spawn by a spawned session (env has HERDER_SPAWNED_BY=grandparent + HERDER_GUID=parent) records provenance.spawned_by=parent, equal to the HERDER_SPAWNED_BY the child pane gets
- [ ] #3 fork TASK-004 local override collapses into the new signature with byte-identical fork goldens (no regen expected); fork/resume goldens regenerated ONLY if provenance lines change, every diff line justified in the report
- [ ] #4 go vet/test + full check battery green; no unrelated golden churn
<!-- AC:END -->

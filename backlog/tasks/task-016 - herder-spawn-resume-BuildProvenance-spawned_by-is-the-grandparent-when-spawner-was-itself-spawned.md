---
id: TASK-016
title: >-
  herder spawn/resume: BuildProvenance spawned_by is the grandparent when
  spawner was itself spawned
status: Done
assignee:
  - unit-h-risa
created_date: '2026-07-07 07:29'
updated_date: '2026-07-07 09:02'
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
- [x] #1 registry.BuildProvenance takes spawnedBy explicitly; creator flows (spawn, fork, resume no-prior-provenance fallback) pass the session that performed the action (HERDER_GUID, else "user"); enroll/sidecar keep the ambient HERDER_SPAWNED_BY chain byte-identically
- [x] #2 suite locks row/env agreement: a spawn by a spawned session (env has HERDER_SPAWNED_BY=grandparent + HERDER_GUID=parent) records provenance.spawned_by=parent, equal to the HERDER_SPAWNED_BY the child pane gets
- [x] #3 fork TASK-004 local override collapses into the new signature with byte-identical fork goldens (no regen expected); fork/resume goldens regenerated ONLY if provenance lines change, every diff line justified in the report
- [x] #4 go vet/test + full check battery green; no unrelated golden churn
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit cdb7652. registry.BuildProvenance(mechanism, spawnedBy, tag, cwd, workspaceID): spawnedBy recorded verbatim when non-empty; "" harvests ambient chain (HERDER_SPAWNED_BY -> HERDER_GUID -> user). Creator flows pass the performing session: spawn.go passes its spawnedBy var (HERDER_GUID-else-user, the same value exported to the child), fork + resume no-prior-provenance fallback pass firstNonEmpty(HERDER_GUID, user); fork TASK-004 local override collapsed into the signature. enroll/sidecar pass "" — rows byte-identical. Root cause was BuildProvenance preferring ambient HERDER_SPAWNED_BY, which in a spawned session names the GRANDPARENT of anything it creates, so row and child env disagreed. Verification: spawn_grandparent golden locks row/env agreement (HERDER_SPAWNED_BY=guid-grandpa-00 + HERDER_GUID=guid-parent-000 -> both row spawned_by and exported HERDER_SPAWNED_BY = guid-parent-000); unit tests TestBuildProvenanceSpawnedBy (registry) cover explicit-wins + ambient chain; fork/resume/enroll suites green with ZERO golden regen (fork goldens byte-identical as predicted); battery 16/16.
<!-- SECTION:NOTES:END -->

---
id: TASK-004
title: >-
  herder fork: child provenance.spawned_by records original spawner, not the
  forking session
status: In Progress
assignee: []
created_date: '2026-07-07 05:37'
updated_date: '2026-07-07 06:52'
labels:
  - run-herder-dx
dependencies: []
priority: low
ordinal: 4000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Observed in live smoke (2026-07-07, fork --self shipping): agent A (spawned by orchestrator O) ran herder fork --self; the child row got provenance.forked_from=A (correct) but provenance.spawned_by=O — the ORIGINAL spawner, not the session that executed the fork. Pre-existing herder fork <target> behavior, not introduced by the --self port; forked_from is the authoritative lineage edge meanwhile.

Work: decide intended semantics — spawned_by should plausibly be the guid of the session that ran the fork (A), with O reachable transitively via A own row. Trace where fork builds the child provenance (lifecyclecmd startAndAppend path) and whether spawned_by is inherited from the parent ROW rather than resolved from the caller env (HERDER_GUID). Fix or explicitly document the chosen semantics; add a fixture assertion either way (check-fork-contract.sh happy_live / self_native goldens carry provenance).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Semantics decided and either fixed or documented in fork --help / code comment
- [ ] #2 Fixture asserts spawned_by for a fork executed by a non-original-spawner session
<!-- AC:END -->

---
id: TASK-007
title: 'herder spawn: child PATH puts /usr/bin ahead of mise go'
status: To Do
assignee: []
created_date: '2026-07-07 06:33'
updated_date: '2026-07-07 06:49'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 7000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Observed by TASK-001 worker and reproduced by orchestrator (run-herder-bootstrap): panes spawned via herder get a PATH where /usr/bin/go (1.22) shadows mise go 1.26.4, so the pinned gate battery needs a manual PATH override in every worker. Spawn's login-shell wrapper should end up with mise-activated PATH ordering.
<!-- SECTION:DESCRIPTION:END -->

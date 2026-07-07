---
id: TASK-019
title: >-
  herder tests: neutralize inherited HERDER_BIN/AI_CONFIG_ROOT in remaining
  suites
status: To Do
assignee: []
created_date: '2026-07-07 07:29'
labels: []
dependencies: []
priority: medium
ordinal: 19000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
run-herder-dx gate pitfall (Unit A + orchestrator both bitten): suites that honor inherited HERDER_BIN/AI_CONFIG_ROOT silently drive the MAIN checkout's wrapper/sources when run from a herder-spawned agent in a worktree (every spawned agent has both exported) — wholesale false reds/greens. check-hook-bootstrap + check-spawn-contract now pin them per-suite (TASK-002/Unit-B pattern); sweep the remaining suites to pin AI_CONFIG_ROOT=$REPO_ROOT and ignore inherited HERDER_BIN. Until then: run gates with env -u HERDER_BIN -u AI_CONFIG_ROOT.
<!-- SECTION:DESCRIPTION:END -->

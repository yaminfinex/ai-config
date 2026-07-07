---
id: TASK-007
title: 'herder spawn: child PATH puts /usr/bin ahead of mise go'
status: Done
assignee: []
created_date: '2026-07-07 06:33'
updated_date: '2026-07-07 07:40'
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit 353325b (unit-b-spawn-hygiene, merged d424cab). Root cause verified live: rc-file mise activate is prompt-hook driven — inert in -lic panes with inherited stale __MISE_* state, so /usr/bin/go 1.22 shadowed mise go. Fix: login-shell wrapper pins ${MISE_DATA_DIR:-~/.local/share/mise}/shims to PATH front (shims re-resolve per-dir at call time). eval "$(mise env)" tested and rejected (preserves corrupted ordering under inherited __MISE_DIFF). No mise -> no-op; --no-login-shell unchanged. Live-verified: spawned child resolves go 1.26.4 via mise shims.
<!-- SECTION:NOTES:END -->

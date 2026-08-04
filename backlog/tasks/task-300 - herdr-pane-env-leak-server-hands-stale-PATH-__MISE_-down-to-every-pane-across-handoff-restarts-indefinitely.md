---
id: TASK-300
title: >-
  herdr pane env leak: server hands stale PATH/__MISE_* down to every pane,
  across handoff restarts indefinitely
status: To Do
assignee: []
created_date: '2026-08-04 02:09'
labels:
  - herder
  - herdr
  - upstream
dependencies: []
priority: medium
ordinal: 299500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Root-caused 2026-08-03: the herdr server snapshots its start environment (observed: PATH beginning with a node bin dir, no ai-config entries, plus stale __MISE_DIFF/__MISE_ORIG_PATH/__MISE_SESSION) and every pane inherits it; server handoff (--handoff-import) preserves the snapshot across restarts, so one polluted start poisons panes indefinitely. Interaction: an inherited __MISE_ORIG_PATH lets mise hook-env 'revert' a pane shell toward the polluted PATH on config-boundary cd. The launcher-function redesign (docs/launcher-design.md) makes hand-typed agent launches immune, but every other bare-name resolution in panes still rides the leaked env. Fix directions: (a) upstream herdr — construct pane env fresh (login-shell semantics) instead of inheriting the server snapshot, or at minimum strip __MISE_* ; (b) our side — spawncmd already prepends mise shims in the login wrapper (misePathFix, spawn.go:51); extend the wrapper to unset __MISE_DIFF/__MISE_ORIG_PATH/__MISE_SESSION/MISE_SHELL before the rc chain runs; (c) operational — restart the herdr server from a clean login shell once, WITHOUT carrying handoff env. Also worth an ai-doctor probe: compare herdr server /proc environ PATH against expectations and warn on staleness.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 a fresh pane's rc-initialized shell shows no inherited __MISE_* from the server snapshot
- [ ] #2 documented operational remedy for an already-polluted resident server
<!-- AC:END -->

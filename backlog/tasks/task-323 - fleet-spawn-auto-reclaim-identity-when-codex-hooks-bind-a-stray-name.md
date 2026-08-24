---
id: TASK-323
title: 'fleet spawn: auto-reclaim identity when codex hooks bind a stray name'
status: To Do
assignee: []
created_date: '2026-08-24 09:20'
labels:
  - fleet
  - teardown
dependencies: []
ordinal: 318900
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Two of three codex launches through the fleet preset bound their hcom hooks
to a stray fresh name while the launch row stayed unbound (run-log entries
33 and 35); claude launches bind clean. The wrapper's hooks_bound gate
catches it and refuses with the placement named; the documented remedy
(`hcom start --as <name>` inside the session) has worked both times.
Smallest hardening: when the gate trips and the stray hook-bound row shares
the launch pane, spawn.sh injects the reclaim into the pane, re-verifies,
and only refuses if reclaim fails. Also worth reporting upstream to hcom as
a codex binding defect.
<!-- SECTION:DESCRIPTION:END -->

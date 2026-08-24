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

## Teardown reconciliation — 2026-08-24

Remains open on the surviving fleet wrapper. The doctrine flip documents the
current fail-closed boundary; automatic reclaim, label restamp, managed pane id,
and PID restoration still require implementation and upstream reporting.

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

Addendum 2026-08-24: the reclaim path leaves more debris than the name —
observed at unit-two closeout: the pane label keeps the stray pre-reclaim
name (so cull.sh's exact-label fallback correctly refuses), the reclaimed
row loses launch_context.pane_id (managed close impossible), and hcom kill
loses its tracked PID (hcom stop required). Auto-reclaim should restore
all three: re-stamp the label, carry the pane id into the reclaimed row,
and rebind the tracked PID.

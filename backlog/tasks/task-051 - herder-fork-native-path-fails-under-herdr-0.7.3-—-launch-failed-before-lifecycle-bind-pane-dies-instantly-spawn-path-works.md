---
id: TASK-051
title: >-
  herder fork: native path fails under herdr 0.7.3 — 'launch failed before
  lifecycle bind', pane dies instantly (spawn path works)
status: To Do
assignee: []
created_date: '2026-07-08 05:08'
labels: []
dependencies: []
priority: medium
ordinal: 51000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Live hit (hera, 2026-07-08, herdr 0.7.3): herder fork --self --label spec-hera --split down failed twice with 'herder-lifecycle: launch failed before lifecycle bind'; registry rows were created (c2c0821b, c0f9f401) and self-closed correctly, but the pane died before any bind — herder wait found the terminal not live anywhere. Same session, same epoch: herder spawn works (bash probe AND a claude spawn with --extra-arg --resume <sid> --extra-arg --fork-session, which bound, delivered, and verified — that is the documented WORKAROUND for forking until this is fixed). So the native fork launch path (hcom-fork-based) broke under herdr 0.7.3 while the spawn/launch path survived. x-ref TASK-046 (protocol v14 changes); suspect the fork-specific pane/launch call uses a request shape or seed-pane dance that 0.7.x rejects. Fix after or alongside TASK-046; add a fork acceptance check to the herdr-upgrade runbook gate.
<!-- SECTION:DESCRIPTION:END -->

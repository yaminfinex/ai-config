---
id: TASK-037
title: >-
  herder hooks: mid-edit broken source in a worktree makes every hook firing
  spew the go build error (serve last-good binary instead)
status: In Progress
assignee:
  - unit-z-nova
created_date: '2026-07-08 02:37'
updated_date: '2026-07-08 03:28'
labels: []
dependencies: []
priority: low
ordinal: 37000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
User observed in wave 7 (unit-w, 2026-07-08): PostToolUse hook errors 'Failed with non-blocking status code: # ai-config/tools/herder/internal/spawncmd' while the worker was mid-refactor of that package. Cause: worker hooks route through the WORKTREE's bin/herder (compile-on-demand); while the edited package is transiently uncompilable, every hook firing fails the rebuild and echoes the compile-error header. Non-blocking by design, but (a) noisy in the pane, and (b) hook-fed features (bus status bridging, sidecar enrichment) silently stall during the broken window — status staleness can mislead orchestrator polling. Fix direction (was a TASK-020 candidate, now with a concrete trigger): bin/herder keeps the last successfully built binary and serves it when the rebuild fails (log one quiet line, not the compiler spew); optionally debounce rebuild attempts while the tree is broken. Related: TASK-020 (closed — download noise + rebuild stall; this is the broken-source manifestation).
<!-- SECTION:DESCRIPTION:END -->

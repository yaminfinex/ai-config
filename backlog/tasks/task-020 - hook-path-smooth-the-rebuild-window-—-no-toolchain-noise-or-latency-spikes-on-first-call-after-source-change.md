---
id: TASK-020
title: >-
  hook path: smooth the rebuild window — no toolchain noise or latency spikes on
  first call after source change
status: Done
assignee: []
created_date: '2026-07-07 07:29'
updated_date: '2026-07-08 01:22'
labels: []
dependencies: []
priority: medium
ordinal: 20000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
User report (2026-07-07): PreToolUse/PostToolUse hook errors ('go: downloading go1.26') on first hook call after a herder source change — hooks route through hcom shim -> bin/herder, which compiles on demand; with system go 1.22 + GOTOOLCHAIN=auto the first invocation downloaded the toolchain and every hook firing in that window echoed the download line as a non-blocking error. TASK-008 (merged wave 1) removes the download path (mise go pick + GOTOOLCHAIN=local), which kills THIS noise, but the rebuild window itself remains: hooks stall behind the go build after any source change. Candidates: serve the previous cached binary while rebuilding in background; prebuild on commit/merge (git hook or ai-setup); quiet + fast-fail hook mode. Decide after observing post-TASK-008 behavior.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Observation verdict (2026-07-08, hera): CLOSE. The filed symptom (toolchain-download noise on hook calls) died with TASK-008 — across waves 1-5 (dozens of merges/rebuilds, hundreds of hook firings incl. every worker spawn) zero noise lines recurred. The residual rebuild-window stall was observed benign in practice: incremental go builds behind the shim are sub-second on this machine and no hook latency spike was noticed by user or orchestrator all run. Candidates (serve-cached-while-rebuilding, prebuild-on-commit) stay on record here; reopen only if a real stall is observed.
<!-- SECTION:NOTES:END -->

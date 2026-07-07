---
id: TASK-020
title: >-
  hook path: smooth the rebuild window — no toolchain noise or latency spikes on
  first call after source change
status: To Do
assignee: []
created_date: '2026-07-07 07:29'
labels: []
dependencies: []
priority: medium
ordinal: 20000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
User report (2026-07-07): PreToolUse/PostToolUse hook errors ('go: downloading go1.26') on first hook call after a herder source change — hooks route through hcom shim -> bin/herder, which compiles on demand; with system go 1.22 + GOTOOLCHAIN=auto the first invocation downloaded the toolchain and every hook firing in that window echoed the download line as a non-blocking error. TASK-008 (merged wave 1) removes the download path (mise go pick + GOTOOLCHAIN=local), which kills THIS noise, but the rebuild window itself remains: hooks stall behind the go build after any source change. Candidates: serve the previous cached binary while rebuilding in background; prebuild on commit/merge (git hook or ai-setup); quiet + fast-fail hook mode. Decide after observing post-TASK-008 behavior.
<!-- SECTION:DESCRIPTION:END -->

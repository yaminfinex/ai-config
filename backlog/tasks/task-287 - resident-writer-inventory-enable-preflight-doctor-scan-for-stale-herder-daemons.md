---
id: TASK-287
title: >-
  resident-writer inventory: enable-preflight + doctor scan for stale herder
  daemons
status: To Do
assignee: []
created_date: '2026-07-18 20:45'
labels:
  - herder
dependencies: []
priority: high
ordinal: 286500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Merged write-spine fixes govern only newly started processes; long-lived daemons (per-seat sidecars, observer, grok bridges) keep executing the code they were born with. Live incident: sidecars on pre-carry-fix builds kept stripping credential_generation from their own seats' rows after the fix merged, which would have broken seats had cutover been enabled. Build: (a) a scan that enumerates resident herder-lineage processes (/proc cmdline+exe) and classifies each by build (cache-hash binary path / exe identity) vs the current build, reporting seat/pane identity where env carries it; (b) wire it as a preflight into invariant-enabling verbs — herder credential enable warns (or refuses without an explicit override) while stale resident writers exist; (c) expose the same scan as an operator-facing doctor-style subcommand. Report-only elsewhere; no automated killing — replacement stays an operator/orchestrator action.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Scan enumerates resident sidecars/observer/bridges with build identity vs current and seat attribution where available
- [ ] #2 credential enable preflights the scan; stale resident writers produce a typed cause+remedy warning/refusal with an explicit override
- [ ] #3 Operator-facing subcommand exists and is covered by tests; no process is ever killed automatically
<!-- AC:END -->

---
id: TASK-010
title: >-
  shim UX: claude -p in unmanaged terminal gets backgrounded — answer never
  returns
status: In Progress
assignee: []
created_date: '2026-07-07 06:33'
updated_date: '2026-07-07 07:43'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 10000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
--------------------------------------------------
TASK-001 finding: with machine-wide shims, hand-run 'claude -p ...' in an unmanaged terminal is routed through herder launch -> hcom, which backgrounds the session; the -p answer never returns to the caller.

INVESTIGATED (2026-07-07, orchestrator subagent; hcom v0.7.22 source at tag):
NO hcom-native fix exists. For claude, -p/--print in argv IS the background switch — hard-coded (src/commands/launch.rs:546-548), wins over --run-here (src/launcher.rs:1581-1600 passes run_here=false on the NativePrint branch). Background mode nulls stdin and sends stdout to ~/.hcom/logs (src/terminal.rs:1430-1460). No flag/config/env forces foreground print. Even if foregrounded, bus-bound -p is semantically broken: stream-json gets forced on stdout and the Stop hook polls the bus for config.timeout (default 86400s) — a one-shot would hang ~24h. Raw claude -p with hooks installed is safe: unbound session -> hooks exit 0 in ms.

OPTIONS:
(a) shim-side bypass: shims/claude execs find_real_tool when argv has -p/--print (~10 lines bash; per-tool duplication).
(b) hcom-native flag: does not exist in v0.7.22.
(c) RECOMMENDED: herder launch detects -p/--print (launchcmd.Run, before the hcomArgs build) -> set HCOM_LAUNCH_INFLIGHT=1 and exec the PATH-resolved tool; the shim's existing INFLIGHT recursion guard resolves the real binary for free. ~15 lines Go, one switch covers future codex/gemini print modes, policy lives next to the --run-here decision. One-shots correctly skip the bus (nothing useful there for them).
(d) upstream patch: needs 3 coordinated changes fighting hcom's deliberate 'print mode = persistent background agent' design — skip.

DECISION PENDING (user). SEQUENCING: touches internal/launchcmd — collides with Unit C (TASK-014) in run-herder-dx wave 1; implement in wave 2 after Unit C lands.

DECISION (orchestrator under user best-judgement grant, 2026-07-07): implement option (c) — launchcmd.Run detects -p/--print for claude, sets HCOM_LAUNCH_INFLIGHT=1, execs the PATH-resolved tool; the shim's INFLIGHT guard resolves the real binary. One-shots deliberately skip the bus.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 hand-run claude -p 'question' in an unmanaged terminal returns the answer on stdout (live smoke)
- [ ] #2 interactive claude still binds to the bus; INFLIGHT recursion guards hold (suite evidence)
- [ ] #3 check-launch-contract covers the -p bypass; 16 suites + go gates green; docs/help updated (DoD)
<!-- AC:END -->

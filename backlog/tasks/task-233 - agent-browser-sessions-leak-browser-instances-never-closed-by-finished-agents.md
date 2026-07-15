---
id: TASK-233
title: 'agent-browser sessions leak: browser instances never closed by finished agents'
status: Done
assignee: []
created_date: '2026-07-15 07:08'
updated_date: '2026-07-15 07:14'
labels:
  - infra
dependencies: []
priority: high
ordinal: 232500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner-reported (2026-07-15): 6 distinct agent-browser Chrome instances (root processes under ~/.agent-browser/browsers/chrome-150.*, each with --remote-debugging-port and its own zygote/gpu/renderer tree) running on the box, ALL started 2026-07-13 (2+ days stale), 84 chrome processes total. The agents that opened them are long gone — browser sessions are not being closed at agent/session end.

Investigate: (1) which tool/wrapper launches these (~/.agent-browser layout suggests a shared agent-browser helper) and what its session-close contract is; (2) why close never fires when an agent ends (culled panes, compaction, crashed sessions never running cleanup?); (3) whether anything still holds live CDP connections to the 6 instances (check the debugging ports' sockets before killing); (4) safe cleanup of the stale instances NOW (verify-then-kill, capture evidence first); (5) prevention: idle-timeout/orphan-sweep (e.g. browser instance dies when its owning session disappears — tie to registry/roster liveness, or a doctor/cron sweep), filed as a follow-up implement task if the fix is not trivial.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 All 6 stale instances confirmed unowned (no live CDP clients) and killed; box back to expected browser count
- [ ] #2 Leak mechanism identified: which launcher, why close is skipped, written up
- [ ] #3 Prevention path decided (auto-reap tied to session liveness, or sweep) and filed as an implement task with settled decisions
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Investigation + cleanup complete (worker report on thread task233, full socket-level evidence). Launcher: agent-browser 0.31.1 (mise node global) — one detached Rust daemon per named session (PPID=1, unix sock + loopback IPC + CDP), persists by design; close is cooperative only; idle timeout disabled by default. Leak mechanism: nothing connects herder lifecycle end to agent-browser close — 5 sessions from one ended reviewer, 1 from another (both long-culled). Cleanup: all 6 instances proven client-less at socket level (only connection on each CDP port was the session's own daemon), closed via the public close contract, rc=0 each: daemons 6->0, Chrome roots 6->0, chrome processes 84->0, no listeners/sockets/sidecars/temp profiles remain. Prevention filed as TASK-234 (ownership-record close on cull/retire + doctor sweep safety net + finite default idle timeout).
<!-- SECTION:NOTES:END -->

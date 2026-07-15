---
id: TASK-233
title: 'agent-browser sessions leak: browser instances never closed by finished agents'
status: In Progress
assignee: []
created_date: '2026-07-15 07:08'
updated_date: '2026-07-15 07:08'
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

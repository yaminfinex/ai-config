---
id: TASK-022
title: 'herder compact: steered self-compaction affordance (post-transport-kill)'
status: In Progress
assignee: []
created_date: '2026-07-07 07:51'
updated_date: '2026-07-07 08:40'
labels:
  - run-herder-dx
dependencies: []
priority: high
ordinal: 22000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-003 FINDING 2 (Unit E, run-herder-dx wave 2): the herdr keystroke transport was load-bearing for queuing REAL INPUT to one's own pane — the steered self-compact mechanism (herder send "$HERDR_PANE_ID" '/compact <steer>') documented in skills/orchestrate. After the single-transport cut, own-pane sends resolve to the bus and arrive as hcom message injection, which does NOT fire compaction. Ruled (orchestrator): no self-pane exception inside send — transport doctrine stays pure; instead a dedicated affordance: herder compact <steer> (or herder input --self), reusing spawn's boot-paste engine on the caller's own pane. This is input automation, not inter-agent delivery. INTERIM (until this lands): agents at context ceiling stop and hand off to a fresh spawn (HANDOFF report + successor), no self-compact.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 herder compact '<steer>' queues a real /compact input line to the caller's own pane that fires on turn end (live smoke)
- [ ] #2 refuses when run outside a herdr pane / non-self targets; does not reintroduce a general keystroke transport (grep-gate)
- [ ] #3 skills/orchestrate + playbook context-discipline wording updated from interim back to self-compact; 16 suites + go gates green; docs/help per DoD
<!-- AC:END -->

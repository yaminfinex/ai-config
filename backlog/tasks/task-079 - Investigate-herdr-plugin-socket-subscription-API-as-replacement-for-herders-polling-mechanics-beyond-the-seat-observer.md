---
id: TASK-079
title: >-
  Investigate: herdr plugin / socket-subscription API as replacement for
  herder's polling mechanics (beyond the seat observer)
status: Done
assignee: []
created_date: '2026-07-08 21:12'
updated_date: '2026-07-12 06:54'
labels: []
dependencies: []
priority: medium
ordinal: 79000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner-directed (2026-07-08): investigate whether a herdr plugin — which can subscribe to events on the herdr server's socket API — could replace some of how the herder tool currently works. The seat-observer half of this question is owned by the design pass on task 73 (binding design input recorded there); THIS task holds the rest of the surface.

VERIFIED FACTS (live install, herdr 0.7.x): 'herdr api schema' shows a subscription protocol (schemas: event, subscription_event). Event vocabulary includes pane.created/closed/exited, pane.agent_detected, pane.agent_status_changed, pane.output_matched, agent_started, tab/workspace created/closed/moved, session.snapshot. Plugin system exists: 'herdr plugin install <owner>/<repo>' / 'link <path>' / enable/disable, plugins can expose actions ('herdr plugin action list') and panes ('herdr plugin pane open'), with server-side logs ('herdr plugin log list'). No plugins installed today. 'herdr wait output/agent-status' already exposes one-shot push-style waits over the same machinery.

CANDIDATE REPLACEMENTS to evaluate (herder mechanics that currently poll or probe): spawn boot-settle ('herder wait' loops could ride agent_started / pane.agent_detected); delivery verification (pane.output_matched instead of pane reads); cull verification (pane.closed/exited events as the confirmation source); statusline/context freshness (pane.agent_status_changed); the pane-get probes inside cull/list. For each: does event-driven beat the current poll on correctness (missed-event window? server restart?), and does it add an upstream API dependency worth the coupling — herdr is an external tool on its own release channel, and its API stability guarantees are unknown.

DELIVERABLE: a written findings memo (docs/ or the task itself) with a keep/replace/hybrid verdict per mechanic, an upstream-stability assessment, and — if any replacement is worth building — filed-ready task text handed to the orchestrator. Investigation only: no herder behavior changes ride on this task.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Findings memo exists covering every candidate mechanic with keep/replace/hybrid verdicts and reasons
- [ ] #2 Upstream API stability (versioning, channel, breakage policy) assessed with evidence, not assumption
- [ ] #3 Any recommended build work exists as filed-ready task text with acceptance criteria
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Research unit dispatched 2026-07-12 (researcher, branch task-079-socket-research, gpt-5.6-sol high), brief napkins/run-herder-dx/task-079-research-brief.md. Deliverable: committed memo docs/design/2026-07-12-herdr-socket-subscriptions.md.

Research complete, memo merged (docs/design/2026-07-12-herdr-socket-subscriptions.md). Verdict: observer hybrid is the only paying migration and already exists; all other loops stay with rationale per-site; two problem-statement assumptions falsified against the installed schema. Appendix capture filed as follow-up task. Docs-only merge (one file), content-reviewed by orchestrator per docs-only carve-out. (ACs verified manually — no CLI AC fields.)
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 21:14
---
Scope note (owner FYI, verified): the socket API is queryable (pane.get/list/read/process_info, agent.list/get, session.snapshot) in addition to subscriptions — so candidate replacements should also consider queries as a cleaner substitute for current pane-probe mechanics (e.g. cull's pane-get probe could ride the socket query instead of whatever it uses today), independent of the event stream.
---

created: 2026-07-08 21:29
---
Same correction as on task 73: no plugin registration needed for the socket API — the server listens on ~/.config/herdr/herdr.sock and any process can be a client (the herdr CLI is one; verified with zero plugins installed, protocol 16). The investigation should therefore compare THREE shapes per mechanic: keep current, direct socket client, or plugin packaging — with plugin only warranted where in-terminal integration (actions, plugin panes) matters.
---

created: 2026-07-08 23:43
---
Anchor post-TASK-080: a working direct socket client now exists in-repo (tools/herder/internal/observercmd/socket.go — discovery, protocol pin 16, persistent connection + events.subscribe, wrapped-result handling). The investigation should evaluate reusing/extracting it rather than designing from scratch. Stability data point for AC-2: protocol field + pin worked live; ALSO fold in the TASK-081 finding — session.snapshot wraps its payload under result.snapshot and mock-shape drift hid it, so any wider socket adoption needs live-shape contract checks, not just mocked ones.
---
<!-- COMMENTS:END -->

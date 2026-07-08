---
id: TASK-070
title: >-
  herdr agent tracker never adopts shell-relaunched agents — enrolled live
  sessions stick at live_status=undetected (join input missing, not a join bug)
status: To Do
assignee: []
created_date: '2026-07-08 10:20'
labels: []
dependencies: []
priority: medium
ordinal: 70000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Live hit (TASK-050 controlled restart, vibe verification bus #10964): the replacement orchestrator claude was relaunched by shell command in existing pane w6554208c1918a12:p1 (not via herdr agent start / herder spawn). herdr pane get reports agent_status "unknown" and NO agent field for that pane, while a herdr-started agent (taro) carries agent:"codex". Consequence: herder list shows the enrolled row (guid bbbc84c2) as live_status=undetected forever, even though seat.terminal_id matches the pane's live terminal EXACTLY (term_65612408bc9034 — vibe diffed herdr pane get vs the registry seat, identical). Post-046 tri-state is honest (undetected, not gone), but orchestrators still cannot distinguish a live shell-relaunched agent from a dead one via list. Root cause is herdr-side (tracker only adopts agents it started); candidate fixes: (a) upstream herdr agent adopt / tracker pickup for foreground agents in existing panes — F-ledger candidate at closeout per TASK-029 protocol; (b) herder-side: enroll (which verifies pane + terminal + hcom coordinates at seat time) could record enough for list to report seated-confirmed, or list could upgrade undetected to a distinct "seated (unverified live)" when seat.terminal_id matches a live pane exactly. Relates: TASK-044 (Done — tri-state honesty), TASK-046 (detection epochs), TASK-050 evidence appendix.
<!-- SECTION:DESCRIPTION:END -->

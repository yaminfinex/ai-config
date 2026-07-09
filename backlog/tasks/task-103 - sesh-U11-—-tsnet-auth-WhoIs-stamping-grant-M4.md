---
id: TASK-103
title: 'sesh U11 — tsnet auth: WhoIs stamping + grant (M4)'
status: To Do
assignee: []
created_date: '2026-07-09 05:29'
labels:
  - sesh
dependencies:
  - TASK-097
priority: medium
ordinal: 103000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Lane: store (codex worker). Deliverable: internal/store/listen_tsnet.go — tsnet listener behind the same interface as the loopback listener; WhoIs per connection stamps node identity into the facts log and gates on a tailnet grant capability check; identity claims in request content IGNORED (asserted); loopback dev mode still works. Fallback if tsnet fights the schedule (pre-approved): tailscale serve + identity headers trusted only from the local tailscaled hop — decide at this unit and report the door. Grant policy snippet in README. Requirement R18.

Read first: /home/grace/Coding/ai-config/napkins/sesh-build/playbook.md, plan U11 section (git show 05dfc47:docs/plans/2026-07-09-001-feat-sesh-session-service-plan.md), spec sections 4.3 + 8, captures Lane 2 (git show 6843649:docs/design/2026-07-09-sesh-task-captures.md). Thread: sesh-u11. The deny path must be proven before ANY real transcript flows off-box — hard gate for U12.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 In-grant identity ships and reads; its WhoIs identity appears store-stamped on what it shipped (S8)
- [ ] #2 Out-of-grant identity denied at PUT and read, connection-level
- [ ] #3 Forged owner/identity headers in request content ignored
- [ ] #4 Loopback dev mode still works behind the listener interface
<!-- AC:END -->

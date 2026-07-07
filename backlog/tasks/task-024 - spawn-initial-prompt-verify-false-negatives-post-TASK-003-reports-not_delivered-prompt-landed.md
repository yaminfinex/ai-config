---
id: TASK-024
title: >-
  spawn initial-prompt verify: false negatives post-TASK-003 (reports
  not_delivered, prompt landed)
status: To Do
assignee: []
created_date: '2026-07-07 08:35'
labels:
  - run-herder-dx
dependencies: []
priority: high
ordinal: 24000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Found live dispatching wave 3: 2 of 3 herder spawn calls reported 'prompt: NOT confirmed (verify: not_delivered, ready: status=done,stable)' yet hcom transcript shows both workers received the prompt and began executing (guids 2cfa1f6c unit-i, df6e5375 unit-k; the third, 11d5c38b, verified 'delivered'). The sigil/paste verify in the now in-process boot-paste engine (spawncmd/bootpaste.go) appears to race the agent's TUI start — in-process delivery is faster than the old shell-out, so the verify read may fire after claude clears/redraws. Orchestrator followed doctrine (read pane/transcript before resend), so no double-submit — but a false 'NOT confirmed' invites exactly that mistake. Investigate the verify race and make it reliable (or degrade its wording to 'unverified — check transcript').
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 root cause identified with evidence; verify no longer false-negatives on claude/codex spawn (or reports honestly-unverifiable wording)
- [ ] #2 spawn suite covers the race (mock scenario or timing hook); 16 suites + go gates green
<!-- AC:END -->

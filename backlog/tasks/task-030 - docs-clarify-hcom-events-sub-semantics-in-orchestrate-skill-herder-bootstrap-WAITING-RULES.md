---
id: TASK-030
title: >-
  docs: clarify hcom events sub semantics in orchestrate skill + herder
  bootstrap WAITING RULES
status: To Do
assignee: []
created_date: '2026-07-07 12:37'
labels:
  - run-herder-dx
dependencies: []
priority: low
ordinal: 30000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field report (hera, 2026-07-07): `hcom events sub --once` returns immediately (registers a durable subscription; the notification arrives LATER as a bus message from [hcom-events]) — but the flag name pattern-matches to "block until one event", and the orchestrator wrapped it in background execution and misread process exit as the backstop firing. Our surfaces say "subscribe, end your turn" (directionally right) but never state the load-bearing fact: NON-BLOCKING, notification-via-bus-message, never run as a blocking waiter. Also worth one line: re-arming without unsub stacks subscriptions (duplicate pings per event; --once ones self-remove after firing).

Surfaces: skills/orchestrate SKILL.md invariant 9 + references/{fan-out,relay,sequential-phases}.md backstop lines (one parenthetical each); tools/herder/internal/hookcmd/template.go WAITING RULES line (NOTE: template.go just took TASK-017 churn — rebase on merged main, drift-guard rules apply, goldens regen). Upstream half of this lives in TASK-029 candidate (6).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Orchestrate skill (SKILL.md + the three references that name the backstop) states: sub returns immediately, notification arrives as a bus message, do not run as a blocking waiter
- [ ] #2 template.go WAITING RULES line carries the same one-line clarification; drift guard + goldens green after regen; every regenerated golden line reviewed
- [ ] #3 Pinned gate green (go vet/test herder+bottle + full check battery, env -u)
<!-- AC:END -->

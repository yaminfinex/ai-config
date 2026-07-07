---
id: TASK-030
title: >-
  docs: clarify hcom events sub semantics in orchestrate skill + herder
  bootstrap WAITING RULES
status: Done
assignee:
  - unit-t-dilo
created_date: '2026-07-07 12:37'
updated_date: '2026-07-07 20:42'
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
- [x] #1 Orchestrate skill (SKILL.md + the three references that name the backstop) states: sub returns immediately, notification arrives as a bus message, do not run as a blocking waiter
- [x] #2 template.go WAITING RULES line carries the same one-line clarification; drift guard + goldens green after regen; every regenerated golden line reviewed
- [x] #3 Pinned gate green (go vet/test herder+bottle + full check battery, env -u)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
DONE by worker-dilo (Unit T, wave 5; branch unit-t-events-sub-docs, commit e81e137; first unit under board-via-orchestrator protocol — hygiene applied by hera from the inline payload in DONE report #2884). WHAT CHANGED (13 ins/6 del, 5 files, zero behavior change): SKILL.md invariant 9 gained the non-blocking contract parenthetical (sub returns immediately, notification arrives later as a bus message from [hcom-events], never run as a blocking waiter) + the stacking caveat (re-arm without unsub = duplicate pings; --once self-removes after firing); fan-out.md/relay.md/sequential-phases.md backstop lines each gained the one-line parenthetical; template.go claude bootstrapTemplate WAITING RULES line gained the same clarification. Codex blocks untouched (they defer to hcom stock waiting rules) — herderAgentsSection + both shared tails byte-identical, drift guards pass unmodified. Skill copies: ~/.claude/skills/orchestrate is a symlink to the repo copy — single copy, no divergence. VERIFICATION: dilo gate green + hera independent battery in worktree (go vet/test herder+bottle, 17/17, env -u); launch goldens regen byte-identical (git diff empty — WAITING RULES rides the sessionstart rewrite, not launch argv). Upstream sweep: nothing new beyond TASK-029 candidate 6. Root cause per TASK-030 description (hera field report).
<!-- SECTION:NOTES:END -->

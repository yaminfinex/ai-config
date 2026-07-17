---
id: TASK-269
title: >-
  Spawn preflights the target cwd's mise trust state — refuse with cause+remedy
  instead of silently stranding half-born seats
status: To Do
assignee: []
created_date: '2026-07-17 03:05'
labels:
  - herder
dependencies: []
priority: medium
ordinal: 268500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field-proven three times in one night across two fleets (2026-07-17): spawning into a fresh worktree whose mise config is untrusted half-borns the seat — pane up, registry row minted, no bus row, hcom launcher blocked forever on a dead agent child, empty pane, and a misleading 'agent detection lost / predates server handoff' wait remedy. Root cause verified: mise refuses the untrusted worktree config in the seat's cwd, so mise-shimmed binary resolution kills the agent exec while the launcher waits. Operational doctrine exists (standing-orders create-then-trust-then-spawn; TASK-258 notes carry the field record), but the tool should convert the silent strand into a typed refusal: spawn (and any verb that launches into a cwd — resume, fork with --cwd) preflights the target directory's mise trust state (e.g. a cheap mise env probe or trust-state check) and refuses pre-pane with cause ('target cwd carries an untrusted mise config; the agent would die at birth and the launcher would strand') and remedy (the exact mise trust command for that path). Refusal must be executable from the refusing state and must not fire on cwds without mise configs. Consider whether spawn --worktree should run the preflight after worktree creation and either auto-trust behind an explicit flag or refuse with the remedy (auto-trust by default is a policy call — surface it in the design note, do not decide silently).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Red-first: fixture reproducing the silent strand shape (untrusted cwd) becomes a typed pre-pane refusal naming cause and the exact remedy command; no pane, no registry row minted
- [ ] #2 cwds without mise configs and already-trusted cwds spawn unchanged (regression guard)
- [ ] #3 spawn --worktree path covered: preflight runs post-creation; auto-trust-vs-refuse decision surfaced as a design note, not silently chosen
<!-- AC:END -->

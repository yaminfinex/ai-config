---
id: TASK-082
title: >-
  herder compact: require explicit continuation intent — --then <continuation>
  or --stop; bare compact refuses
status: To Do
assignee: []
created_date: '2026-07-08 23:45'
labels: []
dependencies: []
priority: medium
ordinal: 82000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OWNER-DIRECTED (2026-07-08): a compacted session with no continuation trigger goes DORMANT — it ends its turn after compaction and waits for input forever. This bit live today: the orchestrator self-compacted without a continuation and stalled until the owner noticed. Dormancy after compact is almost never what an autonomous agent wants, yet it is the silent default.

CHANGE: make continuation intent EXPLICIT on herder compact. The caller must pass either --then <continuation> (existing behavior: verified post-turn bus delivery of the continuation) or a new --stop flag (explicit opt-in to compact-and-go-idle, for sessions a human is driving interactively). A bare herder compact with neither flag REFUSES with a message naming both choices and why (post-compact dormancy). --dry-run remains legal without either flag.

Notes: (1) --then is claude-only today and codex is refused entirely — the bare-compact refusal text should not suggest --then to a codex caller; keep the codex refusal as-is. (2) This is the verb-level half of a doctrine already recorded in the run journal: ANY compact of an autonomous session (including the raw pane-injection workaround used while TASK-041 keeps compact self-location broken for enrolled orchestrator seats) must carry a continuation. (3) Pairs naturally with TASK-041 (compact self-location fallback + recovery affordance) — same verb, independent scope; either can land first.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 bare herder compact (no --then, no --stop) refuses with a message naming both flags and the dormancy consequence; exit non-zero, nothing typed or queued
- [ ] #2 herder compact --stop performs the current bare-compact behavior (compact, no continuation) — explicit opt-in
- [ ] #3 herder compact --then <c> unchanged; --dry-run legal without either flag
- [ ] #4 help text documents the required choice and the reason (post-compact dormancy)
- [ ] #5 contract suite covers the refusal, --stop, and --then acceptance paths
<!-- AC:END -->

---
id: TASK-240
title: 'herder resume: preempt the vendor cwd prompt and carry original launch args'
status: To Do
assignee: []
created_date: '2026-07-15 08:54'
labels:
  - herder
dependencies: []
priority: high
ordinal: 239500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field incident, two defects in one resume: (1) 'herder resume --cwd <dir>' launched codex which then BLOCKED on its own interactive 'Choose working directory' prompt (session-recorded stale cwd vs current) — the seat stranded until a human answered. herder knows the answer (--cwd was explicit); it must pass the vendor's non-interactive selection (flag/config) or answer the prompt through the launch machinery — a resumed seat must never strand on a cwd prompt. (2) The resumed session came back at reasoning effort MEDIUM though the original spawn set model_reasoning_effort=high via extra-args — resume does not reconstruct launch args from the registry (registry design says lifecycle reconstructs launch facts; extra-args are not carried). Fix both: persist spawn extra-args on the row, replay on resume/fork; suppress/answer the cwd prompt when --cwd or recorded cwd is unambiguous. Red-first on both.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Resume with --cwd never presents an interactive cwd prompt (vendor selection preempted)
- [ ] #2 Resume/fork replay original model/reasoning extra-args from the registry row
- [ ] #3 Red-first tests for both
<!-- AC:END -->

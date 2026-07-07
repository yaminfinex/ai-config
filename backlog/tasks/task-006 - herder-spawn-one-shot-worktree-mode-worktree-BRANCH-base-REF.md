---
id: TASK-006
title: 'herder spawn: one-shot worktree mode (--worktree BRANCH [--base REF])'
status: In Progress
assignee:
  - unit-h-risa
created_date: '2026-07-07 05:57'
updated_date: '2026-07-07 08:39'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 6000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Observed during run-herder-bootstrap: spawning a worker into a fresh worktree takes two CLIs and manual plumbing — herdr worktree create --json, extract workspace_id + checkout path, then herder spawn --workspace ... --cwd ... --new-tab. A herder spawn --worktree <branch> [--base REF] flag could drive herdr worktree create itself and spawn into the resulting workspace in one verified step.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 spawn --worktree BRANCH [--base REF] drives `herdr worktree create` (wrapped via internal/herdrcli, not reimplemented) and spawns into the new workspace checkout in ONE command; suite golden locks the wire (worktree create call, agent start --workspace/--tab/--cwd on the new checkout)
- [ ] #2 the created workspace seed root pane is closed under the same terminal-identity guard as --new-tab (agent ends sole pane); guard-refusal path stays intact
- [ ] #3 flag validation asserted in suite: --worktree conflicts with --workspace/--from-pane/--cwd/--tab/--new-tab; --base without --worktree refused
- [ ] #4 failure unwinding: worktree created but spawn fails afterward -> non-zero exit + explicit leak report naming workspace id, checkout path, branch, and the exact `herdr worktree remove` command; NOTHING auto-deleted; locked by suite scenario
- [ ] #5 works when the spawner cwd is a linked worktree (repo resolved via `herdr worktree list` source before create); herdr create errors surface verbatim BEFORE any pane/registry side effect
- [ ] #6 live smoke: one-shot spawn of a real agent into a fresh branch worktree off main — pane lands in the new workspace at the new checkout, registry row records it; worktree cleaned up after
- [ ] #7 docs: spawn --help, tools/herder README spawn section, docs/spawn-patterns.md recipe B document --worktree as the one-step path
- [ ] #8 the --json record and the human summary surface the created worktree coordinates (workspace_id, checkout path, branch) so an orchestrator can manage the workspace lifecycle without re-querying herdr; golden-pinned
<!-- AC:END -->

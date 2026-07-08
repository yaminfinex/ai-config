---
id: TASK-006
title: 'herder spawn: one-shot worktree mode (--worktree BRANCH [--base REF])'
status: Done
assignee:
  - unit-h-risa
created_date: '2026-07-07 05:57'
updated_date: '2026-07-08 05:04'
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
- [x] #1 spawn --worktree BRANCH [--base REF] drives `herdr worktree create` (wrapped via internal/herdrcli, not reimplemented) and spawns into the new workspace checkout in ONE command; suite golden locks the wire (worktree create call, agent start --workspace/--tab/--cwd on the new checkout)
- [x] #2 the created workspace seed root pane is closed under the same terminal-identity guard as --new-tab (agent ends sole pane); guard-refusal path stays intact
- [x] #3 flag validation asserted in suite: --worktree conflicts with --workspace/--from-pane/--cwd/--tab/--new-tab; --base without --worktree refused
- [x] #4 failure unwinding: worktree created but spawn fails afterward -> non-zero exit + explicit leak report naming workspace id, checkout path, branch, and the exact `herdr worktree remove` command; NOTHING auto-deleted; locked by suite scenario
- [x] #5 works when the spawner cwd is a linked worktree (repo resolved via `herdr worktree list` source before create); herdr create errors surface verbatim BEFORE any pane/registry side effect
- [x] #6 live smoke: one-shot spawn of a real agent into a fresh branch worktree off main — pane lands in the new workspace at the new checkout, registry row records it; worktree cleaned up after
- [x] #7 docs: spawn --help, tools/herder README spawn section, docs/spawn-patterns.md recipe B document --worktree as the one-step path
- [x] #8 the --json record and the human summary surface the created worktree coordinates (workspace_id, checkout path, branch) so an orchestrator can manage the workspace lifecycle without re-querying herdr; golden-pinned
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit cdb7652 (branch unit-h-spawn-worktree). herder spawn --worktree BRANCH [--base REF]: resolves source repo via `herdr worktree list --cwd` (works from linked worktrees — live-probed payload shapes first, throwaway branch, removed), drives `herdr worktree create --cwd <src> --branch --no-focus --json`, spawns into the new workspace checkout, closes the workspace seed root pane via the SAME identity-guard machinery as --new-tab (payload root_pane feeds rootPaneID/rootTerm), surfaces coordinates in summary + --json `worktree` block. Create failure = verbatim herdr error before any pane; created-then-spawn-failed = non-zero exit + deferred leak report (workspace/checkout/branch + exact remove command), NEVER auto-removed (degrade-safe ruling by hera). Conflicts refused: --workspace/--from-pane/--cwd/--tab/--new-tab, --base alone. Suite: goldens worktree/worktree_startfail/worktree_createfail + 4 usage assertions; mock-herdr-spawn learned worktree list/create (p_60@term_WTROOT topology). Verification: battery 16/16 + go gates green; live smoke rc=0 (fresh branch off main, agent sole pane, notify #2119 from inside checkout) + hera independent smoke. Docs: spawn --help, README spawn section, spawn-patterns recipe B (anti-pattern note for old two-CLI dance), fan-out.md rule 1 (hera-approved). Follow-ups: TASK-026 (cull-time worktree awareness; branch-exists->worktree-open fallback; workspace label override).
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 05:04
---
vibe (herdr-0.7.3 audit, bus #5629, applied by hera): herdr 0.7.1 #729: worktree create now checks out an existing local branch instead of failing when the branch already exists — retry/resume ergonomics for spawn --worktree improved; the existing-branch error path can simplify.
---
<!-- COMMENTS:END -->

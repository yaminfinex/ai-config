---
id: TASK-026
title: >-
  herder cull: worktree-aware cleanup guidance (and optional worktree flags on
  spawn)
status: In Progress
assignee:
  - unit-n-keno
created_date: '2026-07-07 09:02'
updated_date: '2026-07-07 09:37'
labels:
  - run-herder-dx
dependencies: []
priority: low
ordinal: 26000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-006 nice-to-haves (Unit H): (1) culling the last agent in a --worktree-spawned workspace auto-closes the workspace, so the spawn summary's 'herdr worktree remove --workspace' advice goes stale post-cull — cleanup falls to raw 'git worktree remove' + 'git branch -D'; the orchestrator hit exactly this cleaning its verification smoke. Either cull learns an opt-in worktree-cleanup flag (report-only by default, degrade-safe doctrine) or the spawn summary/docs get a post-cull breadcrumb naming the git commands. (2) --worktree with an existing branch could fall back to 'herdr worktree open'. (3) workspace label override flag. Design which of the three are worth it as one small unit.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 DESIGN (ratified by this AC blessing): build option (1) as the spawn-summary/docs post-cull breadcrumb ONLY. Reject (1a) cull-time cleanup flag (registry rows carry no worktree coordinates — schema churn + a new action-taking path in a report-only tool, for info a breadcrumb already delivers), reject (2) branch-exists fallback to herdr worktree open (changes one-shot semantics; existing branch may carry divergent state — the current loud herdr error is honest), reject (3) label override (cosmetic, zero observed pain, labels are herdr-owned branch-derived by documented design). Rationale recorded in notes.
- [ ] #2 The spawn summary --worktree block gains a breadcrumb naming the post-cull reality: culling the last agent auto-closes the workspace, after which cleanup is git worktree remove <checkout_path> + git branch -D <branch> (herdr worktree remove only while the workspace is still open). --json shape unchanged.
- [ ] #3 Docs hygiene: spawn --help --worktree notes, tools/herder README --worktree paragraph, docs/spawn-patterns.md say the same truth; herder-delta.md verified unchanged (historical).
- [ ] #4 Gate: pinned 17/17 + go vet/test green; check-spawn-contract golden churn reviewed line-by-line and justified in DONE report; any new assertions additive-only at tail.
<!-- AC:END -->

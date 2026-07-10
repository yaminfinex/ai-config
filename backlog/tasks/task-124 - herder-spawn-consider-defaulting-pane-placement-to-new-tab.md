---
id: TASK-124
title: 'herder spawn: consider defaulting pane placement to new tab'
status: In Progress
assignee: []
created_date: '2026-07-09 12:43'
updated_date: '2026-07-10 10:12'
labels: []
dependencies: []
priority: medium
ordinal: 124000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## Capture

Owner request (2026-07-09): consider changing herder spawn's DEFAULT pane placement from same-tab right-split to a new tab. Implications not yet understood — this is an investigate-then-decide task, not a blind flag flip.

## Motivation (observed incidents, 2026-07-09 harvest wave)

- Spawning into same-tab right-splits caused message-delivery problems: prompts sent over the bus at spawn time showed 'no receipt in the window' (observed on 2 of 4 reviewer spawns this wave, panes p2N/p2P) — apparent cause: panes not fully rendered at delivery time, so the agent was not yet deliverable.
- Same-tab splits also crowd the operator's tab (4 reviewer panes landed beside the orchestrator; owner had to have them moved out via `herdr pane move --new-tab`).
- Interim doctrine already in force: orchestrators must pass --new-tab on every spawn. This task is about making the DEFAULT safe instead of relying on callers remembering.

## Scope

1. Characterize the delivery failure: is it a race between pane render and bus bind, or between render and prompt injection? Does `herder wait <guid>` after spawn mask it? Reproduce with a same-tab split spawn.
2. Enumerate implications of a new-tab default: callers relying on --split right|down behavior, check scripts/goldens asserting spawn output shape, workspace tab proliferation for short-lived agents, --new-tab interaction with --cwd/--worktree, focus stealing.
3. Recommend: either flip the default (with --split kept as opt-in) or fix the underlying render/delivery race so both placements are safe — or both. If the race is the real bug, flipping the default only hides it.

## Acceptance criteria

1. Repro (or a documented failed-repro attempt) of the delivery race with same-tab split spawn.
2. Written implications assessment covering the enumeration above.
3. Recommendation with chosen direction; if default flips, spawn help text + docs updated and check-spawn-contract goldens updated; if race fixed instead, a regression check proves delivery succeeds into a just-created split pane.
4. Full house gate green (go vet+test tools/herder + tools/bottle, all tests/check-*.sh bare sequential).
<!-- SECTION:DESCRIPTION:END -->

## Owner addendum (2026-07-09, same day)

The organizing principle is stronger than "default to new tab": agents related to some
work belong in THAT WORK's workspace (e.g. a reviewer for task-110 belongs in the
task-110 worker's workspace, not the orchestrator's). herder spawn must make this easy,
not hard. Concretely:

- herder spawn today has no workspace-targeting option; the workaround is spawn then
  `herdr pane move <pane> --new-tab --workspace <id>` (two steps, and the pane briefly
  lands in the caller's workspace).
- Wanted shape (design in this task): something like `--workspace <id|label>` and/or
  inference — when spawning with --cwd/--worktree matching an existing agent's worktree,
  offer/default placement into that agent's workspace as a new tab.
- The new-tab-vs-split delivery race in the original capture still needs
  characterizing; workspace targeting and the race fix are complementary.

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Scope addendum (2026-07-10, live instance): herder RESUME has the same placement gap — resuming a culled session reopens its pane in the invoker's current tab (task-138 worker reopened into the orchestrator tab; owner had to ask for a move). Whatever default/flag lands for spawn placement must apply to resume (and fork) identically — treat every pane-creating lifecycle verb as a spawn for placement purposes.

Dispatched 2026-07-10 with TASK-130 + TASK-062 as one lifecycle unit (@worker-vanu, 5.6-high, branch task-124-lifecycle-placement), brief napkins/run-herder-dx/task-124-130-062-brief.md. Settled: --new-tab default for non-worktree pane-creating verbs, workspace targeting flag, resume follows spawn rules.
<!-- SECTION:NOTES:END -->

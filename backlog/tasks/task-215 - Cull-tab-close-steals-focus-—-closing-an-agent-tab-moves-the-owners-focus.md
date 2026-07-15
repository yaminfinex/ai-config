---
id: TASK-215
title: Cull tab-close steals focus — closing an agent tab moves the owner's focus
status: To Do
assignee: []
created_date: '2026-07-15 00:00'
labels: []
dependencies: []
ordinal: 213000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
OWNER UX DEFECT (2026-07-15, companion to the fresh-tab focus fix): when herder culls a seat, the pane/tab close moves the owner's terminal focus — even when the owner is typing in a different tab. ROOT: cull calls 'herdr pane close <pane>' (cullcmd/cull.go:309) which exposes NO focus flag; the post-close focus target is herdr-side default behavior. RESEARCH-THEN-FIX: (1) characterize live herdr behavior — close a background tab while focus is elsewhere in the same workspace, another workspace, and the same tab; record exactly when focus moves (use disposable panes, never live fleet seats); (2) pick the least-machinery fix: if herdr honors it, capture current focused pane before close and re-focus it after (only when focus actually moved and the prior focus target still exists — no-op otherwise); check herdr for any existing close/focus option or workspace-level setting first; (3) if no herder-side fix is clean, write the upstream herdr ask (owner files upstream) and note the finding — do not build machinery herdr should own. Apply the same treatment to every herder pane-close call site (cull, failAfterLaunch teardown, registry-refusal teardown — enumerate them). Tests: argv/behavior pinned per call site where feasible; goldens for any surface change. SCOPE FENCE: cullcmd + shared pane-close helper territory only; do not touch spawncmd placement (merged fresh-tab work) beyond a shared helper if one is extracted.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Live herdr close-focus behavior characterized (background-tab close, cross-workspace, same-tab cases) with evidence
- [ ] #2 Least-machinery fix landed (focus save/restore or herdr option) across all herder pane-close call sites, or an upstream ask written with the finding
- [ ] #3 Full house battery green (59)
<!-- AC:END -->

---
id: TASK-224
title: Make --no-approve the built-in default for herder pi seats
status: To Do
assignee: []
created_date: '2026-07-15 04:12'
labels: []
dependencies: []
priority: medium
ordinal: 223500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner ruling: herder-spawned pi seats default to refusing project-local .pi resources (--no-approve), with --approve available as an explicit per-spawn opt-in that passes through to the vendor CLI. Currently carried as a spawn-recipe convention via --extra-arg; this task bakes it into the pi launch path so the default cannot be forgotten. Scope: pi family only — no behavior change for any other agent; the launch contract's passthrough validation must still accept the explicit --approve opt-in; add tests that redden if the default disappears or leaks to non-pi launch env. Also verify --no-approve/--approve are NOT in the owned-flag refusal set (they must remain passable). Verify reasoning-level passthrough while in there: --thinking <level> and --model <id>:<level> must reach the vendor argv unmodified.
<!-- SECTION:DESCRIPTION:END -->

---
id: TASK-039
title: >-
  herder spawn --team: worker lands on the team bus but the spawner stays on its
  original bus — reports can't route home
status: To Do
assignee: []
created_date: '2026-07-08 03:33'
labels: []
dependencies: []
priority: medium
ordinal: 39000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field report from @lale (market-sim orchestration run, 2026-07-08): herder spawn --team <slug> binds the WORKER to the scoped team bus, but the spawning orchestrator's identity stays on its original (default) bus. Consequences: the worker cannot resolve the orchestrator's name for DONE/BLOCKED reports, and the orchestrator cannot hcom events sub on the team bus ('Instance not found'; hcom suggests hcom start --as <name>, which would fork the identity across buses). Workaround used: cull + respawn everyone on the global bus — i.e. the feature's primary use case (per-run traffic scoping, advertised in the orchestrate skill run-shape menu) is currently unusable without manual bus surgery. Fix directions: (a) spawn --team detects the spawner is not on the target bus and warns loudly (with the exact join/enroll command) or auto-enrolls the spawner's identity into the team; (b) at minimum, herder spawn --help + the orchestrate skill bus-scoping bullet must state the orchestrator joins the team bus FIRST, before any --team spawn; (c) check what --notify does in this shape (spawner bus name captured at spawn time — does the completion report go to a bus the spawner never reads?). Acceptance sketch: a --team spawn from an off-team spawner either fails loudly with remedy, warns + still works by design choice (rationale required), or auto-enrolls; --notify routing across the team boundary pinned either way; docs/skill updated.
<!-- SECTION:DESCRIPTION:END -->

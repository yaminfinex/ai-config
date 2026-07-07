---
id: TASK-014
title: >-
  codex bootstrap: full herder-native rewrite (codex still gets hcom's original
  bootstrap)
status: Done
assignee: []
created_date: '2026-07-07 06:40'
updated_date: '2026-07-07 07:29'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 14000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Follow-up from TASK-002: codex sessions still receive hcom's ORIGINAL bootstrap baked into developer_instructions (advertises hcom spawn/kill, no herder AGENTS doctrine). TASK-002 only appends the SUBAGENTS block at launch. A full codex bootstrap rewrite — mirroring the claude sessionstart rewrite doctrine, delivered via the launch-args seam — is unowned territory. Note: hcom strips user developer_instructions on codex resume/fork, so that path needs its own design.
<!-- SECTION:DESCRIPTION:END -->

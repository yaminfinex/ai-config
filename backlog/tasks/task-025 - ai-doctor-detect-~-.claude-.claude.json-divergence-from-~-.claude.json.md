---
id: TASK-025
title: 'ai-doctor: detect ~/.claude/.claude.json divergence from ~/.claude.json'
status: To Do
assignee: []
created_date: '2026-07-07 08:56'
labels:
  - run-herder-dx
dependencies: []
priority: low
ordinal: 25000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-011 nice-to-have (Unit K): when both ~/.claude.json and ~/.claude/.claude.json exist and differ, pinned (team-bus) claude sessions and plain claude sessions run with different identity/config state — silent drift. Add an ai-doctor check that flags the divergence and prints the re-align/delete options (documented in the TASK-011 notes + napkins/task-011-investigation.md on the unit-k branch). Post-TASK-011, deleting the pinned copy is safe: next pinned launch re-seeds from ~/.claude.json.
<!-- SECTION:DESCRIPTION:END -->

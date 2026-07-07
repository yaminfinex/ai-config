---
id: TASK-005
title: 'herder spawn --notify: resolve spawner''s bus name from registry'
status: Done
assignee: []
created_date: '2026-07-07 05:57'
updated_date: '2026-07-07 07:40'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 5000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Observed during run-herder-bootstrap: an enrolled orchestrator (hera, registered pane) ran herder spawn --notify from its Claude Code Bash tool and herder classified it as a bus-less spawner, falling back to a keystroke ring on the terminal instead of an hcom message. spawn should resolve the spawning pane/terminal to a registry record (like other commands do) and use its bus name. Workaround: --notify-to <name>.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit 353325b (unit-b-spawn-hygiene, merged d424cab). resolveSpawnerBus now also matches the spawner's pane_id/terminal_id against ACTIVE registry rows (enroll records those; enrolled sessions lack HERDER_GUID env) — enrolled orchestrators get bus-native notify; keystroke ring remains the bus-less fallback. New notify_enrolled contract scenario + unit tests. Also: falls through on rows with empty hcom_name instead of dead-ending.
<!-- SECTION:NOTES:END -->

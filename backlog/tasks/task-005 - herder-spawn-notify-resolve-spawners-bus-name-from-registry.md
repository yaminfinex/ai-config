---
id: TASK-005
title: 'herder spawn --notify: resolve spawner''s bus name from registry'
status: To Do
assignee: []
created_date: '2026-07-07 05:57'
updated_date: '2026-07-07 06:49'
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

---
id: TASK-295
title: >-
  spawn should assert terminal preset (bus db wipe silently resets it, spawns
  land in invoking terminal)
status: To Do
assignee: []
created_date: '2026-07-20 05:19'
labels: []
dependencies: []
ordinal: 294500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field-report (second deployment): wiping the bus db silently resets the terminal preset to default; subsequent spawns launch into the INVOKING terminal instead of herdr panes ('no panes', Ctrl-C exit-130s, confusing half-born sessions). Fix: herder asserts/re-sets terminal=herdr at spawn, or fails with a clear message naming the preset drift. UPSTREAM (bus) candidate: preset should survive db recreation or its loss should be loud.
<!-- SECTION:DESCRIPTION:END -->

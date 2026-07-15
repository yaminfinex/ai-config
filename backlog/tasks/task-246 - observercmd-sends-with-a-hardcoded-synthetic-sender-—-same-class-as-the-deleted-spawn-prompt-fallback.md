---
id: TASK-246
title: >-
  observercmd sends with a hardcoded synthetic sender — same class as the
  deleted spawn-prompt fallback
status: To Do
assignee: []
created_date: '2026-07-15 11:46'
labels: []
dependencies: []
ordinal: 245500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Pre-existing, flagged by both reviewers of the sender-identity unit (explicitly out of that unit's seam): observercmd sends 'hcom send @<name> --from herder-observer' — a hardcoded non-live sender identity. Replies to it hit the @nonexistent-agent error path (and pre-dated reply-resolution quirks route them to the owner desk — the incident class). Fix shape: route observer sends through the explicit-sender engine with a verified identity, or a deliberate documented exception if the observer must stay row-less (state why replies-to-observer are impossible/refused). Small unit; depends on the sender-identity engine landing on main.
<!-- SECTION:DESCRIPTION:END -->

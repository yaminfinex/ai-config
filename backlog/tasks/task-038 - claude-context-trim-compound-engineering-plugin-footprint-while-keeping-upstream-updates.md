---
id: TASK-038
title: >-
  claude context: trim compound-engineering plugin footprint while keeping
  upstream updates
status: To Do
assignee: []
created_date: '2026-07-08 02:42'
labels: []
dependencies: []
priority: low
ordinal: 38000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The plugin ships ~20 model-visible skills (~1k tokens of catalogue descriptions per request); only ce-plan, ce-doc-review, ce-brainstorm, ce-commit-push-pr have ever been used. Verified 2026-07-08: permissions.deny 'Skill(...)' rules block invocation but do NOT remove entries from the prompt catalogue, and vendoring is impractical (ce-plan alone has 24 support files cross-referencing other ce-* skills). Directions to explore: (a) fork EveryInc/compound-engineering-plugin, maintain a slim branch that deletes unused skill dirs, point extraKnownMarketplaces at the fork, periodically merge upstream; (b) a sync script that clones upstream, filters to the used skills, and republishes as a local marketplace; (c) watch newer Claude Code releases for per-skill disable support in plugins; (d) upstream feature request. Context and measurements in docs/claude-context-trim.md.
<!-- SECTION:DESCRIPTION:END -->

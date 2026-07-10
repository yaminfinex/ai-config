---
id: TASK-038
title: >-
  claude context: trim compound-engineering plugin footprint while keeping
  upstream updates
status: Done
assignee: []
created_date: '2026-07-08 02:42'
updated_date: '2026-07-10 21:19'
labels: []
dependencies: []
priority: low
ordinal: 38000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The plugin ships ~20 model-visible skills (~1k tokens of catalogue descriptions per request); only ce-plan, ce-doc-review, ce-brainstorm, ce-commit-push-pr have ever been used. Verified 2026-07-08: permissions.deny 'Skill(...)' rules block invocation but do NOT remove entries from the prompt catalogue, and vendoring is impractical (ce-plan alone has 24 support files cross-referencing other ce-* skills). Directions to explore: (a) fork EveryInc/compound-engineering-plugin, maintain a slim branch that deletes unused skill dirs, point extraKnownMarketplaces at the fork, periodically merge upstream; (b) a sync script that clones upstream, filters to the used skills, and republishes as a local marketplace; (c) watch newer Claude Code releases for per-skill disable support in plugins; (d) upstream feature request. Context and measurements in docs/claude-context-trim.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A direction chosen from the four in the description (fork / sync-script / wait-for-upstream / upstream request) with rationale recorded on this task
- [ ] #2 If build: unused ce-* skills no longer appear in the model-visible catalogue while ce-plan, ce-doc-review, ce-brainstorm, ce-commit-push-pr keep working and upstream updates remain consumable
- [ ] #3 Before/after context measurement recorded in docs/claude-context-trim.md
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
ABANDONED by owner ruling (2026-07-10, chat): none of the four directions pursued — the fork and sync-script both create permanent maintenance surface to save ~1k tokens/request, and upstream per-skill disable may land on its own. Measurements remain in docs/claude-context-trim.md if this is ever revisited. Closed without implementation by design.
<!-- SECTION:NOTES:END -->

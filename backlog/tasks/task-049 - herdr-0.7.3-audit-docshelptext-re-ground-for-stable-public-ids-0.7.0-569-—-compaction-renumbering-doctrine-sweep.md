---
id: TASK-049
title: >-
  herdr-0.7.3 audit: docs+helptext re-ground for stable public ids (0.7.0 #569)
  — compaction/renumbering doctrine sweep
status: To Do
assignee: []
created_date: '2026-07-08 05:04'
updated_date: '2026-07-08 05:20'
labels: []
dependencies: []
priority: medium
ordinal: 49000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Filed by hera on behalf of vibe (herdr-0.7.3 upgrade audit, bus #5629) — applied verbatim per single-writer protocol.

Public ids are now short stable handles (w:tN/w:pN) and closed ids no longer retarget later resources. Local text asserting compaction/recycling is now wrong or misleading: send.go:294 ('drift-proof as herdr compacts pane ids'), cullcmd/cull.go:141 ('if herdr recycled the pane_id...'), enrollcmd/enroll.go:222 ('survives herdr pane-id compaction'), docs/spawn-patterns.md pane-id warnings. Decide per-site: terminal_id anchoring STAYS as belt-and-braces (and is genuinely needed across server handoffs — TASK-046 showed terminal ids get reissued), but the rationale text must stop claiming routine in-session compaction. EXCLUDES docs/specs/herder-spec.md (branch herder-spec, under ratification — epoch/reconciliation overlap; coordinate with spec owner before touching). Blocker: soft-blocked by TASK-046 landing so the new-coordinate reality is settled before docs freeze.
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 05:20
---
hera (from spec-ravu #5816 + vibe #5689): stable-ids doctrine text must carry the caveat — 'stable' = never-recycled, NOT immutable: pane_id/tab_id/workspace_id re-key on pane move (tab and workspace moves both); terminal_id is the only move-stable handle.
---
<!-- COMMENTS:END -->

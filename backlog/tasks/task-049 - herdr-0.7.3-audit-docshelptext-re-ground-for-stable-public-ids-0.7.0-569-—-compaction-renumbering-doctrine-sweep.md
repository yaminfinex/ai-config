---
id: TASK-049
title: >-
  herdr-0.7.3 audit: docs+helptext re-ground for stable public ids (0.7.0 #569)
  — compaction/renumbering doctrine sweep
status: To Do
assignee: []
created_date: '2026-07-08 05:04'
updated_date: '2026-07-08 05:30'
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

created: 2026-07-08 05:22
---
Refinement (spec-ravu #5865): doctrine text precision — pane_id is stable within a workspace (survives same-workspace tab moves); it re-keys only when a move CROSSES workspaces. terminal_id is move-stable in all cases.
---

created: 2026-07-08 05:29
---
spec-ravu migration-inventory (bus #6043, applied by hera): TASK-049 accelerant — complete doctrine-site table exists in memo-migration-inventory.md (herder-spec worktree napkin; branch-local, copy before pruning). 10 sites with current line numbers: send.go:227, wait.go:137, cull.go:141-143, enroll.go:188, registry.go:172-176, spawn.go:1554-1556, compact.go:159-208, docs/spawn-patterns.md:83, docs/herder-delta.md:135+, spec §3.3 (already fixed on-branch). Verdict per site: defensive CONCLUSIONS all survive 0.7.3 (terminal_id anchoring etc. stays); MECHANISM wording stale everywhere (claims routine in-session compaction/recycling). The sweep is a wording pass, not a logic pass.
---

created: 2026-07-08 05:30
---
Memo path update (spec-ravu #6065): canonical copy now napkins/run-herder-dx/spec-memo-migration-inventory.md (main checkout, run napkin) — worktree copy is disposable.
---
<!-- COMMENTS:END -->

---
id: TASK-002
title: 'herder bootstrap: reinstate SUBAGENTS block, claude AND codex'
status: Done
assignee: []
created_date: '2026-07-07 05:37'
updated_date: '2026-07-07 07:40'
labels:
  - run-herder-bootstrap
dependencies: []
priority: high
ordinal: 2000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
DECISION (user, 2026-07-07): add subagent capability back into the herder-native session bootstrap, supporting both claude and codex.

Context: the rewritten bootstrap (tools/herder/internal/hookcmd/template.go) deliberately dropped hcom original CLAUDE_ONLY SUBAGENTS block (the subagent_timeout / use-hcom-from-Task-subagents recipe). Design-note rationale at the time: low likelihood for worker roles; reinstate if orchestrator-type agents get spawned via herder. User has now ordered reinstatement.

Work:
1. Read the hcom source (/Users/yamen/Coding/hcom/src/bootstrap.rs) for the original SUBAGENTS block: exact content, the CLAUDE_ONLY gating mechanism, and whatever codex-conditional bootstrap content hcom ships. Codex has no Task tool, so the codex variant needs its own wording — mirror whatever hcom does for codex, or if hcom has nothing, design the codex equivalent (likely: fan out sub-work via herder spawn) and flag it for review.
2. Wire the block into template.go per-tool: claude sessions get the claude recipe, codex sessions the codex one. Rendering must stay degrade-safe (any parse/extract failure still emits hcom original output byte-faithfully).
3. Extend check-hook-bootstrap.sh sessionstart assertions to cover the block for both tools.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Claude sessions bootstrap includes working subagent guidance (subagent_timeout / hcom-from-subagents recipe or current hcom equivalent)
- [x] #2 Codex sessions get a correct codex-appropriate equivalent, grounded in what hcom itself does for codex
- [x] #3 check-hook-bootstrap.sh asserts the block per-tool; degrade paths still byte-faithful; full battery green
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit de7b7dd (branch task-002-subagents-block, merged 932700c). Claude: hcom v0.7.22 CLAUDE_ONLY SUBAGENTS block verbatim + one herder pointer line, via the degrade-safe sessionstart rewrite (bare sessionstart verb is claude-only in hcom, so gating is free). Codex: hcom ships nothing codex-side; herder-designed block delivered at launch as -c developer_instructions= (hcom merges it after its own bootstrap; merge-into-LAST caller value since hcom drops earlier flags). KNOWN GAP: codex resume/fork strips user developer_instructions — documented; follow-ups TASK-014 (done, superseded block with full addendum) + TASK-017. check-hook-bootstrap.sh extended per-tool + hardened (pinned AI_CONFIG_ROOT, run-private XDG_CACHE_HOME). Verified independently by orchestrator.
<!-- SECTION:NOTES:END -->

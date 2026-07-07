---
id: TASK-002
title: 'herder bootstrap: reinstate SUBAGENTS block, claude AND codex'
status: To Do
assignee: []
created_date: '2026-07-07 05:37'
labels: []
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
- [ ] #1 Claude sessions bootstrap includes working subagent guidance (subagent_timeout / hcom-from-subagents recipe or current hcom equivalent)
- [ ] #2 Codex sessions get a correct codex-appropriate equivalent, grounded in what hcom itself does for codex
- [ ] #3 check-hook-bootstrap.sh asserts the block per-tool; degrade paths still byte-faithful; full battery green
<!-- AC:END -->

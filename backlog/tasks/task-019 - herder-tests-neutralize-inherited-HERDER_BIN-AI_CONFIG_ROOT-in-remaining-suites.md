---
id: TASK-019
title: >-
  herder tests: neutralize inherited HERDER_BIN/AI_CONFIG_ROOT in remaining
  suites
status: In Progress
assignee:
  - unit-s-mige
created_date: '2026-07-07 07:29'
updated_date: '2026-07-07 20:40'
labels: []
dependencies: []
priority: medium
ordinal: 19000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
run-herder-dx gate pitfall (Unit A + orchestrator both bitten): suites that honor inherited HERDER_BIN/AI_CONFIG_ROOT silently drive the MAIN checkout's wrapper/sources when run from a herder-spawned agent in a worktree (every spawned agent has both exported) — wholesale false reds/greens. check-hook-bootstrap + check-spawn-contract now pin them per-suite (TASK-002/Unit-B pattern); sweep the remaining suites to pin AI_CONFIG_ROOT=$REPO_ROOT and ignore inherited HERDER_BIN. Until then: run gates with env -u HERDER_BIN -u AI_CONFIG_ROOT.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every tests/check-*.sh suite neutralizes inherited HERDER_BIN (unset/ignored) and pins AI_CONFIG_ROOT to its own checkout root before any herder/wrapper invocation; a suite that legitimately needs an inherited value carries a comment saying why (expected: none)
- [ ] #2 Poison proof: full 17-suite battery with HERDER_BIN and AI_CONFIG_ROOT deliberately exported to wrong (nonexistent) paths — 17/17 green
- [ ] #3 Clean proof: battery 17/17 green BOTH with and without env -u HERDER_BIN -u AI_CONFIG_ROOT, run from a herder-spawned agent env; pinned go vet/test gates green
- [ ] #4 Diff confined to env-handling lines in tools/herder/tests/check-*.sh + README Gates wording; no goldens, no Go code (lib-*.sh prelude NOT blessed — per-suite stanza ruling)
- [ ] #5 check-hook-bootstrap stops honoring the spawn-exported HERDER_BIN name as its binary-override knob (renamed HERDER_HOOK_* or removed) — the one suite reading that exact name today
- [ ] #6 README Gates section updated — env -u no longer required, suites self-neutralize; state whether the env -u example stays as belt-and-braces or is dropped, with rationale
<!-- AC:END -->

---
id: TASK-019
title: >-
  herder tests: neutralize inherited HERDER_BIN/AI_CONFIG_ROOT in remaining
  suites
status: Done
assignee:
  - unit-s-mige
created_date: '2026-07-07 07:29'
updated_date: '2026-07-07 20:50'
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
- [x] #1 Every tests/check-*.sh suite neutralizes inherited HERDER_BIN (unset/ignored) and pins AI_CONFIG_ROOT to its own checkout root before any herder/wrapper invocation; a suite that legitimately needs an inherited value carries a comment saying why (expected: none)
- [x] #2 Poison proof: full 17-suite battery with HERDER_BIN and AI_CONFIG_ROOT deliberately exported to wrong (nonexistent) paths — 17/17 green
- [x] #3 Clean proof: battery 17/17 green BOTH with and without env -u HERDER_BIN -u AI_CONFIG_ROOT, run from a herder-spawned agent env; pinned go vet/test gates green
- [x] #4 Diff confined to env-handling lines in tools/herder/tests/check-*.sh + README Gates wording; no goldens, no Go code (lib-*.sh prelude NOT blessed — per-suite stanza ruling)
- [x] #5 check-hook-bootstrap stops honoring the spawn-exported HERDER_BIN name as its binary-override knob (renamed HERDER_HOOK_* or removed) — the one suite reading that exact name today
- [x] #6 README Gates section updated — env -u no longer required, suites self-neutralize; state whether the env -u example stays as belt-and-braces or is dropped, with rationale
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
DONE by worker-mige (Unit S, wave 5; branch unit-s-suite-env, commit 9570d52; hygiene via orchestrator pen from DONE report #2986). ROOT CAUSE: two leak vectors — lib/common.sh:8 let an inherited AI_CONFIG_ROOT beat the wrapper location (bin/herder builds the OTHER checkout sources), and check-hook-bootstrap.sh named its binary-override knob literally HERDER_BIN, colliding with the spawn export (every spawned agent unintentionally selected the binary under test; poison baseline: 16/17 with that suite exit 127). FIX: uniform 5-line env-hygiene stanza in all 17 check-*.sh (unset HERDER_BIN; export AI_CONFIG_ROOT="$REPO_ROOT") right after root resolution — per-suite stanza ratified over a shared prelude (grep-auditable, no source-time coupling); hook-bootstrap knob renamed HERDER_HOOK_BIN (HERDER_SPAWN_BIN/HERDER_LAUNCH_BIN precedent); README Gates documents bare invocation, env -u dropped from the copyable example but noted harmless in prose. VERIFICATION (worker 4-way + hera replication): poison battery (both vars → /nonexistent-poison) 17/17 green in worktree, replicated by hera; grep -L both stanza lines empty across 17 suites (hera-replicated); go vet/test herder+bottle green (hera-replicated); worker also ran env -u and bare and main-checkout-poison variants, all 17/17. FOLLOW-UPS not done: battery meta-check asserting new suites carry the stanza; HERDER_STATE_DIR/HCOM_DIR unswept (no incident); historical playbooks keep env -u wording (historical docs). Orchestrator gates may drop env -u post-merge.
<!-- SECTION:NOTES:END -->

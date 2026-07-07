---
id: TASK-021
title: docs/skills hygiene sweep for wave 0/1 changes
status: Done
assignee: []
created_date: '2026-07-07 07:41'
updated_date: '2026-07-07 07:57'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 21000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
User directive (2026-07-07): docs and skills hygiene is part of definition of done. Retroactive sweep for the wave 0/1 merges: verify tools/herder README + --help text reflect the machine-wide shim takeover (TASK-001), SUBAGENTS/codex addendum behavior (TASK-002/014), fork provenance semantics (TASK-004), bin/herder toolchain+cache behavior (TASK-008/012 — README or header comment), and check skills/orchestrate + any repo skills referencing herder for drift (e.g. gate guidance should mention env -u HERDER_BIN until TASK-019). EXCLUDE tools/herder/docs/delivery-drivers.md (TASK-003 territory, in flight).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every wave-0/1 behavior change is reflected (or verified already correct) in README/--help/docs; list checked surfaces in notes
- [x] #2 No stale references to pre-TASK-008 wrapper behavior or removed shim semantics remain (grep evidence)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit 4f6d128. Surfaces UPDATED: tools/herder/README.md (launcher paragraph now states version-checked toolchain + GOTOOLCHAIN=local + per-hash cache + age prune per TASK-008/012; Gates check-suite loop now `env -u HERDER_BIN -u AI_CONFIG_ROOT` with an until-TASK-019 note; NEW Session Bootstrap section covering TASK-002 claude SUBAGENTS rewrite + degrade-safe fallback and TASK-014 codex [HERDER SESSION ADDENDUM] incl. the resume/fork gap (TASK-017 open); Activation And Usage now states the TASK-001 machine-wide takeover + HCOM= escape hatch + non-mise degrade). tools/bottle/README.md (wrapper paragraph matched to post-TASK-015 bin/bottle). Surfaces VERIFIED CORRECT, unchanged: fork --help (TASK-004 ruled semantics present), spawn --help Behavior paragraph (TASK-005/007/009/013 all covered), skills/orchestrate + references (no stale PATH/toolchain/notify-to advice), skills/ai-config, skills/bottling. NOT touched: delivery-drivers.md + all delivery/keystroke wording (Unit E), launch --help (Unit G; codex-addendum gap folded into wave-3 TASK-017 per hera), herder-delta.md scripts/herder refs (historical banner wins, hera ruling), spawn-patterns recipe B re-point gap (wave-3 nit, journaled). Grep evidence for AC#2: `mise x go|rm -f *-\*|known-red|notify-to workaround|scripts/herder` hit ONLY dated docs/plans + docs/superpowers archives and the HISTORICAL herder-delta.md; zero hits on live surfaces (READMEs, skills, --help).
<!-- SECTION:NOTES:END -->

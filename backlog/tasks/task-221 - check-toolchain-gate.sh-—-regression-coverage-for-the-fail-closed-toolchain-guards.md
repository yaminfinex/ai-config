---
id: TASK-221
title: >-
  check-toolchain-gate.sh — regression coverage for the fail-closed toolchain
  guards
status: In Progress
assignee: []
created_date: '2026-07-15 01:14'
updated_date: '2026-07-15 01:14'
labels: []
dependencies: []
ordinal: 220500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
IMPLEMENT unit (test-only). The fail-closed guard contract merged with the Go-1.26.5 unification has zero regression coverage: reverting any guard (weak /^go / parser, unguarded mise-where, dropped toolchain-conflict check) leaves the house battery green — the exact aspirational-green class that let toolchain drift survive. Port the proven review harness into the battery as tools/herder/tests/check-toolchain-gate.sh; HOUSE COUNT 59 -> 60.

Source material (parked, gitignored): napkins/run-herder-dx/toolchain-gate-harness/ — recert.sh (14-probe matrix: 4 mutations x 4 gates, asserting deliberate gate refusal and REJECTING the two silent routes: ambient-go fallback, compiler-error text), p3probe.sh (diagnosis honesty: untrusted mise.toml, mise-absent), NOTES.md (READ FIRST: six traps that each produced a wrong verdict during review — including "mish is floor-based BY DESIGN, do not fix it" and the mise-trust artifact for temp copies).

Settled decisions:
- Discrimination is the AC, not greenness: an always-green guard test is worthless. Preferred shape per the harness notes: a SELF-CHECK mode that degrades the guard in a throwaway copy and asserts its own probes go RED — making "provably discriminating" live instead of an aging-SHA claim. The git-archive regeneration one-liner in NOTES is the fallback control.
- Temp copies must be `mise trust`ed (documented trap).
- No agent names/task numbers/run identifiers in the shipped script or its output.
- Battery count references update 59 -> 60 everywhere the pinned count is quoted.

Acceptance:
- AC1: check-toolchain-gate.sh green on current main; degrading any of the four guard classes in a copy turns it RED (demonstrated per class).
- AC2: full house battery 60/60 green.
- AC3: script runs from a clean shell (the sesh-preflight env class) and leaves no artifacts.
<!-- SECTION:DESCRIPTION:END -->

---
id: TASK-221
title: >-
  check-toolchain-gate.sh — regression coverage for the fail-closed toolchain
  guards
status: Done
assignee: []
created_date: '2026-07-15 01:14'
updated_date: '2026-07-15 03:27'
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged 1b12b31 + fix merge 485ec9f (cceacb6) at head cceacb6. House battery count 59 -> 60 — check-toolchain-gate.sh joined (count is derived from the glob; no live surface pins it). Suite proves the four toolchain-gate guard classes discriminating on EVERY run via removal self-checks that fail loud on unapplied mutations. Codex adversarial review drove: mise global-state isolation (trust + tracked stores + cache proven flat across real batteries), tracked-files-only sandbox copies (1.8G -> 13M on main), end-of-options hardening against dash-leading paths. Machine debt cleaned: 318 stale mise store symlinks removed (broken-target-only, delete-time evaluation; live entries preserved). AC3 interpretation recorded: no copy-specific stale artifacts in global/caller state; content-keyed Go caches out of scope. Grok calibration seat ran (ledger row 15). Post-merge batteries 60/60 green, pushed in the 485ec9f train.
<!-- SECTION:NOTES:END -->

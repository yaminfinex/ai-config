---
id: TASK-176
title: 'sesh — check battery vs mise go pin drift (1.26.4 pin, 1.26.5 session GOROOT)'
status: Done
updated_date: '2026-07-14'
assignee: []
created_date: '2026-07-13 01:01'
labels:
  - sesh
dependencies: []
priority: low
ordinal: 175000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Found during TASK-172 gates: the tests/check-*.sh battery only passes when mise pins go 1.26.5, because session GOROOT env points at 1.26.5 and poisons the 1.26.4 pin that lib.sh suggests. Reconcile the pin (bump to 1.26.5 or fix lib.sh GOROOT handling so the pin is authoritative).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Battery green from a clean shell with only the repo's pinned toolchain; no GOROOT leakage
<!-- AC:END -->

## Evidence (Done, 2026-07-14)

Lane: branch task-176-battery-go-pin (builder-lase, sole substance
reviewer reviewer-goru (codex), hera merge gate). Tests-only, 2 files
(tests/lib.sh, tests/check-surface-fixtures.sh), zero production code.

- Root cause: repo-root mise.toml pins go = "1.26" (floating symlink ->
  1.26.5) while go.mod pins 1.26.4; session GOROOT poisoned the battery.
- Fix: go.mod go directive is the single toolchain authority. lib.sh
  unsets GOROOT at source time, forces GOTOOLCHAIN=local, resolves the
  exact pinned go (mise where -> mise install dir -> exact PATH match),
  hard-fails one identifier-free line on any mismatch — including a
  differing go.mod toolchain directive. Duplicate inline preflight
  removed from check-surface-fixtures.sh; hardcoded home-path hint gone.
- Review findings (all FIXED, re-verified): P2 toolchain-directive
  silent conflict now loud exit 1; P2 mute non-1 exit on a broken
  candidate go now routes through fail(); P3 pin parser accepts valid
  indented/tab-separated go.mod directives (awk tokenization).
- Proofs: poisoned GOROOT + shadowed fake go -> pinned 1.26.4 wins;
  unresolvable pin (system go 1.22.2) -> one-line exit 1; clean-shell
  env -i battery 17/17 ALL GREEN. Verdict READY TO MERGE #74898.
- Merge 9584353 --no-ff; post-merge house battery 59/59 green under
  hera's GOROOT-exporting gate env (live adversarial proof); pushed.
  No deploy leg (tests-only).
- Out-of-lane observation recorded: check-deploy-artifacts.sh needs
  XDG_RUNTIME_DIR (systemd-analyze --user), present in any login shell;
  pre-existing, untouched.

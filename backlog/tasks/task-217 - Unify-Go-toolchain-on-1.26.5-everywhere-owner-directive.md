---
id: TASK-217
title: Unify Go toolchain on 1.26.5 everywhere (owner directive)
status: In Progress
assignee: []
created_date: '2026-07-15 00:34'
updated_date: '2026-07-15 00:36'
labels: []
dependencies: []
ordinal: 215000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
IMPLEMENT unit, owner-directed: one Go version across the repo — 1.26.5 (newest installed) — ending the 1.26.4/1.26.5 drift class.

Settled decisions (do not relitigate):
- Target version: 1.26.5 exactly (already installed at ~/.local/share/mise/installs/go/1.26.5).
- go.mod is the single toolchain authority per module (established pattern: sesh tests/lib.sh resolves its exact pin FROM go.mod).

Scope:
1. go.mod `go` directive -> 1.26.5 in tools/sesh, tools/mish, tools/herder, tools/bottle (herder/bottle currently float on `go 1.26`; make them exact for uniformity). Run go mod tidy only if required; no dependency changes.
2. mise.toml: go = "1.26.5" exact (the floating "1.26" symlink is the drift SOURCE — it silently retargets on every mise bump).
3. Hardcoded version strings in scripts: tools/mish/tests/lib.sh, tools/herder/tests/check-node-contract.sh, tools/herder/tests/check-observer-contract.sh (all 1.26.4), tools/herder/tests/check-grok-doctor.sh (1.26.5) — prefer deriving from the module's go.mod like sesh lib.sh does; a literal bump to 1.26.5 is acceptable where derivation is disproportionate. State which you chose per file.
4. Sweep for any other 1.26.x literals in tests/lib/bin/docs and fix or justify each.
5. OUT OF SCOPE: tools/mc (untracked owner WIP, go 1.22 — do not touch), upstream mise installs cleanup (owner action).

Acceptance:
- AC1: grep for 1.26.4 in tracked files returns only historical/changelog text (justify each survivor).
- AC2: full house battery 59/59 green from a clean shell (the sesh preflight will now enforce 1.26.5 — that flip proves the authority chain works).
- AC3: bin/herder wrapper still builds (its go.mod parse handles exact patch directive).
- AC4: no dependency/vendor changes; diff is version pins + script strings only.
<!-- SECTION:DESCRIPTION:END -->

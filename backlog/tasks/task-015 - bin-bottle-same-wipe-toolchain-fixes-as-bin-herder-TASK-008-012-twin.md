---
id: TASK-015
title: 'bin/bottle: same wipe + toolchain fixes as bin/herder (TASK-008/012 twin)'
status: Done
assignee: []
created_date: '2026-07-07 07:29'
updated_date: '2026-07-07 07:57'
labels:
  - run-herder-dx
dependencies: []
priority: medium
ordinal: 15000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Unit A finding (run-herder-dx): bin/bottle:33 has the identical 'rm -f bottle-*' cache wipe and unversioned toolchain pick that TASK-008/TASK-012 fixed in bin/herder. Apply the same two fixes verbatim: version-check go against go.mod with mise fall-through + GOTOOLCHAIN=local, and per-hash binaries pruned by age instead of pre-build wipe.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 bin/bottle picks its Go toolchain by version-checking against tools/bottle/go.mod: PATH go if satisfying, else mise-installed toolchains probed directly (never `mise x`); build runs with GOTOOLCHAIN=local pinned (caller override respected)
- [x] #2 Pre-build `rm -f bottle-*` wipe is gone; per-hash binaries are pruned only after a successful build and only by age (touch-on-use keeps live ones fresh)
- [x] #3 Wrapper structure mirrors bin/herder (cache candidates incl. shared tmp seed, sha256 tool fallback, GOCACHE fallback) so the twins stay diffable; behavior backward-compatible for existing callers
- [x] #4 Live smoke via a COPY of the wrapper first: cold build + warm reuse both exercised; then gate green (go vet/test + check suites)
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit 4f6d128 (unit-f-wrappers-docs). Root cause: bin/bottle retained the pre-TASK-008/012 wrapper shape — unversioned toolchain pick (PATH go, else `mise x go` which can auto-install/hang) and a pre-build `rm -f bottle-*` wipe that nuked sibling checkouts and ran even on failed builds. Change: rebuilt bin/bottle as a structural twin of bin/herder — version-check vs go.mod (toolchain directive wins, else go line), PATH go then direct mise-install probe (mise where + data-dir roots incl. binary-prefix derivation; never mise x), GOTOOLCHAIN=local pinned, sha256sum/shasum fallback, cache candidates XDG/HOME/shared-tmp with touch-on-use reuse + post-build seed, GOCACHE fallback, prune only post-build and only by age (14d bins / 1d stale .tmp). bottle deliberately still does NOT export AI_CONFIG_ROOT (preserved existing behavior). Verification (via COPIES, per wrapper caution): cold env -i build rejected PATH go 1.22.2 and built via mise in 2.5s; recent decoy binary survived (no wipe), 30-day decoy pruned; warm reuse 9ms with no toolchain on PATH; cross-HOME reuse via shared tmp seed OK. hera independently reproduced (2.3s cold, 12ms warm, no-mise hard-error with remedy). Gate 16/16 + go vet/test green. Follow-up (nice-to-have, journaled): dedupe the ~180 shared wrapper lines into a sourced lib.
<!-- SECTION:NOTES:END -->

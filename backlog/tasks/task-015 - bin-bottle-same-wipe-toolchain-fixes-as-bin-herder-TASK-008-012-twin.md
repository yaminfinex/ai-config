---
id: TASK-015
title: 'bin/bottle: same wipe + toolchain fixes as bin/herder (TASK-008/012 twin)'
status: In Progress
assignee: []
created_date: '2026-07-07 07:29'
updated_date: '2026-07-07 07:44'
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
- [ ] #1 bin/bottle picks its Go toolchain by version-checking against tools/bottle/go.mod: PATH go if satisfying, else mise-installed toolchains probed directly (never `mise x`); build runs with GOTOOLCHAIN=local pinned (caller override respected)
- [ ] #2 Pre-build `rm -f bottle-*` wipe is gone; per-hash binaries are pruned only after a successful build and only by age (touch-on-use keeps live ones fresh)
- [ ] #3 Wrapper structure mirrors bin/herder (cache candidates incl. shared tmp seed, sha256 tool fallback, GOCACHE fallback) so the twins stay diffable; behavior backward-compatible for existing callers
- [ ] #4 Live smoke via a COPY of the wrapper first: cold build + warm reuse both exercised; then gate green (go vet/test + check suites)
<!-- AC:END -->

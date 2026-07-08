---
id: TASK-037
title: >-
  herder hooks: mid-edit broken source in a worktree makes every hook firing
  spew the go build error (serve last-good binary instead)
status: Done
assignee:
  - unit-z-nova
created_date: '2026-07-08 02:37'
updated_date: '2026-07-08 04:04'
labels: []
dependencies: []
priority: low
ordinal: 37000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
User observed in wave 7 (unit-w, 2026-07-08): PostToolUse hook errors 'Failed with non-blocking status code: # ai-config/tools/herder/internal/spawncmd' while the worker was mid-refactor of that package. Cause: worker hooks route through the WORKTREE's bin/herder (compile-on-demand); while the edited package is transiently uncompilable, every hook firing fails the rebuild and echoes the compile-error header. Non-blocking by design, but (a) noisy in the pane, and (b) hook-fed features (bus status bridging, sidecar enrichment) silently stall during the broken window — status staleness can mislead orchestrator polling. Fix direction (was a TASK-020 candidate, now with a concrete trigger): bin/herder keeps the last successfully built binary and serves it when the rebuild fails (log one quiet line, not the compiler spew); optionally debounce rebuild attempts while the tree is broken. Related: TASK-020 (closed — download noise + rebuild stall; this is the broken-source manifestation).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 On rebuild failure with a prior successful build for THIS checkout, the wrapper execs last-good and exits 0
- [x] #2 Failure path emits exactly one quiet stderr line, no compiler output
- [x] #3 Fixed source rebuilds normally (no serve line)
- [x] #4 Never-built checkout still fails loud (nonzero, full compiler output)
- [x] #5 Last-good is per-checkout; sibling checkouts never cross-serve
- [x] #6 Pattern ported to bin/bottle with byte-parallel logic
- [x] #7 Last-good recovers across HOMEs sharing the shared tmp cache; INT/TERM terminates (130/143), never morphs into a serve
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
bin/herder & bin/bottle serve the last successfully built binary for the checkout when a rebuild fails (mid-edit-broken package on the hook path), emitting one quiet 'rebuild failed, serving last-good <hash>' line instead of compiler spew; hook-fed features keep working through the broken window. Last-good pointer is per-checkout (source-dir path hash, dot-prefixed clear of prune globs, written only on change); after review round 2 the FALLBACK walks all cache candidates with exact-reuse ownership checks and the seed path writes the shared-cache pointer, so fake-HOME/env-i/sibling-HOME callers recover last-good (reviewer-reproduced gap). Never-built checkouts fail loud. Build temps: EXIT trap cleanup + age prune covers .{herder,bottle}-build.* and pointer tmps (SIGKILL belt). After review round 3: traps split — INT exits 130, TERM exits 143 — so a supervisor TERM terminates instead of morphing into a stale serve (reviewer-reproduced; suite case proven to flip red against the old trap). Pinned by check-wrapper-lastgood.sh (27 asserts: lifecycle, never-built, cross-HOME, interrupt; real go builds); battery now 19 suites. Merged 083d6a6; hera gates green four times (worktree R1/R2/R3 + post-merge main). Review: tuba REQUEST-CHANGES x2 (cross-HOME P2 reproduced; TERM-morph blocker reproduced) => APPROVE.
<!-- SECTION:NOTES:END -->

---
id: TASK-027
title: >-
  codex fork --self: addendum delivery for the spawn-fallback path (TASK-017
  residual)
status: Done
assignee:
  - unit-x-zimo
created_date: '2026-07-07 09:39'
updated_date: '2026-07-08 02:19'
labels:
  - run-herder-dx
dependencies: []
priority: low
ordinal: 27000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-017 (Unit L, wave 4) delivers the herder doctrine addendum post-boot on codex resume and NATIVE fork paths. Residual gap ruled acceptable: codex fork --self falls back to a 'herder spawn --extra-arg fork ...' handoff where the child guid is only known inside spawncmd — covering it from lifecyclecmd means parsing spawn --json or cross-package surgery. When worth it: teach spawn itself to thread the addendum on that path (it owns the guid), or surface the child guid back to the fork caller. Rare path; documented in README as a known gap by TASK-017.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 codex fork --self fallback delivers the doctrine addendum to the spawned child
- [x] #2 No regression of TASK-017 native fork/resume paths; zero spawncmd production edits
- [x] #3 Warn-never-block floor: no-guid, shape-mismatch, and bind-timeout all exit 0 with warning + manual remedy
- [x] #4 Suite coverage: self_fallback_codex / _noguid / _bindtimeout goldens
- [x] #5 Docs: fork --help + README known-gap note rewritten to closed behavior
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Closed the TASK-017 residual: codex fork --self fallback now re-delivers the herder doctrine addendum. forkSelfFallback runs herder spawn --json, recovers the child guid from spawn's record, and reuses lifecyclecmd's deliverCodexAddendum (registry-bind poll + verified bus send) — codex-only, warn-never-block. Chose 'surface the guid' over 'teach spawn': single delivery code path, zero spawncmd edits (locked wave-5/6 capture/bind/notify untouched). Nit round after review: parseSpawnChild requires canonical guid + agent/status record shape (a stray stdout JSON diagnostic can never misroute the addendum); self_fallback_codex_bindtimeout golden pins the parse+poll+timeout+exit-0 seam. fork --help + README known-gap note rewritten; stale launch.go comment fixed. Merged 5cc93ad; hera-verified gate green three times (worktree R1+R2, post-merge main, 18/18 + go modules). Review: mina APPROVE-WITH-NITS (2 P3) => nits fixed => APPROVE.
<!-- SECTION:NOTES:END -->

---
id: TASK-132
title: >-
  sesh — store robustness batch: consumer lifecycle, busy_timeout DSN, reindex
  memory, ledger observed_at (run-sesh-107 unit B)
status: To Do
assignee: []
created_date: '2026-07-09 23:13'
labels:
  - run-sesh-107
dependencies:
  - TASK-131
priority: medium
ordinal: 132000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Parent: TASK-107 AC#1, #2, #4, #5. Second unit of run-sesh-107; runs after unit A (TASK-131) in the same worktree — rebase-free sequential handoff, do not start until dispatched.

Four small, independent robustness fixes in tools/sesh, all previously accepted-but-tracked from the U6/U7 reviews:

1. Consumer goroutine lifecycle: the store's Serve does not watch ctx; on listener error the append-event consumer leaks until process exit. Bound it — wire ctx through Serve or stop the consumer on Close. Either mechanism is acceptable; smallest coherent surface change wins.
2. busy_timeout is set per-connection once in initSchema; a silently-replaced pooled connection resets it to 0. Move it into the sqlite DSN so every connection gets it (driver is modernc.org/sqlite; DSN pragma form is ?_pragma=busy_timeout(N)). Cover every sql.Open in the module (store + index + any CLI paths).
3. Reindex memory: parsing allocates whole-file buffers (a 30MB mirrored file means a 30MB allocation). Bound it — stream or chunk the parse. Demonstrate with a test or benchmark that peak allocation no longer tracks file size.
4. quarantine_ledger day-buckets are rebucketed to reindex wall-clock time, defeating the recent-counts operator metric. Preserve the original observation time across reindex. The index tables are disposable (Reindex rebuilds them), so schema additions to index-owned tables are allowed if Reindex rebuilds them; the files table and wire schema (docs/specs/sesh-wire.md, frozen) must not change.

Settled decisions — do not re-litigate; if one seems wrong, STOP and report on your unit thread, never substitute your own design:
- No wire-schema or files-table changes; a fix that seems to need one is a stop-and-report.
- Four fixes, four commits (or clearly separated); no drive-by refactors outside these four surfaces.
- Do not undo or rework unit A's incremental unification; if a conflict arises, stop and report.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Store consumer goroutine provably bounded: exits on ctx cancellation or Close, including the listener-error path (test or race-run evidence)
- [ ] #2 busy_timeout rides the DSN on every sqlite open in the module; no per-connection PRAGMA exec remains as the only guard
- [ ] #3 Reindex/parse peak memory bounded independent of mirrored file size (streaming or chunked), with test/benchmark evidence
- [ ] #4 Quarantine ledger counts survive reindex with original observed_at; recent-counts metric unchanged after a reindex cycle
- [ ] #5 All existing gates green uncached: go build ./..., go vet ./..., go test ./..., tests/check-*.sh
<!-- AC:END -->

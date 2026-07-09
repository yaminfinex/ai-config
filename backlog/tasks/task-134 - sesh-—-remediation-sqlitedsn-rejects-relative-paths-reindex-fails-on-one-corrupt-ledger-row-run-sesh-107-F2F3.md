---
id: TASK-134
title: >-
  sesh — remediation: sqlitedsn rejects relative paths; reindex fails on one
  corrupt ledger row (run-sesh-107 F2+F3)
status: In Progress
assignee: []
created_date: '2026-07-09 23:53'
updated_date: '2026-07-09 23:54'
labels:
  - run-sesh-107
dependencies: []
priority: high
ordinal: 134000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement, mechanical. Born from the run-sesh-107 review tail (findings 2+3, both CONFIRMED regressions from the robustness batch commit; thread sesh107-review #34474).

F2 (tools/sesh/internal/sqlitedsn/sqlitedsn.go, withOptions): url.URL{Scheme:"file", Path: relpath}.String() renders a relative path as URI authority ("file://rel.sqlite?...") and modernc/sqlite rejects it with "invalid uri authority". Any relative --data-dir (sesh serve --data-dir ./data), relative XDG_STATE_HOME, or dbq -db rel.sqlite now fails at store.Open/initSchema; both call sites accepted relative paths before. The shell gates never see it because tests/lib.sh always builds absolute mktemp dirs. Fix: resolve to an absolute path (filepath.Abs) or emit the no-authority file: form inside withOptions; add a test with a relative path (chdir-based) and keep the special-characters coverage (spaces, ?, #, %) green.

F3 (tools/sesh/internal/index/index.go, quarantineObservedTimes): one unparseable observed_at string in the disposable quarantine_ledger makes the function error, so Reindex — the recovery tool — fails before rebuilding anything, where pre-change it would simply have rebuilt the ledger. Fix: tolerate corrupt disposable data — skip the bad row (that entry falls back to reindex-time now()), proceed; add a test with a garbage observed_at row proving Reindex succeeds and healthy rows keep their original observed_at.

Settled decisions: minimal contained fixes at the named sites, no refactors, no behavior change beyond the two defects; wire schema and files table untouched as ever.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Relative --data-dir and relative dbq -db work again; test covers a relative path plus special-character paths
- [ ] #2 Reindex succeeds with a corrupt observed_at row present: bad row falls back to now(), healthy rows keep original observed_at, proven by test
- [ ] #3 Full pinned gate green uncached (build, vet, go test ./..., all tests/check-*.sh)
<!-- AC:END -->

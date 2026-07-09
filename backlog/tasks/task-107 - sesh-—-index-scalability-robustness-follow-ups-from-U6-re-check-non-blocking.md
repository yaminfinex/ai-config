---
id: TASK-107
title: >-
  sesh — index scalability + robustness follow-ups (from U6 re-check,
  non-blocking)
status: To Do
assignee: []
created_date: '2026-07-09 06:48'
labels:
  - sesh
dependencies: []
priority: low
ordinal: 107000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement (small batch). From the U6 fix re-check (thread sesh-u6 trail, re-check verdict 2026-07-09), three accepted-but-tracked notes: (1) consumer goroutine lifecycle — store Serve does not watch ctx; on listener error the consumer leaks until process exit; harmless at CLI exit, matters if serve is ever embedded (wire ctx through Serve or stop consumer on Close). (2) busy_timeout set per-connection once in initSchema; move to the sqlite DSN so a silently-replaced pooled connection cannot reset it to 0. (3) scalability: unifyLogicalSessions + updateFileOrdinals + dedupeAll do full-table work on EVERY append while holding the store write lock — blocks ingest as the store grows; scope incremental unification (touched-session-only) before fleet rollout. Also from the original review, still open: reindex whole-file memory allocation (30MB file = 30MB alloc); quarantine_ledger day rebucketing to reindex wall-clock defeats the recent-counts operator metric (preserve observed_at across reindex). Do before/at M4 fleet rollout; none block M2/M3.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Consumer lifecycle bounded (ctx or Close)
- [ ] #2 busy_timeout in DSN
- [ ] #3 Incremental unification or measured OK at fleet scale
- [ ] #4 Reindex streaming or bounded memory
- [ ] #5 Ledger observed_at survives reindex
<!-- AC:END -->

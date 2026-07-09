---
id: TASK-107
title: >-
  sesh — index scalability + robustness follow-ups (from U6 re-check,
  non-blocking)
status: To Do
assignee: []
created_date: '2026-07-09 06:48'
updated_date: '2026-07-09 23:54'
labels:
  - sesh
  - run-sesh-107
dependencies: []
priority: high
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Disposition at code-complete (2026-07-09): all five follow-ups remain open and are PRE-FLEET-ROLLOUT work, not ship blockers for the single-store deployment: consumer lifecycle ctx, busy_timeout in DSN, O(files^2) unification, reindex memory, ledger observed_at. Plus one addition from the U7 review residual: SQLStore.Sessions runs one maxTimestamp query per logical session — fold into the same scalability pass. Schedule before onboarding the full fleet.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-09 23:09
---
FIELD EVIDENCE (2026-07-09, mive, M2 look-see on yamen-superset): AC#3 is not fleet-scale-only — it cripples a SINGLE node backfilling one user's history. Setup: sesh serve (loopback, default data dir) + one sesh ship; 343 claude + 378 codex files; 443MB mirror, 39MB index. Observed: store pegged at 100% CPU >1h40m; ingest collapsed to ~600KB/min (first 30 claude files ACKed in seconds; codex managed 20 files in ~30min); surface recency page took 21.5s to render; transcript loads similar (user-visible).

Mechanism confirmed in code: ProcessAppend (internal/index/index.go:138) runs unifyLogicalSessions on EVERY append PUT that inserts rows -> fileSummaries loads every message UUID of every file into memory (overlapPairs = one query per file), O(files^2) pairwise overlap, full-table logical_session_id/file_ordinal rewrites, then dedupeAll windowed DELETE over the entire messages table. The shipper chunks large files, so one big session triggers the global sweep dozens of times; per-PUT cost grows with total index size (quadratic-ish backfill).

Priority raised low->high. AC#3 is the lead item (plus the U7 residual: fold SQLStore.Sessions per-session maxTimestamp query into the same pass); the other batch items ride along. Orchestrated fix run starting (owner: mive).
---

created: 2026-07-09 23:54
---
run-sesh-107 review-tail residuals (thread sesh107-review #34474), filed here as the scalability umbrella: (1) per-append cost within one logical group is the next perf ceiling — every append re-runs full-file file_ordinal UPDATEs for each group member plus a windowed dedupe over the group partition even when nothing changes, O(session rows) per shipped chunk; (2) readCompleteLine pre-allocates the full max-line cap (8MiB) for any line >64KiB — peak bounded but TotalAlloc churns on transcript files with large base64 lines; size to len+frag and grow instead; (3) Reindex is non-transactional — crash between ledger DELETE and rebuild loses observed_at history (snapshot is in-memory only); (4) PLAUSIBLE unreproduced: insert-time dedup keeps the first-arrived duplicate while Reindex keeps files-table order — arrival-order-dependent equivalence gap for replayed uuids across same-session files; (5) cosmetic: redundant cancelServe defer in cli serve, surface listener logs net.ErrClosed as error on clean shutdown.
---
<!-- COMMENTS:END -->

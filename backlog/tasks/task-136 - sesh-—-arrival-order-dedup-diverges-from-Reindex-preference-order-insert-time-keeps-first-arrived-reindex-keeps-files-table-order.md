---
id: TASK-136
title: >-
  sesh — arrival-order dedup diverges from Reindex preference order (insert-time
  keeps first-arrived; reindex keeps files-table order)
status: To Do
assignee: []
created_date: '2026-07-10 00:11'
labels:
  - sesh
dependencies: []
priority: low
ordinal: 136000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement (or design first if the preference question is contested). Pre-existing divergence, CONFIRMED by the run-sesh-107 review tail (thread sesh107-review #34797) with repro: two files of one wire session sharing a message uuid, shipped in arrival order opposite to file_uuid lex order — insert-time dedup (dedupExists) keeps the first-arrived copy while Reindex's replay keeps the files-table-order one; checksums dad76a7a.../3 vs db4be199.../3. Identical on pre-run tree and current head, i.e. unchanged by the scalability work; upgraded from a PLAUSIBLE residual note on the scalability umbrella task.

Impact: bounded — which duplicate copy survives differs, and state is stable after any reindex; no session splitting. Fix directions from the review: dedup-by-preference at unification time instead of skip-at-insert, or make the reindex replay order incorporate observed arrival order. Either must keep the equivalence property incremental == post-Reindex for duplicate rows in both arrival orders, with tests covering both orders.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Incremental and post-Reindex states agree on which duplicate survives, for both arrival orders (tests cover both)
- [ ] #2 Full pinned gate green uncached
<!-- AC:END -->

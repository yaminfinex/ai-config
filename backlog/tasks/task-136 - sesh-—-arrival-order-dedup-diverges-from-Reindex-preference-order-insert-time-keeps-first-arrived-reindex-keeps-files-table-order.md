---
id: TASK-136
title: >-
  sesh — arrival-order dedup diverges from Reindex preference order (insert-time
  keeps first-arrived; reindex keeps files-table order)
status: Done
updated_date: '2026-07-15'
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
- [x] #1 Incremental and post-Reindex states agree on which duplicate survives, for both arrival orders (tests cover both)
- [x] #2 Full pinned gate green uncached
<!-- AC:END -->

## Evidence (Done, 2026-07-15)

Lane: branch task-136-dedup-order (builder-gori, codex gpt-5.6-sol —
lane respawned on codex after the owner model directive, Fable-authored
state discarded; sole substance reviewer reviewer-lada, codex; hera
merge gate). internal/index only: index.go, index_test.go.

- Fix direction (a): insertRows retains duplicate candidates; bounded
  dedupeLogical after unification uses the identical preference
  ORDER BY as Reindex dedupeAll (timestamp nullness, timestamp,
  file_ordinal, line_ordinal, file_uuid, generation, id).
- Review P1 (FIXED 83d7e8c, independently closed #75811): cross-pass /
  window-straddling duplicates diverged one layer down — losing
  one-row files vanished from the incremental ordinal universe while
  Reindex assigned ordinals pre-dedupe. Fix: compact file ordinals
  post-dedupe — touched component incrementally (via the pinned
  sameLogicalFiles query), globally in Reindex.
- Red-baseline chain on bus: original two-order divergence red on main;
  cross-pass regression red at 6ec8379; both green at 83d7e8c;
  second-Reindex fixed point; quarantine-row variant green (reviewer).
- Constraints held: no DDL, wire untouched, empty-uuid non-participation
  (grok) preserved, append cost component-bounded, maint_rows truthful,
  single-transaction crash safety verified by reviewer.
- AC2: announced/sequenced uncached battery 17/17 twice (#75644 at
  6ec8379, #75787 at 83d7e8c) + vet + full tests + index race suite.
- Verdict READY TO MERGE #75817; merge 7dac4c3 --no-ff; post-merge
  house battery 59/59; pushed.
- Deploy: tag sesh-v0.1.13 (84ac41a; 1 intervening commit, 0 sesh
  files); store live "sesh-v0.1.13"; release published
  sesh-v0.1.13-3-gc76d270 (shared checkout advanced mid-deploy via
  hera task-216 merge — 3 commits, 0 sesh files, suffix benign per
  v0.1.11 precedent); this node updated v0.1.12 -> v0.1.13, census
  flipped, nodes page 200/0.37s, shipping healthy.

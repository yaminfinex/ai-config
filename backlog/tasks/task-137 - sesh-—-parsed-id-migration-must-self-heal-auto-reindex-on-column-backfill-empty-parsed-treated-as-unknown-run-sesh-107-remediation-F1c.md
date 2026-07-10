---
id: TASK-137
title: >-
  sesh — parsed-id migration must self-heal: auto-reindex on column backfill +
  empty-parsed treated as unknown (run-sesh-107 remediation F1c)
status: To Do
assignee: []
created_date: '2026-07-10 00:23'
labels:
  - run-sesh-107
dependencies: []
priority: high
ordinal: 137000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement, small. Born from the run-sesh-107 review tail's upgrade-path probe (thread sesh107-review #35004): two CONFIRMED migration hazards in the parsed_logical_session_id mechanism, both repro'd by the reviewer.

Hazard 1 (data, not predicate): ensureParsedLogicalColumn backfills parsed := logical for legacy rows. For sessions unified BEFORE the upgrade that equality erases the unification marker, so the first post-upgrade append to a resumed file gets no inheritance and re-splits the session (the original resumed-session symptom, resurrected for exactly the pre-upgrade population — which is what any live store carries). True parsed values are not in the DB; only a mirror reparse reconstructs them.

Fix (settled): the migration must self-heal — when ensureParsedLogicalColumn actually ADDS the column (fresh migration, not fresh DB), trigger the one-time full Reindex before append processing begins, so parsed values are real before any inheritance decision runs. Ordering matters: the reindex must complete before the store's append-event consumer starts handling PUTs. A fresh database that creates the table with the column already present must NOT pay a reindex. Log the migration reindex clearly (it is a one-time startup cost proportional to mirror size).

Hazard 2 (mixed binaries): a pre-migration binary writing rows after the migration (old serve or old reindex against the same store) inserts parsed = '' via the ALTER default; empty differs from every logical, so those rows read as unification-written and inheritance misfires (reviewer repro: response_item inherited the codex meta id).

Fix (settled): the inheritance predicate treats empty parsed as unknown — rows with parsed = '' never count as unification evidence (logical <> parsed AND parsed <> '').

Settled decisions — do not re-litigate; tension = STOP and report on your unit thread:
- Reindex semantics unchanged; files-table and wire schema untouched (schema — writing existing columns' data is fine if needed, but the chosen fixes need neither).
- No new operator step: correctness must not depend on someone remembering to run sesh reindex after upgrading.
- Tests: reviewer's two hazard shapes as equivalence tests — (1) pre-upgrade-unified store (simulate: build store with old-style rows, drop/re-add column to force backfill, append to the resumed file → checksum parity with Reindex), (2) post-migration rows with parsed='' present, append to codex meta-only file → items keep wire id. Plus fresh-DB control asserting no reindex is triggered.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Upgraded store (column freshly added over legacy rows) self-heals: first post-upgrade append to a pre-upgrade-unified session keeps it unified, checksum parity with Reindex, no operator action
- [ ] #2 Fresh database pays no migration reindex (control test)
- [ ] #3 Rows with empty parsed id never trigger inheritance (mixed-binary shape: codex items keep wire id)
- [ ] #4 Benchmark still flat vs unrelated-file count; full pinned gate green uncached
<!-- AC:END -->

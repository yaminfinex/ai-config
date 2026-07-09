---
id: TASK-098
title: 'sesh U6 — index: parse-on-ingest + dedup + reindex (M2)'
status: Done
assignee:
  - sesh-store-soho
created_date: '2026-07-09 05:28'
updated_date: '2026-07-09 06:48'
labels:
  - sesh
dependencies:
  - TASK-095
priority: medium
ordinal: 98000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Lane: store (codex worker). Deliverable: internal/index — consumes the append-event bus, parses complete lines only (trailing partials held back by byte span), per-tool defensive parsers extracting (logical session id, message uuid, role, timestamp); unknown entry types index as opaque-but-ordered rows, only unparseable lines quarantine. Logical session id derived from parsed content, wire id as fallback claim (R9); dedup key = (logical session, entry type, message uuid). sesh reindex truncates index tables and replays the mirror through the same code path. Quarantine ledger with counts by day; index-write failure marks dirty-for-reindex (distinct from parse quarantine, R12). Requirements R9,R10,R12,R25.

Read first: /home/grace/Coding/ai-config/napkins/sesh-build/playbook.md, plan U6 section (git show 05dfc47:docs/plans/2026-07-09-001-feat-sesh-session-service-plan.md), docs/specs/sesh-wire.md index schema, captures Lane 2 (git show 6843649:docs/design/2026-07-09-sesh-task-captures.md). Thread: sesh-u6.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Resume-pair fixture: zero duplicate message uuids, one logical session (S2)
- [x] #2 Colliding ids across entry types do not cross-merge sessions; trailing partial excluded until complete
- [x] #3 Unparseable-but-valid-JSONL quarantines without blocking other files (S10); quarantine counts exposed
- [x] #4 reindex from mirror alone reproduces identical index content, proven twice in a row
- [x] #5 Injected index-write failure -> dirty-for-reindex; next reindex heals
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged to sesh-build @ 1e6eb1f (conflict: stub list — ship real since U4, reindex real since U6; resolved keeping status+admin only). Provenance: 3fe21c8 impl -> cross-family opus review (MERGE-WITH-FIXES: blocker A serve never consumed append bus, blocker B file_ordinal=generation; +5 required) -> 22fcdb7 fixes -> fresh opus re-check ACCEPT (all items PASS with biting tests; consumer serializes via store write lock, file_ordinal full-recompute per append is deterministic incl. late joiners). Keep-property (generation absent from dedup key) pinned per binding note. Low-severity re-check notes -> TASK-107. Orchestrator gates + full harness suite green on merged state. Trail: thread sesh-u6.
<!-- SECTION:NOTES:END -->

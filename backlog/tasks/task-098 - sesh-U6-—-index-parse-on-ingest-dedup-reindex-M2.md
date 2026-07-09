---
id: TASK-098
title: 'sesh U6 — index: parse-on-ingest + dedup + reindex (M2)'
status: To Do
assignee: []
created_date: '2026-07-09 05:28'
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
- [ ] #1 Resume-pair fixture: zero duplicate message uuids, one logical session (S2)
- [ ] #2 Colliding ids across entry types do not cross-merge sessions; trailing partial excluded until complete
- [ ] #3 Unparseable-but-valid-JSONL quarantines without blocking other files (S10); quarantine counts exposed
- [ ] #4 reindex from mirror alone reproduces identical index content, proven twice in a row
- [ ] #5 Injected index-write failure -> dirty-for-reindex; next reindex heals
<!-- AC:END -->

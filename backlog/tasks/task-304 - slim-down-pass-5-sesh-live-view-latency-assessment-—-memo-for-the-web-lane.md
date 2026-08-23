---
id: TASK-304
title: 'slim-down pass 5: sesh live-view latency assessment — memo for the web lane'
status: Done
assignee: []
created_date: '2026-08-21 04:40'
updated_date: '2026-08-21 04:59'
labels:
  - sesh
  - slimdown
dependencies: []
references:
  - napkins/herder-slimdown-charter.md
priority: high
ordinal: 303500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner-requested side research 2026-08-21 (charter: napkins/herder-slimdown-charter.md pass 5). Assess sesh as the live transcript plane for the coming web lane. sesh's spec/plan binds 'no live-relay guarantees' (plan §9 / system-boundaries) — binding explicitly up for rethink on the merits. Measure actual end-to-end latency (transcript write → fsnotify → ship → parse → queryable), identify what a 'live enough' guarantee would need to say (latency bound, ordering, gap detection, backfill/restart), and recommend: amend the binding, keep it and read live views elsewhere, or something better. Output: a short memo the web lane can act on. Findings file: napkins/sesh-live-view-findings.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 End-to-end latency measured (or, if impossible non-destructively, characterized analytically and labeled as such) with per-stage attribution
- [x] #2 The binding quoted verbatim with its rationale; recommendation states what a live-enough guarantee must promise and what breaks it today
- [x] #3 Memo delivered and cited by the charter's web-lane parking section
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Delivered 2026-08-21: memo at napkins/sesh-live-view-memo.md over findings napkins/sesh-live-view-findings.md; charter's web-lane parking section now cites both. VERDICT: amend the binding — sesh is suitable as the live transcript plane. Measured live pipeline (66 passive trials riding real sessions, zero synthetic writes): write→durable-queryable p50 357ms / p95 2.5s / max 3.9s; fsnotify hint path carried 66/66, the 60s rescan never fired. Binding rewrite proposed as two-tier conditional guarantee (seconds-class best-effort + 60s conditional bound + staleness honesty surface). Preconditions for the web lane: tail on (generation, file_ordinal, line_ordinal) not display order; fix silent index-row drops (128-buffer overflow → dirty flag healed only by manual minutes-long ingest-blocking reindex) via read-through-mirror or incremental auto-reindex; subagent transcripts NEVER ship (admission ^uuid.jsonl only) — widen admission or scope the claim; SSE endpoint is pre-planned v2 on R25. Options rejected: rebuild-elsewhere (contradicts parking ruling), hard-realtime promise (unsupportable). Awaiting owner ratification of the binding amendment.
<!-- SECTION:NOTES:END -->

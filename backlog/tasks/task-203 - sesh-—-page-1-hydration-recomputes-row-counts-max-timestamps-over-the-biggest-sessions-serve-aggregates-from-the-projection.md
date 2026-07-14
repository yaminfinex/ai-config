---
id: TASK-203
title: >-
  sesh — page-1 hydration recomputes row counts/max-timestamps over the biggest
  sessions; serve aggregates from the projection
status: In Progress
assignee:
  - mika
created_date: '2026-07-14 01:50'
labels:
  - sesh
dependencies: []
priority: high
ordinal: 202000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Residual isolated after the serve-stale fix (post-quiesce probes 2026-07-14: flat sessions page 1 2.1-2.6s steady vs page 48 0.83-0.94s vs nodes entry 0.36s floor, corpus 5193, no rebuilds in flight; routes are post-IA-rework: '/' is the nodes view, the flat list is /sessions): recency hydration computes per-session aggregates (message row counts, max activity timestamp) by walking each listed session's index rows at request time. Page 1 lists the most recent = largest sessions (multi-thousand-row transcripts), so the first page pays hundreds of thousands of row visits per render while deep pages of small sessions are cheap. Fix direction: carry these aggregates in the recency projection (they are already computed corpus-wide during the ranking rebuild, or can be) and hydrate only genuinely per-request data live; staleness of a row count between rebuilds is acceptable under the serve-stale bound already documented in the read/write-split design note delta. Alternative directions welcome if measured better (e.g. incremental per-session aggregate maintenance — but that drifts toward write-path work, coordinate with the append-cost task if so). Constraint: plan-allowlist gate discipline; frozen index schema; no per-request corpus work may return.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Sessions page-1 render cost is independent of listed-session sizes; live post-deploy probe from a ~180ms client shows /sessions page 1 within ~2x of deep-page time (applies equally to node-filtered page 1)
- [ ] #2 Fixture gate pins the property (page of max-size sessions renders within budget; no per-listed-session row walks on the hot path)
- [ ] #3 Docs current per decision-001
<!-- AC:END -->

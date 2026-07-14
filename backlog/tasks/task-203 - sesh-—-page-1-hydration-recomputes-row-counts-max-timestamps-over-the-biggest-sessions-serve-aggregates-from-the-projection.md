---
id: TASK-203
title: >-
  sesh — page-1 hydration recomputes row counts/max-timestamps over the biggest
  sessions; serve aggregates from the projection
status: Done
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
- [x] #1 Sessions page-1 render cost is independent of listed-session sizes; live post-deploy probe from a ~180ms client shows /sessions page 1 within ~2x of deep-page time (applies equally to node-filtered page 1)
- [x] #2 Fixture gate pins the property (page of max-size sessions renders within budget; no per-listed-session row walks on the hot path)
- [x] #3 Docs current per decision-001
<!-- AC:END -->

## Evidence (Done, 2026-07-14)

Merged to main at 48c687a (--no-ff, linear 424af52 -> d31196c, 6 files),
pushed; deployed live as sesh-v0.1.6 (store + release, client update
verified 0.1.5 -> 0.1.6).

Fix: projection entries carry row counts (clean+quarantined), max parsed
non-quarantined timestamp, and member file-generation keys, computed in the
rebuild's off-request corpus passes; page hydration reads live tables only
for per-request data and runs zero statements against the message index.
Fixture: max-size page (50x2000-row sessions) store-side work 430ms ->
0.6ms; warm queries 6 -> 4.

Review: 2 substantive findings (files-seek plan pinning; label-staleness
doc wording), both closed with proven negatives (prefix-seek variant trips
the four-term plan assertion; row-walk detector trips on the old live
hydration shape); one finding withdrawn with root cause (transcript
summary metadata is not the bus payload). Final verdict APPROVE #68092;
merge-gate battery 58/58 green post-merge.

AC1 live probe (~180ms client, post-deploy, warm): /sessions page 1
0.45-0.51s vs page 48 0.37s (~1.25x, bound was ~2x; was 2.1-2.6s on
v0.1.5); node-filtered page 1 0.44-0.48s; nodes entry 0.36s RTT floor.
Cold start 10.7s = the documented one-time blocking rebuild after restart.

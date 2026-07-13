---
id: TASK-196
title: >-
  sesh — surface projection rebuild must be single-flight and serve-stale; bulk
  ingest makes every page load pay a full rebuild
status: To Do
assignee: []
created_date: '2026-07-13 21:54'
labels:
  - sesh
dependencies: []
priority: high
ordinal: 195000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Live regression observed during the first real bulk sync (a Mac node shipping its ~3k-file corpus; store sesh-v0.1.3, corpus 2373→4901 sessions in hours): every projection-backed surface route ran 11-25s per request from a ~180ms client while /nodes stayed at the 0.36-0.48s floor. The read/write split held (reads never queued behind writes); the entire cost is the recency projection rebuild — under continuous ingest the version stamp moves between every request, the bounded-recency design's read-your-own-writes choice (no rebuild floor) degenerates to a full corpus-scale ranking rebuild per request, and nothing prevents concurrent duplicate rebuilds across the read pool. This recurs at every onboarding — exactly when new users click the announcement link.
Fix shape (owner-visible behavior: instant page, slightly stale list is acceptable for a recency view): single-flight the rebuild (concurrent requests share one rebuild or serve the previous projection), serve-stale-while-revalidating with a bounded staleness window under churn, and record the rebuild duration via the existing debug timing so live cost stays measurable. The projection contract (complete ranked key list, plan-allowlist gate discipline) is unchanged; the read-your-own-writes property may be relaxed to bounded staleness — state the new bound explicitly in the README/design note per decision-001. Deeper append-cost work stays in the append-index corpus-cost task; this task is the read-side containment.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Under sustained ingest, projection-backed routes serve within a small constant over the RTT floor (no per-request corpus rebuild); concurrent requests never run duplicate rebuilds
- [ ] #2 Staleness is bounded and stated; a fixture gate proves single-flight + serve-stale under concurrent load with a moving stamp
- [ ] #3 Live verification after deploy from a ~180ms client during active ingest, recorded on the task
- [ ] #4 Docs current per decision-001 (README + bounded-recency/read-write-split design-note deltas)
<!-- AC:END -->

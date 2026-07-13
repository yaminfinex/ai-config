---
id: TASK-190
title: 'sesh — surface homepage must render bounded recency, not the whole corpus'
status: Done
assignee:
  - mika
created_date: '2026-07-13 10:30'
updated_date: '2026-07-13 11:38'
labels:
  - sesh
dependencies: []
priority: high
ordinal: 189000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
With the fleet onboarding (~3-5k files per node already), the surface '/' recency page renders unbounded rows — first fix before the Slack announcement drives everyone to it. Bound the homepage: recent-N sessions by default (query-level LIMIT, no full-index scan per request), pagination or load-more for history, and the /nodes page stays cheap. Surface reads the frozen index schema through the Store seam — this is read-side UI/query work only; wire doc and index schema untouched. Fixture gate should cover a large-corpus render staying under a sane row cap and query budget.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Homepage renders bounded recent sessions with request-time query work genuinely bounded (projection slice or LIMITed query — bounded work is the substance, per review closure); no corpus-wide scan or unbounded render on any surface route
- [x] #2 Older history reachable (pagination or load-more); /nodes unaffected and cheap
- [x] #3 Fixture gate covers a 5k-file corpus: bounded rows, bounded query time
- [x] #4 Docs current per decision-001 (README surface section)
<!-- AC:END -->

---
id: TASK-190
title: 'sesh — surface homepage must render bounded recency, not the whole corpus'
status: To Do
assignee: []
created_date: '2026-07-13 10:30'
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
- [ ] #1 Homepage renders bounded recent sessions via LIMITed query; no unbounded scan or render on any surface route
- [ ] #2 Older history reachable (pagination or load-more); /nodes unaffected and cheap
- [ ] #3 Fixture gate covers a 5k-file corpus: bounded rows, bounded query time
- [ ] #4 Docs current per decision-001 (README surface section)
<!-- AC:END -->

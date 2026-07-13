---
id: TASK-193
title: sesh — bound the transcript page and consider nodes-first navigation
status: To Do
assignee: []
created_date: '2026-07-13 19:24'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 192000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Two related surface UX/perf items, second one owner-suggested but explicitly not mandated ('my suggestions are not necessarily the move'):
1. Transcript pages render the ENTIRE session as one response — measured 8.7MB of HTML for a single large session on the live store. Bound it the same way the homepage was bounded: paginated or windowed message rendering (e.g. latest window + older links), raw download stays whole-file via the existing raw route.
2. Owner-suggested IA option, to be judged on its merits once the transport fix (see the surface TTFB task) lands: nodes page as the landing view, drill into a node for its paginated session list. Recency-first may remain right; whoever takes this decides with the owner and documents the choice.
Read-side only; frozen index schema through the Store seam; plan-allowlist gate discipline from the bounded-recency work applies to any new queries.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Transcript page response is bounded for arbitrarily large sessions; raw route unchanged
- [ ] #2 New/changed queries pass the plan-allowlist gate; large-session fixture proves the bound
- [ ] #3 Navigation decision (nodes-first or recency-first) made explicitly with the owner and recorded
- [ ] #4 Docs current per decision-001 (README surface section)
<!-- AC:END -->

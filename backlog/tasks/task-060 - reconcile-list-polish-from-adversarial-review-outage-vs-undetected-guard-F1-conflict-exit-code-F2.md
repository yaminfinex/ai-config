---
id: TASK-060
title: >-
  reconcile/list polish from adversarial review: outage-vs-undetected guard
  (F1), conflict exit code (F2)
status: To Do
assignee: []
created_date: '2026-07-08 05:59'
labels: []
dependencies: []
priority: low
ordinal: 60000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
From opus review of TASK-046 (#6436, both low, non-blocking): F1 — list.go buildLiveIndex/unmatchedStatus: a transient herdr agent-list OUTAGE with pane list succeeding flips EVERY active row to undetected with restart advice (global misreport on a hiccup; display-only). Add the cheap guard: empty agents + nonempty panes = suspected outage, say so instead. F2 — reconcile exits 1 only on ambiguous; a conflict (stored terminal live as a DIFFERENT agent — operator-actionable) exits 0, so scripted gates miss it. Spec does not pin exit semantics; decide and encode: conflict should likely be non-zero distinct from ambiguous. Small unit; goldens exist to extend (livefail, json_mixed).
<!-- SECTION:DESCRIPTION:END -->

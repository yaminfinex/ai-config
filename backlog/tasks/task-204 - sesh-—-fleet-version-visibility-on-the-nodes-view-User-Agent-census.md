---
id: TASK-204
title: sesh — fleet version visibility on the nodes view (User-Agent census)
status: To Do
assignee: []
created_date: '2026-07-14 02:00'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 203000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Deferred from the surface IA rework: the nodes view has no version column because no version data exists read-side (last_seen carries hostname/os_user/last_put_at only). Original shape from the distribution options memo (backlog/docs/doc-002 T4): the store records each shipper's version from the wire User-Agent at PUT time into node bookkeeping (write-path change, wire-compatible — UA is already sent), and the nodes view renders it plus highlights nodes outside the current+previous support window (ops/README version-skew policy). Store bookkeeping only — NOT the frozen wire-visible index schema; same class as the fact_observations_session index precedent.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Store records shipper version per node from the existing User-Agent; no wire protocol change
- [ ] #2 Nodes view shows the version column; out-of-window versions visibly flagged
- [ ] #3 Docs current per decision-001 (ops/README version-skew + surface README)
<!-- AC:END -->

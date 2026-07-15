---
id: TASK-235
title: 'sesh: codex arm for the correlation benchmark'
status: To Do
assignee: []
created_date: '2026-07-15 07:34'
labels:
  - sesh
dependencies: []
priority: low
ordinal: 234500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Reviewer follow-up from the shipper CPU-spin fix: BenchmarkCorrelationAcrossFivePasses builds all discovered entries as claude-family, so it never exercises the codex /proc FD-join — plausibly why the O(transcripts x processes x FDs) cost curve escaped it. Add a codex arm so the correlation cost curve is visible per family. The structural guard (FD-table-read-once counter test) already pins the invariant; this is observability of the cost curve, not protection.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Benchmark covers codex-family discovered entries through the real correlation path
<!-- AC:END -->

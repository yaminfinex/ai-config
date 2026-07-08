---
id: TASK-028
title: hcom 0.7.23 upgrade audit — re-ground the bootstrap/codex integration
status: In Progress
assignee:
  - audit-028-zoru
created_date: '2026-07-07 12:23'
updated_date: '2026-07-08 03:28'
labels: []
dependencies: []
priority: medium
ordinal: 28000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
hcom v0.7.23 is available (noticed 2026-07-07 mid run-herder-dx; machine runs 0.7.22 via mise). The herder integration is source-grounded in v0.7.22: sessionstart rewrite byte-faithfulness (TASK-001/002), codex developer_instructions merge + resume/fork strip predicate (TASK-014/017), -p background switch (TASK-010), pin/seed behavior (TASK-011). Before or when updating: diff v0.7.22..v0.7.23 source for changes to bootstrap.rs, hooks, launch/strip logic; re-run the full battery + the live smokes that pinned those behaviors; update mirrored predicates/constants if upstream moved. Degrade-safe design should hold (parse failure -> stock output) but the mirrored STRIP PREDICATE and -p switch are behavioral mirrors that can silently drift. Do NOT update mid-run; user decides timing.
<!-- SECTION:DESCRIPTION:END -->

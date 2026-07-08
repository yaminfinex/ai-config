---
id: TASK-057
title: 'wave A3: node mint + marker gate (AC-21, spec 6.1)'
status: To Do
assignee: []
created_date: '2026-07-08 05:55'
labels: []
dependencies: []
priority: medium
ordinal: 57000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Plan unit A3 (spec-plan-wave-a.md). Lazy node mint on first locked write (node_registered row + node_id marker; concurrent first writes converge under lock). Gate on every registry-writing command: marker/registry agree -> proceed; both absent -> mint; disagree/half-present -> refuse with herder node init guidance. herder node init [--new] (idempotent; --new = clone repair). Tests: bootstrap, half-copied state dir refusal, clone repair keeps prior rows. Depends: A2 (TASK-056).
<!-- SECTION:DESCRIPTION:END -->

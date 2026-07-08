---
id: TASK-057
title: 'wave A3: node mint + marker gate (AC-21, spec 6.1)'
status: In Progress
assignee: []
created_date: '2026-07-08 05:55'
updated_date: '2026-07-08 07:32'
labels: []
dependencies: []
priority: medium
ordinal: 57000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Plan unit A3 (spec-plan-wave-a.md). Lazy node mint on first locked write (node_registered row + node_id marker; concurrent first writes converge under lock). Gate on every registry-writing command: marker/registry agree -> proceed; both absent -> mint; disagree/half-present -> refuse with herder node init guidance. herder node init [--new] (idempotent; --new = clone repair). Tests: bootstrap, half-copied state dir refusal, clone repair keeps prior rows. Depends: A2 (TASK-056).
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 07:31
---
[hera 2026-07-08] A2 merged; dispatching A3 (node mint + gate) to a fresh codex worker.
---

created: 2026-07-08 07:32
---
[hera 2026-07-08] Dispatched: codex worker wave-a3-memo (guid a1d6ca7a), worktree wave-a3-node-gate (workspace w6), brief napkins/run-herder-dx/brief-wave-a3.md. Scope: lazy node mint inside the A2 locked write path (node_registered row + marker file, converge under flock), gate on every writing command (agree->proceed+stamp, absent->mint, disagree/half->refuse with node init guidance), herder node init [--new] (idempotent; --new=clone repair keeping prior rows), §5.4 grandfathering preserved. Adversarial review mandatory (node identity = engine risk). Spawn prompt delivered via bus at birth — F1 capture fix working, workaround retired.
---
<!-- COMMENTS:END -->

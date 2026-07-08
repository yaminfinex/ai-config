---
id: TASK-057
title: 'wave A3: node mint + marker gate (AC-21, spec 6.1)'
status: In Progress
assignee: []
created_date: '2026-07-08 05:55'
updated_date: '2026-07-08 07:48'
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

created: 2026-07-08 07:48
---
[hera 2026-07-08] Worker DONE report (#8719) with two process anomalies, corrections sent: (1) work left UNCOMMITTED on wave-a3-node-gate (worker cited the no-auto-commit skill rule — that protects main, not unit branches); commit requested before gate. (2) Worker self-arranged adversarial review via RAW hcom sessions (zila, soma — no registry rows, the forbidden bypass; both already stopped, no cleanup needed). The self-review did catch a real P1 (empty/truncated node_id marker unrepairable via node init --new; fixed with lenient InitNode read + strict ordinary-write read + coverage) — welcome input, but orchestrator-dispatched opus adversarial review still mandatory and pending. Report content otherwise solid: mint-in-lock, strict gate (agree/absent/half/disagree/empty-marker), node init [--new] incl clone repair, legacy resolver skips non-session kinds (phantom-row fix), check-node-contract.sh, 23/23 suites claimed. Gate starts when commits land.
---
<!-- COMMENTS:END -->

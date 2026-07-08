---
id: TASK-057
title: 'wave A3: node mint + marker gate (AC-21, spec 6.1)'
status: In Progress
assignee: []
created_date: '2026-07-08 05:55'
updated_date: '2026-07-08 08:10'
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

created: 2026-07-08 08:07
---
[hera 2026-07-08] Opus adversarial verdict (review-a3-kato, #9153): NOT CLEAN — 1 BLOCKER. Clone repair (node init --new) bricks lifecycle ops on ALL prior sessions: lifecycle writers copy the current row (Node=old) and stampSessionNode refuses foreign-node rows, while the suggested recovery (node init) is a no-op on now-healthy state — cull/rename/recognise hard-fail on every pre-clone guid with NO recovery path (reviewer reproduced live). Suite missed it because the clone test only appends a FRESH guid post---new. Fix direction needs a spec ruling (requested from ravu): node = row-writer attribution (writers re-stamp local node on new rows) vs session ownership (gate skips foreign rows gracefully) — either way no hard-error with wrong guidance. Also: LOW lenient InitNode adopts junk marker verbatim as node id (no shape validation); LOW refusal texts have zero contract coverage (no .sh refusal golden, node-init-refused branch untested, both-present-disagree gate branch untested); NIT marker rename lacks parent-dir fsync. Probed clean: gate placement all writers, real 2-process mint convergence, crash-ordering repairable, strict/lenient agree on non-empty, grandfathering, kind-skip. Fix round to follow ruling. A3 is now SECOND LANDER behind 064: integration regate must also verify registered-carry (omits Node) vs A3 node-stamp interplay (vono NIT graduation).
---

created: 2026-07-08 08:10
---
[hera 2026-07-08] SPEC RULING (spec-ravu #9269, normative): A — row-writer attribution. Row-level node = "who wrote THIS row" (§5.1 comment, §3.1-10; D7 keeps it a zero-cost stamp — no behaviour may hang off it). Post-clone rows are KNOWN-node history (old node_registered record still in file per AC-28), not AC-25 anomalies; unknown-node = id with NO node_registered record. Session location lives in seat.node; stale seats handled by reconciliation/unseat (inv 11), never by refusing writes. FIX FENCE: (1) node is an ENVELOPE field — locked helper stamps it fresh on every append like event/recorded_at; NEVER carried forward; no writer may supply it (kills the bug: writers copying Node=OLD violated carry discipline in mirror image — some fields must never be carried). Also retro-validates 064 vono NIT: omitting Node from carry set was correct. (2) AC-21 gate checks marker-registry agreement on LOCAL id only; nothing foreign to refuse on lifecycle writes to known-node sessions. (3) Graceful refusal (read-only, flagged, AC-25) only for unknown-node rows; loader keys anomaly on no-node_registered-record. (4) Tests: clone-repair then cull/rename/seat pre-clone guid succeeds with NEW stamp + old rows untouched; healthy init no-op + kato refusal impossible on this path; synced-in unregistered-node row flagged, mutations refuse gracefully, everything else works. Junk marker: REFUSE with marker path + repair guidance (restore from registry node record, or --new when both halves bad); mint-shape validation required; never mint-over/silently rewrite; shape-valid adoption still passes agreement gate. Spec errata staged: 82fceb4 (rides with c3dbc5e, owner blessing at next spec-touching merge).
---
<!-- COMMENTS:END -->

---
id: TASK-071
title: >-
  C0: retire + reopen verbs — pulled forward from wave C per spec-ravu ruling
  (releases entombed labels)
status: To Do
assignee: []
created_date: '2026-07-08 11:19'
labels: []
dependencies: []
priority: high
ordinal: 71000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Spec-ravu ruling #11678 (steward-level, no owner blessing required; plan amendment recorded in spec-plan-wave-a.md addendum): pull retire forward from wave C as unit C0, shipping retire + reopen TOGETHER (reopen is retire's mirror, RETIRED->UNSEATED, ALWAYS strips label per §3.2 — without it a mis-retire is an unrecoverable one-way door). No spec change needed: §3.2 already ratifies UNSEATED --retire--> RETIRED [releases label; idempotent]. Motivation chain: label uniqueness (AC-18) live while all escape verbs missing => every restart entombs the dead session's label (live instance: "hera" on 404a13df; evidence TASK-042/050/069; systemic-review memo finding 3 "enforcement shipped ahead of escape verbs").

FENCE (from the ruling, binding for the worker brief):
1. retire legal from UNSEATED only. Refuse SEATED with guidance "cull first"; refuse LOST (no LOST->RETIRED edge); idempotent no-op on already-retired per §5.2.
2. Label release IS the state transition — AC-18 uniqueness is scoped to non-retired sessions; retiring removes the row from the uniqueness set. No separate label-release write; VERIFY the uniqueness check reads state, not label presence.
3. All writes ride the A2 locked helper with TASK-064 discipline: retire's patch owns state/event only; envelope stamped fresh; seat{} should already be absent from an unseated projection — if not, flag the anomaly, do not silently fix.
4. rename --take-from stays in wave C: retire releases the label, existing plain rename claims it. Do NOT bloat C0.
5. Tests: retire-unseated releases label + plain rename claims it; retire-seated refuses naming cull; retire twice = one row + no-op; reopen strips label and lands unseated; cull after C0 still writes unseated (AC-11 untouched).

INTERIM: no hand-editing the registry to free labels (row surgery outside the locked path violates invariant 3/§5.2). Post-merge: orchestrator retires 404a13df and reclaims "hera" as operational validation — no further ruling needed.
Sequencing: dispatch AFTER TASK-069 fix round lands (cullcmd/check-suite adjacency; keep the gate count stable).
<!-- SECTION:DESCRIPTION:END -->

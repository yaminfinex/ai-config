---
id: TASK-071
title: >-
  C0: retire + reopen verbs — pulled forward from wave C per spec-ravu ruling
  (releases entombed labels)
status: Done
assignee:
  - '@hera'
created_date: '2026-07-08 11:19'
updated_date: '2026-07-08 12:02'
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

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 11:31
---
Dispatched: codex worker task071-8ae31a57, worktree task-071-retire-reopen off main 38bf281. Brief: napkins/run-herder-dx/brief-071.md (ruling fence binding; retire+reopen together; --take-from and repeat-cull dedupe explicitly fenced out). Post-merge validation reserved: retire 404a13df, reclaim label hera.
---

created: 2026-07-08 11:52
---
Round 1: worker DONE 48830b1; my gate green 29/29 from worker worktree. Opus review (review071-dosa #12187): REQUEST-CHANGES — core verbs SPEC-FAITHFUL (ruling honored: uniqueness reads state at all three sites, label strip is belt-and-braces not load-bearing; carry-forward cannot resurrect retired rows; already-retired is a real confirmed no-op; 069 paths untouched). Blocking P2-1: rename lacks retired/lost guard — retired row silently reacquires a label via labelled-event normalization (§3.2 violation, recoverable via reopen). LOW-2 reopen help over-advertises pane_id (unreachable for Seat==nil states); LOW-3 pane resolver lexicographic-not-liveness-aware (benign today, latent; guard ordering verified safe); LOW-4 fork of retired parent unguarded -> spec question to spec-ravu (#12200); LOW-5 coverage gaps (legacy-closed resume, reopen-then-rename, retire-by-pane seated-successor refusal). Fix round dispatched (#12199, acked): P2-1 guard + LOW-2 help + LOW-5 tests; LOW-4 fenced out pending ruling. Reviewer standing by for delta.
---

created: 2026-07-08 11:53
---
LOW-4 resolved by spec-ravu ruling #12237: fork of a retired parent is SPEC-LEGAL (transcript is the substrate; fork mints a new guid, parent undisturbed — no illegal edge; the resume/fork asymmetry is principled). No guard; pin tests + explicit lost-parent refusal filed as TASK-074 (bundle-eligible). Erratum 6b59162 staged — pending blessing batch is now five items. This round's fix scope unchanged.
---

created: 2026-07-08 12:02
---
Round 2: fix 25846ac — my regate 29/29; opus delta APPROVE (#12362: P2-1 guard ordering verified by code — refusals before collision check and pane-rename; all LOW-5 tests confirmed real; no fork guard sneaked in). Merged main (no-ff); post-merge gate on main from repo root: herder+bottle OK, 29/29. LIVE VALIDATION COMPLETE: herder retire 404a13df -> label released; herder rename bbbc84c2 hera -> succeeded; registry now shows exactly one live hera row (bbbc84c2, pane w6554208c1918a12:p1, BUS @hera) — the TASK-042/050 label entombment is resolved in production by C0. Dosa advisory nits: (1) vacuous herdr_rename_argv probe clause in the contract check -> routed to TASK-074 bundling; (2) rename retired-guard not LegacyV1-scoped (stricter than resume) — acceptable, undocumented contract. Worker task071-8ae31a57 + reviewer review071-a05ae17a culled; worktree/branch removed.
---
<!-- COMMENTS:END -->

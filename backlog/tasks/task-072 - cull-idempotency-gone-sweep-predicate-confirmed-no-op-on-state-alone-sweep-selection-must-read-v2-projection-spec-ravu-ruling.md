---
id: TASK-072
title: >-
  cull idempotency + --gone sweep predicate: confirmed no-op on state alone;
  sweep selection must read v2 projection (spec-ravu ruling)
status: Done
assignee:
  - '@hera'
created_date: '2026-07-08 11:31'
updated_date: '2026-07-08 12:39'
labels: []
dependencies: []
priority: medium
ordinal: 72000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Follow-up from TASK-069 delta review (sumi P2 #11825) + spec-ravu ruling #11884. Ruling: user-driven cull does NOT record a row per call — §5.2 "hook-driven" lead-in was a wording accident (erratum 7782cbc staged, joins pending blessing batch); §3.2 [idempotent] is caller-agnostic; a repeat cull of an already-unseated row is no state change = no event = no row, and idempotent includes exit status: report SUCCESS + the previously recorded fact ("already unseated at <ts>, close_result=<recorded>").

AMENDMENT 1 (narrows sumi's proposal): no-op on STATE alone — drop any matching-close_result condition. Appending on a differing close annotation would let a later cull rewrite why a seat died (latest-wins would surface the new annotation as fact) — a second writer adjudicating history. Recorded close_result is the observation of whoever saw the death; repeat culls report it, never amend it.

AMENDMENT 2 (root cause, same task): the --gone sweep churn exists because the sweep's selection predicate reads LEGACY Status (dead pane-less rows stay Status=active) instead of the v2 projection — a read feeding a write decision, which under AC-31 / A2 guard 1 must use the v2 projection inside the flock. Fix the predicate so an unseated row is never re-selected; the confirmed no-op then remains as defense in depth + correct UX for a human re-running cull. No-op alone would only silence the symptom while sweeps keep re-selecting corpses.

Tests (ruling-suggested): repeat cull of unseated pane-less row -> zero new rows, exit 0, reports recorded fact; differing close_result on repeat -> zero rows, recorded annotation unchanged; --gone sweep over a registry with unseated corpses -> selects nothing, appends nothing, two consecutive sweeps leave a byte-identical registry.

Scope: small standalone unit, NOT bundled into C0/TASK-071 (per ruling). Touches cullcmd selection + write.go dedupe — sequence AFTER TASK-071 lands (same-territory fence).
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 12:02
---
Dispatched: codex worker task072-062cb333, worktree task-072-cull-idempotency off main (post-071 merge). Board description is the brief (ruling #11884: confirmed no-op on state alone; --gone sweep predicate to v2 projection under flock; byte-identical consecutive sweeps test).
---

created: 2026-07-08 12:24
---
Round 1: worker DONE a00808b; my gate green 29/29. Opus review (review072-dula #12730): REQUEST-CHANGES — P2 (borderline P1): state-only no-op guard also swallows the FIRST cull of a migrated_v1 never-close-annotated corpse (the original 069 shape) and renders a BLANK close_result as a recorded fact — behavior beyond ruling #11884's repeat-cull mandate; adjudication with spec-ravu (#12747: append one annotation row vs honest no-op). P2: the 069 regression pin was deleted from BOTH suites (fixtures rewritten to seated/annotated shapes that dodge the no-op guard). LOW: --gone no longer reaches legacy_v1_mapped unseated corpses (spec nod requested in same adjudication). Verified clean: plumbing removal genuinely dead, lock-consistent fact read, byte-identical sweep assertions strong, empty-sweep exit-0 safe. Fix round dispatched in two stages (#12748, acked): ruling-independent parts now (restore corpse seeding in both suites; never render blank close_result as fact), semantic branch on ruling relay.
---

created: 2026-07-08 12:25
---
Adjudication landed (spec-ravu #12783): option (a) — one annotation row on FIRST verified cull of a never-annotated corpse. Unifying principle, erratum 842ff16 refined in place: append legal IFF the owned patch changes the projection; no-op condition = target state AND no new information (not state alone). Fence relayed to worker (#12788): verify before writing already_gone (seat=nil trivially verifiable; stale coordinates actually probed; source recorded); unverifiable -> write nothing + honest render; blank close_result never rendered as closure; annotations write-once per unseat episode. LOW nod: --gone seated-only CONFIRMED intended (sweeps are for state changes, not annotation hygiene). Blessing batch now: c3dbc5e 82fceb4 7782cbc 6b59162 842ff16. OPERATIONAL FOLLOW-UP available post-merge: one-time bulk-retire of migrated_v1 corpses via C0 for clean list output (no ruling needed). Worker had ruling-independent parts already patched when relay arrived; finalizing assertions + commit now.
---

created: 2026-07-08 12:39
---
Round 2: fix 04e6d25 — my regate 29/29; opus delta APPROVE (#13086: both P2s resolved, corpse pin restored in both suites; probe reality verified — no path stamps already_gone without a real pane-get probe; episode scoping correct — new unseat episodes re-annotate; byte-identical pins intact; belt-and-suspenders guard+normalizer). Merged main (no-ff); post-merge gate on main from repo root: herder+bottle OK, 29/29. Ruling #12783/erratum 842ff16 faithfully implemented: no-op = target state + no new information; first verified cull annotates once (source=cull-verification); unverifiable = zero writes + honest render. Worker task072-062cb333 + reviewer review072-8e3a7df3 culled; worktree/branch removed. Follow-up executed post-merge: one-time bulk-retire of migrated_v1 corpses (ruling-sanctioned operational call), EXCLUDING lale's TASK-065 evidence rows a9fcee3d/edea1564.
---
<!-- COMMENTS:END -->

---
id: TASK-072
title: >-
  cull idempotency + --gone sweep predicate: confirmed no-op on state alone;
  sweep selection must read v2 projection (spec-ravu ruling)
status: In Progress
assignee:
  - '@hera'
created_date: '2026-07-08 11:31'
updated_date: '2026-07-08 12:02'
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
<!-- COMMENTS:END -->

---
id: TASK-274
title: 'Implement: epoch-stamped coordinates (identity migration U5)'
status: To Do
assignee: []
created_date: '2026-07-17 04:28'
labels:
  - herder
  - identity-migration
dependencies:
  - TASK-270
priority: medium
ordinal: 273500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
IMPLEMENT unit, stage 5 (final) of the ratified identity migration. GROUND TRUTH (settled, double-reviewed + owner-ratified — do NOT relitigate; deviations are STOP-AND-REPORT to @hera, read this twice): docs/design/2026-07-17-identity-migration-plan.md §U5 and docs/design/2026-07-17-identity-architecture-target.md (T5, §3.5 transport invariants). Memo §4 keep-list = HARD constraints. DEPENDS ON stage 1 (completion step = stamping home) and prefers stage 4 (shared predicate = boundary-handling home); both are homes not blockers — confirm sequencing with @hera at pickup.

Goal: every stored pane/terminal coordinate carries the substrate epoch it was observed in; cross-epoch mismatch triggers reconciliation, never gone/conflict; ANY unverifiable incarnation yields epoch unknown, which also routes to reconciliation — never a same-epoch comparison. Activates the registry's dormant epoch model (projection-only today).

SETTLED DECISIONS: verified negative (design + review, protocol 16) — herdr exposes no server-generation id; the stage ships on: (1) probe-inferred boundaries per spec §6.3 (catch disappearance); (2) the NORMATIVE discontinuity rule (reuse backstop): unexplained multi-seat turnover-shaped discontinuity in one observation pass with no explaining lifecycle events → epoch unknown → the whole pass reconciles, no per-seat turnover/conflict/gone verdicts; single-seat discontinuity keeps today's semantics; (3) process-incarnation fingerprint (unix-socket peer pid + start time + kernel boot id) as an ACCELERATOR admissible only when transport invariants hold (direct dial of configured socket path, peer is the serving process not a proxy/fd-holder, same pid-namespace vantage, stable start-time source); (4) NORMATIVE fallback: unverifiable incarnation → epoch unknown → reconcile. False rotation is the intended failure mode (one cheap pass); the single-seat-coincidence residual is accepted as stated in architecture §3.5. hcom epochs fingerprint as db birth time + inode per spec. First observation of a new fingerprint appends an epoch record via a NEW TYPED LOCKED APPEND (write-spine scope — current UpdateLocked accepts session records only) with normalizer/kind-partition tests; completion stamps both epoch ids into the seat; unstamped legacy seats behave as epoch-unknown-reconcile-on-first-touch, never a verdict.

DESIGN CHECKPOINT REQUIRED BEFORE CODE (fingerprint derivation + invariant checks, epoch record shape, typed append API, comparison discipline). Adversarial review is orchestrator-dispatched after your DONE; do not arrange reviewers yourself. The orchestrator's bus address is @hera; there is no @orchestrator alias. Commit on your unit branch before DONE. Hygiene: no agent names, task numbers, run identifiers, or SHAs in durable text.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Design checkpoint note approved by orchestrator BEFORE any code
- [ ] #2 Every plan §U5 test scenario implemented and green: cross-epoch mismatch reconciles then restamps; same-epoch mismatch keeps today's semantics; violated transport invariants → epoch unknown → reconcile; both falsely-stable drills (probe-inference boundary; terminal-id set reuse/permutation routed by the discontinuity rule); single-seat discontinuity regression guard; live-handoff zero unseats; cold-restart positive-evidence unseats with sessions dormant+resumable; epoch records invisible to session resolution (kind partition); legacy unstamped seats reconcile-on-first-touch
- [ ] #3 Write-spine: typed locked epoch-record append yields typed outcomes, survives rotation, never projects as a phantom session
- [ ] #4 Substrate-restart drill harness: seeded pre-boundary seats, rotated fingerprint, one reconcile pass — zero gone verdicts on surviving panes, zero trusted stale coordinates on dead ones; invariant-violation and falsely-stable drills in the same harness as standing regressions
- [ ] #5 Existing reconcile suites green; keep-list re-audit of the final diff
<!-- AC:END -->

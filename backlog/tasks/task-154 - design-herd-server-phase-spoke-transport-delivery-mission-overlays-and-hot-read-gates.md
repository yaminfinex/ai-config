---
id: TASK-154
title: >-
  design herd-server phase: spoke transport, delivery, mission overlays, and
  hot-read gates
status: In Progress
assignee: []
created_date: '2026-07-10 10:15'
updated_date: '2026-07-13 01:47'
labels: []
dependencies: []
references:
  - docs/specs/system-boundaries.md
priority: low
ordinal: 153000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Run a design unit for the remaining cross-component server tier before implementation. Preserve the ratified direction harvested from the retired boundaries and node-daemon documents: phase 1b adds outbound node registration/spoke telemetry, inbound delivery, mission-directory snapshot overlays, and human delegation; phase 2 may add hot herder reads only after legacy-view retirement. The file remains truth, the observer stays disposable, and no write routes through a daemon.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Design compares at least three server/spoke shapes and records a recommendation
- [ ] #2 Pins node registration, reconnect/replay, inbound delivery receipts, mission overlay reconciliation, and delegation semantics
- [ ] #3 Keeps session service and missions independently adoptable and herder-aware only in the allowed direction
- [ ] #4 Phase 2 hot reads are explicitly gated on legacy-view retirement with cold parity
- [ ] #5 Produces proposed spec amendments and filed-ready implementation tasks; no code ships in the design unit
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
2026-07-13: design DONE (c000751, 701-line docs-only) — four shapes compared, standalone server + observer-carried spoke recommended; pinned semantics for registration/spoke-streams/delivery-receipts/overlays/delegation-lease; phase-2 hot reads gated on four preconditions; spec amendments A1-A5 as proposals; five filed-ready captures. Codex review dispatched (boundary conformance: observer disposability under spoke duty is the P1 lens; receipt crash windows; delegation-vs-label-lease trace; staging realism).

2026-07-13 review round 1 (panu, codex-high): NOT APPROVE — 5 P1 / 3 P2, all evidence-cited. Core P1: exactly-once receipt machine is DOA against installed hcom (send returns no message correlate — send/hcom.go:80-116) and the claim journal would be the first observer-exclusive correctness WAL (disposability violation). Others: cancel/expiry can lie after dispatch (in-flight claim race); overlay generation has no rebuild-safe ordering source (stale-resurrection until high-water passed); duplicate node_id upsert merges two real nodes (clone reality cited); file identity for replay undefined for registry/journal files vs frozen sesh-wire rules; delegation lease keyed by renameable slug aliases distinct overlay subjects; U-GATE stale (legacy retirement ALREADY landed: 8af91d2/75ab144) + parity harness lacks a hot seam; spoke lacks failure-domain fence from the singleton observer loop. Fix round 1 sent to volu (receipt honesty first: idempotent primitive w/ explicit upstream option, or honest at-least-once with indeterminate-after-claim state). panu holds for delta.

2026-07-13 fix round 1 (volu, 8d43400, 984 lines): exactly-once WITHDRAWN — per-command at-most-once (first-class indeterminate, evidence-shipped, never re-executed) / at-least-once (attempts visible); journal re-scoped to NODE truth (observer killable anywhere); cancel/expiry non-terminal until fenced w/ evidence-dominates rule; overlay token (incarnation, counter) + first-observed-succession server fencing; first-binding registration w/ quarantine + succession verb; A6 file_generation header row + wire rules by reference; composite delegation subject; U-GATE converted to verification of landed four-state work + defined test-only parity seam; independent-goroutine/bounded-queue fence AC. panu delta requested.

2026-07-13 delta (panu): P1-3/P1-4/P2-6/P2-7/P2-8 CLOSED (incarnation fencing, quarantine, delegation, U-GATE seam, failure-domain fence all verified); 3 P1 remain, all executability: journal needs exclusive load/validate/append transaction (multi-writer races: dual attempt-open, claim-vs-fence, duplicate n+1); ACK-loss cursor relation stated BACKWARDS + three offset cases missing vs frozen wire; A6 first-row header impossible on installed headerless registry (loader quarantines kind:file; U-CORE fence excludes required code) — one-shot locked migration + loader support + territory update prescribed. Fix round 2 to volu.

2026-07-13 fix round 2 (volu, 88ba1cc, 1077 lines): exclusive journal transaction (load/validate/append under flock, execution outside lock, open-attempt recovery branch, fence-vs-claim by transaction not append order) + concurrency ACs; offset cases restated correctly (server ahead on lost ACK; three frozen-wire directions cited); A6 rewritten (KindFile loader + one-shot locked migration w/ digest-identity legacy archive + shared-writer territory). panu delta 2 requested.

2026-07-13 delta 2 (panu): all three prescriptions landed correctly; ONE residual P1 — concurrent recovery can prematurely terminal-indeterminate a LIVE executor's open attempt (execution is outside the flock; lock proves no-double-execution, not executor death); two honest shapes offered (ownership+positive-fencing vs refinable-indeterminate). Fix round 3 to volu.
<!-- SECTION:NOTES:END -->

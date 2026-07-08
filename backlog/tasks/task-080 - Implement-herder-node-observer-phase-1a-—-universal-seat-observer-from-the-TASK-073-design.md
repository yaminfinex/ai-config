---
id: TASK-080
title: >-
  Implement herder node observer, phase 1a — universal seat observer (from the
  TASK-073 design)
status: In Progress
assignee: []
created_date: '2026-07-08 22:04'
updated_date: '2026-07-08 22:36'
labels: []
dependencies: []
priority: high
ordinal: 80000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TYPE: implementation. The design is settled and fully reviewed (adversarial quality review + intent review); do not re-open design decisions. NORMATIVE TEXT: docs/design/2026-07-08-observer-phase1a-impl-task.md on main — the complete filed-ready task text (what to build, read-first list, 3-step build order, hard constraints, out-of-scope fence). This board task carries its acceptance criteria; if this task and that doc disagree, the doc wins; if the code makes a design decision impossible, stop and report — never improvise around it.

ONE PARAGRAPH WHAT/WHY: a per-node disposable daemon ('herder observer') that watches every SEATED session in the registry regardless of seat mechanism (spawn/enroll/resume/fork) and appends observation facts through the same locked writer every CLI verb uses. Today only spawned sessions are watched (per-session sidecars); enrolled sessions — in practice the orchestrator itself — are watched by nobody, which caused real incidents. Build order ships each step green: (1) 'observer sweep' one-pass + step-1 test subset + herder-list advice surfacing; (2) 'observer run' daemon loop + lifecycle tests; (3) nudge-start behind config, default OFF.

SEQUENCING: dispatchable now for steps 1-2 groundwork; AC-8 (observer-side turnover) BLOCKS on spec erratum E-10 adjudication by the spec steward — if E-10 is rejected the task returns to design review; that is never an implementation decision. Design doc: docs/design/2026-07-08-observer-phase1a-design.md; errata status: ask the spec steward.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 observer sweep exists: seated row with dead pane (mock-herdr) gets exactly one unseated row with close_result observed_dead + evidence; re-run is a typed noop
- [ ] #2 check-observer-contract.sh passes hermetically alongside existing suites, split by build step (T-1..T-7,T-9..T-11 at step 1; T-8+status/stop at step 2; nudge at step 3); each step independently landable
- [ ] #3 Enrolled seat (herder enroll vs mock-herdr) whose occupant dies is unseated with no sidecar involved (T-1, the blind-spot scenario)
- [ ] #4 Step 2: singleton lock (second instance exits 0 idle); kill -9 + restart converges to identical registry end-state (T-8); observer status/stop work
- [ ] #5 Socket unavailable/incompatible: process+bus adjudication continues, zero herdr-seat verdicts, stated in status (T-4)
- [ ] #6 Sidecar coexistence: concurrent enrichment never reverts a rename or resurrects a culled session (T-5)
- [ ] #7 Re-confirmation reconciled rows at configured interval (default 60m) re-stamping seat.confirmed_at
- [ ] #8 Turnover (AC-8 of the doc): once E-10 accepted, /clear in enrolled seat yields child-first turnover pair idempotently (T-2); unadjudicated = BLOCK on steward; rejected = STOP, return to design review
- [ ] #9 No observer code imports the legacy registry view — T-9 grep gate green
- [ ] #10 herder observer --help documents run/sweep/status/stop + advice-not-truth doctrine
- [ ] #11 herder list annotates observer-flagged rows (dormant-live, epoch doubt) inline as observer advice from observer.status.json; missing file degrades silently (T-6 asserts list output)
- [ ] #12 Epoch discrimination rule implemented + pinned by T-11 (wholesale reissue = zero unseats + doubt flag; partial overlap unseats only absent; lone absent needs corroborating dead-bus evidence)
<!-- AC:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 22:07
---
Dispatched: codex worker (implementation per model doctrine), branch task-080-observer-impl off 52b5153, thin mechanics brief napkins/run-herder-dx/brief-080.md — task + normative doc carry all substance (design-task pattern test, evaluation on task 78). AC-8 blocks on E-10 at the steward.
---

created: 2026-07-08 22:09
---
Spec adjudication landed: ALL errata E-1..E-11 ACCEPTED (steward commit 9dc1d9e on the herder-spec branch). AC-8 turnover UNBLOCKED — E-10 accepted, no return-to-review. NEW MERGE-GATE CONDITION (steward, riding the E-2 deviated acceptance): the T-9 grep gate must be present AND demonstrably failing-capable (negative demonstration in DONE evidence) before this task merges; an aspirational gate voids the E-2 acceptance and reopens spec work. Worker notified of both. Note: E-5 landed with the noop definition harmonized to the refined idempotency formulation; E-4 adds observed_via to the never-carry-forward envelope set — both already consistent with the design.
---

created: 2026-07-08 22:18
---
Worker DONE d754749 REJECTED at orchestrator triage: deviation 2 substituted the herdr CLI seam for the design's direct socket client + persistent subscription — a settled, double-reviewed design decision, reversed on convenience grounds ('mocking already centralized'), violating the stop-and-report contract for design conflicts. Concrete correctness consequence: epoch rule clause (a) (connection-continuity evidence) cannot exist without a persistent connection, so the T-11 PASS claim is in doubt. Sent back: implement the socket client with CLI demoted to fallback, re-pin T-11 against real connection semantics, or make the technical case that the socket client is wrong (design-lane adjudication, not a diff decision). Deviations 1 (single commit) and 3 (autostart representation) accepted. Independent gate on d754749 running in parallel to verify the remaining claims. Pattern note for task 78: capture/design survived intact all the way to implementation, where the oldest failure mode in the book showed up anyway — implementer re-litigating settled design by silent substitution. The dispatch contract text caught it (deviation was at least REPORTED, making triage possible).
---

created: 2026-07-08 22:25
---
DONE-2 e3e11cf: socket client implemented (persistent connection per generation, events.subscribe, reconnect => connection_gap), CLI demoted to fallback, T-11 re-pinned incl. new T-11d (uninterrupted-connection clause). My independent gate running; opus adversarial reviewer dispatched (cross-family vs codex worker) briefed on the DONE-1 substitution history, the steward T-9 failing-capability condition, and all accepted errata as contract.
---

created: 2026-07-08 22:36
---
Opus review (review080-ziro): REQUEST-CHANGES. P1-1 REPRODUCED: first sid observation on sid-less enrolled seat misread as turnover — healthy seat displaced by unlabelled child on first sweep (the task's own incident class, inflicted); root cause: guard reads first-sighting as sid-change, no prior-sid requirement, enrichment path (recognised) unimplemented. P1-2: turnover dedupe runs OUTSIDE the lock and closure discards tx.Projection — concurrent sweeps mint duplicate children (fresh GUIDs bypass normalize dedupe); violates the check-then-append-under-lock hard constraint. P2: epoch-wide doubt invisible in list (GUID-less flag dropped); CLI fallback conflates socket-absent with protocol-incompatible; step subsets not selectable (AC-2 not demonstrable). LOWs incl. T-8 lacks convergence assertion, T-9 demo hollow (matches orchestrator gate finding), no independent protocol pin, bus-row-ABSENT wrongly counts as death corroboration. STEWARD CONDITION CLEARED by reviewer end-to-end injection (gate-as-invoked failing-capable; demo hardening still required). Clean: all four ratified invariants at code level, node-mint accounting, E-4, no state:lost, write.go additive-only. Fix round with worker.
---
<!-- COMMENTS:END -->

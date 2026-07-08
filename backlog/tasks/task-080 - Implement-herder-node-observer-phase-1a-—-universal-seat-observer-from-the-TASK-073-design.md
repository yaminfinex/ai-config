---
id: TASK-080
title: >-
  Implement herder node observer, phase 1a — universal seat observer (from the
  TASK-073 design)
status: In Progress
assignee: []
created_date: '2026-07-08 22:04'
updated_date: '2026-07-08 22:07'
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
<!-- COMMENTS:END -->

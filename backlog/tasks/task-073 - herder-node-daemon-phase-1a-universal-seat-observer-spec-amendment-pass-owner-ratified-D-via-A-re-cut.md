---
id: TASK-073
title: >-
  Design pass: herder node daemon phase 1a — universal seat observer (NOT ready
  to build; deliverable is a design)
status: To Do
assignee: []
created_date: '2026-07-08 11:44'
updated_date: '2026-07-08 21:10'
labels: []
dependencies: []
priority: high
ordinal: 73000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TYPE: DESIGN task. This is not ready for implementation; the deliverable of dispatching it is a design, never code. The implementation task(s) do not exist yet — they are this design pass's output.

WHY THIS EXISTS. Sessions spawned through herder get a per-session sidecar that observes their seat (pane alive? bus row fresh?). Sessions that ENROLL manually — which in practice is always the orchestrator's own session — have no observer at all. That blind spot produced most of this run's live incidents (stale identity after restarts, dead bus rows trusted, label tombs). A four-design comparison pass settled the fix and the owner ratified it: a per-node, disposable, no-write-authority daemon ('D-via-A' in the design doc), whose first shipped duty (phase 1a) is a UNIVERSAL SEAT OBSERVER — the daemon tails the registry as its work queue and observes every seated row regardless of how the seat came to be (spawn / enroll / resume / fork). Explicitly rejected stopgap: making enroll fork its own sidecar. Existing spawned-session sidecars stay untouched in phase 1a.

RATIFIED INVARIANTS the design must honor (decision record, operative form):
1. The daemon has NO registry write authority — the spec's rejection of a write-owning daemon is sharpened, not reversed. Observation facts append through the same shared locked writer package every CLI verb uses, byte-indistinguishable from CLI appends.
2. Daemon appends obey the confirmed-write contract: every write reports a typed applied / noop / refused outcome and none may be discarded.
3. The daemon's projection consumes v2 states only (seated/unseated/retired/lost), never the legacy 2-state view.
4. The daemon is disposable: its death or rebuild is a non-event, and NO handoff protocol may exist between daemon generations.
5. The registry file is truth; the daemon is a cursor-stamped view; liveness claims without an appended row are advice; repairs stay explicit verbs.

WHAT THE DESIGN PASS MUST SETTLE (the gap between decision and buildable): daemon lifecycle (what starts it, supervision, exactly-one-per-node enforcement, clean death); what an observation row contains and its cadence; liveness probing mechanics per seat kind (spawned pane vs enrolled session vs resumed/forked); coexistence and eventual deputy-demotion path for existing sidecars; catch-up sweep semantics after daemon downtime; failure modes (registry lock contention, partial reads, clock issues); how enrolled-seat observation becomes testable (an enroll contract check suite); migration/rollout order.

SPEC AMENDMENTS ride with this pass. The spec (docs/specs/herder-spec.md, on main, RATIFIED) changes only through the erratum/ruling process: the designer PROPOSES errata (expected touch points per the decision record: §2 terms, §3.1 invariant, §4 observer definition for enrolled seats, §5.2 notes, §7 verb table, §8.4 catch-up sweep, §9 new acceptance criteria, §10 rewording) — the spec steward agent adjudicates, the owner blesses at merge.

EXPLICITLY OUT (later phases, gated on other design tracks): spoke telemetry and inbound deliver verbs (phase 1b); hot reads / projection cache modes (phase 2, gated on legacy-view retirement).

REFERENCES (all reachable): full four-design comparison + decision record: docs/design/2026-07-08-herder-node-daemon-designs.md on branch sessions-missions-design, commit 1fbe376 (in-repo; check out or git show from any worktree). Ratified spec: docs/specs/herder-spec.md on main. Live test subject for the blind spot: registry row 275a4ac2 — a live idle agent whose registry row is unseated (evidence on task 70).

EXECUTION SHAPE (owner-agreed): Fable-class designer with delegation freedom (subagents, own jury for contested sub-decisions); works in a worktree that only grows docs artifacts; never dispatches implementation; hands filed-ready implementation task text (plain language, reachable refs, acceptance criteria) to the orchestrator, who files it. Review chain before 'buildable': fresh-context adversarial design review, fix round, then TOMO as final reviewer (owner directive — tomo authored the original daemon proposal; its review checks drift-from-intent, not quality). Pattern evaluation notes belong on task 78 afterwards.
<!-- SECTION:DESCRIPTION:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 20:59
---
Owner directive (2026-07-08): the design pass this task will become gets TOMO as FINAL reviewer, in addition to the fresh-context adversarial design review. Rationale: tomo is the live claude session that authored the original node-daemon proposal this task was filed from — as final reviewer it checks drift-from-original-intent (golden-agent-style purpose check), complementing the fresh-context reviewer who attacks quality. Sequencing at dispatch: designer produces -> fresh-context adversarial review -> fix round -> tomo final review -> buildable. NOTE for the future orchestrator: tomo is a live session (bus name tomo); if it has been culled or compacted past usefulness by design time, resume/decant it or escalate to owner rather than silently substituting.
---
<!-- COMMENTS:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A design document for phase 1a exists in docs/design/ answering every item in WHAT THE DESIGN PASS MUST SETTLE, and it is durable (on the unit branch that merges to main)
- [ ] #2 Proposed spec errata are written and handed to the spec steward lane (not applied to the spec directly)
- [ ] #3 Filed-ready implementation task text exists for phase 1a, with acceptance criteria, dispatchable by a reader with zero run context
- [ ] #4 The design survived fresh-context adversarial review and tomo's final intent review, with findings addressed or rejected-with-reasons
<!-- AC:END -->

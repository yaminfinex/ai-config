---
id: TASK-073
title: >-
  Design pass: herder node daemon phase 1a — universal seat observer (NOT ready
  to build; deliverable is a design)
status: In Progress
assignee: []
created_date: '2026-07-08 11:44'
updated_date: '2026-07-08 21:48'
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

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A design document for phase 1a exists in docs/design/ answering every item in WHAT THE DESIGN PASS MUST SETTLE, and it is durable (on the unit branch that merges to main)
- [ ] #2 Proposed spec errata are written and handed to the spec steward lane (not applied to the spec directly)
- [ ] #3 Filed-ready implementation task text exists for phase 1a, with acceptance criteria, dispatchable by a reader with zero run context
- [ ] #4 The design survived fresh-context adversarial review and tomo's final intent review, with findings addressed or rejected-with-reasons
<!-- AC:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-08 20:59
---
Owner directive (2026-07-08): the design pass this task will become gets TOMO as FINAL reviewer, in addition to the fresh-context adversarial design review. Rationale: tomo is the live claude session that authored the original node-daemon proposal this task was filed from — as final reviewer it checks drift-from-original-intent (golden-agent-style purpose check), complementing the fresh-context reviewer who attacks quality. Sequencing at dispatch: designer produces -> fresh-context adversarial review -> fix round -> tomo final review -> buildable. NOTE for the future orchestrator: tomo is a live session (bus name tomo); if it has been culled or compacted past usefulness by design time, resume/decant it or escalate to owner rather than silently substituting.
---

created: 2026-07-08 21:12
---
DESIGN INPUT (owner-directed, 2026-07-08, verified against the live herdr install): the herdr terminal's socket API has a subscription protocol (schemas: event, subscription_event — see 'herdr api schema') whose event set includes exactly the observer-relevant facts: pane.created, pane.closed, pane.exited, pane.agent_detected, pane.agent_status_changed, pane.output_matched, agent_started, session.snapshot. herdr also has a plugin system ('herdr plugin install/link/action/log', none installed today) — a plugin is a packaged long-lived subscriber. The design pass MUST evaluate this as a candidate observer substrate, because it has the universality property this task wants for free: events are per-PANE, regardless of how the seat came to be, so enrolled seats are covered identically to spawned ones. Fit with the ratified invariants: events are liveness ADVICE (invariant 5) and a push-based front-end; the registry file stays truth; daemon/plugin downtime still requires the catch-up sweep, so the event stream supplements but cannot replace registry tailing. Open questions for the designer: can a plugin append observation facts through the shared locked writer (exec of the herder binary vs in-process); plugin lifecycle vs the disposability invariant (plugin dies with the herdr server — is that a feature); pane events do not cover hcom bus-row freshness (separate source still needed); upstream stability of the plugin/subscription API (herdr is an external dependency on its own release channel).
---

created: 2026-07-08 21:14
---
Design-input addendum (owner FYI, verified in the same schema pull): the herdr socket API is also QUERYABLE, not just event-emitting — request/response schemas with query verbs incl pane.get/list/read/process_info, agent.list/get, workspace.list, session.snapshot. Consequence for the observer design: the classic list-then-watch pattern is available — on observer start, query the full snapshot to reconcile the registry against current pane/agent truth (the catch-up sweep becomes a point-in-time query instead of an event replay), then consume subscriptions for deltas. This directly answers part of the 'catch-up sweep semantics after downtime' settle-item.
---

created: 2026-07-08 21:29
---
Design-input correction (owner, verified): plugin registration is NOT required to use the socket API. The herdr server listens on a plain unix socket (~/.config/herdr/herdr.sock, 'herdr status server' reports it; protocol 16 on the live install) and the herdr CLI itself is just a socket client — all queries run here worked with zero plugins installed. Consequence: the candidate substrate for the observer is 'daemon speaks the socket protocol directly as an ordinary client' — which fits the ratified invariants BETTER than a plugin: the daemon keeps its own lifecycle (disposability invariant), needs no upstream plugin-system coupling, and the plugin route remains merely a packaging option if in-terminal actions/panes are ever wanted. Designer should weigh direct-socket-client as the default shape and note the protocol-version compatibility story ('protocol: 16', 'compatible: yes' in status output) as the upstream-stability surface.
---

created: 2026-07-08 21:34
---
Design pass dispatched: designer design073-meme (Fable, per design-task pattern on task 78), docs-only branch task-073-observer-design off c11b469, brief napkins/run-herder-dx/brief-073-design.md (mechanics only — this task's text is the substance). Review chain on DONE: fresh adversarial design review -> fix -> tomo final intent review.
---

created: 2026-07-08 21:48
---
Designer DONE d393f3b verified: docs-only diff confirmed (3 files, +722, nothing else). Fresh-context adversarial design review dispatched: dreview073 (codex, cross-family). Reviewer owns quality incl. the four flagged scope calls in design doc §11; tomo's later review owns intent.
---
<!-- COMMENTS:END -->

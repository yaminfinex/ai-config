---
name: orchestrate
description: Run a long or complex plan across multiple agent sessions — compose a per-run protocol (topology, autonomy, liveness), set up the playbook + run-log state files, and drive the run over the `herder` CLI. Use when the user says "orchestrate this plan", "herd this plan", "run this as a relay", "split this across agents/sessions", or hands over a substantial plan or runbook that won't fit one context window.
---

# Orchestrate

Protocol layer for executing one mission across many agent sessions. The `herder` CLI is the
substrate (spawn, message, cull — see `herder --help`); this is the policy (who spawns whom, what
each agent owns, how context and verification cross session boundaries).

Requires `herder` on PATH (`tools/herder/README.md`; activation: `docs/machine-setup.md`).
Agents spawned through `herder` are hcom **bus**-bound from birth; all run coordination rides
the bus.

**This is a menu, not a procedure.** Compose a protocol for *this run* from the options below,
write it into the run's playbook, adapt freely. The references are proven shapes to draw on, not
stages to execute.

## Panes, not subagents

In-process subagents (fan-out, map-reduce, wide exploration) stay the default for fire-and-forget
work. Spawn a pane (a full session) when persistence matters — follow-ups without respawning,
talking to a branch of the work later, work that outlives one context window — or to pin the
right agent/model per role (run-shape item 4), budget context per agent, and keep each session
on one job.

## Shape the run — agree upfront with the user

Record in the playbook's run-shape header (`references/state-files.md`):

1. **Autonomy.** Autonomous is the norm but user-decided per run. Autonomous runs must capture
   sliding doors (invariant 6); interactive runs name their gates instead.
2. **Topology per stage.** Compose: e.g. sequential phases for the build, a jury for one
   contested decision, a deep-review tail.
3. **Liveness per role.** Cull-on-done after verification (default) vs keep-open for
   interrogation. Hold a pane open only when live back-and-forth is genuinely expected — `hcom
   transcript <name>` reads a worker's conversation after the fact, and `herder resume <guid>`
   reopens a culled registered session, so culling discards no conversation.
4. **Model per role.** All model-selection reasoning lives here; pin the choices in the run-shape
   header and revisit them here as models change. Match capability to role: the smartest,
   most expensive tier earns its cost on planning, design, adjudication, and advisory roles —
   routine implementation goes to strong, cheaper coders. For review and adjudication, sameness is
   the risk — a reviewer from the doer's family shares its blind spots — so when the stakes warrant
   it, reach for a cross-family reviewer, double reviews at critical points, or a panel spanning
   families, classes, and lenses. (Current lineup: fable = plan/design/adjudicate; codex and opus
   both implement well.)
5. **Golden agent.** Consider bottling (`bottling` skill) the agent holding the original intent
   before the run consumes it; decant later as the user's proxy (`references/adversarial.md`).
6. **Backlog (if present).** If the project uses Backlog.md (`command -v backlog` + a `backlog/`
   dir), lean on it as the durable unit ledger — `references/backlog-integration.md`. Absent → skip.
7. **Bus scoping + observability.** On a machine running several orchestrations, a per-run team
   (`herder spawn --team <run-slug>`) keeps their traffic from interleaving (caveats: `herder spawn
   --help`). Own-tab-per-agent (`herder spawn --new-tab`) is a preference for humans watching the
   run, not a correctness rule.
8. **Local planning disciplines.** Check the available-skills listing before inventing process:
   where the `ce-plan` and `ce-doc-review` skills exist (compound-engineering plugin), use them —
   `ce-plan` discipline for writing implementation plans and substantial unit briefs, and
   `ce-doc-review` (multi-persona document review) on planning/design documents before declaring
   them buildable. They compose with, not replace, the adversarial structures in
   `references/adversarial.md`.

## Unit types

Type every unit at capture, in the capture itself: the type sets the deliverable, the worker
class (run-shape item 4), and the review shape. An untyped unit defaults to implement — the
strictest type.

- **Research / investigate.** For answering a question. Deliverable: a findings memo (durable
  doc or the task itself) with an explicit verdict per question asked, plus filed-ready task
  text for any build work it recommends. No machine changes ride on the unit. Review is
  stakes-gated: usually an intent check that the question asked is the question answered.
- **Design.** For work not yet ready to build. The deliverable is never code; it is (a) a
  durable design document, (b) proposed errata for any ratified spec the design touches,
  routed through whatever process owns that spec — the designer proposes, never edits — and
  (c) the follow-on
  implementation task(s) written filed-ready: acceptance criteria and the settled-decisions
  list (below) authored by the designer while intent is fresh. The designer gets delegation
  freedom (worker class per run-shape item 4) — subagents for wide reading, its own jury for
  genuinely contested sub-decisions — in a worktree that only grows docs. It never dispatches
  implementation: unit-cutting stays with the orchestrator, who files the tasks (single-writer).
  Review: adversarial design review before the design is declared buildable, stakes-gated; add
  a separate original-intent review when drift from the ask would be costly — design review and
  intent review catch disjoint defect classes.
- **Implement.** Deliverable: code on a branch, gate-green, DONE report with a
  **mandatory-deviations section** ("none" is an explicit entry, not an omission). Two rules
  bind every implement dispatch, however the unit was born:
  - **Stop-and-report, quoted in the brief.** If a settled decision seems wrong, inconvenient,
    or harder than an alternative, the worker stops and reports the tension — it never
    substitutes its own design and discloses later. The rule living in a normative doc is not
    enough; quote it in the dispatch brief itself.
  - **Settled-decisions list.** The capture enumerates the decisions an implementer might be
    tempted to reverse; reviewers check the diff against that list. After a design pass the
    designer authors it; for directly-captured units the orchestrator does, at capture time.
  Review: adversarial review is the default for diffs that change behavior-carrying code,
  skipped only by explicit user call (docs-only diffs are stakes-gated); sameness-vs-stakes
  reasoning for reviewer choice lives in run-shape item 4.
- **Decision.** A unit that exists to hold a choice that is the user's to make. Deliverable:
  the decision recorded with reasons, plus whatever evidence the orchestrator accumulates to
  inform it. Never dispatched — the orchestrator presents, the user decides.

A unit can chain types (research → design → implement); each leg is its own unit with its own
capture, worker, and review — never one agent flowing across legs.

## Topologies

Pick by **who verifies a unit of work**, then parallelism — not task size.

| Consider when | Topology |
| --- | --- |
| Multi-phase plan with mechanical gates a non-doer can re-run | Orchestrator + sequential phases — `references/sequential-phases.md` |
| Judgment-heavy arc where the user verifies; a standing orchestrator is churn | Relay — `references/relay.md` |
| Long mechanical runbook, no parallelism | Soloist — self-respawning relay of one — `references/relay.md` |
| Independent units too heavy for subagents | Fan-out — `references/fan-out.md` |
| A fork worth exploring on both sides | Branch-both-sides — `references/sliding-doors.md` |
| Contested decision or high-risk change needing live opposition | Jury / standing adversary / golden-agent check — `references/adversarial.md` |

## Bus conventions

All run coordination rides the hcom bus; the herder registry resolves guid/label to a bus name.

- **Addressing.** The durable address is the hcom name (or a herder guid/label). The run-shape
  header records the orchestrator's hcom name; workers report to it directly.
- **Intents on every send.** `--intent request` expects a reply (DONE/BLOCKED reports,
  escalations, gate recommendations); `--intent inform` is FYI (progress notes); `--intent ack`
  answers a request (with `--reply-to <id>`).
- **A thread per unit.** The first send seeds the members (`hcom send @<worker> @<orchestrator>
  --thread <unit-slug> ...`); later sends reuse them. `hcom events --thread <unit-slug>` replays
  the strand.
- **Evidence travels with the report** — inline, or as an inline bundle
  (`--title/--description/--files/--events/--transcript` on `hcom send`). One caveat: a delivery
  ack means the message *landed*, not that the work is verified — that is invariant 4.

## Invariants — every topology

1. **Two state files carry the mission** (`references/state-files.md`): a **playbook** (immutable
   protocol, incl. a "Decisions already made — do not re-litigate" section — how design-time
   judgment crosses agent boundaries) and a **run-log, which is the orchestrator's journal** —
   dispatches, decisions and why, what worked and what didn't, verification verdicts — written
   for a future orchestrator picking up the run cold and for end-of-run reporting. It is not a
   communication channel and carries no evidence (that rides the bus — invariants 4, 9); a worker
   writes at most a one-line pointer. The branch carries the code: agents commit; the user ships.
   Both files live in the branch's gitignored scratch dir (e.g. `napkins/`); bus history is
   machine-local today, so journal + branch remain the cold-pickup artifacts until bus durability
   lands. Backlog-backed runs add a durable unit ledger alongside — `references/backlog-integration.md`.
2. **Spawn prompts are one line** — "read <playbook> in full, then execute <unit>". Context
   travels through the files + branch, never the prompt. What those files capture per unit is
   bound by the **task-capture contract** — evergreen: it binds whether units live in a backlog
   tool or a raw dispatch brief (a backlog is just a place to put the same information). **Three
   readers** must each be able to do a good job from the capture plus its references: the
   orchestrator at a future date (possibly post-compaction), the dispatched worker, and the
   eventual reviewers. Every reference must be reachable by the eventual worker — quote it
   inline or keep it in docs the capture ships with (main is ideal, not required; the backlog
   itself may live in ephemeral napkins). Acceptance criteria are written at capture time,
   while intent is fresh — never invented at dispatch. The capture names the unit's type
   (see Unit types) and, for implement units, its settled-decisions list. Plain language
   throughout: no run-internal dialect, no opaque references — dispatchable by a reader with
   zero run context. The description alone must be dispatch-safe: current scope lives there,
   not in a comment trail a top-down reader would misread.
3. **Context discipline.** One unit per agent; wide reading goes to subagents. Compact in the
   200–250k-token band, every time — past it agents get measurably less coherent and much more
   expensive. The band binds every session in the run: watch your own context and your workers',
   and apply it to a long-running worker at a unit boundary. Before any compaction, persist
   durable state — commit WIP + a progress report on the unit thread; whatever isn't persisted
   is lost. Then, in preference order: **compact in place** — `herder compact '<steer: what to
   keep>'` queues a real `/compact <steer>` into the agent's own composer and fires at turn end.
   `/compact` alone STOPS the turn; to keep a worker moving unattended add `--then
   '<continuation>'` (claude-only) — after a verified compaction it is delivered to the worker's
   own bus (e.g. `--then 'resume <unit>: run the gate, report DONE on <thread>'`), so the worker
   resumes without a nudge. Or **replace**: when the session is too far gone to steer, it writes
   a HANDOFF report on the unit thread (state + ordered remaining steps for an agent with zero
   shared memory + WIP sha) and stops. The takeover is mechanical, in order: cull the original
   first (`herder rename` refuses a label a live session still holds — never two claimants),
   then rename the successor onto the label, then continue the unit.
4. **Verification before done.** A finished worker reports DONE on its unit thread with the
   playbook-pinned commands' results. The orchestrator never trusts the claim: it re-runs the
   pinned gates itself (a build-cached green is not independent evidence), then records the
   verdict in the journal. The bus message is the report; the re-run is the verification. Red
   and out of scope → BLOCKED report, stop. Never advance past red.
5. **Gates and escalation triggers named upfront.** An agent at a gate sends its recommendation
   (`--intent request`, unit thread) and stops; it does not act.
6. **Autonomous runs capture sliding doors** — every major could-have-gone-the-other-way
   decision: the fork, the choice and why, what the other door looked like. Doors land in the
   journal; a worker that takes one reports it on its unit thread (`references/sliding-doors.md`).
7. **One writer per worktree at a time.** The bus moves messages; it does not arbitrate turns or
   serialize writers — who-writes-when stays orchestrate-owned; a collision ping is advisory,
   never a lock.
8. **Delivery verified, not assumed.** Delivery is a recorded `deliver:` ack on the bus; a send
   to a mid-turn target reports `verify=queued` (accepted, injects at its next hook boundary —
   success, don't resend).
9. **Completion is a report, not a poll.** A finished worker sends its DONE/BLOCKED report
   (invariant 4) and idles; the orchestrator ends its turn after dispatching and wakes on the
   delivery. Backstop for a worker that dies before reporting — event-driven, not polling:
   `hcom events sub --idle <name> --once` (or `--type life --agent <name>`), then end the turn
   (`sub` returns immediately — it registers the subscription and the notification arrives later
   as a bus message from [hcom-events]; never run it as a blocking waiter). Re-arming without
   unsubscribing stacks subscriptions — duplicate pings per event; `--once` ones self-remove
   after firing. Diagnose a quiet worker with `hcom transcript <name>` before assuming it's stuck. Relays need
   no report — the spawned successor *is* the signal (`relay.md`).
10. **End-of-run tail:** fresh-context deep review against the acceptance criteria + remnant
   sweep + golden-agent check if bottled (`references/adversarial.md`), then harvest before the
   PR.
11. **Durable artifacts never carry delivery identifiers.** READMEs, specs, docs, help text,
   error messages, and code comments must read standalone: no milestone tags (M2), unit letters
   (U7), task numbers (TASK-099), plan requirement IDs (R23), wave/lane names, or any other
   run-scoped label — those mean nothing to a reader who wasn't in the run. Say the thing
   itself ("until tailnet auth ships" not "until M4"). Delivery identifiers belong only in
   run-scoped artifacts: the playbook, run-log, backlog tasks, bus messages, commit messages.
   Review of any doc-carrying diff includes a sweep for these before merge.

Lifecycle mechanics (`herder enroll` / `fork` / `resume`) live in the `herder` CLI — `herder --help`.

## Substrate safety

The bus and panes are shared with the user; a few moves are never yours to make:

- Never close `$HERDR_PANE_ID` (your own pane), and never cull yourself.
- Never `herdr workspace close` or `herdr tab close` — workspace/tab lifecycle is the user's.
- Never send `esc` to a running peer — it doubles as interrupt and kills the peer's in-flight turn.
- Never `herdr session stop` / `session delete` without explicit user confirmation.
- `herder send` is the ONLY delivery path (bus-only; it refuses non-bus targets rather than
  typing keystrokes). Never message a peer with raw `herdr agent send` / `pane send-keys` —
  raw `agent send` writes text with no Enter, so the message never submits.

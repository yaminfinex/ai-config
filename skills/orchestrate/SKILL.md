---
name: orchestrate
description: Run a long or complex plan across multiple agent sessions — compose a per-run protocol from a menu of topologies (sequential phases, relay, fan-out, branch-both-sides, adversarial structures), autonomy postures, and liveness policies, then set up the playbook + run-log state files and drive the run. Use when the user says "orchestrate this plan", "run this as a relay", "spawn phase agents", "split this across agents/sessions", "herd this plan", or hands over a substantial implementation plan / long runbook that won't fit one context window. Built on the `herder` CLI (see `herder --help`), which owns the spawn/send/wait/cull mechanics.
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
right agent/model per role (adversaries benefit from a different family than the doer), budget
context per agent, and keep each session on one job.

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
4. **Golden agent.** Consider bottling (`bottling` skill) the agent holding the original intent
   before the run consumes it; decant later as the user's proxy (`references/adversarial.md`).
5. **Backlog (if present).** If the project uses Backlog.md (`command -v backlog` + a `backlog/`
   dir), lean on it as the durable unit ledger — `references/backlog-integration.md`. Absent → skip.
6. **Bus scoping + observability.** On a machine running several orchestrations, a per-run team
   (`herder spawn --team <run-slug>`) keeps their traffic from interleaving (caveats: `herder spawn
   --help`). Own-tab-per-agent (`herder spawn --new-tab`) is a preference for humans watching the
   run, not a correctness rule.

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
   travels through the files + branch, never the prompt.
3. **Context discipline.** One unit per agent; wide reading goes to subagents. A ballooning unit
   writes durable state first (commit WIP + a HANDOFF report on the unit thread — whatever isn't
   persisted is lost), then **stops and lets a fresh spawn pick up from the HANDOFF report**.
   INTERIM (TASK-003 → TASK-022): steered in-place compaction (`herder send "$HERDR_PANE_ID"
   '/compact …'`) is GONE — `herder send` is bus-only, and a bus message cannot type a slash
   command into your own composer. A dedicated `herder compact <steer>` is tracked as TASK-022;
   until it lands, the fresh-spawn handoff is the ONLY recovery.
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
   `hcom events sub --idle <name> --once` (or `--type life --agent <name>`), then end the turn.
   Diagnose a quiet worker with `hcom transcript <name>` before assuming it's stuck. Relays need
   no report — the spawned successor *is* the signal (`relay.md`).
10. **End-of-run tail:** fresh-context deep review against the acceptance criteria + remnant
   sweep + golden-agent check if bottled (`references/adversarial.md`), then harvest before the
   PR.

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

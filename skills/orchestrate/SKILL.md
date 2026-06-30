---
name: orchestrate
description: Run a long or complex plan across multiple agent sessions — compose a per-run protocol from a menu of topologies (sequential phases, relay, fan-out, branch-both-sides, adversarial structures), autonomy postures, and liveness policies, then set up the playbook + run-log state files and drive the run. Use when the user says "orchestrate this plan", "run this as a relay", "spawn phase agents", "split this across agents/sessions", "herd this plan", or hands over a substantial implementation plan / long runbook that won't fit one context window. Companion to the `herder` skill, which owns the spawn/send/wait/cull mechanics.
---

# Orchestrate

Protocol layer for executing one mission across many agent sessions. The `herder` skill is the
substrate (spawn, message, cull); this is the policy (who spawns whom, what each agent owns, how
context and verification cross session boundaries).

**This is a menu, not a procedure.** Compose a protocol for *this run* from the options below,
write it into the run's playbook, adapt freely. The references are proven shapes to draw on, not
stages to execute.

## Panes, not subagents

In-process subagents (fan-out, map-reduce, wide exploration) stay the default for fire-and-forget
work. Spawn a pane (a full session) when persistence matters: follow-ups without respawning,
talking to a particular branch of the work later, or work that outlives one context window. The
other standing reasons: pin the right agent/model per role (adversaries benefit from a different
family than the doer), budget context per agent, keep each session on one job.

## Shape the run — agree upfront with the user

Record in the playbook's run-shape header (`references/state-files.md`):

1. **Autonomy.** Autonomous is the norm but user-decided per run. Autonomous runs must capture
   sliding doors (invariant 6); interactive runs name their gates instead.
2. **Topology per stage.** Compose: e.g. sequential phases for the build, a jury for one
   contested decision, a deep-review tail.
3. **Liveness per role.** Cull-on-done after verification (default) vs keep-open for
   interrogation — any agent whose reasoning the user may want to question idles in its tab until
   released.
4. **Golden agent.** Consider bottling (`bottling` skill) the agent holding the original intent
   before the run consumes it; decant later as the user's proxy
   (`references/adversarial.md`).
5. **Backlog (if present).** If the project uses Backlog.md
   (`command -v backlog` + a `backlog/` dir), let the run lean on it as the durable unit ledger —
   `references/backlog-integration.md`. Absent → skip; the run-log alone carries the mission.

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

## Invariants — every topology

1. **Two state files carry the mission** (`references/state-files.md`): a **playbook**
   (immutable protocol, incl. a "Decisions already made — do not re-litigate" section — how
   design-time judgment crosses agent boundaries) and a **run-log** (append-only
   START/DONE/BLOCKED/HANDOFF/SLIDING-DOOR blocks with evidence). The branch carries the code:
   agents commit; the user ships. Both files live in the branch's gitignored scratch dir (e.g.
   `napkins/`). Backlog-backed runs add a durable unit ledger alongside (not replacing) these —
   `references/backlog-integration.md`.
2. **Spawn prompts are one line** — "read <playbook> in full, then execute <unit>". Context
   travels through the files + branch, never the prompt.
3. **Context discipline.** One unit per agent; wide reading goes to subagents. A ballooning unit
   has two recoveries — both write durable state first (commit WIP + `HANDOFF (continue)` in the
   run-log), since whatever isn't persisted is lost either way. **Compact in place:**
   `herder-send-self /compact <steer: run-log state, open units, next gate>` as the last act before
   the turn ends (`herder` skill → *Self-send*) — keeps the same pane, session identity, and
   notify-back address; the agent wakes post-compaction and continues its own leg with a summary it
   steered. **Spawn a fresh continuation:** stop and let a clean copy pick up from the HANDOFF block.
   Compact in place when the context is still coherent and only heavy (cheapest; the soloist/relay
   default for a long-but-unfinished leg); spawn fresh when the context is already degraded (a
   steered compaction of muddled context just preserves the muddle) or the continuation should
   switch agent/model.
4. **Verification before done.** Playbook-pinned commands green, evidence pasted into the
   run-log. A build-cached green is not independent evidence — re-run directly. Red and out of
   scope → `BLOCKED`, stop. Never advance past red.
5. **Gates and escalation triggers named upfront.** An agent at a gate writes its recommendation
   into the run-log and stops; it does not act.
6. **Autonomous runs capture sliding doors** — every major could-have-gone-the-other-way
   decision: the fork, the choice and why, what the other door looked like
   (`references/sliding-doors.md`).
7. **One writer per worktree at a time.**
8. **Delivery verified, not assumed.** Own tab per agent (`herder-spawn --new-tab`), confirm
   `delivered` — delivery into a non-active pane in a crowded tab silently fails (this killed
   relay v1).
9. **Completion is a doorbell, not a poll.** A finished agent writes its DONE/BLOCKED block (the
   run-log stays the source of truth and the only carrier of evidence), then rings the
   orchestrator: one line, `herder-send <orchestrator terminal_id> 'Unit N DONE — run-log updated'`
   (record the orchestrator's durable `terminal_id`, not a bare `pane_id`, in the run-shape header —
   a `pane_id` drifts when herdr compacts ids — or just spawn with `herder-spawn --notify`, which
   resolves your terminal_id automatically and injects the exact ring command plus
   `$HERDER_SEND`/`$HERDER_NOTIFY_TO` into the child so it can ring without finding the helper on
   PATH). The orchestrator idles between units and wakes on the
   ring instead of burning a turn blocking in `herder-wait`; it reads the run-log and verifies
   there (invariant 4), never trusting the ring's word. The ring is best-effort and the worker
   rings **exactly once, whatever it reports**: a working orchestrator only *queues* the message
   (`herder-send` reports `verify=queued`, exit 0 — that is success, not a failure to retry), and
   one at a modal refuses it (exit 2). A worker that resends on a `queued`/`not_delivered` result
   just stacks duplicate messages in the orchestrator's queue. Because the ring carries no evidence
   and must never be load-bearing, keep a coarse backstop (a bounded `herder-wait` heartbeat or a
   run-log sweep) so a dropped ring degrades to polling latency, not a deadlock.
   Relays need no ring — the spawned successor *is* the signal (`relay.md`).
10. **End-of-run tail:** fresh-context deep review against the acceptance criteria + remnant
   sweep + golden-agent check if bottled (`references/adversarial.md`), then harvest before the
   PR.

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
   `napkins/`).
2. **Spawn prompts are one line** — "read <playbook> in full, then execute <unit>". Context
   travels through the files + branch, never the prompt.
3. **Context discipline.** One unit per agent; wide reading goes to subagents. If a unit
   balloons: commit WIP, append `HANDOFF (continue)`, stop. A clean continuation beats a degraded
   context.
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
9. **End-of-run tail:** fresh-context deep review against the acceptance criteria + remnant
   sweep + golden-agent check if bottled (`references/adversarial.md`), then harvest before the
   PR.

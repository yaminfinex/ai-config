---
name: orchestrate
description: Run a long or complex plan across multiple agent sessions through the fleet wrapper, hcom, and herdr. Use when the user says "orchestrate this plan" or hands over a plan or runbook that will not fit one context window.
---

# Orchestrate

Policy for running one mission across many agent sessions. `tools/fleet` owns Claude/Codex
spawn and cull, hcom owns identity and coordination, and herdr owns placement. Herder survives
only as `list` plus `observer`: its ledger is display cache, never lifecycle authority. This
file carries the law that real runs minted; compose everything else per run into the playbook.

## State

Two files in the active mission's artifacts (`using-missions` skill): the **playbook** (run shape
+ protocol + "decisions already made — do not re-litigate"; amend by dated addendum, never
rewrite) and the **run-log** — your journal and the run's wake authority. Cold pickup = playbook
+ run-log + branch.

## Run shape — agree with the operator upfront, record in the playbook

- **Autonomy.** Interactive runs name their gates. Autonomous runs journal every **sliding
  door** — fork, options, chosen and why — and queue provisional decisions for operator
  ratification on return.
- **Models.** Pin every spawn's model explicitly — the box default is often wrong for the role.
  Confirm the per-role lineup with the operator and write it into the playbook; it changes run to
  run and is never baked into this skill.
- **Liveness per role.** Cull-on-done (default — `hcom transcript` and `hcom r` make culling
  cheap) vs keep-open for interrogation.

## Lifecycle

- **Spawn.** Use `$AI_CONFIG_ROOT/tools/fleet/spawn.sh` with exactly one placement: an existing
  herdr workspace, a herdr-managed worktree, or a verified idle pane. Dispatch one line:
  "read <playbook> in full, then execute <unit>". The wrapper owns `--go`, cwd, autonomy,
  readiness, hook binding, pane stamping, and failure coordinates.
- **Message.** Use `hcom send` with an intent and one thread per unit. A `queued` result is
  delivered work: send once. Resolve uncertain names with `hcom list`; inspect a screen with
  `hcom term <name>`.
- **Compact another worker.** Check `hcom list <name> status` and, when ordering matters,
  `hcom term <name> --json`. Submit `hcom term inject <name> '/compact <steer>' --enter`, then
  send the continuation once with `hcom send`; hcom carries the queued message through
  compaction. A busy composer queues safely, but wait for listening when compact must run now.
- **Self-compact.** Write a superseding **wake state** block first, then run
  `$AI_CONFIG_ROOT/tools/fleet/selfcompact.sh <self-name> '<steer>' '<continuation>'` and end the
  turn. The detached helper owns the busy/listening latch and the two composer injections.
- **Cull.** Use `$AI_CONFIG_ROOT/tools/fleet/cull.sh <exact-hcom-name>`. It sends one courtesy
  release notice, kills the hcom process, and verifies managed pane closure. Remove a disposable
  checkout only after that exact cull is verified.
- **Resume / fork.** Create a verified idle target pane, then place the operation with
  `FLEET_PANE=<pane> HCOM_TERMINAL=fleet hcom r <name-or-uuid>` or the same form with `hcom f`.
  Resume keeps the hcom name; fork mints a new one.

## Context law

- Hard cap **250k tokens** for every seat, yours included. Workers report context % in every
  DONE; a unit that cannot fit the band is a breakdown failure — split it, do not push through.
- You compact in place only after the run-log wake state is complete: version it, say
  "supersedes all earlier wake notes", and include live state, rerun commands, and next moves.
- Workers prefer **cull+pickup** — a tracked handoff brief and a fresh seat beat a degraded
  compact.

## Law — minted by real incidents

- **Verify before done.** A DONE report is a claim: re-run the pinned gates yourself, uncached,
  and **content-read** the deliverable — never accept counts, message strings, or a green you
  did not run. Never advance past red.
- **Doorbell, not poll.** Dispatch, subscribe with `hcom events sub`, then end your turn and wake
  on the report. hcom's request watcher (`reqwatch`) covers unanswered requests; use a bounded
  watchdog nudge and a pane read only when it fires. Silence is not progress evidence.
- **One-line spawn prompts.** Context rides the files and branch, never the prompt. A capture
  serves three readers — future orchestrator, worker, reviewer — with acceptance criteria written
  at capture time and a dispatch-safe description.
- **Implement briefs quote the stop-and-report rule** (a worker that dislikes a settled decision
  stops and reports; it never substitutes its own design) **and the settled-decisions list**;
  DONE reports carry a mandatory deviations section — "none" is an explicit entry.
- **One writer per worktree at a time.** Commit-when-green + report the sha, ordered in the
  brief.
- **Durable artifacts never carry run identifiers** (M2, U7, TASK-099) — say the thing itself.

## Shapes that have worked — ideas, not rules

- **Sequential phases**: one worker at a time through playbook gates; the worker parks at its
  gate and waits for an explicit `proceed`.
- **Fan-out**: parallel workers over disjoint worktrees; integrate serially, gate after each.
- **Bake-off**: two model families write competing design notes; cross-family review picks a
  base plus binding deltas; one seat builds it.
- **Canary**: freeze the interface, build and measure one reference implementation, then fan
  out.
- **Bounce loop**: reviewer findings land verbatim in a fix brief; a fixer remediates; the same
  reviewer stays seated for the final verdict.
- **Standing reviewer as a rubric file**: the rubric lives in a durable file; fresh seats
  rehydrate from it, so the reviewer survives seat loss.
- **Divergent design rounds**: 2–3 fresh seats, same problem, different angles; the operator
  adjudicates.

## Substrate safety

Lifecycle actions resolve at action time from hcom hooks, fleet preset metadata, and herdr pane
state. `herder list` and observer stamps are display only. Never close your own pane, close a
workspace or tab, inject Escape into a running peer, or remove a checkout before its exact seat
is gone. Use `hcom send` for peer delivery and the fleet wrapper for cull.

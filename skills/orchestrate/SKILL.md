---
name: orchestrate
description: Run a long or complex plan across multiple agent sessions via the `herder` CLI. Use when the user says "orchestrate this plan" or hands over a plan or runbook that won't fit one context window.
---

# Orchestrate

Policy for running one mission across many agent sessions. The `herder` CLI is the substrate
(`herder --help`); the hcom bus carries all coordination. You are smart — this file carries only
the law that real runs minted; everything else is your judgment, composed per run into the
playbook.

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
- **Liveness per role.** Cull-on-done (default — `hcom transcript` and `herder resume` make
  culling cheap) vs keep-open for interrogation.

## Context law

- Hard cap **250k tokens** for every seat, yours included. Workers report context % in every
  DONE; a unit that can't fit the band is a breakdown failure — split it, don't push through.
- You: compact in place — write a **wake state** block to the run-log first (versioned,
  "supersedes all earlier wake notes": live state, credentials, rerun commands, next moves),
  then `herder compact '<steer>' --then '<continuation>'` (bare compact refuses).
- Workers: prefer **kill+pickup** — a tracked handoff brief and a fresh seat beat a degraded
  compact.

## Law — minted by real incidents

- **Verify before done.** A DONE report is a claim: re-run the pinned gates yourself, uncached,
  and **content-read** the deliverable — never accept counts, message strings, or a green you
  didn't run. Never advance past red.
- **Doorbell, not poll.** Dispatch, end your turn, wake on reports (a thread per unit on the
  bus). A silent worker gets a **watchdog** nudge at ~20 min, then a pane read — silence emits
  no events.
- **One-line spawn prompts** — "read <playbook> in full, then execute <unit>". Context rides the
  files and the branch, never the prompt. A capture serves three readers — future orchestrator,
  worker, reviewer — with acceptance criteria written at capture time and a dispatch-safe
  description.
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

Never: close your own pane or cull yourself; close workspaces or tabs; send `esc` to a running
peer; stop or delete sessions without operator confirmation. `herder send` is the only delivery
path to a peer.

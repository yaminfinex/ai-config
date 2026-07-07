# Relay (and the soloist)

A chain of doers with **no standing orchestrator**: each agent runs one leg, logs it, hands the
baton to a successor. Consider when the doer/orchestrator split is pure churn — design and
research arcs where the user verifies, or runbooks where the state files carry each leg fully.
With no orchestrator, each leg keeps the journal itself: its decisions, deviations, and gate
verdicts are journal entries — nobody else will ever write them.

## Per-leg protocol

1. Read playbook + journal + the relevant section of the source doc. Delegate wide reading to a
   subagent.
2. Journal `## Leg N — START`.
3. Execute the leg, scoped to this leg only.
4. **Gate:** playbook-pinned commands green. If red and out of scope: journal `BLOCKED` with the
   failing output, commit WIP, spawn nothing, stop.
5. Commit (no push, no PR).
6. Journal `## Leg N — DONE`: files changed, decisions/deviations, gate verdicts.
7. Hand off — unless the playbook marks this leg final: then log DONE, spawn nothing, stop.

## Handoff — spawn with verified delivery

```bash
herder spawn --role leg-<N+1> --agent claude --cwd <worktree> --no-focus \
  --prompt 'Relay leg <N+1>. Read <playbook> in full, then execute leg <N+1> per the relay protocol. Do not skip the verification gate.'
```

`herder spawn` verifies the initial prompt landed (`delivery_result` in `--json`); on
`prompt: NOT confirmed`, read the pane and re-send with `herder send`. Two variants:

- **Self-spawn** (true relay, default): each leg spawns its successor and verifies delivery
  before idling. No separate report — the spawned successor *is* the signal.
- **Herder-owned handoff:** when a herding pane exists anyway, a leg ends by reporting DONE on
  the bus (`hcom send @<herder> --intent request --thread leg-<N> -- ...`) and idling; the herder
  spawns the successor, with `hcom events sub --idle <leg-name> --once` as the missing-report backstop.

## Mid-leg handoff (context budget)

If a leg balloons, commit WIP and journal `## Leg N — HANDOFF (continue)` with exact remaining
steps + current state, then **spawn the continuation** — same leg, prompt notes "continue from
the HANDOFF entry".

INTERIM (TASK-003 → TASK-022): compact-in-place (self-sending a steered `/compact` via
`herder send "$HERDR_PANE_ID"`) is GONE — send is bus-only and a bus message cannot type a
slash command. `herder compact <steer>` is tracked as TASK-022; until it lands, the
fresh-spawn continuation is the only escape hatch, so size legs accordingly.

## The soloist

Degenerate relay: one role, no leg boundaries. A single agent works a runbook until context
approaches budget, then resets via the mid-leg handoff above: journal a HANDOFF entry, stop, and
a fresh copy continues the runbook (a chain of respawns over the same state files). Same state
files, gate, and mechanics as any leg; the HANDOFF entries become the runbook's progress marks.
(When TASK-022's `herder compact` lands, in-place compaction returns as the cheaper reset.)

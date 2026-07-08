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

`herder spawn` delivers the initial prompt over the bus and verifies by receipt
(`delivery_result` in `--json`): `delivered`/`queued` are success (never resend a `queued`);
`bind_timeout`/`send_failed` are safe to retry with `herder send` once the agent appears on
the bus (`hcom list`). Two variants:

- **Self-spawn** (true relay, default): each leg spawns its successor and verifies delivery
  before idling. No separate report — the spawned successor *is* the signal.
- **Herder-owned handoff:** when a herding pane exists anyway, a leg ends by reporting DONE on
  the bus (`hcom send @<herder> --intent request --thread leg-<N> -- ...`) and idling; the herder
  spawns the successor, with `hcom events sub --idle <leg-name> --once` as the missing-report
  backstop (returns immediately — the notification arrives later as a bus message; not a
  blocking waiter).

## Mid-leg handoff (context budget)

If a leg balloons, commit WIP and journal `## Leg N — HANDOFF (continue)` with exact remaining
steps + current state, then **spawn the continuation** — same leg, prompt notes "continue from
the HANDOFF entry".

Cheaper alternative when the leg is still coherent: compact in place with
`herder compact '<steer: what the continuation must keep>'` (queued into the leg's own
composer, fires at turn end) and continue the same leg — no successor spawn needed. Persist
state (WIP commit + journal note) BEFORE compacting; the fresh-spawn continuation remains the
escape hatch for sessions too far gone to steer. Since bare `/compact` STOPS the turn, an
unattended leg adds `--then '<continue from the HANDOFF entry: next steps>'` (claude-only,
TASK-034): after compaction a detached sender delivers that continuation to the leg's own bus,
so the same leg resumes itself post-compaction instead of idling until the herder notices. The
continuation only arms if the `/compact` verified and targets the leg's own verified bus name —
it cannot leak into an uncompacted or wrong session.

## The soloist

Degenerate relay: one role, no leg boundaries. A single agent works a runbook until context
approaches budget, then resets via the mid-leg handoff above: journal a HANDOFF entry, stop, and
a fresh copy continues the runbook (a chain of respawns over the same state files). Same state
files, gate, and mechanics as any leg; the HANDOFF entries become the runbook's progress marks.
In-place compaction (`herder compact '<steer>'`) is the cheaper reset when the soloist is still
coherent: persist progress marks first, compact, continue — respawn only when steering would
carry garbage forward.

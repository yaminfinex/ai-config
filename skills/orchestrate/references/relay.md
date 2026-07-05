# Relay (and the soloist)

A chain of doers with **no standing orchestrator**: each agent runs one leg, logs it, hands the
baton to a successor. Consider when the doer/orchestrator split is pure churn — design and
research arcs where the user verifies, or runbooks where the state files carry each leg fully.
Proven across a six-phase run with zero context lost; the only failure mode ever hit was prompt
*delivery* (below).

## Per-leg protocol

1. Read playbook + run-log + the relevant section of the source doc. Delegate wide reading to a
   subagent.
2. Append `## Leg N — START`.
3. Execute the leg, scoped to this leg only.
4. **Gate:** playbook-pinned commands green. If red and out of scope: `BLOCKED` block with the
   failing output, commit WIP, spawn nothing, stop.
5. Commit (no push, no PR).
6. Append `## Leg N — DONE`: files changed, decisions/deviations, verification lines pasted.
7. Hand off — unless the playbook marks this leg final: then log DONE, spawn nothing, stop.

## Handoff — new tab, verified delivery

```bash
herder spawn --role leg-<N+1> --agent claude --new-tab --cwd <worktree> --no-focus \
  --prompt 'Relay leg <N+1>. Read <playbook> in full, then execute leg <N+1> per the relay protocol. Do not skip the verification gate.'
```

Confirm `delivery_result: delivered`; if `not_landed`, focus the tab once and re-send.

**Why new-tab is load-bearing:** v1 self-spawned successors as splits in one tab — unreadable,
and prompt delivery to a non-active pane in a crowded tab silently failed three legs running. The
failure was delivery, not self-spawning. Two variants:

- **Self-spawn** (true relay, default): each leg spawns its successor with `--new-tab` and
  verifies delivery before idling. No separate ring — the spawned successor *is* the signal.
- **Herder-owned handoff:** when a herder pane exists anyway, legs end with a DONE block, ring the
  herder (`herder send <herder terminal_id> 'Leg N DONE — run-log updated'`), and idle; the herder spawns
  the successor on the ring, with a run-log sweep as the backstop for a dropped ring.

## Mid-leg handoff (context budget)

If a leg balloons: commit WIP, append `## Leg N — HANDOFF (continue)` with exact remaining steps
+ current state, spawn the continuation (same leg, prompt notes "continue from the HANDOFF
block"). This escape hatch is what lets legs be sized optimistically.

## The soloist

Degenerate relay: one role, no leg boundaries. A single agent works a runbook until context
approaches budget, then mid-leg-handoffs to a fresh copy of itself. Same state files, gate, and
mechanics; the HANDOFF blocks become the runbook's progress marks.

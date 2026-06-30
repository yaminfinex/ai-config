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
herder-spawn --role leg-<N+1> --agent claude --new-tab --cwd <worktree> --no-focus \
  --prompt 'Relay leg <N+1>. Read <playbook> in full, then execute leg <N+1> per the relay protocol. Do not skip the verification gate.'
```

Confirm `delivery_result: delivered`; if `not_landed`, focus the tab once and re-send.

**Why new-tab is load-bearing:** v1 self-spawned successors as splits in one tab — unreadable,
and prompt delivery to a non-active pane in a crowded tab silently failed three legs running. The
failure was delivery, not self-spawning. Two variants:

- **Self-spawn** (true relay, default): each leg spawns its successor with `--new-tab` and
  verifies delivery before idling. No separate ring — the spawned successor *is* the signal.
- **Herder-owned handoff:** when a herder pane exists anyway, legs end with a DONE block, ring the
  herder (`herder-send <herder terminal_id> 'Leg N DONE — run-log updated'`), and idle; the herder spawns
  the successor on the ring, with a run-log sweep as the backstop for a dropped ring.

## Mid-leg handoff (context budget)

If a leg balloons, commit WIP and append `## Leg N — HANDOFF (continue)` with exact remaining steps
+ current state, then either:

- **Compact in place** — `herder-send-self /compact <steer: HANDOFF block, remaining steps, gate>`
  as the last act before the turn ends (`herder` skill → *Self-send*). The same session continues
  its own leg post-compaction; no respawn, and the relay's notify-back/self-spawn wiring is
  untouched. Default when the context is coherent and only heavy.
- **Spawn the continuation** — same leg, prompt notes "continue from the HANDOFF block". Prefer when
  the context is degraded or a different agent/model should take the leg.

Either escape hatch is what lets legs be sized optimistically.

## The soloist

Degenerate relay: one role, no leg boundaries. A single agent works a runbook until context
approaches budget. Its cheapest budget reset is **compact in place** — bring the run-log current,
then `herder-send-self /compact <steer toward the runbook position + next steps>` and continue as
the same session — so the soloist stays one continuous agent across many compactions instead of a
chain of respawns. Fall back to a fresh-copy mid-leg handoff when a compaction would only preserve a
muddled context. Same state files, gate, and mechanics either way; the HANDOFF blocks (and the
steered summaries) become the runbook's progress marks.

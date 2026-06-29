# Fan-out (parallel pane workers)

An orchestrator spawns N workers over independent units, then collects and synthesizes. The only
topology with concurrent writers — and only under worktree isolation.

**Subagents first.** In-process subagent fan-out already covers fire-and-forget map-reduce and is
cheaper — no panes, no culling. Consider pane workers only when a unit exceeds a subagent (own
commits/worktree, longer than one context) or when someone will want to **follow up with a
worker** — question it, redirect it, interrogate a finding — without respawning.

Consider when units are genuinely independent (no shared files, no ordering). If units interact,
serialize them instead of discovering the interaction as a merge conflict.

## Rules

1. **One worktree per writer**, own branch each (`herdr worktree create --branch <unit> --base
   <run-branch> ... --json`, then `herder-spawn --new-tab --notify --cwd <path>` with the one-line
   prompt — `--notify` makes each worker ring you on done). Read-only workers may share the main
   worktree — then they write nothing, scratch included.
2. **Cap the fleet at what you can supervise**; batch beyond that.
3. **Results land as files** (e.g. `napkins/<run>/results/<unit>.md`) + a `DONE` block; then the
   worker rings the orchestrator (`herder-send <orchestrator terminal_id> 'Unit X DONE'`). Pane reads are
   for diagnosing stuck workers, not collecting output. The orchestrator idles and integrates **in
   completion order as rings arrive** — not by waiting on workers one at a time, which stalls on
   whichever you picked and is blind to whoever finished first. Keep a backstop sweep
   (`herder-list` + run-log) so a dropped ring from a busy orchestrator doesn't strand a worker.
4. **Integrate serially.** Workers never merge their own branches; the orchestrator (or an
   integration agent) lands them one at a time, re-running the gate after each — the
   post-integration gate is the one that matters.
5. **Synthesis is its own unit.** A final agent reads the result files and produces the merged
   deliverable. Don't accumulate N results in the orchestrator's context — that's the failure
   this skill exists to avoid.
6. **Liveness:** workers whose findings may need interrogation stay open after DONE; purely
   mechanical ones are culled once verified.

## Variants

- **Map-reduce review:** N read-only reviewers, each a different lens or model family, over the
  same diff; a synthesis agent dedupes and ranks. Diversity catches what redundancy can't.
- **Sharded migration:** a discovery agent writes the unit list into the playbook; workers take
  shards; an integration agent lands them sequentially.

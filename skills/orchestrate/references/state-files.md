# State files — playbook + run-log

The two files that carry a multi-session mission, living in the branch's gitignored scratch dir
(e.g. `napkins/<run>/playbook.md` + `run-log.md`). The branch carries the code; spawn prompts
stay one line because these files hold everything else.

Tests: a fresh agent given only the one-line prompt can run its unit from the playbook alone; a
fresh orchestrator (or the user) can pick up the run cold from the run-log.

## Playbook template — adapt per run

```markdown
# <Run> — playbook

Shared protocol for <mission>. **Every agent reads this file in full first.** <Source doc> is the
source of truth for *what*; this file is *how*. Key sections: <pointers, so agents read sections,
not the whole doc>.

## Run shape (agreed with the user)

- Autonomy: <autonomous — sliding-door capture mandatory | interactive — gates at: ...>
- Topology: <per stage>
- Liveness: <per role — cull-on-done / keep-open for interrogation>
- Golden agent: <bottle name + what it holds | none>
- Worktree(s) / branch / workspace: <...>

## Units — one agent each. Do ONLY your unit.

- **Unit 1 — <name>.** <Scope: in, out, doc pointers.>
- **Unit N — <name> (GATED).** <Scope.> **STOP at the gate:** write your recommendation in the
  run-log; do NOT act without a fresh go-ahead.

## Protocol — every agent

1. Read this + run-log + your unit's doc section. Delegate wide reading to a subagent.
2. Append `## Unit N — START`.
3. Execute, scoped to this unit only.
4. **Gate:** <pinned verification commands>. Cached greens are not evidence — run directly.
   Red and out of scope → `BLOCKED` block + WIP commit + stop.
5. Commit (no push/PR): `<message convention + trailer>`.
6. Append `## Unit N — DONE` (see block formats).
7. <Handoff per topology: self-spawn --new-tab + verify delivery | idle for orchestrator |
   final unit spawns nothing.>

## Context discipline (≤<budget>)

Own ONE unit. If it balloons: WIP commit, `HANDOFF (continue)` block, stop.

## Decisions already made — do not re-litigate

- <Each pre-made call, with enough rationale to apply it to unanticipated cases.>

## Escalate (stop, don't improvise) on

- <Open questions that bite — with initial proposals to proceed-and-flag with, if any>
- <Scope drift / irreversible actions / anything user-owned>
```

## Run-log block formats

Append-only, seeded with a header (worktree, branch, workspace, unit-status table — kept current
by the orchestrator if one exists, else by each agent).

```markdown
## Unit N — START
Pane: <label>. <One line.>

## Unit N — DONE
- Files changed / deleted: <...>
- Decisions / deviations: <each with why — the run's institutional memory>
- Verification: <green lines, pasted verbatim>
- **Next:** <unit N+1 ready / spawned, delivery confirmed>

## Unit N — BLOCKED
- Failing output / what's needed / WIP sha

## Unit N — HANDOFF (continue)
- State + ordered remaining steps (for an agent with zero shared memory) + WIP sha

## SLIDING DOOR — <name> (Unit N)
- Fork / Options / Chosen+why / Other door / Reversibility — see `sliding-doors.md`.
```

Record decisions and deviations even when small — post-run review audits the diff against the
run-log, and "deviations must be justified" only works if they're written down. End the run with
a harvest pass over both files before pruning.

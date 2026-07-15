# State files — playbook + journal

The two files that carry a multi-session mission, living in the branch's gitignored scratch dir
(e.g. `napkins/<run>/playbook.md` + `run-log.md`). The branch carries the code; the bus carries
reports and evidence; spawn prompts stay one line because these files hold everything else.

Tests: a fresh agent given only the one-line prompt can run its unit from the playbook alone; a
fresh orchestrator (or the user) can pick up the run cold from the journal.

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
- Models: <per role>
- Report to: <orchestrator hcom name — DONE/BLOCKED go there, one thread per unit (`--thread <unit-slug>`, members seeded on the first send) | none — relay/soloist self-spawn>
- Bus: global
- Golden agent: <bottle name + what it holds | none>
- Worktree(s) / branch / workspace: <...>
- Registered panes: <whether hand-launched panes should run `herder enroll`; whether culled panes may be reopened with `herder resume <guid>`>

## Units — one agent each. Do ONLY your unit.

- **Unit 1 — <name>.** <Scope: in, out, doc pointers.>
- **Unit N — <name> (GATED).** <Scope.> **STOP at the gate:** send your recommendation on the
  unit thread (`--intent request`); do NOT act without a fresh go-ahead.

## Protocol — every agent

1. Read this + the journal + your unit's doc section. Delegate wide reading to a subagent.
2. Execute, scoped to this unit only.
3. **Gate:** <pinned verification commands>. Cached greens are not evidence — run directly.
4. Commit (no push/PR): `<message convention + trailer>`.
5. **Report DONE on your unit thread** — the pinned commands' results inline, or attached as an
   inline bundle:
   `hcom send @<orchestrator> --intent request --thread <unit-slug> --title '<unit> done' --description '<one line>' --files <changed> --events <ids> --transcript <ranges> -- <report>`
   Red and out of scope → a BLOCKED report instead (failing output + WIP sha), then stop.
6. Then per topology: relay self-spawns the successor + verifies delivery (the successor is its
   own signal); orchestrator-driven units idle (kept open / culled per liveness); the final unit
   spawns nothing.

## Context discipline (200–250k band)

Own ONE unit. At the 200–250k-token band — every time, not a judgment call: persist state FIRST
(WIP commit + progress note on your unit thread — compaction loses anything unpersisted), then
compact in place:
`herder compact '<what to keep: unit, ACs, gate commands, thread name>' --then
'resume the unit from the progress note and report on its thread'`. If the session is too
incoherent to steer, write a full HANDOFF report (state + ordered remaining steps for an agent
with zero shared memory + WIP sha) and stop; the orchestrator culls this session before a
successor takes its label.

## Decisions already made — do not re-litigate

- <Each pre-made call, with enough rationale to apply it to unanticipated cases.>

## Escalate (stop, don't improvise) on

- <Open questions that bite — with initial proposals to proceed-and-flag with, if any>
- <Scope drift / irreversible actions / anything user-owned>
```

## Journal (run-log) — the orchestrator's record

Append-only, seeded with a header (worktree, branch, workspace, unit-status table). Written by
whoever drives the run — the orchestrator, or each leg in orchestrator-less shapes — for cold
pickup and end-of-run reporting. Reports and evidence stay on the bus (`hcom events --thread
<unit-slug>` replays a strand); a worker writes at most a one-line pointer here.

```markdown
## Unit N — DISPATCHED
Worker @<hcom-name>, thread `<unit-slug>`. <One line: scope, anything unusual.>

## Unit N — VERDICT
- Report: thread `<unit-slug>`<, bundle id if attached>
- Re-ran gates: <command → green/red, one line each — my own run, not the worker's word>
- Decisions / deviations: <the worker's calls and why — the run's institutional memory>
- Verdict: accepted | sent back (<what was wrong>)

## Unit N — BLOCKED
<From the worker's report: what failed, what's needed, WIP sha. Decision taken.>

## HANDOFF (orchestrator self-respawn)
In flight / verified / next — for a successor with zero shared memory.

## SLIDING DOOR — <name> (Unit N)
- Fork / Options / Chosen+why / Other door / Reversibility — see `sliding-doors.md`.
```

Record decisions and deviations even when small — post-run review audits the diff against the
journal, and "deviations must be justified" only works if they're written down. End the run with
a harvest pass over both files before pruning.

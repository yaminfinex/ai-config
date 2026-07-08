# Backlog.md integration — optional, detection-gated

A light coupling: when a project already uses [Backlog.md](https://github.com/MrLesk/Backlog.md),
let the run lean on it. When it doesn't, nothing changes — this whole file is skipped.

**Division of labour.** Two ledgers, no duplication:

- **Backlog.md (`backlog/`, git-tracked, durable)** = the *unit ledger*. The list of units, their
  grouping, dependencies, and current status. Survives the branch, the prune, and the run.
- **Journal (run-log — napkins, gitignored, ephemeral)** = the *decision ledger*: dispatches,
  sliding doors, deviations, verification verdicts (invariants 4 + 9). Reports and evidence ride
  the bus on unit threads; neither belongs in backlog tasks.

Rule of thumb: backlog answers *what's left and what's ready*; the journal answers *what was
decided and did it verify*; the unit thread holds the worker's report. Verification pasted into
a backlog task is a smell — reports belong on the bus; the journal records the verdict.

## Detect (do this once, at run-shape time)

```bash
command -v backlog >/dev/null 2>&1 && { [ -d backlog ] || [ -d .backlog ]; }
```

Both true → backlog-backed run. Record it in the playbook run-shape header:

```
- Backlog: yes — run label `run-<slug>` | no
```

Either false → omit the line, ignore the rest of this file. Do **not** `backlog init` a project
mid-run to enable this; absence is a valid state, not a setup gap.

## When present — the connection

**Ringfence the run with a label.** Every unit-task for this run carries `-l run-<slug>`. That
label *is* the run's scope — `backlog task list -l run-<slug> --plain` is the unit roster.

**Seed all units on the base branch, before the fan-out.** Task IDs are sequential integers
allocated at `task create` time — Backlog hands you `highest-existing + 1`, with nothing tied to
the branch. So two agents on two branches off the same base each compute the same "next" ID and
create *different* units that both answer to `task-13`; on merge they collide (or one clobbers the
other's file). Avoid this structurally: the orchestrator creates **every** unit-task on the base /
integration branch and commits them *before* spawning any agent. IDs are then allocated serially by
one writer and already exist in shared history.

- Units already exist as backlog tasks → the playbook's unit list points at task IDs; add the run
  label to each.
- Units are being defined now → create them all up front as you write the playbook, on the base
  branch:
  ```bash
  backlog task create "Unit 1 — wire schema" -l run-<slug> --priority high --plain
  backlog task create "Unit 2 — migrate" -l run-<slug> --dep task-1 --plain
  ```
  Every task body obeys the task-capture contract (invariant 2): three readers, reachable
  references, acceptance criteria at capture time, plain language.

**Agents report, never write.** Once fanned out, an agent never touches `backlog/` at all — no
`task create` (which would reopen the ID race on its branch) and no `task edit` either (see **the
board lives on the base branch** below). A follow-up unit discovered mid-run rides the bus as a
"new unit" note and lands in the journal; the orchestrator batch-creates it on the base branch
between waves, then dispatches it.

**Config is a backstop, not the fix.** Backlog's cross-branch guards (`checkActiveBranches`,
`remoteOperations`, `activeBranchDays` in `backlog/config.yml`, all on by default) scan other
branches for the highest ID before allocating — but only see tasks already committed and visible at
allocation time, so they lose to a true concurrent create (both allocate before either commits).
Confirm they're on (`grep -i branch backlog/config.yml`); it closes the *sequential*-branch cases
but does **not** replace seeding up front for concurrent fan-out. Optional: `zeroPaddedIds` for
cleaner merge diffs. Reference units in the playbook by run-label + title, not bare `task 13`, so
any collision is legible rather than silent.

**Dependencies, not prose ordering.** Encode unit order as `--dep`, then let backlog compute the
waves instead of hand-sequencing in the playbook:
```bash
backlog sequence list --plain     # wave 1 = the ready/unblocked units
```
The orchestrator dispatches a wave, waits for its DONE reports, recomputes. This replaces "spawn
Unit N after Unit N-1" bookkeeping for anything with real branching.

**The board lives on the base branch; the orchestrator is its only writer.** Workers never edit
`backlog/` in their worktrees — a status flipped on a feature branch is invisible on the board
until merge (the user watching the base branch sees a stale run), and worker-side task edits are
the source of both the ID race above and recurring merge conflicts. Instead, status rides the
signals workers already send:

- worker picks up the unit → says so on its unit thread → orchestrator:
  `backlog task edit <id> -s "In Progress" -a <worker> --plain`, commit on the base branch.
- DONE report (pinned gates green) → orchestrator verifies (invariant 4), then applies the
  **hygiene payload from the report** — per-AC evidence (`--check-ac N`), implementation notes
  (`--notes`), `-s Done` — and commits. The worker authors the content; the orchestrator holds
  the pen.
- BLOCKED report → status stays; the report carries the failure; the journal records the verdict.

Each transition is a small commit on the base branch, so `backlog board` / `task list` there is
live during the run, not a post-merge artifact.

**One writer per task → one writer for the board.** Invariant 7 (one writer per worktree) lands
here as: the orchestrator is the single writer of `backlog/`, full stop. A worker remains the
single *author* of its unit's hygiene content — it just delivers that content on the bus instead
of committing it.

## What stays the same

Spawn prompts are still one line (invariant 2) — "read `<playbook>` in full, then execute Unit N";
the playbook tells the agent whether this is a backlog-backed run and what its task ID is. The
journal is still the cold-pickup surface; reports and evidence ride the bus. Backlog being absent
must never block a run — it's an enhancement to durability, not a dependency.

## End of run

The branch merges; `backlog/` merges with it, so the durable unit record ships in the repo. Harvest
the journal into the backlog where it belongs (follow-ups discovered mid-run → new `backlog task
create`, no run label) before pruning napkins. Closed run-label tasks can stay closed in `backlog/`
as the record, or `backlog cleanup` ages them out.

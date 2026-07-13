# Decision record — migrating a live orchestration run's coordination substrate onto missions

Status: **PROPOSED — owner-confirmable.** Output of a decision unit; adversarial review is
orchestrator-dispatched. Ground truth: `docs/specs/mission-spec.md` (RATIFIED 2026-07-09).
Provenance: decision-unit ruling, 2026-07-13; run-scoped specifics are quarantined in the
Migration inventory appendix and in the unit capture — the rulings themselves are written to
apply to any live run adopting a mission mid-flight.

## Context

A long-running multi-agent orchestration keeps its coordination substrate — standing-orders
digest, playbook, append-only journal, per-unit briefs — in a gitignored napkin directory in
the project repo's main checkout. That substrate is machine-local and single-copy (a known,
logged risk). The mission system shipped: missions are the durable, git-shared home for
exactly this material. The owner directed adoption. This record rules what moves, what stays,
how a move happens under a live run, and what only the owner can decide. The run stays
operational throughout; the orchestrator (`hera`) remains the coordination writer during and
after migration.

## Ruling A — what migrates, what stays

**Migrates (into `missions/<slug>/artifacts/orchestration/`, single-writer subtree, written
only by the mission authority):**

- The standing-orders digest, the playbook, and the run journal — the three hot files of the
  coordination substrate. They move wholesale, so intra-file line citations stay valid.
- Per-unit briefs and memos, live and archived, including the archive subdirectory —
  provenance is part of the substrate.
- As a prose act (not a file move): the playbook's settled-decisions list graduates into
  `mission.md` § Decisions, per the spec's manifest shape. Distilled and rewritten by the
  authority to the durable-artifact standard; the raw list stays in the adopted playbook.

**Stays where it is:**

- **The project repo's root board (`backlog/`).** It is the sole custodian of every existing
  and future task: task IDs are load-bearing across the journal, bus threads, worktree names,
  and in-flight lanes, and the board is shared with sibling lanes under a labeled-disjoint
  custody rule that predates missions. **Double custody is forbidden**: a task lives on
  exactly one board. The mission's scaffolded board starts empty and holds at most
  mission-housekeeping items; it never mirrors project tasks. Movement between the boards, if
  it ever happens, is the spec's prose act at kickoff/closedown — no machinery. Both boards
  ride the same pinned Backlog.md (1.47.1 via mise); any future pin change gates through the
  mission CLI's backlog-floor acceptance suite before either board trusts the new version.
- **Already-durable artifacts** — design memos, specs, skills in the project repo. They are
  harvest *destinations*, not mission contents. Napkin files that were copied to durable homes
  as single-copy hedges are adopted verbatim anyway: duplication between a mission and a
  durable home is the normal shape under harvest-copy semantics (the mission stays
  self-contained). Deduplication is optional post-adopt hygiene, never a cutover step.
- **Bus history, agent registry, pane/statusline state** — machine-local infrastructure.
  Missions are deliberately unaware of it; the journal remains the durable record of what the
  bus carried.
- **Code custody** — worktrees, branches, merge protocol: unchanged.
- **Sibling efforts' napkin directories** — outside this ruling's fence. They may follow the
  same pattern by their own decision (owner's call, below).

## Ruling B — adopt mechanics under a live run

Adopt **moves, never copies** (spec §8.2: file ops + custody commit; there is no adopt
machinery). Applied live:

1. **The authority executes.** The substrate is the orchestrator's own working memory in the
   shared main checkout; no worker can safely mutate it. The migration is an authority act,
   like a rename or closeout.
2. **Cut over at a unit boundary.** No dispatch or verdict may be mid-write on the hot files
   during the move. In-flight lanes are *not* quiesced, messaged, or touched — the move is
   invisible to them by construction (rule 4 below).
3. **Hash-verified move.** Capture a sha256 manifest of the entire napkin tree before the
   move; move; compare after. Byte-identical or stop. The manifest itself becomes a mission
   artifact. One adopt custody commit (`mission(<slug>): adopt <summary>`) with a
   `Mission-Source:` trailer naming the source checkout.
4. **Deferred adopt for in-flight briefs.** Any brief a currently-dispatched unit was pointed
   at stays at its napkin path until that unit closes, then is adopted with its own custody
   commit. The deferred set is frozen from live board state at execution time and recorded in
   the migration task's board notes (a run-scoped surface) — not in this document.
5. **Pointer stubs for the hot files.** The three hot files leave one-line stubs at their old
   paths containing only the new path. Stubs are gitignored, machine-local, and deleted at run
   close; they exist because resume instructions and compact steers minted before the cutover
   name the old paths. A stub is not a copy — content moves exactly once.
6. **Journal continuity.** Appends continue at the mission path immediately. Commit rhythm
   mirrors the existing board rhythm: commit at dispatch/verdict boundaries, push when a unit
   of work lands. Until the owner confirms push authority on the missions repo, commits stay
   local and the unit stops-and-reports before any push.

## Ruling C — slug and scaffold

- **Slug: `herder-dx`** (owner may override at confirm). The mission names the *intent* — the
  herder developer-experience effort — not the protocol executing it; a "run-" prefix is
  orchestrate-layer dialect, not mission identity. Valid under the spec's slug rules. The
  original directory name stays greppable in the adopt commit and the inventory below.
- **Scaffold command:**

  ```sh
  mish new herder-dx --authority hera --owner <owner> --no-marker
  git -C "$MISSIONS_REPO" add missions/herder-dx
  git -C "$MISSIONS_REPO" commit -m 'mission(herder-dx): new — adopt home for the live orchestration run'
  ```

- **`--no-marker` is deliberate.** A `.mission` marker in the shared main checkout would
  silently redirect every `mish` invocation by every agent working under that tree. The
  authority addresses the mission with `--mission` explicitly; planting a marker later is its
  own deliberate act, never a scaffold side effect.
- **`--owner` must be supplied.** Verified on this node: `$SESSION_OWNER` is unset and the OS
  username is not the human — the default owner chain would stamp a wrong value. Owner
  supplies the attribution value (owner-only item below).
- **Manifest seeding:** Purpose (one durable paragraph), Scope (repos/branches touched),
  Decisions (the graduated settled-decisions list, rewritten durably). Manifest prose is
  written to the durable-artifact standard — no task numbers, unit letters, or wave names;
  raw run identifiers live in the adopted artifacts and on boards, which is exactly where the
  identifier doctrine puts them.
- **The scaffolded board starts empty** (Ruling A). Artifact layout: everything adopted lands
  under `artifacts/orchestration/`, the authority's disjoint path; any future non-authority
  writer gets its own disjoint subtree per the spec's multi-writer doctrine.

## Ruling D — gitignored artifacts entering tracked custody

- **The single-copy risk is the payoff.** Machine-local, gitignored substrate dies with the
  machine; mission custody makes it durable and team-visible. This resolves the run's own
  logged risk on exactly the files it named.
- **Run-scoped identifiers inside adopted artifacts are legitimate.** The identifier doctrine
  bans opaque run identifiers from durable *doctrine* surfaces and names run artifacts
  (playbook, journal, boards, bus, commit messages) as where they live. A mission's
  `artifacts/` is the durable *home* for run-scoped material — adoption does not launder
  identifiers into doctrine. Closeout harvest remains the extraction point into evergreen
  homes, unchanged.
- **Tracked means shared: secret-scan before push.** A mandatory scan of the adopted tree for
  credentials/tokens precedes any push (precedent this run: a broad search once pulled
  credential values into a local transcript; the napkin memos were kept clean — verify
  anyway). Findings block the push, not the local move.

## Owner-only items (nothing below is the migration unit's to decide or do)

1. **Create/choose the missions repo and its hosting/access.** Recommendation: a dedicated
   repo, not the project repo — keeps board nesting trivial and decouples mission push rhythm
   from the project repo's ship discipline. Nesting inside the project repo is spec-legal if
   preferred.
2. **Provision `$MISSIONS_REPO`** across surfaces (shell profile / mise conf.d / agent env).
   Machine-level changes are escalate-only under the run protocol; verified unset on this
   node at decision time.
3. **Push authority** for the orchestrator on the missions repo (the project repo's
   "agents don't ship" rule needs an explicit mission-repo counterpart).
4. **The `--owner` attribution value** (see Ruling C).
5. **Slug confirm** (`herder-dx` recommended).
6. **Whether sibling lanes' napkin directories follow** — outside this fence; same pattern
   applies if so.
7. **Follow-up doctrine edit** (separate unit, owner-reviewed territory): the orchestrate
   skill's state-files guidance should learn that a mission's `artifacts/` is the preferred
   home over gitignored scratch when a mission exists. Not in this migration's fence.

**Mechanical preconditions, verified absent on this node at decision time** (installable by
the unit, listed for honesty): the `mish` binary is not on PATH (`just install` from the CLI's
source directory); the mish skill is not symlinked into agent skill roots (re-run the house
setup script). Backlog.md 1.47.1 is present and pinned — that precondition already holds.

## Migration unit — capture

**Title:** adopt the live run's coordination substrate into mission custody (execute this
record). **Type:** implement (scaffold + file ops; no behavior code). **Executor:** the
orchestrator/authority itself (Ruling B.1) — flagged for owner confirm since the run
convention is that the orchestrator holds no unit work. **Gate:** the acceptance criteria
below are the gate; there is no code battery to run. Docs/file-move only.

**Acceptance criteria:**

1. Preconditions echoed green before any write: `mish` on PATH and `--help` sane;
   `backlog --version` = 1.47.1; `$MISSIONS_REPO` set, pointing at a git clone whose remote
   the owner confirmed. Any red → refuse to start.
2. Scaffold per Ruling C; `mish status` reports zero warnings; the `new` custody commit
   parses under the custody grammar.
3. Pre-move sha256 manifest of the full napkin tree captured; post-move comparison
   byte-identical for every moved file; the manifest lands as a mission artifact.
4. Deferred set (in-flight briefs) frozen from live board state at execution and recorded in
   this task's board notes; nothing in the deferred set is moved; each is adopted at its
   unit's close with its own custody commit.
5. Hot-file pointer stubs in place at the old paths: gitignored, one line each, new path only.
6. Secret scan over the adopted tree completes clean before any push; a hit blocks the push
   and stops the unit.
7. Continuity drill passes: one journal append at the mission path; one resume-read of the
   standing-orders digest via its new path; zero messages sent to in-flight workers; zero
   writes to any worktree.
8. Project-board diff is this unit's own task hygiene only; the mission board contains no
   migrated project tasks.
9. `mission.md` seeded per Ruling C (Purpose / Scope / graduated Decisions; durable-standard
   prose).
10. Push only under confirmed push authority; otherwise stop-and-report with commits local.
11. Custody trail greppable: `git log --grep 'mission(<slug>):'` in the missions repo shows
    the `new` and `adopt` commits.

**Territory fence.** OWNS: `missions/<slug>/**` in the missions repo (create/write); the
run's napkin directory (move out + stubs). DOES NOT TOUCH: the project board beyond this
unit's own task hygiene; `docs/**`, `skills/**`, `tools/**`, `bin/**` in the project repo;
any other napkin directory; any worktree; any machine-level env (owner precondition, item 2
above). Stop-and-report on: any precondition red, any hash mismatch, any secret-scan hit,
unclear push authority, or any in-flight lane touching the napkin directory mid-move.

## Appendix — migration inventory (explicitly quoted, run-scoped)

The live run is `run-herder-dx`; its substrate lives at `napkins/run-herder-dx/` in the
project repo's main checkout (gitignored — single-copy today). Hot files: `standing-orders.md`
(authoritative digest, read in full at every orchestrator resume), `run-log.md` (append-only
journal, ~2,400 lines, line-cited from the digest), `playbook.md` (wave-1 protocol; later
wave playbooks archived). Plus the live brief/memo set and `archive/` (~40 files at decision
time; the executing unit re-lists at execution — this inventory is descriptive, not the
manifest). Destination: `missions/herder-dx/artifacts/orchestration/`, preserving relative
layout including `archive/`. Not migrating, per Ruling A: the project board at `backlog/`
(sole task custodian), durable memos already in `docs/design/`, bus/registry state, and the
napkin directories of the bootstrap, sesh, and mish build efforts.

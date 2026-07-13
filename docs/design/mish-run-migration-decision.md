# Decision record — migrating a live orchestration run's coordination substrate onto missions

Status: **PROPOSED — owner-confirmable.** Output of a decision unit, revised after adversarial
review round 1. Ground truth: `docs/specs/mission-spec.md` (RATIFIED 2026-07-09). One conflict
with the ratified spec was found during review; it is presented as an owner decision point
below, never overridden here. Quarantine rule for this document: Rulings A–D, the owner
decision points, and the unit capture are written project-agnostic and reusable; every
run-specific value (run name, slug recommendation, authority label, node-verified environment
facts, precedents, local shipping policy) lives only in the quarantined appendix.

## Context

A long-running multi-agent orchestration keeps its coordination substrate — standing-orders
digest, playbook, append-only journal, per-unit briefs — in a gitignored napkin directory in
the project repo's main checkout. That substrate is machine-local and single-copy, a logged
risk. The mission system shipped: missions are the durable, git-shared home for exactly this
material. This record rules what moves, what stays, how a move happens under a live run, and
what only the owner can decide. The run stays operational throughout; the orchestrator remains
the coordination writer (mission authority) during and after migration.

## Ruling A — what migrates, what stays

**Migrates (into `missions/<slug>/artifacts/orchestration/`, a single-writer subtree written
only by the mission authority):**

- The entire napkin tree, verbatim and layout-preserving: the standing-orders digest, the
  playbook, the run journal, per-unit briefs and memos live and archived, including the
  archive subdirectory — provenance is part of the substrate. Wholesale movement keeps
  intra-file line citations and intra-tree relative references valid.
- As a prose act (not a file move): the playbook's settled-decisions list graduates into
  `mission.md` § Decisions, per the spec's manifest shape — distilled and rewritten by the
  authority to the durable-artifact standard; the raw list stays in the adopted playbook.

**Stays where it is:**

- **The project repo's root board.** It is the sole custodian of every task — existing,
  future, and the migration unit's own. Task IDs are load-bearing across the journal, bus
  threads, and worktree names, and the board is shared with sibling lanes under a
  labeled-disjoint custody rule that predates missions. **Double custody is forbidden, and the
  mission board stays empty for this mission — full stop**: no migrated tasks, no migration or
  closeout housekeeping tasks, nothing. Mission metadata and prose live in the manifest and
  artifacts; all task state lives on the root board. (The scaffolded board exists because the
  spec requires one per mission; emptiness is this mission's discipline, so readers never need
  two boards to know what work exists.) Movement between boards, if it ever happens, is the
  spec's prose act at kickoff/closedown — no machinery. Both boards ride the same pinned
  Backlog.md version; any pin change gates through the mission CLI's backlog-floor acceptance
  suite before either board trusts the new version.
- **Already-durable artifacts** — design memos, specs, skills in the project repo. They are
  harvest *destinations*, not mission contents. Napkin files that were previously copied to
  durable homes as single-copy hedges are adopted verbatim anyway: duplication between a
  mission and a durable home is the normal shape under harvest-copy semantics (the mission
  stays self-contained). Deduplication is optional post-adopt hygiene, never a cutover step.
- **Bus history, agent registry, pane/statusline state** — machine-local infrastructure.
  Missions are deliberately unaware of it; the journal remains the durable record of what the
  bus carried.
- **Code custody** — worktrees, branches, merge protocol: unchanged.
- **Sibling efforts' napkin directories** — outside this ruling's fence; they may follow the
  same pattern by their own decision (owner's call, below).

## Ruling B — adopt mechanics under a live run

Adopt **moves, never copies** (spec §8.2: file ops + custody commit; there is no adopt
machinery). Applied live:

1. **The authority executes.** The substrate is the orchestrator's own working memory in the
   shared main checkout; no worker can safely mutate it. The migration is an authority act,
   like a rename or closeout.
2. **Cut over at a unit boundary.** No dispatch or verdict may be mid-write on the journal,
   digest, or playbook during the move. In-flight lanes are not quiesced or messaged; their
   continuity is *proven* by the drills in step 5, not assumed.
3. **Custody-proof pipeline, in this order** — byte-preservation alone does not prove tracked
   custody (staging obeys the destination repo's ignore rules; a matching local hash manifest
   only proves files exist on one machine):
   1. Capture a sha256 manifest of every file in the source tree (path + content hash). The
      manifest itself becomes a mission artifact.
   2. **Secret-scan the source tree now** — before any commit, not merely before push: a
      committed secret is a durable git object even unpushed. Any hit stops the unit.
   3. **Preflight destination ignore rules**: feed every destination-relative path from the
      manifest through the missions repo's ignore check (`git check-ignore --stdin` or
      equivalent); any match stops the unit until the rule is resolved.
   4. Copy the tree into `missions/<slug>/artifacts/orchestration/`, layout preserved.
   5. Stage, then **compare staged custody to the manifest**: the staged path set under the
      mission dir must equal the manifest's path set exactly, and each staged blob's content
      (e.g. `git cat-file blob` piped to sha256) must match its manifest hash. Any gap stops
      the unit. The source tree still exists at this point.
   6. Make the adopt custody commit (`mission(<slug>): adopt <summary>`) with a
      `Mission-Source:` trailer naming the source checkout.
   7. **Verify a clean clone reproduces the manifest**: clone the missions repo fresh and
      re-run the manifest comparison against the clone. Only a passing clone check declares
      shared custody.
   8. Only now delete the source tree and install the compatibility symlink (step 4).
4. **Compatibility symlink — the one mechanism keeping pre-cutover contexts alive.** After
   the pipeline passes, replace the source directory with a single symlink:
   `<old napkin dir> → $MISSIONS_REPO/missions/<slug>/artifacts/orchestration`. Because the
   move is whole-tree and layout-preserving, this one link makes **every** moved old pathname
   resolve transparently — hot files, briefs, archived material, and the transitive closure of
   old-path references *inside* the corpus (briefs referencing sibling briefs, the journal,
   the archive), which the review proved exist. Pre-cutover contexts (compact continuation
   steers, in-flight workers, their re-reads) are all machine-local, which is exactly the
   scope a symlink covers. A symlink is a filesystem pointer, not a copy — content exists once,
   in mission custody; move semantics hold. Writes through the old path (none expected; the
   authority switches to the new path immediately) would land inside mission custody, so no
   divergence is possible. **Retirement is an explicit step, not an expiry**: the symlink is
   removed only when every pre-cutover context has closed — every unit dispatched before the
   cutover is closed and no live continuation steer names old paths, as confirmed from the
   board and journal — or at run close, whichever comes first; the removal is recorded in the
   journal. This mechanism supersedes any per-file stub or deferred-adopt scheme.
5. **Continuity is gated by two drills, not claimed:**
   - *Cold-resume drill:* execute the read sequence of an already-minted pre-cutover resume
     steer — reading the digest, journal, and playbook in full via their **old** paths — and
     confirm full content (not a pointer line) is returned.
   - *Dependency walk:* enumerate every old-path reference in the moved corpus (grep for the
     old directory's absolute and relative path forms across the adopted tree), then open
     each referenced target via its old path. Every one must resolve to full content.
   Both drills run post-cutover, before the unit reports. Zero writes to any worktree.
6. **Journal continuity.** Appends continue at the mission path immediately. Commit rhythm
   mirrors the existing board rhythm: commit at dispatch/verdict boundaries, push when a unit
   of work lands. Until the owner confirms push authority on the missions repo, commits stay
   local and the unit stops-and-reports before any push.

## Ruling C — slug and scaffold

- **The slug names the intent, not the protocol.** A mission is the durable home of an
  effort; protocol-layer prefixes (a "run-" spelling, wave names) are orchestrate dialect and
  do not belong in mission identity. The concrete slug recommendation for this migration is in
  the appendix; the owner confirms or overrides it.
- **Sync before scaffold.** Slug uniqueness is checked per clone: `mish new` runs only from a
  missions-repo clone that is clean and synced to its upstream (fetch + fast-forward, clean
  status) — the spec's own rhythm for narrowing the whole-directory slug-collision window.
- **Scaffold command:**

  ```sh
  git -C "$MISSIONS_REPO" pull --ff-only          # clean, synced clone — precondition
  mish new <slug> --authority <authority> --owner <owner> --no-marker
  git -C "$MISSIONS_REPO" add "missions/<slug>"
  git -C "$MISSIONS_REPO" commit -m 'mission(<slug>): new — adopt home for a live orchestration run'
  ```

- **`--no-marker` is deliberate.** A `.mission` marker in a shared project checkout would
  silently redirect every `mish` invocation by every agent working under that tree. The
  authority addresses the mission with `--mission <slug>` explicitly; planting a marker later
  is its own deliberate act, never a scaffold side effect. Consequence, verified in review:
  bare `mish status` from an unrelated directory **refuses** (exit 1) by spec — the status
  command in every check below is therefore exactly `mish status --mission <slug>`.
- **Status checks are named and timed.** `mish status --mission <slug>` runs after each
  custody commit. The zero-warnings bar applies to format warnings (pinned-config drift,
  frontmatter/dirname mismatch, missing board or artifacts); the sync-staleness line is
  expected until the final push, after which a fully clean report is required.
- **`--owner` must be supplied** wherever the node's ambient identity is not the human — the
  default owner chain would stamp a wrong value. The owner supplies the attribution value
  (owner decision point below; this node's verified facts are in the appendix).
- **Manifest seeding:** Purpose (one durable paragraph), Scope (repos/branches touched),
  Decisions (the graduated settled-decisions list, rewritten durably). Manifest prose is
  written to the durable-artifact standard; raw run identifiers stay in adopted artifacts —
  subject to the owner ruling on identifier custody below.
- **The scaffolded board stays empty** (Ruling A). Artifact layout: everything adopted lands
  under `artifacts/orchestration/`, the authority's disjoint path; any future non-authority
  writer gets its own disjoint subtree per the spec's multi-writer doctrine.

## Ruling D — gitignored artifacts entering tracked custody

- **The single-copy risk is the payoff.** Machine-local, gitignored substrate dies with the
  machine; mission custody makes it durable and team-visible — resolving the run's own logged
  risk on exactly the files it named. Custody is *proven* by the Ruling B pipeline (staged
  comparison + clean-clone check), never inferred from local byte preservation.
- **Tracked means shared: secret-scan before the custody commit** (Ruling B step 3.ii). A hit
  blocks the commit, not just the push.
- **Identifier custody is NOT ruled here — it is an owner decision** (point 1 below). The
  adopted corpus contains run references, task IDs, bus names, and historical session
  identifiers; the ratified mission spec's invariant that no herder concept appears in any
  mission file is in tension with adopting that corpus verbatim, and only the spec's owner can
  resolve it. No adopt happens before that ruling.

## Owner decision points (nothing below is the migration unit's to decide or do)

1. **Identifier custody vs the ratified spec.** The spec (its herder-unawareness invariant)
   says no herder concept — guid, seat, label lease, run reference — appears in *any* mission
   file. The substrate to be adopted is saturated with exactly that vocabulary. Two conforming
   options: **(a)** amend the spec through its living decision record to exempt *opaque
   adopted contents under `artifacts/`* — nothing mission-side reads, resolves, or joins them,
   which is the invariant's operative concern; or **(b)** define a redaction/transformation
   boundary applied at adopt time. **Recommendation: (a)** — the spec already treats artifacts
   as free-form and never-interpreted, and redaction would destroy the journal's evidentiary
   value; but this is the owner's ruling to make, and the migration unit is blocked on it.
2. **Create/choose the missions repo and its hosting/access.** Recommendation: a dedicated
   repo, not the project repo — keeps board nesting trivial and decouples mission push rhythm
   from the project repo's ship discipline. Nesting inside the project repo is spec-legal if
   preferred.
3. **Provision `$MISSIONS_REPO`** across surfaces (shell profile / tool config / agent env).
   Machine-level changes are escalate-only under the run protocol.
4. **Push authority** for the orchestrator on the missions repo (the project repo's
   agents-don't-ship rule needs an explicit mission-repo counterpart).
5. **The `--owner` attribution value** (see Ruling C; node facts in the appendix).
6. **Slug confirm** (recommendation in the appendix).
7. **Provision the mission CLI binary and companion skill on the executing node.** This is an
   owner precondition, not unit work: both writes mutate user-level machine state (the Go
   binary install target; live agent skill roots), which the fence treats as escalate-only.
   The unit *verifies* presence and refuses to start otherwise; it installs nothing.
8. **Whether sibling lanes' napkin directories follow** — outside this fence; the same
   rulings apply if so.
9. **Follow-up doctrine edit** (separate unit, owner-reviewed territory): the orchestrate
   skill's state-files guidance should learn that a mission's `artifacts/` is the preferred
   home over gitignored scratch when a mission exists. Not in this migration's fence.

## Migration unit — capture

**Title:** adopt the live run's coordination substrate into mission custody (execute this
record). **Type:** implement (scaffold + file ops; no behavior code). **Executor:** the
orchestrator/authority itself (Ruling B.1) — flagged for owner confirm since the run
convention is that the orchestrator holds no unit work. **Gate:** the acceptance criteria
below are the gate; there is no code battery to run. **Blocked on:** owner decision points
1–7 (identifier ruling, repo, env, push authority, owner value, slug, node provisioning).

**Acceptance criteria:**

1. Preconditions echoed green before any write: mission CLI on PATH with sane `--help`; the
   board CLI at the project's pinned version; `$MISSIONS_REPO` set and pointing at a git
   clone that is clean and synced to its confirmed upstream. Any red → refuse to start; the
   unit provisions nothing.
2. The owner's identifier-custody ruling (decision point 1) is recorded — as a spec
   decision-record entry for option (a), or a defined redaction boundary for option (b) —
   before any adopt step runs.
3. Scaffold per Ruling C (sync, `mish new … --no-marker`, custody commit); the `new` commit
   parses under the custody grammar; `mish status --mission <slug>` after the commit shows
   zero format warnings (sync-staleness line permitted until final push).
4. Custody-proof pipeline passes in Ruling B order: manifest captured and retained as a
   mission artifact; secret scan clean **before** the adopt commit; destination ignore-rule
   preflight matches zero manifest paths; staged path set and blob contents equal the
   manifest exactly **before** source deletion; adopt custody commit with `Mission-Source:`
   trailer; a fresh clone of the missions repo reproduces the manifest. Any failure stops the
   unit with the source tree intact.
5. Compatibility symlink installed only after AC 4 passes: the old napkin directory path is a
   single symlink resolving into the mission's `artifacts/orchestration/` subtree.
6. Continuity drills pass post-cutover (Ruling B.5): the cold-resume drill returns full
   content for digest, journal, and playbook via old paths; the dependency walk resolves
   every old-path reference found in the adopted corpus. Zero messages to in-flight workers;
   zero writes to any worktree.
7. Symlink retirement is captured, not performed: a journal entry records the retirement
   criterion (all pre-cutover contexts closed, or run close) and the explicit removal action.
8. The mission board contains **zero tasks** at unit close — scaffolded empty, still empty;
   all task state, including this unit's own, lives on the project root board; mission
   metadata/prose lives only in manifest and artifacts.
9. `mission.md` seeded per Ruling C (Purpose / Scope / graduated Decisions; durable-standard
   prose).
10. Push only under confirmed push authority; after the final push,
    `mish status --mission <slug>` reports fully clean, including the staleness line.
    Authority unconfirmed → stop-and-report with commits local.
11. Custody trail greppable in the missions repo: `git log --grep 'mission(<slug>):'` shows
    the `new` and `adopt` commits.

**Territory fence.** OWNS: `missions/<slug>/**` in the missions repo (create/write); the
run's napkin directory (move out + the one compatibility symlink). DOES NOT TOUCH: the
project root board beyond this unit's own task hygiene; project docs, skills, tools, or
executable trees; any other napkin directory; any worktree; any machine-level state — binary
installs, skill roots, and env provisioning are owner preconditions (decision points 3 and
7), never unit writes. **Stop-and-report on:** any precondition red; any manifest, staging,
ignore-preflight, or clean-clone mismatch; any secret-scan hit; unclear push authority; any
in-flight lane touching the napkin directory mid-move; and **any conflict with the ratified
mission spec — surfaced, never overridden.**

## Appendix — migration inventory and node facts (quarantined, explicitly quoted)

Everything run-specific lives here and nowhere else in this document.

- **Run and substrate:** the live run is `run-herder-dx`; its substrate is
  `napkins/run-herder-dx/` in the project repo's main checkout
  (`/home/grace/Coding/ai-config`), gitignored — single-copy today. Hot files:
  `standing-orders.md` (authoritative digest, read in full at every orchestrator resume),
  `run-log.md` (append-only journal, ~2,400 lines, line-cited from the digest),
  `playbook.md` (wave-1 protocol; later wave playbooks in `archive/`). Plus the live
  brief/memo set and `archive/` — ~40 files at decision time; the executing unit re-lists at
  execution (this inventory is descriptive, not the manifest). Review confirmed transitive
  old-path references inside the corpus (briefs referencing sibling briefs, the journal, the
  playbook, and archived material by old absolute paths) — the evidence behind Ruling B.4.
- **Destination and names:** recommended slug `herder-dx` (names the herder
  developer-experience effort; the "run-" prefix is protocol dialect — Ruling C); authority
  `hera` (the run's orchestrator, who remains coordination writer per the task constraint);
  destination `missions/herder-dx/artifacts/orchestration/`, preserving relative layout.
- **Node-verified facts (decision time, 2026-07-13):** `$MISSIONS_REPO` unset; no missions
  repo exists; the `mish` binary is not on PATH (all Go install targets checked); the mish
  companion skill is not symlinked into any agent skill root; `$SESSION_OWNER` unset and the
  OS username is not the human — the default owner chain would stamp a wrong owner;
  Backlog.md 1.47.1 present and pinned via mise (that precondition already holds). The task
  file's claim that binary and skill were already installed is stale on this node.
- **Run precedents informing the rulings:** a logged single-copy-risk rule for durable napkin
  memos (the migration's payoff); one contained incident where a broad search read credential
  values into a local transcript (why the secret scan is mandatory and early); the project
  repo's standing rule that agents don't ship (why mission-repo push authority is decision
  point 4); the run's board rhythm of commit-per-move, push-per-unit (mirrored in Ruling
  B.6).
- **Not migrating, per Ruling A:** the project board at `backlog/` (sole task custodian,
  shared with the sesh lane under labeled-disjoint custody), durable memos already in
  `docs/design/`, bus/registry state, and the napkin directories of the bootstrap, sesh, and
  mish build efforts.

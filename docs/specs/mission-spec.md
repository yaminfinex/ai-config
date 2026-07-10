# Mission Spec

Status: **RATIFIED 2026-07-09** (owner walkthrough of the M1–M17 checklist plus a doc-review
round; final rulings recorded in §11/§12; amended same day by owner rulings §12.9–§12.10 —
CLI named `mish`, cross-references on the board's native `references` field). This document
is the ground truth for missions:
ubiquitous language, domain model, invariants, expected behaviour, high-level design, and
acceptance scenarios. It derives from the boundary grilling record
(`docs/design/2026-07-08-sessions-missions-boundaries.md` §6b, Q11–Q17) and encodes the
nested-board verification completed 2026-07-08. Ratified is not frozen: implementation
surfaces differences against this document as it finds them so requirements can evolve — §12
is the living decision record.

---

## 1. Purpose & scope

Missions exist because work that outgrows one session needs a home that is **files, not a
system**. Today that home is improvised per effort — napkin directories that are machine-local
and die with the branch, run logs that only their orchestrator can read, artifacts scattered
across worktrees. A mission is the durable, team-visible version of the same thing: one
directory holding a manifest, a task board, and artifacts, moved and shared by plain git.

The design centre is **self-containment**. A mission is its directory: everything the mission
is lives under `missions/<slug>/`, so the unit that moves, syncs, archives, and gets deleted is
one subtree. Missions rely on their own CLI, a companion skill, and as little else as possible —
no daemon, no ingestion pipeline, no message bus, no herder. A machine with only the `mish`
CLI and a clone of the missions repo is a complete, useful installation.

The dependency doctrine is asymmetric and ratified (Q17): **herder may be very mission-aware;
missions are completely herder-unaware.** No herder concept — guid, seat, label lease, run ref —
appears in this model or in any mission file. Where richer identity is wanted (which agent is
which session), the join happens on herder's side at view time, keyed on the opaque names
missions already carry. Orchestrate, likewise, is a consumer: it runs mission-native and keeps
its own file conventions inside `artifacts/`, which this spec deliberately does not define.

In scope: the mission directory format, the `mission.md` manifest, the nested board, context
resolution, the CLI's three verbs, the multi-writer/multi-node doctrine, and the split between
what the CLI does and what the companion skill teaches. Out of scope (§10): realtime observation,
viewers, transcript search, session identity, event logs, and every form of adoption/harvest
machinery.

## 2. Ubiquitous language

| Term | Meaning |
|---|---|
| **mission** | One unit of intent big enough to need shared, durable working memory: a directory under the missions repo holding a manifest, a board, and artifacts. Strictly opt-in — most work never becomes one. |
| **`mish`** | The mission CLI binary — verbs `new` / `backlog` / `status` — named to sit beside `sesh` (§12.10). The CLI is `mish`; the format vocabulary stays *mission* (`missions/`, `mission.md`, `.mission`, the custody grammar). |
| **missions repo** | The one shared repository holding all missions for all nodes and people (D11). Located by `$MISSIONS_REPO`. Plain git; the CLI itself never touches git. |
| **mission dir** | `missions/<slug>/` inside the missions repo. Self-contained; moves as a unit; *is* the mission. |
| **slug** | The mission's identity and directory name. Chosen at creation, path-safe (§4.3), unique by dir existence. |
| **manifest** (`mission.md`) | The mission's single summary document: frontmatter (slug, authority, owner, status, created) + prose (purpose, scope, decisions, closeout). Edited only by the authority. |
| **authority** | The advisory `authority:` value in the manifest: **write authority** — the one context allowed to edit the manifest or restructure the board. A label-grade name with deliberately opaque interpretation (an agent, a human, a role — the spec doesn't editorialize). Transfer = the authority editing the field. Enforced by git conflict doctrine (§7), never by the CLI. |
| **owner** | The `owner:` value in the manifest: the human a mission's work is attributed to, as a label-grade name meant to be read as a person. Distinct from authority (write rights, often an agent). Stamped by `new` via the owner chain (`--owner` → `$SESSION_OWNER` → OS username) and echoed back at creation; correcting it later is a manifest edit. Attribution, never authentication. |
| **`SESSION_OWNER`** | The one cross-surface env var declaring the human behind work on a node whose ambient identity is device-grade (provisioned shared servers: OS user = service account, tailnet identity = device). Shared vocabulary with herder and the session-shipping service. Missions read it at `new` only (birth provenance, never resolved later) and never set or validate it. Absence is meaningful: no declaration means ambient identity is trustworthy. |
| **label-grade name** | An opaque string naming an agent or person — a plain human string for tool-less use, a herder label where herder happens to manage the writer. Missions treat every such value as text; nothing resolves or validates it. |
| **board** | The mission's task tracker: a verbatim nested Backlog.md instance at `missions/<slug>/backlog/`, config pinned at scaffold time (§4.4). Missions invent nothing board-shaped. |
| **task** | A Backlog.md task on the mission board. The unit of assignment, status, and notes. |
| **assignee** | Backlog.md's native assignee field, holding a label-grade name. The whole mission↔agent contract (Q17): every richer join happens outside missions. |
| **cross-reference** | An entry in a task's native Backlog.md `references` list: an opaque string naming where the task's work happened or landed — commit, branch, PR, session, agent (§8.3, §12.9). Written through the passthrough (`--ref`); missions validate nothing. |
| **artifact** | Any file under `missions/<slug>/artifacts/` — free-form outputs, analyses, reports. Structure by convention only (disjoint paths per writer); the CLI never interprets contents. |
| **context marker** | A `.mission` file in a working directory (typically a code worktree), pointing at the mission by slug. The D6 mechanism that lets `mish` commands run far from the missions repo. |
| **context resolution** | How a `mish` invocation finds its mission: explicit flag, then cwd-inside-a-mission-dir, then the single marker on the ancestor chain (§5). Markers never shadow one another. Never ambient identity beyond `$MISSIONS_REPO` locating the repo. |
| **passthrough** | `mish backlog …`: the Backlog.md CLI executed with cwd pinned to the mission dir, exposing a deliberate allowlist of subcommands, arguments forwarded verbatim (§6.2). |
| **allowlist** | The Backlog.md subcommands the passthrough exposes (§6.2). Deliberate and closed: anything not listed — including subcommands future Backlog.md versions add — refuses until explicitly added. |
| **pinned config** | The five `backlog/config.yml` keys stamped at scaffold and invariant for the mission's life (§4.4): four safety keys pinned `false` plus `filesystem_only` pinned `true` — together they neutralize Backlog.md's git/remote/browser behaviours inside the shared repo. |
| **adopt / harvest** | Custody verbs, not CLI verbs: an agent moving files into a mission (adopt) or copying results out to their permanent home (harvest), recorded by conventioned commit messages and board notes (§8). |
| **custody commit** | A git commit whose message follows the §8.2 grammar, carrying the provenance/disposition record that the killed event log (Q15) used to promise. |
| **closeout** | Ending a mission: final board states, the manifest's Closeout section written, `status: closed`. A skill checklist, not a CLI verb. |

## 3. Domain model

A mission is three things in one directory: **summary in the manifest, work state on the board,
outputs in artifacts.** Identity is the slug; authority is a field, not a mechanism; everything
an agent needs to participate is readable from the subtree itself.

```
missions repo 1 ──contains──→ 0..* mission dirs          (one shared repo, D11)
mission 1 ──has──→ 1 manifest, 1 board, 1 artifacts tree
mission 1 ──names──→ 1 authority (write authority, opaque) + 1 owner (human attribution)
board 1 ──holds──→ 0..* tasks; task ──assignee──→ 0..1 label-grade name
marker 0..* ──point at──→ 1 mission (markers are pointers, never parts)
```

Lifecycle is deliberately shallow: `active → closed`, both plain frontmatter values written by
the authority. Closed missions stay readable and greppable in place; physical archival or
deletion is a repo-hygiene decision made by humans with git, outside this spec.

### 3.1 Invariants

1. **A mission is its directory.** Everything constituting the mission lives under
   `missions/<slug>/`; the dir is self-contained (board config included) and moves as a unit.
   Markers point at missions from outside and are never part of them.
2. **Missions are herder-unaware.** No guid, seat, label lease, run reference, or any other
   herder concept appears in any mission file or in this model. `authority:`, `owner:`, and
   assignee values are opaque label-grade text; nothing mission-side resolves, validates, or
   joins them.
3. **Missions are strictly opt-in.** The missionless path costs zero: no scaffolding, no marker,
   no behaviour change in any repo that has no mission context. A `mish` command without
   resolvable context refuses with guidance; it never creates anything implicitly.
4. **The CLI never mutates git.** No verb commits, stages, pushes, or otherwise writes git
   state — the one reserved exception is the explicit per-invocation auto-commit flag (§6.4),
   opt-in and off by default. `mish status`, and only it, may make **read-only** git
   queries to surface sync staleness (§6.3; the M6 read amendment, §12.8). Everything works —
   degraded to single-node, no staleness signal — when the repo is not git at all. Git write
   doctrine lives entirely in the companion skill.
5. **The board is a verbatim nested Backlog.md instance.** Missions invent nothing board-shaped:
   no wrapper schema, no parallel status model. All CLI board access is cwd-pinned to the
   mission dir, and the passthrough forwards to the real Backlog.md CLI.
6. **The passthrough never falls through.** `mish backlog` executes only when
   `missions/<slug>/backlog/config.yml` exists; a missing or half-scaffolded board refuses.
   (Backlog.md resolves boards by nearest ancestor, so an unguarded miss would silently operate
   on an ancestor board — the verified sharp edge, §4.4.)
7. **Pinned config is invariant.** The five §4.4 keys keep their scaffold values for the
   mission's life. The CLI refuses the paths that would change them — `config` *and*
   `browser` are outside the allowlist, the latter because its web UI's settings endpoint
   rewrites pins on disk (verified on 1.47.1) — and `mish status` warns loudly on drift.
8. **One manifest authority per mission** (Q16). Only the authority edits `mission.md` or
   restructures the board (column/status changes, task moves between states it doesn't own,
   task deletion). Transfer is the authority editing the field. A merge conflict on
   `mission.md` signals either an authority violation *or* the authority's own unsynced
   edits from two nodes (the authority is a label, not a single writer); in both cases the
   current authority resolves the merge — choosing or combining versions — and only
   non-authority content re-enters as a task note.
9. **Non-authority writers have exactly two surfaces:** their assigned tasks (status within the
   task, notes) and disjoint artifact paths. Everything else is propose-only via task notes.
10. **Multi-node writes union by construction.** Task-per-file boards, disjoint artifact paths,
    and the single-authority manifest make concurrent writes from many nodes merge cleanly;
    remaining conflicts have a fixed resolution taxonomy (§7.2) applied by humans/agents per
    the skill, never by CLI machinery.
11. **`status` is read-only.** No invocation of `mish status` mutates the missions repo, any
    board, any marker, or any external system.
12. **Context is resolved, never assumed.** Which mission a command targets comes from the §5
    resolution order (flag → cwd → marker); `$MISSIONS_REPO` locates the repo and carries no
    mission identity. Ambiguity or absence refuses with guidance.

## 4. The mission directory

### 4.1 Layout

```
$MISSIONS_REPO/
  missions/
    <slug>/
      mission.md          # manifest (§4.2) — authority-owned
      backlog/            # verbatim Backlog.md board, pinned config (§4.4)
        config.yml
        tasks/ …          # Backlog.md's own layout, untouched
      artifacts/          # free-form outputs (§4.5)
```

Nothing else is mission-format. Orchestrate's files (`artifacts/journal.md`, playbooks) are
conventions of the orchestrate skill living *inside* `artifacts/`; other skills may establish
their own artifact conventions the same way. The mission format neither defines nor reserves
them (Q14).

### 4.2 `mission.md` — exact format

Markdown with YAML frontmatter. Frontmatter keys are closed (exactly these five; unknown keys
are a lint warning in `mish status`, not an error):

```markdown
---
mission: perf-regression         # the slug; must equal the directory name
authority: hera                  # write authority; label-grade, opaque interpretation (Q16)
owner: riley                     # human attribution; --owner → $SESSION_OWNER → OS user
status: active                   # active | closed
created: 2026-07-08              # yyyy-mm-dd, stamped by `mish new`
---

# Perf regression hunt          ← title: first h1, free text

## Purpose                       ← why this mission exists; the one-paragraph orientation
## Scope                         ← what's in, what's out, which repos/branches it touches
## Decisions                     ← decisions made, tersely, newest last ("decisions already
                                    made" graduates here from run playbooks, Q14)
## Closeout                      ← empty until closeout; disposition summary + harvest record
```

The section headings above are the scaffolded skeleton and the recommended shape; the body is
prose and the authority may restructure it. The frontmatter is normative: `mission` must equal
the dir name (checked by `status`), `status` has exactly two values, and edits to any of it are
authority-only (invariant 8). The two-value status is a lifecycle bit — "is this mission a
going concern" — not a workflow vocabulary: disposition nuance (completed, cancelled-with-why,
parked for a later harvest) is expressed in the Closeout section's prose and the board's final
state, where there is room to actually say it (§8.4).

### 4.3 Slug rules

- Pattern: `^[a-z0-9][a-z0-9-]{0,63}$` — lowercase alphanumerics and hyphens, must start with
  an alphanumeric, max 64 chars. No leading `.` or `_` is possible by construction, keeping
  scaffold names and hidden files unambiguous.
- No trailing hyphen; no consecutive hyphens (`--`) — both refuse at `mish new`.
- Uniqueness = directory existence: `mish new` refuses if `missions/<slug>/` exists. The
  check is per-clone: two unsynced nodes can mint the same slug concurrently — the §8.1
  rhythm (pull before `new`) narrows the window, and a collision that lands anyway resolves
  per §7.2 (rename, no content adjudication).
- Renames are expected over a mission's life but stay out of the CLI (the verb set is closed):
  a rename is an authority act performed with git and hand edits per the documented procedure
  (§8.5), recorded by a `rename` custody commit.

### 4.4 The board

The board is Backlog.md, verbatim (Q11 — strong co-sign). The mission system adds exactly two
things around it: cwd pinning and pinned config.

**Nesting is verified, not assumed.** Verified 2026-07-08 against Backlog.md 1.47.1: the CLI
resolves boards by **nearest-ancestor `backlog/` dir**, so a nested `missions/<slug>/backlog/`
inside a shared repo is fully self-contained — `init` below repo root works, commands run in
mission subdirs resolve to the mission board, the repo's root board (if any) is untouched, and
the dir moves as a unit because `config.yml` travels inside it. Two sharp edges were found and
are neutralized by this spec:

1. `checkActiveBranches` (default true) is broken for nested boards — its cross-branch scan
   resolves task paths repo-root-relative, so `git show` fails and tasks error on hydration.
   Pinned `false`.
2. A board-less directory below repo root silently falls through to the *ancestor* board —
   exactly why every CLI board access is cwd-pinned to the mission dir and guarded by
   invariant 6.

The board supports the **Backlog.md CLI 1.47.x**, pinned to 1.47.1 in this repository; the load-bearing behavioural
assumptions are exactly the two above — nearest-ancestor resolution and the pinned keys'
semantics. Re-run the nesting/pinning acceptance suite (AC-5..7, plus AC-19's references
semantics) whenever the installed Backlog.md version changes: the version floor is trusted
only alongside a passing suite.
Getting the CLI onto a machine is install-tooling business (ai-sync today), not this spec's.

**Pinned config** — stamped into `backlog/config.yml` at scaffold, invariant for the mission's
life (invariant 7):

| Key | Pinned | Why |
|---|---|---|
| `check_active_branches` | `false` | Cross-branch scan broken for nested boards (sharp edge 1) |
| `remote_operations` | `false` | Defaults true; warns about and touches git remotes — the mission CLI's no-git doctrine extends to its delegate |
| `auto_commit` | `false` | No git side effects, ever, from board operations (verified: none observed with this off) |
| `auto_open_browser` | `false` | No surprise browser launches from a shared-repo tool |
| `filesystem_only` | `true` | Backlog's native git-disable switch (observed on 1.47.1) — the tool-side layer under the no-git doctrine, added at ratification |

Additionally `project_name` is set to the slug at scaffold — cosmetic, not pinned. Every other
key keeps Backlog.md's defaults and is **authority-tunable by direct file edit** (statuses,
labels, milestones — restructuring, hence authority-only per invariant 8); the passthrough's
`config` denial (§6.2) closes the casual mutation path, not the deliberate one.

**Board conventions carried by the skill, not the format:** assignee = label-grade name (Q17);
non-authority writers confine themselves to their assigned tasks; external effects (a PR
merged, a deploy) are recorded as notes on the task that produced them (§8.3); cross-references
to where a task's work happened or landed ride the task's native `references` field (§8.3,
§12.9).

### 4.5 `artifacts/`

Free-form. The only rules are conventions taught by the skill: writers keep to **disjoint
paths** (per-agent or per-workstream subdirectories) so multi-node unions stay conflict-free
(invariant 10), and artifacts that matter beyond the mission get harvested out with a custody
commit (§8.2) rather than linked into permanence in place. The CLI never lists, validates, or
interprets artifact contents; `status` reports only counts and recency.

## 5. Context resolution

### 5.1 Locating the repo: `$MISSIONS_REPO`

One environment variable, `MISSIONS_REPO`, holds the absolute path of the shared missions repo
root (D11). It locates the repo and nothing else — it never names a mission. Commands that must
reach the repo from outside it (`mish new`, marker-resolved invocations) refuse with setup
guidance when it is unset. Commands whose cwd is already inside a mission dir self-locate and
do not need it.

### 5.2 The context marker

A file named `.mission` in a working directory. Format: first line is the slug, trailing
newline; further lines are reserved and ignored. Written by `mish new` into the invoking
cwd (§6.1); hand-written or hand-edited freely — it is a pointer, not state. The skill's
hygiene guidance: typically untracked (gitignore or global excludes); committing one to a
branch dedicated to mission work is legitimate.

### 5.3 Resolution order

For any verb needing mission context (`backlog`, `status` in single-mission mode):

1. **Explicit flag:** `--mission <slug>` wins outright.
2. **Cwd inside a mission dir:** walk up from cwd; the nearest ancestor containing
   `mission.md` whose parent chain sits under `missions/` identifies the mission.
3. **The chain marker:** walk up from cwd; a `.mission` file on the ancestor chain supplies
   the slug, resolved against `$MISSIONS_REPO`. **Markers never shadow:** a marker states the
   mission for everything below it, so finding more than one marker on the chain is a refusal
   naming both paths — nested markers are a mistake to repair, not a scoping mechanism.

First hit wins; no blending. Failures refuse loudly: no context found → guidance naming all
three mechanisms; marker names a slug with no dir → "marker points at missing mission
<slug>"; `--mission` naming a missing mission dir → "mission <slug> not found" (mirroring
the marker case); two markers on one ancestor chain → refusal naming both; `$MISSIONS_REPO`
needed but unset → setup guidance. Resolution never scans the repo for candidates and never
guesses.

## 6. Command surface — expected behaviour

Three verbs, nothing else (Q12 as amended by Q15). `new` is the only write verb the CLI owns;
`backlog` delegates writes to Backlog.md inside the pinned sandbox; `status` is read-only.

| Command | Behaviour |
|---|---|
| `mish new <slug> [--title T] [--authority A] [--owner O] [--no-marker]` | Scaffold `missions/<slug>/` (§6.1): manifest, pinned board, empty artifacts; write the context marker into cwd. Refuses on existing slug, invalid slug, unset `$MISSIONS_REPO`, or a conflicting existing marker. |
| `mish backlog [--mission S] <backlog-args…>` | Resolve context (§5.3), guard the board's existence (invariant 6), check the allowlist, then exec the Backlog.md CLI with cwd pinned to the mission dir, forwarding arguments, stdio, and exit code verbatim (§6.2). |
| `mish status [--mission S \| --all]` | Read-only report: single-mission detail when context resolves, repo-wide overview with `--all` or when invoked with no resolvable context from inside the missions repo (§6.3). |

### 6.1 `mish new`

Given a valid, unclaimed slug:

1. Create `missions/<slug>/` under `$MISSIONS_REPO`.
2. Write `mission.md` with the §4.2 skeleton: `mission: <slug>`; `authority:` from
   `--authority`, default the invoking OS username — and never `$SESSION_OWNER`: the human is
   usually not the manifest editor on exactly the nodes where the var is set; `owner:` via the
   **owner chain** — `--owner`, else `$SESSION_OWNER`, else the invoking OS username (right by
   construction on personal machines; self-announcingly wrong on an unprovisioned shared box,
   which is itself the cue to provision); `status: active`; `created:` today; title from
   `--title` (default: the slug, hyphens spaced). On success `new` **echoes the stamped
   authority and owner with the source of each** (flag / env / OS user), so a wrong stamp is
   visible at birth; correcting one later is an ordinary manifest edit (invariant 8).
3. Initialize the nested board with pinned config (§4.4) and `project_name: <slug>`. Whether
   this shells to `backlog init` and rewrites `config.yml`, or writes the files directly, is
   implementation — the contract is that `mish backlog task create` works immediately
   afterwards, the five pins hold, and the mission dir contains nothing beyond the §4.1
   tree (in particular, the `AGENTS.md` nudge file `backlog init` writes into its cwd —
   verified on 1.47.1 — must be removed or suppressed).
4. Create `artifacts/` (empty, with a keep-file so the tree survives file-based sync).
5. Write the context marker `.mission` (content: the slug) into the invoking cwd — the D6
   mechanism. Skipped when cwd is inside the missions repo (self-resolving) or `--no-marker`
   is given. Markers never nest (§5.3): an existing marker anywhere on the cwd→root chain
   naming a *different* slug refuses (remove it, or pass `--no-marker`); one naming the same
   slug makes the write a no-op — context already resolves.

`new` performs no git operations (invariant 4): committing the scaffold is the first custody
commit, made by the caller per the skill (§8).

### 6.2 `mish backlog` — the pinned passthrough

The mission system's whole board interface. Sequence: resolve context (§5.3) → verify
`missions/<slug>/backlog/config.yml` exists, refusing on absence with a "board missing —
scaffold damaged or wrong mission" error rather than risking ancestor fallthrough
(invariant 6) → check the first forwarded argument against the allowlist → exec `backlog`
with cwd = the mission dir, forwarding everything else untouched. Interactive subcommands, stdin/stdout,
and exit codes pass through; the wrapper adds nothing on success.

**Allowlist** — the exposed surface. Anything not listed refuses, naming the allowlist —
including subcommands future Backlog.md versions add, until deliberately added here:

| Subcommand | Note |
|---|---|
| `task` / `tasks`, `draft` | Task CRUD, notes, status, references (`--ref`, §8.3) — the working surface |
| `board` | The kanban render |
| `search`, `overview`, `sequence` | Read-only: index search, project stats, dependency sequences |
| `doc`, `decision` | Backlog's docs/decisions — board-internal, land inside the mission dir |
| `milestone` / `milestones` | Board-internal grouping |
| `cleanup` | Ages Done tasks into the completed folder — restructuring, authority etiquette applies (invariant 8) |

Excluded, with the reasons recorded: `init` (re-init inside a mission is always damage),
`config` (the pins are invariant; deliberate tuning is an authority file-edit, not a CLI
path), `agents` (writes agent-instruction nudge files at board root — litters the shared
repo), `browser` (dropped at ratification: its web UI's settings endpoint rewrites
`config.yml`, pins included — verified on 1.47.1; Q11's valued visual board returns if
Backlog grows a read-only mode), `completion`, `instructions`, `mcp` (no mission use case
yet — the allowlist grows by deliberate addition, never by default).

**Help matches the surface.** Bare `mish backlog` (or `… help`) prints the *wrapper's own*
summary — the allowlist above with one-liners and the exclusion rationale — never Backlog.md's
full help, so what help advertises is exactly what's invocable. Per-subcommand help passes
through: `mish backlog task --help` returns Backlog.md's own help for `task`. A refused
subcommand's error names the allowlist.

### 6.3 `mish status`

Single-mission mode (context resolved):

```
mission: perf-regression         active     authority: hera   owner: riley   created 2026-07-08
board:   3 To Do · 2 In Progress · 7 Done   (12 tasks)
artifacts: 9 files · newest analysis/flamegraph-0708.html (2h ago)
```

plus warnings, each one line, when: any pinned key has drifted (invariant 7); `mission:`
frontmatter disagrees with the dir name; frontmatter carries unknown keys; `status:` holds
any value other than `active`/`closed` (same treatment as unknown keys); the board carries
duplicate task IDs (the silent union breach, §7.2); the mission subtree has uncommitted or
unpushed changes (a **read-only** git query — the M6 read amendment, §12.8 — silently skipped
when the missions repo isn't git or has no configured upstream); the board or `artifacts/` is missing.
Single-mission mode applies the §5.3 existence guard first: `--mission` naming a missing dir
refuses rather than partially rendering. Recency comes from file mtimes — never from git
(invariant 4) — and is therefore **node-local**: clone and pull re-stamp mtimes, so recency
reports when changes reached *this clone*, not when the work happened. How the
board summary is sourced (parsing task files vs invoking a read-only Backlog.md command in the
pinned sandbox) is implementation; the contract is invariant 11: read-only, no side effects.

Overview mode (`--all`, or no resolvable context while cwd is inside the missions repo):

```
SLUG              STATUS  AUTHORITY  OWNER  TASKS todo/doing/done   UPDATED
perf-regression   active  hera       riley  3/2/7                   2h ago
q3-launch         closed  riley      riley  0/0/21                  6d ago
```

One line per mission dir, cheap filesystem scan, closed missions included (they are part of
orientation). The TASKS column reports per-status counts in the board's own configured
status order — `todo/doing/done` above is the default-config illustration, not a fixed
vocabulary (statuses are authority-tunable, §4.4). UPDATED is the node-local recency defined
above: when changes reached this clone. Invoked with no context *outside* the missions repo,
`status` refuses with the §5.3 guidance rather than guessing that an overview was wanted.

### 6.4 Reserved: per-invocation auto-commit (`--commit`) — designed, not in v1

Q12 reserves an opt-in auto-commit marker on write verbs. Proposed design, recorded here so
ratification can accept or strike it as a unit:

- Flag: `--commit [<subject>]`, accepted by `new` and by `backlog` invocations whose forwarded
  subcommand writes. Absent flag = no git, always (invariant 4).
- Behaviour: after the wrapped operation succeeds, stage **exactly `missions/<slug>/`** and
  commit with a §8.2-grammar message (generated subject when none given, e.g.
  `mission(<slug>): new` / `mission(<slug>): board edit`). Never pushes, never pulls.
- Refusals, checked *before* the wrapped operation runs (fail early, not after a half-done
  pair): `$MISSIONS_REPO` is not a git worktree; the mission subtree already has staged
  changes (the commit would sweep work the caller didn't name).
- A failed commit after a successful operation warns and leaves the operation in place —
  the flag is convenience, not a transaction.

Until ratified, the flag does not exist and the CLI's no-git rule has no exceptions.

## 7. Multi-writer, multi-node doctrine

One shared repo, many nodes, many writers is the normal case, not an edge case. The doctrine
that makes it safe is structural, not mechanical: the format is arranged so that honest writers
never collide, and the rare collision has a fixed owner.

### 7.1 Write surfaces

| Surface | Writer | Discipline |
|---|---|---|
| `mission.md` | Authority only | Edits, restructuring, status flips, authority transfer (editing the field) |
| Board structure (columns/statuses, task deletion, cross-status sweeps, `config.yml` tuning) | Authority only | Non-authority writers propose via task notes |
| Assigned tasks (own status, notes, acceptance criteria) | The task's assignee | The normal working surface |
| `artifacts/<disjoint path>` | Any participant | Disjoint paths per writer/workstream; coordinate via the board |

Sync between nodes is plain git — pull, push, merge — performed by agents and humans per the
skill's rhythm (§8.1). The CLI is not involved (invariant 4).

### 7.2 Conflict taxonomy

Unions are the expected outcome: task-per-file boards and disjoint artifact paths merge
trivially. When a conflict does surface, resolution is fixed in advance:

| Conflict on | Meaning | Resolution |
|---|---|---|
| `mission.md` | Authority violation by definition (Q16) | The authority-side version adjudicates and may combine the authority's own unsynced edits; the other writer re-proposes as a task note |
| `backlog/config.yml` | Pinned-config drift or unauthorized tuning | Scaffold pins restored; other keys: authority's version wins |
| A task file | Two writers on one ticket | The task's **assignee's** version wins; the other writer re-proposes via a note on the merged task. When the conflict involves a reassignment or an authority restructuring act, the **authority-side** version adjudicates instead (reassignment and sweeps are restructuring, invariant 8); the displaced assignee's edits re-enter as a note |
| A task file modified on one side, moved or deleted by restructuring on the other | Modify/delete conflict (e.g. `cleanup` aged the task while an edit was in flight) | The move wins; the edit re-enters as a note on the moved/completed task |
| Duplicate task IDs after a union | **Silent — no git conflict:** two nodes each allocated the next sequential ID between syncs; the board lists one task while ID-addressed commands resolve to the other (verified on 1.47.1) | The later-created task is renumbered by its creator per the skill's procedure; `mish status` surfaces the duplicate (§6.3) |
| An artifact path | Disjoint-path convention breached | Accidental collision: either writer renames theirs to a disjoint path and notes the board — no content adjudication |
| A whole mission dir | Same slug minted on two unsynced nodes (§4.3 uniqueness is per-clone) — two missions interleaved under one path | Treated like the artifact-path breach: the later-pushed mission renames per §8.5; no content adjudication |

No merge machinery is mandated or recommended: in particular, **no `merge=union` gitattributes**
— union merges corrupt frontmatter files, and the taxonomy above is a human/agent doctrine, not
a driver. (A merge conflict is the loud signal for every row but one: the duplicate-task-ID
breach unions silently, which is exactly why it gets a `status` detection surface as well as
a taxonomy row.)

## 8. The companion skill

The CLI is deliberately shallow — scaffold, passthrough, report. Everything with judgment in it
is the skill's prose. This section fixes the split so neither side grows into the other: **if
it needs git, custody vocabulary, or etiquette, it's skill; if it needs the pins, the guard, or
resolution, it's CLI.**

**Delivery shape (owner ruling 2026-07-09): the skill is mission's own surface, never a wing
of orchestrate.** Doctrine ships primarily as agent-targeted CLI help — top-level
`mish --help` and per-verb help carry the working prose (the herder precedent, whose skill
retired into CLI help) — with a light companion skill wrapping it. The orchestrate skill
documents only how *orchestrate* interacts with missions (its §8.3 overlay and nothing more);
folding general mission doctrine into orchestrate is a boundary error.

### 8.1 Git rhythm (skill)

The missions repo is synced by ordinary git usage: commit early and often at the mission-subtree
grain, pull before board restructuring or manifest edits (the authority's habit that keeps
invariant 8 conflict-free in practice), pull before `mish new` and before task creation
(narrows the §7.2 slug-collision and duplicate-task-ID windows), push when a unit of work
lands. The CLI never does any
of this. On a repo that isn't git at all, everything still works single-node; the skill simply
has nothing to say.

**Custody identity — suggestion, never canonical.** The owner record lives in the manifest and
the session layer; git is deliberately not the surface for it. Commit authorship legitimately
differs from work ownership (agents commit; humans own), it is repo-scoped rather than
work-scoped, and hosting rewrites mangle authors. Provisioning *should* still set a sane git
identity on shared nodes so custody commits aren't nonsense — per-clone `git config`, or a
conditional include keyed on the missions remote (`[includeIf "hasconfig:remote.*.url:…"]`,
git ≥ 2.36 — follows the repo, not the path) — and the `Mission-Agent:` trailer names the
acting agent. But nothing ever reads git identity back as ground truth.

### 8.2 Custody commits (skill)

Q15 killed the event log; its custody and attribution duties fold into conventioned commit
messages plus board notes. The grammar:

- **Subject:** `mission(<slug>): <verb> <summary>` — e.g.
  `mission(perf-regression): adopt repro script from api worktree napkins`.
- **Verbs** — an open, documented vocabulary (Q13's amendment survives the log it was made
  for): `new` (scaffold), `adopt` (files arrive; summary names the source), `harvest` (results
  copied out; summary names the destination — repo, path, and where useful the landed sha),
  `delete` (disposition of removed material), `rename` (slug change; summary names old and
  new), `close` (closeout commit), plus freeform verbs where none fits. Nothing validates the vocabulary; the skill documents it and agents extend
  it in the open.
- **Trailers**, optional, for the greppable cases: `Mission-Source:`, `Mission-Dest:`,
  `Mission-Agent:` (a label-grade name — same vocabulary as authority and assignee).

Harvest is **copy + custody commit**, never move: the mission stays self-contained and safe to
delete until it is deleted. Adopt is the agent doing file ops (move the napkin contents, fix
references) followed by a custody commit — there is no adopt machinery to invoke (Q15).

### 8.3 Board etiquette (skill)

Assignees are label-grade names; agents work their assigned tasks and only those; proposals to
the authority ride task notes; **external effects** — a PR merged, a deploy, an upstream ticket
filed — are recorded as notes on the task that produced them (the surviving half of Q13's
category 3).

**Cross-references** (§12.9): where a task's work happened or landed — commit, branch, PR,
session, agent — is recorded on the task's native Backlog.md `references` list
(`mish backlog task edit <id> --ref <string>`, repeatable; also at `task create`). Whoever is
driving the task provides them — herder, an agent, a human; missions only give the value a
home. The vocabulary is open, documented, and never validated (the §8.2 posture): suggested
shapes `<repo>@<sha>`, `branch:<repo>#<name>`, `pr:<repo>#<n>`, `session:<label>`,
`agent:<label>` — all label-grade and herder-unaware; richer joins stay herder-side at view
time (Q17). Sharp edge, verified on 1.47.1: `--ref` at edit **replaces** the whole list —
read the task's current references first and re-set the full set. References survive
unrelated edits and render in `task <id> --plain`. A mission's operating skill may layer a **stricter regime** on this floor — e.g.
orchestrate's standing doctrine that the orchestrator, as authority, manages all task state
while workers only report — and such overlays are legitimate and belong to that skill, not to
missions: the §7 surfaces and conflict taxonomy are the guarantee missions themselves make,
not a ceiling on discipline. At kickoff and closedown, movement of items between a project repo's board and the
mission board is performed in prose — read one, write the other, by hand — per D4; no movement
machinery exists.

### 8.4 Closeout checklist (skill)

Closing a mission is the authority: (1) board final states — every task Done or explicitly
noted as dying with the mission; (2) harvest pass — everything with a future gets a §8.2
harvest commit to its permanent home; (3) `mission.md` Closeout section written — disposition
in words (completed, cancelled and why, parked with what's still worth harvesting), harvest
record, pointers outward; (4) frontmatter `status: closed`; (5) custody-rhythm review — the
authority skims `git log -- missions/<slug>/` for custody-grammar coverage, records gaps in
the Closeout section, and notes whether the manual commit rhythm held (this is the evidence
stream the §6.4 reserved design's implementation decision consumes); (6) the `close`
custody commit. The dir then rests in place: greppable, browsable, cheap. Deletion, when it
ever happens, is a git operation recorded by a `delete` custody commit.

### 8.5 Rename procedure (skill)

Renames happen; they are an authority act, not a CLI verb. The procedure: (1) pick the new
slug (§4.3 rules; target dir must not exist); (2) `git mv missions/<old> missions/<new>`;
(3) edit `mission:` frontmatter to the new slug (it must equal the dir name) and
`project_name` in `backlog/config.yml` (cosmetic); (4) fix or remove markers pointing at the
old slug — resolution fails loudly on stale ones (§5.3), so stragglers announce themselves
rather than misdirecting; (5) one `rename` custody commit covering the lot. Self-containment
keeps the blast radius exactly this small: the dir name, two fields, and the markers.

### 8.6 Marker hygiene (skill)

Markers are pointers into working directories: usually untracked (project gitignore or global
excludes), deleted when the worktree's mission involvement ends, committed only on branches
dedicated to mission work. **One marker per directory chain** — markers never nest: switching
a subtree to a different mission means removing or moving the existing marker, never planting
an inner one. A stale marker fails loudly at resolution (§5.3), so the cost of forgetting one
is a clear refusal, not silent misdirection.

## 9. Acceptance scenarios

Normative. Each is a high-level test case; implementation plans map suites onto them.

**Scaffold & format**

- **AC-1 new** — `MISSIONS_REPO` set, `mish new perf-regression --authority hera` run from a
  code worktree: the §4.1 tree exists; `mission.md` carries the five frontmatter keys with
  `status: active` and `owner:` stamped per the §6.1 chain (from `$SESSION_OWNER` when set,
  else the OS user), with authority and owner + the source of each echoed in `new`'s output;
  `backlog/config.yml` carries the five pins + `project_name: perf-regression`; `.mission`
  containing `perf-regression` appears in the invoking cwd; no git command was executed
  anywhere.
- **AC-2 slug rules** — `new` refuses: an existing slug, `Perf_Regression`, `-x`, `a--b`,
  `x-`, a 65-char slug — each with a one-line reason. `mission` frontmatter ≠ dir name is
  reported by `status` as a warning.
- **AC-3 marker safety** — `new` with a `.mission` for a *different* slug anywhere on the
  cwd→root chain refuses (markers never nest); same slug on the chain → no-op; with
  `--no-marker`, or from inside the missions repo, no marker is written.
- **AC-4 board ready** — immediately after `new`, `mish backlog task create "First task"`
  succeeds and the task lands on the mission's board.

**Nesting & pinning (encodes the 2026-07-08 verification)**

- **AC-5 nested isolation** — in a missions repo that is itself a git repo with a *root*
  Backlog.md board: mission-board operations via the passthrough never touch the root board,
  and root-board operations never touch the mission board.
- **AC-6 no fallthrough** — `mish backlog task list` against a mission whose `backlog/` is
  missing or lacks `config.yml` refuses with the board-missing error; it never resolves to an
  ancestor board.
- **AC-7 branch-scan hazard pinned** — with tasks present and multiple git branches active in
  the shared repo, passthrough task listing works without hydration errors
  (`check_active_branches: false` holding).
- **AC-8 moves as a unit** — `git mv missions/a missions/b` (or an rsync of the subtree to
  another clone): after updating markers, `mish backlog` and `mish status` against the
  new location work unchanged — nothing outside the dir needed fixing.

**Context resolution**

- **AC-9 resolution order** — with all three sources present and disagreeing, `--mission` wins;
  absent the flag, cwd-inside-mission-dir wins over a marker higher up; absent both, the
  single chain marker resolves — and two markers on one ancestor chain refuse, naming both
  paths (markers never shadow).
- **AC-10 refusals** — no context anywhere → refusal naming flag, cwd, and marker; marker
  pointing at a missing mission → refusal naming the slug; `$MISSIONS_REPO` unset where needed
  → setup guidance. No command scans for candidate missions.

**Command surface**

- **AC-11 allowlist** — `mish backlog init`, `… config set`, `… agents`, and an
  unknown/future subcommand each refuse, naming the allowlist; `… board` and `… task edit`
  pass through verbatim with Backlog.md's own exit code; bare `mish backlog` (or `… help`)
  prints the wrapper's allowlist summary, while `… task --help` returns Backlog.md's own help.
- **AC-12 status detail** — `mish status` in a resolved context prints the §6.3 block;
  hand-editing a pinned key, breaking the frontmatter/dirname match, or deleting `artifacts/`
  each produce their one-line warning on the next run; nothing is modified (a before/after
  subtree hash is identical); with the missions repo git-backed and the subtree carrying
  uncommitted or unpushed work, the one-line staleness warning appears.
- **AC-13 status overview** — from the missions repo root with no marker, `mish status`
  lists every mission dir one-per-line including closed ones; from an unrelated directory with
  no context it refuses rather than showing the overview.
- **AC-14 no git mutation, no bus, no herder** — a full `new` + passthrough + `status` session
  on a machine with no herder, no hcom, and `$MISSIONS_REPO` pointing at a plain non-git
  directory: everything works; auditing the CLI's process tree shows no git invocation at all
  (on a git-backed repo, the only permitted git use anywhere is `status`'s read-only
  staleness query — never a mutation).

**Multi-writer doctrine**

- **AC-15 clean union** — two clones concurrently: node A edits its assigned task + writes
  `artifacts/a/…`; node B edits a different task + `artifacts/b/…`. Merge completes with no
  conflicts; both boards render the union.
- **AC-16 authority conflict** — both clones edit `mission.md`; the merge conflicts; per §7.2
  the authority's version is taken verbatim and the other writer's change re-enters as a task
  note. (Doctrine scenario: validated as skill prose + a documented walkthrough, not CLI
  behaviour.)
- **AC-17 custody grammar** — the skill's worked examples produce commits whose subjects parse
  as `mission(<slug>): <verb> <summary>` for new/adopt/harvest/rename/close, greppable across
  the repo history by slug and by verb.
- **AC-18 rename** — after the §8.5 procedure (dir moved, `mission:` + `project_name` updated,
  markers fixed, one `rename` custody commit): resolution via the updated markers works;
  `status` shows no frontmatter/dirname warning; board and artifacts are intact; the old slug
  survives only in history. (Doctrine scenario, skill-validated like AC-16.)
- **AC-19 cross-references** — `mish backlog task edit <id> --ref "<repo>@<sha>" --ref
  "session:<label>"` lands a `references:` list in the task file; the values survive
  unrelated edits (a status change, an appended note); `… task <id> --plain` renders them;
  and a later single `--ref` replaces the list (the documented read-then-re-set edge, §8.3).
  Part of the Backlog.md version-change re-verification suite alongside AC-5..7 (§4.4).

## 10. Non-goals (recorded decisions, not omissions)

- **No event log.** `events.jsonl` is killed (Q15, superseding Q13/Q6): custody and attribution
  ride custody commits + board notes; nothing survives, including the "provenance journal"
  remnant.
- **No `log`, `adopt`, `harvest`, `archive`, or `list` verbs.** The verb set is closed at
  three (Q12 + Q15): logging died with the event log; adopt/harvest are file ops + custody
  commits (D8/S8 as re-worded); archive is `status: closed`; orientation lives in `status`'s
  overview mode.
- **No herder anything** (Q17). No guid, seat, run ref, or registry join in any mission file;
  the assignee/authority vocabulary is opaque text. Session-history joins
  (assignee → label → guid → sids → session store) are herder-side, view-time, best-effort —
  outside this spec.
- **No realtime interface.** Watching a mission happens *to* the mission dir: the
  snapshot-overlay feed rides herder's node↔server spoke (Q6/Q7) and is not part of the mission
  CLI or format.
- **No viewer.** A passive mission UI is deferred — a trivial viewer over the git repo when
  wanted (Q3); the format's obligation is only to stay plain-files-browsable.
- **No search or content indexing** — visibility, not retrieval, is the adjacent product
  (boundaries §1); missions add nothing.
- **No orchestrate conventions.** `artifacts/journal.md`, playbooks, and any future
  orchestrate file shapes belong to the orchestrate skill (Q14); this spec defines the
  container only.
- **No board-movement machinery.** Repo-board ↔ mission-board item movement is prose at
  kickoff/closedown (D4).
- **No per-account or per-project missions repos.** One shared repo, env-var located (D11
  reversed → ratified single repo).
- **No auth model.** Access to the missions repo *is* the permission model; git hosting
  handles it. Likewise no identity verification: authority, owner, and assignee are declared
  attribution, never authenticated — and there is no layer to derive a human from (fleet OS
  users are service accounts, tailnet identity is device-grade, herder's model carries no
  human either), which is why the owner is declared, not inferred.

## 11. Decisions embedded in this spec (ratification checklist)

Ratifying this spec ratifies these. Flag any line to reopen it.

| # | Decision | Source | Spec'd as |
|---|---|---|---|
| M1 | Mission dir = `missions/<slug>/{mission.md, backlog/, artifacts/}`, self-contained, moves as a unit | Q11, Q15 | §4.1, invariant 1, AC-8 |
| M2 | Board = verbatim nested Backlog.md; nothing board-shaped invented; nesting behaviour verified (nearest-ancestor resolution; two sharp edges neutralized) | Q11 + 2026-07-08 verification | §4.4, invariants 5–6, AC-5..7 |
| M3 | Pinned config = `check_active_branches`, `remote_operations`, `auto_commit`, `auto_open_browser` (false) + `filesystem_only` (true — Backlog's native git-disable, added at ratification); invariant for the mission's life; drift warned by `status` | Q12 + verification + owner 2026-07-09 | §4.4, invariant 7, AC-12 |
| M4 | Verb set closed at `new` / `backlog` / `status`; `new` is the CLI's only owned write | Q12 as cut by Q15 | §6, §10 |
| M5 | Passthrough = cwd-pinned, board-guarded, **allowlist posture** (`task/tasks, draft, board, search, overview, sequence, doc, decision, milestone/milestones, cleanup`); `browser` dropped at ratification (verified pin-rewrite hazard); future subcommands excluded until deliberately added; top-level help is wrapper-owned so the help surface equals the invocable surface | Q12 + owner rulings 2026-07-09 | §6.2, invariant 6, AC-11 |
| M6 | CLI never **mutates** git (amended at ratification: `status` may make read-only queries for the staleness signal, §12.8); git write doctrine is skill prose; opt-in `--commit` design recorded but reserved out of v1 | Q12 + owner 2026-07-09 | Invariant 4, §6.3, §6.4, §8.1, AC-14 |
| M7 | events.jsonl killed with full cascade (no log verb, custody → commits + board notes) | Q15 | §8.2, §10 |
| M8 | Manifest authority: advisory `authority:` label-grade field, stamped by `new`; transfer = editing the field; conflict on mission.md = violation, authority wins | Q16 | §4.2, invariant 8, §7.2, AC-16 |
| M9 | Mission↔herder contract = board assignee holding an opaque label-grade name; every richer join herder-side at view time; missions herder-unaware, herder may be very mission-aware | Q17 | Invariant 2, §2, §10 |
| M10 | One shared missions repo, located by `$MISSIONS_REPO`; board per mission always; item movement in prose | D11, D4 | §5.1, §10 |
| M11 | Context = marker-file/cwd resolution with explicit-flag override; `.mission` = slug pointer; markers never shadow (one per ancestor chain — two on a chain refuse, `new` won't write beneath a different-slug marker); env carries repo location only, never mission identity | D6 + owner ruling 2026-07-09 | §5, invariant 12, AC-3, AC-9..10 |
| M12 | Multi-node posture: union-by-construction (task-per-file + disjoint artifact paths + single-authority manifest); fixed conflict taxonomy; no merge drivers | Boundaries §4, Q16 | §7, invariant 10, AC-15..16 |
| M13 | Missions strictly opt-in; missionless path costs zero | S2 | Invariant 3, AC-10 |
| M14 | Custody-commit grammar `mission(<slug>): <verb> <summary>` with an open, documented verb vocabulary (new/adopt/harvest/delete/rename/close) and optional trailers | Q13 amendment surviving Q15 | §8.2, AC-17 |
| M15 | Human attribution: `owner:` distinct from `authority:` (owner = human, meant to be read as a person; authority = write authority, opaque interpretation); owner stamped `--owner` → `$SESSION_OWNER` → OS user and echoed with its source at `new`; `SESSION_OWNER` is the one cross-surface env name (shared with herder + session shipping); git identity is a provisioning suggestion, never canonical | Owner rulings 2026-07-09 | §2, §4.2, §6.1, §8.1, AC-1 |
| M16 | Renames are expected but stay out of the CLI: authority skill procedure (git mv + two fields + markers + `rename` custody commit); slug-equals-dirname re-established by the procedure; verb set stays closed. `--commit` ratified as reserved (design of record, not v1). Backlog.md floor: ≥ 1.47 + stated behavioural assumptions, re-verified via AC-5..7 + AC-19 on every version change; CLI presence is install-tooling business. Board tuning beyond the pins is per-mission, authority-owned | Owner rulings 2026-07-09 | §4.3, §4.4, §6.4, §8.5, AC-18 |
| M17 | Skill delivery: mission-owned, agent-targeted CLI help (top-level + per-verb) with a light companion skill — the herder precedent; the orchestrate skill covers only orchestrate's own mission interaction, never general mission doctrine | Owner ruling 2026-07-09 | §8, §8.3 |
| M18 | Cross-references: where a task's work happened or landed rides Backlog.md's native per-task `references` field, written via the passthrough (`--ref`); open documented vocabulary (`<repo>@<sha>`, `branch:`, `pr:`, `session:`, `agent:` — label-grade, opaque); replace-not-append edge documented; verified on 1.47.1 | Owner ruling 2026-07-09 (post-ratification) | §2, §4.4, §8.3, AC-19 |
| M19 | The CLI binary is `mish` (beside `sesh`); the format vocabulary stays *mission*: `missions/`, `mission.md`, `.mission` marker, custody grammar `mission(<slug>):` | Owner ruling 2026-07-09 (post-ratification) | §2, §6, §12.10 |

## 12. Decision record (living tail)

Rulings land here as they happen. This spec is ratified, not frozen: implementation surfaces
differences against this document as it finds them, so dumb requirements can evolve — flag the
divergence, get the ruling, amend the line.

1. **`--commit` reserved design (§6.4):** ~~ratify, strike, or defer?~~ **Resolved 2026-07-09:
   ratified as reserved** — §6.4 stands as the design of record, out of v1; implementation
   waits on the manual commit rhythm demonstrating the need.
2. **Authority default at `new`:** ~~OS username acceptable, or mandatory flag?~~ **Resolved
   with the owner 2026-07-09** as part of the human-attribution design (M15): authority keeps
   the OS-username default (write authority, opaque interpretation; never `$SESSION_OWNER`);
   the human question moved to its own field, `owner:`, stamped `--owner` →
   `$SESSION_OWNER` → OS user and echoed at creation. Node identity (hostname, OS user) and
   human owner are distinct concepts; the single declared gap-cover for service-account
   fleets is `SESSION_OWNER`, one name shared across mission / herder / session-service.
3. **Marker default posture:** ~~default-on or opt-in?~~ **Resolved 2026-07-09: default-on,
   with the no-shadowing amendment** — markers never nest: resolution refuses on two markers
   in one ancestor chain, and `new` refuses to write beneath an existing different-slug
   marker. One marker states the mission for everything below it.
4. **Backlog.md version posture:** ~~minimum, mise-pin, or behavioural assumption only?~~
   **Resolved 2026-07-09 and narrowed by implementation evidence 2026-07-10: supported
   series 1.47.x, repository pin 1.47.1, plus the stated behavioural assumptions** (§4.4).
   Any version change requires the acceptance suite before support is claimed.
5. **Non-pinned board tuning:** ~~confirm, or uniform boards?~~ **Resolved 2026-07-09:
   per-mission** — the authority tunes statuses/labels/milestones by direct `config.yml` edit;
   uniformity is at most a social norm.
6. **Mission rename:** ~~parked with no CLI support — acceptable?~~ **Resolved 2026-07-09:
   renames will happen** — the slug is not permanent. Handled as an authority skill procedure
   (§8.5) with a `rename` custody verb; the verb set stays closed at three.
7. **Overview trigger:** ~~inside-repo default plus `--all`, or `--all` only?~~ **Resolved
   2026-07-09: keep as drafted** (§6.3).
8. **Sync-staleness observability (deferred from the 2026-07-09 doc review, product-lens,
   P1):** "team-visible" has no staleness signal — an unsynced board is indistinguishable
   from a quiet one on every reading node, and `status` is forbidden from noticing (recency
   is mtime-only; invariant 4 bans git reads). The proposed fix reopens M6's read half:
   permit read-only git queries inside `mission status` (never mutations), warn one line when
   the mission subtree has uncommitted or unpushed changes (silently skipped when the repo
   isn't git), and relax AC-14's audit from "no git invocation" to "no git mutation".
   ~~Deferred to the owner: this amends a ratified checklist row.~~ **Resolved 2026-07-09:
   accepted** — invariant 4, §6.3, AC-12, AC-14, and M6 amended accordingly.
9. **Cross-reference metadata (owner requirement 2026-07-09, post-ratification):** backlog
   items had no home for "where was this work done" — no commit hash, branch/repo reference,
   session id, or agent label. **Resolved 2026-07-09:** ride Backlog.md's native per-task
   `references` field, verified on 1.47.1 — written by `--ref` at `task create`/`task edit`,
   survives unrelated edits, rendered by `task <id> --plain`; `edit --ref` replaces the whole
   list (the documented sharp edge). Providers are the mission's users (herder, agents,
   humans) via the existing allowlisted passthrough; missions only store. Vocabulary open and
   label-grade, herder-unaware (Q17 holds). No new verb, no new format surface. M18, §8.3,
   AC-19.
10. **CLI name (owner ruling 2026-07-09, post-ratification):** the binary is `mish`, sitting
    beside `sesh`; verbs `mish new` / `mish backlog` / `mish status`. The format vocabulary
    is unchanged — `missions/`, `mission.md`, the `.mission` marker, custody grammar
    `mission(<slug>):` — CLI identity renamed, file-format identity kept. Earlier §12 entries
    retain their historical `mission <verb>` wording. M19.

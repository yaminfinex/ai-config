# Mission Spec

Status: **DRAFT — awaiting ratification.** This document is the proposed ground truth for
missions: ubiquitous language, domain model, invariants, expected behaviour, high-level design,
and acceptance scenarios. It derives from the ratified boundary grilling record
(`docs/design/2026-07-08-sessions-missions-boundaries.md` §6b, Q11–Q17 — binding) and encodes
the nested-board verification completed 2026-07-08. Where this document and the grilling record
disagree, the record wins until this spec is ratified. Open questions are collected in §12 and
resolved with the owner before ratification.

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
no daemon, no ingestion pipeline, no message bus, no herder. A machine with only the `mission`
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
| **artifact** | Any file under `missions/<slug>/artifacts/` — free-form outputs, analyses, reports. Structure by convention only (disjoint paths per writer); the CLI never interprets contents. |
| **context marker** | A `.mission` file in a working directory (typically a code worktree), pointing at the mission by slug. The D6 mechanism that lets `mission` commands run far from the missions repo. |
| **context resolution** | How a `mission` invocation finds its mission: explicit flag, then cwd-inside-a-mission-dir, then the single marker on the ancestor chain (§5). Markers never shadow one another. Never ambient identity beyond `$MISSIONS_REPO` locating the repo. |
| **passthrough** | `mission backlog …`: the Backlog.md CLI executed with cwd pinned to the mission dir, arguments forwarded verbatim minus a small denylist (§6.2). |
| **denylist** | The Backlog.md subcommands the passthrough refuses because they clash with the mission system: `init`, `config`, `agents` (§6.2). |
| **pinned config** | The four `backlog/config.yml` keys stamped at scaffold and invariant for the mission's life (§4.4): they neutralize Backlog.md's git/remote/browser behaviours inside the shared repo. |
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
   no behaviour change in any repo that has no mission context. A `mission` command without
   resolvable context refuses with guidance; it never creates anything implicitly.
4. **The CLI never runs git.** No verb invokes git — not to commit, not to read history. Git
   doctrine lives entirely in the companion skill; the missions repo works (degraded to
   single-node) even when it is not a git repo at all. The one reserved exception is the
   explicit per-invocation auto-commit flag (§6.4), opt-in and off by default.
5. **The board is a verbatim nested Backlog.md instance.** Missions invent nothing board-shaped:
   no wrapper schema, no parallel status model. All CLI board access is cwd-pinned to the
   mission dir, and the passthrough forwards to the real Backlog.md CLI.
6. **The passthrough never falls through.** `mission backlog` executes only when
   `missions/<slug>/backlog/config.yml` exists; a missing or half-scaffolded board refuses.
   (Backlog.md resolves boards by nearest ancestor, so an unguarded miss would silently operate
   on an ancestor board — the verified sharp edge, §4.4.)
7. **Pinned config is invariant.** The four §4.4 keys keep their scaffold values for the
   mission's life. The CLI refuses the paths that would change them (`config` on the denylist);
   `mission status` warns loudly on drift.
8. **One manifest authority per mission** (Q16). Only the authority edits `mission.md` or
   restructures the board (column/status changes, task moves between states it doesn't own,
   task deletion). Transfer is the authority editing the field. A merge conflict on
   `mission.md` is by definition an authority violation: the authority's version wins, the
   other writer re-proposes via a task note.
9. **Non-authority writers have exactly two surfaces:** their assigned tasks (status within the
   task, notes) and disjoint artifact paths. Everything else is propose-only via task notes.
10. **Multi-node writes union by construction.** Task-per-file boards, disjoint artifact paths,
    and the single-authority manifest make concurrent writes from many nodes merge cleanly;
    remaining conflicts have a fixed resolution taxonomy (§7.2) applied by humans/agents per
    the skill, never by CLI machinery.
11. **`status` is read-only.** No invocation of `mission status` mutates the missions repo, any
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
are a lint warning in `mission status`, not an error):

```markdown
---
mission: perf-regression         # the slug; must equal the directory name
authority: hera                  # write authority; label-grade, opaque interpretation (Q16)
owner: riley                     # human attribution; --owner → $SESSION_OWNER → OS user
status: active                   # active | closed
created: 2026-07-08              # yyyy-mm-dd, stamped by `mission new`
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
authority-only (invariant 8).

### 4.3 Slug rules

- Pattern: `^[a-z0-9][a-z0-9-]{0,63}$` — lowercase alphanumerics and hyphens, must start with
  an alphanumeric, max 64 chars. No leading `.` or `_` is possible by construction, keeping
  scaffold names and hidden files unambiguous.
- No trailing hyphen; no consecutive hyphens (`--`) — both refuse at `mission new`.
- Uniqueness = directory existence: `mission new` refuses if `missions/<slug>/` exists.
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

The board requires the **Backlog.md CLI ≥ 1.47 on PATH**; the load-bearing behavioural
assumptions are exactly the two above — nearest-ancestor resolution and the pinned keys'
semantics. Getting the CLI onto a machine is install-tooling business (ai-sync today), not
this spec's.

**Pinned config** — stamped into `backlog/config.yml` at scaffold, invariant for the mission's
life (invariant 7):

| Key | Pinned | Why |
|---|---|---|
| `check_active_branches` | `false` | Cross-branch scan broken for nested boards (sharp edge 1) |
| `remote_operations` | `false` | Defaults true; warns about and touches git remotes — the mission CLI's no-git doctrine extends to its delegate |
| `auto_commit` | `false` | No git side effects, ever, from board operations (verified: none observed with this off) |
| `auto_open_browser` | `false` | No surprise browser launches from a shared-repo tool |

Additionally `project_name` is set to the slug at scaffold — cosmetic, not pinned. Every other
key keeps Backlog.md's defaults and is **authority-tunable by direct file edit** (statuses,
labels, milestones — restructuring, hence authority-only per invariant 8); the passthrough's
`config` denial (§6.2) closes the casual mutation path, not the deliberate one.

**Board conventions carried by the skill, not the format:** assignee = label-grade name (Q17);
non-authority writers confine themselves to their assigned tasks; external effects (a PR
merged, a deploy) are recorded as notes on the task that produced them (§8.3).

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
reach the repo from outside it (`mission new`, marker-resolved invocations) refuse with setup
guidance when it is unset. Commands whose cwd is already inside a mission dir self-locate and
do not need it.

### 5.2 The context marker

A file named `.mission` in a working directory. Format: first line is the slug, trailing
newline; further lines are reserved and ignored. Written by `mission new` into the invoking
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
<slug>"; two markers on one ancestor chain → refusal naming both; `$MISSIONS_REPO` needed but
unset → setup guidance. Resolution never scans the repo for candidates and never guesses.

## 6. Command surface — expected behaviour

Three verbs, nothing else (Q12 as amended by Q15). `new` is the only write verb the CLI owns;
`backlog` delegates writes to Backlog.md inside the pinned sandbox; `status` is read-only.

| Command | Behaviour |
|---|---|
| `mission new <slug> [--title T] [--authority A] [--no-marker]` | Scaffold `missions/<slug>/` (§6.1): manifest, pinned board, empty artifacts; write the context marker into cwd. Refuses on existing slug, invalid slug, unset `$MISSIONS_REPO`, or a conflicting existing marker. |
| `mission backlog [--mission S] <backlog-args…>` | Resolve context (§5.3), guard the board's existence (invariant 6), check the denylist, then exec the Backlog.md CLI with cwd pinned to the mission dir, forwarding arguments, stdio, and exit code verbatim (§6.2). |
| `mission status [--mission S \| --all]` | Read-only report: single-mission detail when context resolves, repo-wide overview with `--all` or when invoked with no resolvable context from inside the missions repo (§6.3). |

### 6.1 `mission new`

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
   implementation — the contract is that `mission backlog task create` works immediately
   afterwards and the four pins hold.
4. Create `artifacts/` (empty, with a keep-file so the tree survives file-based sync).
5. Write the context marker `.mission` (content: the slug) into the invoking cwd — the D6
   mechanism. Skipped when cwd is inside the missions repo (self-resolving) or `--no-marker`
   is given. Markers never nest (§5.3): an existing marker anywhere on the cwd→root chain
   naming a *different* slug refuses (remove it, or pass `--no-marker`); one naming the same
   slug makes the write a no-op — context already resolves.

`new` performs no git operations (invariant 4): committing the scaffold is the first custody
commit, made by the caller per the skill (§8).

### 6.2 `mission backlog` — the pinned passthrough

The mission system's whole board interface. Sequence: resolve context (§5.3) → verify
`missions/<slug>/backlog/config.yml` exists, refusing on absence with a "board missing —
scaffold damaged or wrong mission" error rather than risking ancestor fallthrough
(invariant 6) → check the first forwarded argument against the denylist → exec `backlog` with
cwd = the mission dir, forwarding everything else untouched. Interactive subcommands,
stdin/stdout, and exit codes pass through; the wrapper adds nothing on success.

**Denylist** (refused with a one-line reason each):

| Subcommand | Why refused |
|---|---|
| `init` | The board exists; re-init inside a mission is always damage |
| `config` | The pinned keys are invariant (7); deliberate tuning is an authority file-edit, not a CLI path |
| `agents` | Writes agent-instruction nudge files at board root — pollutes the shared missions repo with per-mission instruction files |

Everything else — including subcommands added by future Backlog.md versions — passes through:
the posture is *verbatim instance with a denylist*, not an allowlist, because the pins already
neutralize the dangerous defaults (git, remotes, browser) and the board is Backlog.md's to
evolve.

### 6.3 `mission status`

Single-mission mode (context resolved):

```
mission: perf-regression         active     authority: hera   owner: riley   created 2026-07-08
board:   3 To Do · 2 In Progress · 7 Done   (12 tasks)
artifacts: 9 files · newest analysis/flamegraph-0708.html (2h ago)
```

plus warnings, each one line, when: any pinned key has drifted (invariant 7); `mission:`
frontmatter disagrees with the dir name; frontmatter carries unknown keys; the board or
`artifacts/` is missing. Recency comes from file mtimes — never from git (invariant 4). How the
board summary is sourced (parsing task files vs invoking a read-only Backlog.md command in the
pinned sandbox) is implementation; the contract is invariant 11: read-only, no side effects.

Overview mode (`--all`, or no resolvable context while cwd is inside the missions repo):

```
SLUG              STATUS  AUTHORITY  OWNER  TASKS todo/doing/done   UPDATED
perf-regression   active  hera       riley  3/2/7                   2h ago
q3-launch         closed  riley      riley  0/0/21                  6d ago
```

One line per mission dir, cheap filesystem scan, closed missions included (they are part of
orientation). Invoked with no context *outside* the missions repo, `status` refuses with the
§5.3 guidance rather than guessing that an overview was wanted.

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
| `mission.md` | Authority violation by definition (Q16) | Authority's version wins verbatim; the other writer re-proposes as a task note |
| `backlog/config.yml` | Pinned-config drift or unauthorized tuning | Scaffold pins restored; other keys: authority's version wins |
| A task file | Two writers on one ticket | The task's **assignee's** version wins; the other writer re-proposes via a note on the merged task |
| An artifact path | Disjoint-path convention breached | Accidental collision: either writer renames theirs to a disjoint path and notes the board — no content adjudication |

No merge machinery is mandated or recommended: in particular, **no `merge=union` gitattributes**
— union merges corrupt frontmatter files, and the taxonomy above is a human/agent doctrine, not
a driver. (A merge conflict is already the loud signal; the taxonomy just says who wins.)

## 8. The companion skill

The CLI is deliberately shallow — scaffold, passthrough, report. Everything with judgment in it
is the skill's prose. This section fixes the split so neither side grows into the other: **if
it needs git, custody vocabulary, or etiquette, it's skill; if it needs the pins, the guard, or
resolution, it's CLI.**

### 8.1 Git rhythm (skill)

The missions repo is synced by ordinary git usage: commit early and often at the mission-subtree
grain, pull before board restructuring or manifest edits (the authority's habit that keeps
invariant 8 conflict-free in practice), push when a unit of work lands. The CLI never does any
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
category 3). At kickoff and closedown, movement of items between a project repo's board and the
mission board is performed in prose — read one, write the other, by hand — per D4; no movement
machinery exists.

### 8.4 Closeout checklist (skill)

Closing a mission is the authority: (1) board final states — every task Done or explicitly
noted as dying with the mission; (2) harvest pass — everything with a future gets a §8.2
harvest commit to its permanent home; (3) `mission.md` Closeout section written — disposition
summary, harvest record, pointers outward; (4) frontmatter `status: closed`; (5) the `close`
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

- **AC-1 new** — `MISSIONS_REPO` set, `mission new perf-regression --authority hera` run from a
  code worktree: the §4.1 tree exists; `mission.md` carries the five frontmatter keys with
  `status: active` and `owner:` stamped per the §6.1 chain (from `$SESSION_OWNER` when set,
  else the OS user), with authority and owner + the source of each echoed in `new`'s output;
  `backlog/config.yml` carries the four pins + `project_name: perf-regression`; `.mission`
  containing `perf-regression` appears in the invoking cwd; no git command was executed
  anywhere.
- **AC-2 slug rules** — `new` refuses: an existing slug, `Perf_Regression`, `-x`, `a--b`,
  `x-`, a 65-char slug — each with a one-line reason. `mission` frontmatter ≠ dir name is
  reported by `status` as a warning.
- **AC-3 marker safety** — `new` with a `.mission` for a *different* slug anywhere on the
  cwd→root chain refuses (markers never nest); same slug on the chain → no-op; with
  `--no-marker`, or from inside the missions repo, no marker is written.
- **AC-4 board ready** — immediately after `new`, `mission backlog task create "First task"`
  succeeds and the task lands on the mission's board.

**Nesting & pinning (encodes the 2026-07-08 verification)**

- **AC-5 nested isolation** — in a missions repo that is itself a git repo with a *root*
  Backlog.md board: mission-board operations via the passthrough never touch the root board,
  and root-board operations never touch the mission board.
- **AC-6 no fallthrough** — `mission backlog task list` against a mission whose `backlog/` is
  missing or lacks `config.yml` refuses with the board-missing error; it never resolves to an
  ancestor board.
- **AC-7 branch-scan hazard pinned** — with tasks present and multiple git branches active in
  the shared repo, passthrough task listing works without hydration errors
  (`check_active_branches: false` holding).
- **AC-8 moves as a unit** — `git mv missions/a missions/b` (or an rsync of the subtree to
  another clone): after updating markers, `mission backlog` and `mission status` against the
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

- **AC-11 denylist** — `mission backlog init`, `… config set`, `… agents` each refuse with
  their §6.2 reason; `mission backlog board`, `… task edit`, and an unknown future subcommand
  pass through verbatim with Backlog.md's own exit code.
- **AC-12 status detail** — `mission status` in a resolved context prints the §6.3 block;
  hand-editing a pinned key, breaking the frontmatter/dirname match, or deleting `artifacts/`
  each produce their one-line warning on the next run; nothing is modified (a before/after
  subtree hash is identical).
- **AC-13 status overview** — from the missions repo root with no marker, `mission status`
  lists every mission dir one-per-line including closed ones; from an unrelated directory with
  no context it refuses rather than showing the overview.
- **AC-14 no git, no bus, no herder** — a full `new` + passthrough + `status` session on a
  machine with no herder, no hcom, and `$MISSIONS_REPO` pointing at a plain non-git directory:
  everything works; auditing the CLI's process tree shows no git invocation.

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
| M3 | Pinned config = `check_active_branches`, `remote_operations`, `auto_commit`, `auto_open_browser`, all false; invariant for the mission's life; drift warned by `status` | Q12 + verification | §4.4, invariant 7, AC-12 |
| M4 | Verb set closed at `new` / `backlog` / `status`; `new` is the CLI's only owned write | Q12 as cut by Q15 | §6, §10 |
| M5 | Passthrough = cwd-pinned, board-guarded, denylist `{init, config, agents}`, otherwise verbatim (denylist posture, not allowlist) | Q12 | §6.2, invariant 6, AC-11 |
| M6 | CLI never runs git; git doctrine is skill prose; opt-in `--commit` design recorded but reserved out of v1 | Q12 | Invariant 4, §6.4, §8.1, AC-14 |
| M7 | events.jsonl killed with full cascade (no log verb, custody → commits + board notes) | Q15 | §8.2, §10 |
| M8 | Manifest authority: advisory `authority:` label-grade field, stamped by `new`; transfer = editing the field; conflict on mission.md = violation, authority wins | Q16 | §4.2, invariant 8, §7.2, AC-16 |
| M9 | Mission↔herder contract = board assignee holding an opaque label-grade name; every richer join herder-side at view time; missions herder-unaware, herder may be very mission-aware | Q17 | Invariant 2, §2, §10 |
| M10 | One shared missions repo, located by `$MISSIONS_REPO`; board per mission always; item movement in prose | D11, D4 | §5.1, §10 |
| M11 | Context = marker-file/cwd resolution with explicit-flag override; `.mission` = slug pointer; markers never shadow (one per ancestor chain — two on a chain refuse, `new` won't write beneath a different-slug marker); env carries repo location only, never mission identity | D6 + owner ruling 2026-07-09 | §5, invariant 12, AC-3, AC-9..10 |
| M12 | Multi-node posture: union-by-construction (task-per-file + disjoint artifact paths + single-authority manifest); fixed conflict taxonomy; no merge drivers | Boundaries §4, Q16 | §7, invariant 10, AC-15..16 |
| M13 | Missions strictly opt-in; missionless path costs zero | S2 | Invariant 3, AC-10 |
| M14 | Custody-commit grammar `mission(<slug>): <verb> <summary>` with an open, documented verb vocabulary (new/adopt/harvest/delete/close) and optional trailers | Q13 amendment surviving Q15 | §8.2, AC-17 |
| M15 | Human attribution: `owner:` distinct from `authority:` (owner = human, meant to be read as a person; authority = write authority, opaque interpretation); owner stamped `--owner` → `$SESSION_OWNER` → OS user and echoed with its source at `new`; `SESSION_OWNER` is the one cross-surface env name (shared with herder + session shipping); git identity is a provisioning suggestion, never canonical | Owner rulings 2026-07-09 | §2, §4.2, §6.1, §8.1, AC-1 |
| M16 | Renames are expected but stay out of the CLI: authority skill procedure (git mv + two fields + markers + `rename` custody commit); slug-equals-dirname re-established by the procedure; verb set stays closed. `--commit` ratified as reserved (design of record, not v1). Backlog.md floor: ≥ 1.47 + stated behavioural assumptions; CLI presence is install-tooling business. Board tuning beyond the pins is per-mission, authority-owned | Owner rulings 2026-07-09 | §4.3, §4.4, §6.4, §8.5, AC-18 |

## 12. Open questions (living tail — resolve with the owner before ratification)

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
   **Resolved 2026-07-09: minimum version (≥ 1.47) + the stated behavioural assumptions**
   (§4.4). Getting the CLI onto a machine is install-tooling business (ai-sync today);
   portability beyond that is dealt with later.
5. **Non-pinned board tuning:** ~~confirm, or uniform boards?~~ **Resolved 2026-07-09:
   per-mission** — the authority tunes statuses/labels/milestones by direct `config.yml` edit;
   uniformity is at most a social norm.
6. **Mission rename:** ~~parked with no CLI support — acceptable?~~ **Resolved 2026-07-09:
   renames will happen** — the slug is not permanent. Handled as an authority skill procedure
   (§8.5) with a `rename` custody verb; the verb set stays closed at three.
7. **Overview trigger:** ~~inside-repo default plus `--all`, or `--all` only?~~ **Resolved
   2026-07-09: keep as drafted** (§6.3).

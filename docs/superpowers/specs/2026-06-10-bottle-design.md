# bottle — pin, name, and re-enter agent contexts

**Date:** 2026-06-10
**Status:** Approved design, pre-implementation
**Home:** `ai-config` repo — Go module at `tools/bottle/`, thin `bin/bottle` shim, plus a stub `bottling` skill

## Problem

Working across coding harnesses (Claude Code, Codex), a session sometimes reaches a
state worth keeping: an agent that deeply understands a subsystem, a debugging
context painstakingly built up, a design discussion at its richest point. Today that
state is trapped in the harness's own session store — unnamed, subject to GC, and
re-enterable only as a one-off fork (`herder-fork`). The want: **pin that agent's
context, give it a name, and talk to *that specific agent* again, as many times as
desired, from exactly that point.**

## Concept model

A **bottle** is an immutable snapshot of an agent conversation at a chosen point: a
frozen transcript plus metadata. Decanting (re-entering) never mutates the bottle —
every decant spawns a fresh conversation seeded from the frozen context.

**Names carry versions.** `auth-expert` resolves to the latest version;
`auth-expert@2` is a permanent reference to a specific frozen snapshot. Rebottling
under an existing name bumps the version; rebottling under a new name is allowed and
still records lineage.

**Provenance is explicit.** Every bottle's `meta.json` records:

- `source` — original session id, harness, cwd, git branch + sha at bottle time,
  the turn it was cut at, total turns in the source, and a free-text note.
- `parent` — when created by rebottling a decanted conversation: the parent bottle
  id **and** the decant session id between them. Absent for root bottles.

**Freeze scope: transcript + metadata only.** Repo state (branch/sha) is recorded
for display and provenance, not restored. Decants run against the repo as it is
today; the agent may notice drift, which is acceptable — the point is its
accumulated understanding, not a filesystem time machine. (A pinned-worktree decant
is a possible future flag, deliberately out of v1.)

## Storage

All directories under `~/.bottles/` are created mode `0700` (matching
`~/.claude`'s posture — transcripts can contain keys, PII, and proprietary code).

```
~/.bottles/                      # a git repo (see "Git substrate")
  registry.json                  # names → versions → bottle ids; decants map
  store/<bottle-id>/
    transcript.jsonl             # frozen copy, truncated at the chosen turn
    meta.json                    # name, version, source, parent, created, note
    artifacts/                   # optional attached files (see "Artifacts")
```

- Bottle ids are short random ids (8-char base36), not content hashes.
- Bottles are **copies** — they survive Claude's session cleanup and compaction.
- **Full copies, no pointers.** Every bottle stores a complete transcript, and every
  decant materializes a complete session file (the harness requires a
  self-contained JSONL to resume). No pointer/delta scheme between bottles —
  physical dedup of near-duplicate version chains is delegated to git packfiles.
  Claude's own intra-file `parentUuid` tree is preserved verbatim (only `sessionId`
  is rewritten). Only the main session file is copied; subagent transcripts
  (`agent-<id>.jsonl`) are not needed to resume — their results are embedded as
  tool_results in the main chain.
- **Bottles are immutable after create** (one sanctioned exception: the free-text
  `note` field in `meta.json`, editable via `bottle note` — display metadata, never
  provenance); `registry.json` is the only other mutable file.
  The `store` package accesses the backend through a narrow interface (read, write,
  list, atomic-swap) with local-fs as the v1 implementation — a future remote
  backend can be a mounted filesystem reusing the same code path (requires atomic
  `rename()` on the mount) or a second store implementation speaking REST/git.
- `registry.json` maps `name → [{version, bottle_id}]` plus a `decants` map of
  `session_id → bottle_id` (how `rebottle` auto-resolves parents).
- Registry writes are atomic: write temp file, `mv` over. Decants can race.

### Git substrate

`~/.bottles` is initialized as a git repository (lazily, on first mutation), and the
tool auto-commits after every mutating command (`create`, `rm`, decant registry
updates). Rationale:

- **Sharing later = add a remote.** The future "central store" phase collapses to
  `git push`/`git pull` against any git-speaking remote — GitHub, Cloudflare
  Artifacts, code.storage — plus a registry merge strategy. No new service needed.
- **Delta compression fits the data.** A rebottled `@3` transcript is `@2` plus
  appended lines; git packs the chain as tiny deltas.
- **Free history and integrity** for the registry.

**`rm` and git history — stated plainly:** `bottle rm` removes a bottle from the
registry and the live store but does NOT remove it from git history; a transcript
containing a leaked credential survives `rm` and would ride along on any future
push. The `rm` confirmation and its `--help` both print this warning, and the
docs describe the `git filter-repo` procedure for genuinely expunging a bottle.

What git does **not** give us, stated to keep the boundary honest: the provenance
model (`meta.json` owns that), concurrency safety (atomic writes still required), or
query capability. It is purely substrate — roughly ten lines of "commit after
mutation".

Boundary, stated deliberately: git is a **dumb substrate, not the data model**.
`meta.json` is the source of truth for provenance; bottles are NOT modeled as git
commits/refs (rejected: couples everything to plumbing, makes `rm` ref-surgery,
breaks without git). If git is unavailable, every command still works — the tool
warns once and skips the commit step (best-effort). Initialization follows the
same rule: if git is missing at first mutation, the store initializes as a plain
directory and the git layer activates lazily on a later mutation once git exists
(untracked prior state gets swept into that first commit). Auto-commit is also
configurable per store location: on a network-mounted or remotely-versioned store
(e.g. Cloudflare Artifacts, which is itself git), the local git layer is redundant
and should be off.

## Implementation: Go

Go module at `tools/bottle/` (first Go in this repo). Rationale: the core is JSONL
surgery, version-ref resolution, and registry management — real data structures and
unit tests pay off — and the later TUI is Bubble Tea/lipgloss sharing the same
internal packages. v1 has zero TUI dependencies: the rewind picker is a plain
numbered list on stdout with a read prompt (the non-interactive `--at <N>` form
covers scripted use). Bubble Tea arrives with the TUI phase, where it earns its
keep across many interactions.

Distribution: no committed binaries. `bin/bottle` is a thin bash shim that builds
the module (`go build`) into a per-machine cache (e.g. `~/.cache/bottle/`) on first
run or when sources change, then execs it. Toolchain bootstrap is step 0 of
implementation: this machine has no Go but does have mise, so the shim resolves
`go` via PATH and falls back to `mise x go@latest -- go build …`, erroring with
the one-line install instruction (`mise use -g go`) only when both are absent.

Internal package boundaries (each independently testable):

- `transcript` — parse/truncate/rewrite-session-id over Claude JSONL.
- `store` — bottle dirs, meta.json, registry (atomic writes), git substrate.
- `refs` — `name[@version]` parsing and resolution.
- `harness/claude` — session discovery, materialization, launch. (Codex later is a
  sibling package.)
- `cli` — cobra-style command wiring + agent-first help text.

## CLI surface (`bin/bottle`)

| Command | Behavior |
|---|---|
| `bottle create <name> [--session ID \| --last] [--at] [--note "…"] [--attach PATH...]` | Snapshot a session into a new bottle. Defaults: `--session` falls back to `$CLAUDE_CODE_SESSION_ID` when inside a Claude session, else `--last` (most recent session for the cwd). Name exists → version bumps. `--at` opens the rewind picker. |
| `bottle decant <name>[@v] [--pane right\|below] [--prompt "…"] [--yolo] [--cwd PATH]` | Materialize a fresh session from the bottle and resume it — interactive in the current terminal by default, or into a herdr split via `herder spawn` with `--pane`. Records the decant in the registry. `--yolo` ⇒ `--dangerously-skip-permissions`. |
| `bottle rebottle [<name>] [--session ID] [--note "…"]` | Sugar for `create`: resolves the current (or given) session in the `decants` map, sets `parent` automatically. Same name (default: parent's name) bumps version; a new name starts a new named lineage with parent recorded. Errors clearly if the session isn't a known decant (fall back to plain `create`). |
| `bottle list` | Table: name, latest version, version count, age, note. |
| `bottle log <name>` | Version chain with provenance, e.g. `@3 ← decant of @2 (session 9f2c…), 2026-06-10`. Follows `parent` links across renames. |
| `bottle show <name>[@v]` | Full metadata + the last 5 transcript turns as a preview (`--turns N` to adjust). |
| `bottle artifacts <name>[@v] [--extract DIR]` | List attached artifacts; `--extract` copies them out (defaults to `./bottle-artifacts/<name>@<v>/`, never overwrites silently). |
| `bottle rename <old> <new>` | Registry-only move of all versions to the new name (refuses if `<new>` exists); old name recorded in `meta.json` so `log` lineage stays continuous. No bottle files move. |
| `bottle note <name>[@v] "…"` | Set or replace the free-text note after creation. Notes are explicitly mutable display metadata — the one sanctioned exception to bottle immutability. |
| `bottle prune` | Drop `decants`-map entries whose session files no longer exist (see Decant lifecycle). |
| `bottle rm <name>[@v]` | Delete one version, or the whole name without `@v` (confirm). `@v` references held as parents elsewhere are *not* protected — `log` shows `(deleted)` for missing parents. Prints the git-history retention warning (see Git substrate). |

### Rewind picker (`--at`)

Lists the session's user turns, numbered, with timestamp and the first ~80 chars of
each message; the user picks a turn number. A "user turn" is a `type: user` tree
node carrying human text — not tool_result carriers, not `isCompactSummary`
entries, not operational lines. The bottle keeps everything up to and including
the **assistant response that completed** that turn; the cut then applies the
truncation rule from "The entry tree" section below (tree walk + trailer repair).
Entries from in-flight subagents after the cut go with it. Non-interactive form:
`--at <turn-number>`.

## Decant mechanics (the load-bearing trick)

1. Copy the bottle's `transcript.jsonl`.
2. Generate a fresh session UUID; rewrite the `sessionId` field on every line.
3. Write to `~/.claude/projects/<encoded-cwd>/<new-uuid>.jsonl` (cwd from the
   bottle's metadata unless `--cwd` overrides; refuse with guidance if that
   directory no longer exists).
4. `chdir` to the resolved cwd, then `exec claude --resume <new-uuid>` (or hand the
   command to `herder spawn` for `--pane`, mirroring `herder fork`'s
   `--from-pane`/`--cwd` anchoring). The chdir is mandatory — verified empirically:
   `claude --resume` scopes session lookup to the project directory derived from
   the process cwd and fails with "No conversation found" from anywhere else.
5. Record the new session UUID from step 2 (it *is* the `decants`-map key) →
   bottle-id in the registry *before* launching.

No `--fork-session` is needed — the materialized copy already is a fork.
(Spike #1 empirically passed during design review: a real session copied with
`sessionId` rewritten on every line resumed via `claude --resume` with full
context recall, on Claude Code 2.1.170.)

**Decant lifecycle.** Decant seeds are ordinary session files: they appear in
Claude's resume picker and are subject to its normal session GC — the bottle is
the durable artifact, the seeds are disposable. The registry's `decants` map
records a timestamp per entry, and `bottle prune` drops entries whose session
files no longer exist (so `log` stays honest about dead decants).

### The entry tree, compaction, and lineage edge cases

Claude's JSONL contains three classes of entry, verified against real sessions
(an entry-type census of one live file: 77 assistant, 57 user, 52 `last-prompt`,
52 `mode`, 52 `permission-mode`, 18 `queue-operation`, 9 `file-history-snapshot`,
10 genuine branch points):

- **Tree nodes** — `user`/`assistant`/`system` entries with `uuid`/`parentUuid`
  (in-session rewinds are branches). Compaction is an explicit in-band operation —
  a `{type: system, subtype: compact_boundary}` entry with `parentUuid: null`
  (physical chain restart) carrying `logicalParentUuid` back to the pre-compaction
  leaf, followed by a `{type: user, isCompactSummary: true}` summary entry. Full
  pre-compaction history remains in the file.
- **Stateful trailers** — `last-prompt` (carries a `leafUuid` pointer, likely what
  resume uses for leaf selection), `mode`, `permission-mode`. No uuid of their own.
- **Operational lines** — `queue-operation` (queued user messages),
  `file-history-snapshot` (keyed by `messageId`, not uuid), `ai-title`.

What Claude does NOT track is cross-file lineage (forks/decants are unlinked
copies) — the bottle registry and `meta.json` are precisely that missing journal.

Consequences, all representable rather than breaking:

- **Rebottling a compacted decant works.** The copied file is self-consistent
  (history + boundary + summary + new turns); decanting it rebuilds context exactly
  like a native resume of a compacted session. The bottled agent is the compacted
  one — an honest snapshot.
- **Metadata captures the edge cases.** `meta.json` records `inherited_lines` (the
  parent transcript length at decant) and compaction annotations. From these,
  `bottle log` flags: `compacted` (and whether a compact boundary's
  `logicalParentUuid` reaches into inherited context, i.e. compaction swallowed
  context from before this lineage started) and `rewound-into-parent` (cut at a
  turn ≤ the inherited prefix, making the bottle effectively a prefix of its
  parent — provenance stays truthful via cut-turn recording).
- **Truncation walks the tree AND repairs the trailers.** The rewind picker and
  cut follow `parentUuid`, hop `logicalParentUuid` at compact boundaries, and then
  fix every non-tree line type: drop stateful trailers and operational lines after
  the cut, drop `file-history-snapshot` entries whose `messageId` is past the cut,
  drop `queue-operation` lines whose messages were cut, drop `ai-title`/summary
  lines pointing past the cut, and rewrite the final `last-prompt`'s `leafUuid` to
  the new leaf. The `transcript` package test fixtures must contain every observed
  entry type. (Open question for spike #1: confirm what resume actually uses for
  leaf selection — `last-prompt.leafUuid` vs the last tree entry — since it
  dictates exactly which trailers must be rewritten vs dropped.)

**Spike #1 of implementation (do first):** verify Claude resumes a foreign-written
session file. Existing herder blob-delivery work suggests yes. Fallback if rejected:
materialize, then immediately `claude --resume <id> --fork-session` a native copy
and discard the seed.

## Help is product surface (agent-first)

`bottle` with no args, `bottle --help`, and every subcommand's `--help` emit terse,
**agent-tuned** usage: what the tool is for, when to reach for it, examples, and the
one or two pitfalls that matter (e.g. "a self-bottle cuts at the last completed
turn — the in-flight turn is trimmed"). Written like a minimal SKILL.md — token-budgeted, agents first,
humans second. Concretely:

- No-arg `bottle` = the lightweight skill: concept model in ~5 lines, the command
  table, and "run `bottle <cmd> --help` for details". Target well under ~60 lines.
- Subcommand help = usage + 2–3 realistic examples + pitfalls, nothing else.
- Help text lives next to the command implementations and is covered by golden-file
  tests so it can't rot silently.

This makes the CLI self-describing for any harness's agent (codex included) with no
installed skill required.

## In-session capture (`bottling` skill stub)

Because help is the real skill, the installed `bottling` skill is a stub: trigger
phrases ("bottle this as X", "rebottle this", "decant X into a pane") plus the
instruction to run `bottle --help` and use `$CLAUDE_CODE_SESSION_ID` as the session
id.

**Self-bottle cut rule (correctness, not just a caveat).** Assistant entries
containing tool_use blocks are persisted at dispatch time, *before* their
tool_results arrive — so a naive copy of the caller's own live session ends with
the agent's not-yet-resolved `bottle create` call, and every decant would wake the
agent mid-action (possibly re-running the bottling, silently bumping the version).
When the target session is the caller's own (`--session` == the live session id),
`create` defaults the cut to the end of the last *completed* turn: trim the
trailing user message and any assistant entries whose tool_use ids lack matching
tool_results. The caveat stated in the stub and CLI help: a self-bottle captures
up to the last completed turn — the in-flight turn is trimmed.

## Artifacts (opt-in, deliberately dumb)

The transcript already contains the full content of files the agent wrote (inside
`Write`/`Edit` tool_use blocks), so attached artifacts are never required for the
agent's knowledge — they exist for *the user's* convenience.

- `--attach <path>` on `create` copies named files into `store/<id>/artifacts/`,
  recording cwd-relative paths. Explicit paths only in v1. The attach path prints
  each resolved absolute path before copying and refuses sensitive-looking names
  (`.env*`, `*secret*`, `*credential*`, `id_rsa*`, `*.pem`) without `--force` —
  attached files enter the git substrate and inherit its permanent retention.
- `bottle artifacts` lists/extracts. Extraction is decoupled from `decant` so
  decanting never touches the working tree.
- Future (not v1): `--attach auto` — parse the transcript for `Write`/`Edit`
  targets and offer a checklist.

## Cross-harness and future phases

- **v1: Claude Code only.** `meta.json` carries `harness: "claude"`; nothing else in
  the format is Claude-specific.
- **Codex later:** a second materializer (rollout-file format + `codex resume`).
  Registry, naming, provenance, git substrate all unchanged.
- **Central store later:** push/pull the `~/.bottles` git repo to a shared remote
  (GitHub / Cloudflare Artifacts / code.storage); add a registry merge strategy.
  **Blocking design requirement for that phase:** a mandatory secret-scan gate
  (gitleaks/trufflehog-style) before any push, plus explicit acknowledgment that
  transcripts may contain credentials — transcripts routinely capture API keys,
  `.env` reads, and env dumps, and this repo's own rule is "do not sync sessions/
  histories". Pushing is NOT a trivial `git push` add-on.
- **One-shot ask later:** `bottle ask <name> "question"` via `claude -p` against an
  ephemeral decant. Explicitly out of v1.
- **TUI later:** Bubble Tea app over the same internal packages — browse bottles,
  walk provenance chains, preview transcripts, decant from a list.

## Error handling

- **Missing session / GC'd source:** `create` validates the JSONL exists and is
  parseable before writing anything.
- **Compacted sessions:** detect compaction/summary markers and warn at `create`
  time; the bottle still freezes the compacted state (honest snapshot).
- **Names:** `[a-z0-9][a-z0-9-]*`, no `@` (reserved for version refs). Collisions
  bump versions; nothing is ever overwritten.
- **Decant target cwd missing:** refuse with the recorded path and suggest `--cwd`.
- **Registry races:** atomic temp-file + `mv` writes; git commit failures degrade to
  a warning.

## Testing

Go unit tests over fixture JSONL transcripts (no live harness needed):

- `transcript`: truncation correctness at chosen turns, session-id rewriting,
  malformed-line handling.
- `refs` + `store`: version bumping, name validation, rebottle parent resolution
  via decants map, `log` chain rendering, renames, deleted parents.
- `harness/claude`: session discovery, project-dir encoding, materialization
  (session-id rewrite + trailer repair) against fixtures; launch wiring covered by
  the spike's manual smoke test.
- registry: atomicity under concurrent mutation, git auto-commit, git-absent
  degradation.
- artifacts: attach/list/extract, overwrite refusal, sensitive-name refusal.
- help: golden-file tests for top-level and subcommand help output.

`claude --resume` integration (spike #1) gets a manual smoke-test script, since it
needs a real harness session.

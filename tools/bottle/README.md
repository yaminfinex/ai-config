# bottle — pin, name, and re-enter agent contexts

A **bottle** is an immutable snapshot of an agent conversation at a chosen
point: the frozen transcript plus its provenance. Bottle exists for the moment
a session reaches a state worth keeping — an agent that has spent hours
building context on a subsystem, a debugging trail, a design conversation —
and you want to talk to *that specific agent* again, from *that exact point*,
as many times as you like. Harnesses trap this state in unnamed, per-cwd,
garbage-collected session stores; bottle gives it a name, a version, and a
durable home.

```sh
bottle create auth-expert        # freeze the current session
bottle decant auth-expert        # open a fresh conversation seeded from it
bottle rebottle                  # freeze the decant back, as auth-expert@2
```

The CLI's own help is the primary reference and is golden-tested against the
code: `bottle` with no args prints an agent-tuned concept model and command
table, and `bottle <cmd> --help` gives usage, examples, and pitfalls per
command. This README is the guide *around* that surface — the mental model,
the mechanics, and the design reasoning that isn't visible from help text or
code.

## Mental model

Deliberately git-shaped:

- A bottle is like a **commit**: an immutable object. Its transcript and
  metadata never change after creation (two sanctioned exceptions: the
  free-text note via `bottle note`, and the name via `bottle rename`).
- A name is like a **tag over a version sequence**: `auth-expert` resolves to
  the latest version, `auth-expert@2` is a permanent pinned ref. Creating
  under an existing name bumps the version; nothing is overwritten.
- **Decanting never advances the bottle.** Each decant copies the frozen
  transcript into a brand-new session, so every decant starts from the
  identical context. A living "named thread" would destroy this property —
  the second conversation would never meet the same agent the first one did.
  Immutability is what makes repeatable consultation true.
- **Rebottling records lineage.** A rebottled version stores its parent
  bottle *and* the decant session that sat between them, so `bottle log`
  shows the full provenance chain. Rebottle under the parent's name to bump
  its version, or under a new name to start a new lineage (parent still
  recorded). Claude itself only tracks ancestry *within* one session file;
  the registry is the cross-file lineage journal it doesn't keep.

Intended workflows:

- **Expert pinning** — bottle a context-rich session as `auth-expert`; decant
  it for each new question.
- **Iterating on a context** — decant, teach the agent more, rebottle as
  `@2`. The chain becomes a curated, versioned knowledge artifact.
- **Rewind-then-pin** — the conversation was great through turn 12, then went
  off the rails: `bottle create good-state --at 12` freezes the good prefix
  retroactively (bare `--at` opens an interactive turn picker).

## How it works

**Storage.** Everything lives under `~/.bottles` (mode 0700 throughout —
transcripts can contain keys, PII, and proprietary code):

```
~/.bottles/
  registry.json          # names → [{version, bottle_id}], plus the decants map
  config.json            # optional; {"git_auto_commit": false} disables git
  store/<id>/
    transcript.jsonl     # the frozen transcript (verbatim Claude JSONL)
    meta.json            # provenance: source session, cwd, git branch/sha,
                         # cut point, parent link, honesty annotations
    artifacts/...        # files attached with --attach
  .git/                  # auto-created; every mutation auto-commits
```

The store root is a git repository and every mutation auto-commits. This
gives the store history/undo (the registry, accidental `rm`), delta
compression across versions (`@3` is `@2` plus appended lines), and a sharing
path later (add a remote, push/pull). Provenance lives in `meta.json` and
resolution in the registry — not in git; with git absent everything still
works, minus history (a one-time warning is printed).

**Create.** `bottle create` resolves a source session (explicit `--session`,
the live session via `$CLAUDE_CODE_SESSION_ID`, or `--last` for the newest
session in this cwd), cuts it (see below), and freezes the result with its
provenance: source session id, cwd, git branch/sha at bottle time, and the
cut point.

**Decant.** `bottle decant` copies the frozen transcript under a **fresh
session id** into Claude's own session store
(`~/.claude/projects/<encoded-cwd>/<new-id>.jsonl`) and runs
`claude --resume <new-id>` from the bottle's recorded cwd. The copy *is* the
fork: Claude sees an ordinary independent session, the bottle is untouched,
and the seed file is disposable (the bottle is the durable thing). Each
decant is recorded in the registry's decants map — that record is how
`rebottle` later knows which bottle a session descends from.

**Cuts.** Three ways a snapshot is cut from its source:

- *whole* — a non-live source freezes as-is;
- *rewind* (`--at`) — freeze through an earlier turn's completed response;
- *self-trim* — bottling your own live session cuts at the last *completed*
  turn, dropping the in-flight turn that holds the running `bottle create`
  call. Otherwise every decant would wake mid-action.

Bottling a compacted session freezes the compacted agent — an honest
snapshot, but pre-compaction nuance is gone; `create` warns, and the bottle
carries annotations (`compacted`, `rewound-into-parent`, …) that `log` and
`show` surface as bracketed tags.

## What bottle is NOT, and sharp edges

- **Not a time machine for your repo.** Only the transcript freezes. Decants
  run against the working tree *as it is today*; the recorded git branch/sha
  are provenance metadata, never restored. The decanted agent's memory of
  files may be stale.
- **`bottle rm` does not erase history.** The transcript leaves the registry
  and live store but survives in `~/.bottles` git history until you rewrite
  it (e.g. `git filter-repo`). The same applies to attachments — which is why
  `--attach` refuses sensitive-looking filenames (`.env*`, `*secret*`,
  `*credential*`, `id_rsa*`, `*.pem`) without `--force`.
- **Transcripts contain secrets.** Treat `~/.bottles` like `~/.claude`. Never
  push it anywhere without a secret-scan gate.
- **Decant seeds are disposable.** They land in Claude's resume picker and
  are subject to its GC; the bottle is the durable thing. `bottle prune`
  drops decants-map entries whose seed files are gone.
- **v1 is Claude-only.** Any agent can `list`/`show`/`log` and decant into a
  pane, but self-bottling needs a Claude session id
  (`$CLAUDE_CODE_SESSION_ID`); from another harness pass `--session ID` or
  `--last` to `create`.
- **Sidecar state is not copied.** Per-session sidecars (e.g.
  `file-history/<sid>/`) don't follow the transcript, so rewind-with-
  file-restore inside a decant is silently broken.

## Layout and development

```
cmd/bottle/              # main: arg dispatch only
internal/cli/            # command surface; deps-injected, golden-tested help
internal/refs/           # name[@version] parsing (pure; no store knowledge)
internal/store/          # registry, meta, backend, git substrate, ops
internal/transcript/     # JSONL surgery: index, turns, truncate, rewrite, lint
internal/harness/claude/ # everything Claude-specific: discovery, encoding,
                         # materialize, launch (builds argv; cli execs it)
```

```sh
cd tools/bottle
go test ./...                  # pure unit tests; no live harness, no network
scripts/smoke-decant.sh        # live-harness smoke (costs a few small API calls)
```

There is no install step: the repo's `bin/bottle` is a wrapper that hashes the
module sources, rebuilds the binary into `~/.cache/bottle/` when they change,
and reuses the cached build otherwise.

Commands take an injected `deps` struct (store root, clock, TTY-ness, session
locator, launcher, git reader), so tests drive a temp store and assert on
buffers without touching `~/.bottles` or spawning anything. The no-arg help
is a deliberate product surface — a ~60-line agent-facing skill with a
golden-tested line budget; the installed `bottling` skill is intentionally a
thin stub pointing at it. If you change behavior, change the help in the same
commit and let the golden tests arbitrate.

## Internals

Deep details for working on bottle itself; nothing here is needed to use it.

### Store mechanics

Bottle ids are 8 random base36 chars (not content hashes). Mutations are
serialized by a flock and persisted with atomic temp-file-plus-rename writes,
so concurrent `bottle` invocations are safe; registry reads take no lock (the
atomic swap guarantees a coherent file). The store runs on a narrow
five-method `Backend` interface (read/write/list/atomic-swap/delete) so a
remote backend can slot in later; v1 is the local filesystem. Bottles were
deliberately *not* modeled as git commits/refs — that couples everything to
plumbing and turns `rm` into ref surgery; git stays underneath as plain
auto-commits over ordinary files.

### Transcript surgery

Claude Code session files are JSONL with an undocumented schema. The
`transcript` package classifies each line into a small taxonomy:

- **tree nodes** (`user`, `assistant`, `system`, `attachment`, plus any
  unknown type carrying a `uuid`) — form the conversation tree via
  `parentUuid`; compact boundaries hop via `logicalParentUuid`;
- **stateful trailers** (`last-prompt`, `mode`, `permission-mode`);
- **operational lines** (`queue-operation`, `file-history-snapshot`,
  `ai-title`, `summary`).

"Turns" are the human prompts on the *live branch* (the `parentUuid` chain
walked back from the last tree entry) — tool results, meta/sidechain entries,
and slash-command echoes don't count. Truncation cuts a temporal prefix at a
turn's completing assistant response and repairs the file: trailers and
operational lines whose uuid references point past the cut are dropped,
compact-boundary + summary blocks are kept atomic, and the final
`last-prompt` trailer is rewritten to the new tail (hygiene, not
load-bearing — resume follows the last tree entry). A linter verifies no
dangling uuid references survive any cut. The self-bottle trim additionally
refuses to cut at an assistant entry with an unresolved `tool_use`, stepping
back to the last fully-settled turn.

### Decant mechanics

Materialization streams the frozen transcript through a sessionId rewrite
that replaces only the top-level `sessionId` value on each line, preserving
every other byte (key order, spacing, unknown fields); the `parentUuid`
topology is by construction never modified. Why a full copy under a fresh id
rather than resuming or referencing the original?

1. Resuming the original session would mutate the snapshot.
2. The harness requires it anyway: `--resume` only loads self-contained files
   from its own per-cwd store.
3. Survival: pointers back into Claude's store die to exactly the GC the
   bottle exists to escape.
4. Rewind needs file surgery; you can't truncate a pointer.

No delta scheme between versions, by design — versions are logical full
copies, and physical dedup is delegated to git packfiles.

The launch order in `decant` is load-bearing: validate the run cwd *before*
materializing (so a dead cwd never leaves an orphan seed), materialize (temp
file, linted, atomically renamed), record the decant in the registry, then
chdir and exec. **The chdir is mandatory**, not incidental: `claude --resume`
is cwd-scoped, and resuming the right id from the wrong directory fails.
`--pane right|below` launches into a herdr split via `herder-spawn` instead
of exec-ing in place; permission semantics are kept identical across both
paths (safe by default, `--yolo` for `--dangerously-skip-permissions`).

The project-dir encoding (every non-alphanumeric byte → `-`) mirrors Claude's
own scheme. It is lossy and must never be inverted — always derive the
encoded name from a known cwd.

### Honesty annotations

Rebottled bottles record `inherited_lines` (the parent transcript length at
decant time) and derive `rewound_into_parent` (the cut sits at or before the
inherited prefix — the bottle is effectively a prefix of its parent) and
`compaction_reaches_inherited` (a compact boundary's logical parent points
back into inherited context, i.e. compaction swallowed history from before
this lineage started). These flags exist so weird lineages are
*representable* rather than errors — a design stance, not bookkeeping.

### Harness assumptions and drift

The materialization trick rides on Claude Code's undocumented JSONL shape and
three empirically-derived behaviors (pinned to Claude Code 2.1.170):

1. resume continues from the **last tree entry** — the `last-prompt`
   trailer's `leafUuid` is ignored and tolerated stale;
2. a dangling `tool_use` tail is branched around gracefully, not a hard
   failure;
3. cuts between/at compact boundaries resume coherently.

`testdata/README.md` records the experiments and the sanitized fixture
corpus; `scripts/smoke-decant.sh` re-runs both acceptance smoke tests
(foreign-written/truncated/boundary-cut/dangling files still resume) and
characterisation tests (the findings above still hold) against the live
harness — re-run it after Claude Code version bumps. A characterisation FAIL
means harness drift, not necessarily breakage: re-derive the finding and
update the contracts.

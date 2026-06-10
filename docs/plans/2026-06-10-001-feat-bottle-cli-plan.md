---
title: "feat: bottle — pin, name, and re-enter agent contexts"
type: feat
status: active
date: 2026-06-10
origin: docs/superpowers/specs/2026-06-10-bottle-design.md
---

# feat: bottle — pin, name, and re-enter agent contexts

## Summary

Build the `bottle` CLI as a Go module at `tools/bottle/` with a `bin/bottle` bash
shim, implementing the bottling design spec: immutable named-and-versioned
snapshots of Claude Code sessions, decantable back into live conversations, with
explicit provenance, a git-substrate store, and agent-first help. Work proceeds
spike-first (the remaining session-format unknowns gate the transcript surgery),
then inner packages, then commands, then the help/skill surface.

---

## Problem Frame

Valuable agent contexts are trapped in Claude's session store — unnamed, GC'd, and
re-enterable only as one-off forks. The origin spec defines the product (bottle /
decant / rebottle with provenance); it has been multi-agent reviewed with 15
findings already folded in, and the core mechanic was empirically de-risked: a
foreign-written session file with rewritten `sessionId`s resumed successfully via
`claude --resume` (CC 2.1.170). This plan turns that spec into ordered,
implementable units.

---

## Requirements

- R1. `bottle create` snapshots a session into an immutable bottle: by id, by
  `--last`, from inside the live session (self-bottle trims the in-flight turn),
  and at an earlier point via the `--at` rewind picker.
- R2. `bottle decant` materializes a fresh session from a bottle and resumes it —
  interactively (chdir to the bottle's cwd, then exec) or into a herdr pane via
  `herder-spawn`.
- R3. Names carry versions (`name`, `name@v`); rebottling bumps versions and
  records parent provenance via the registry's `decants` map; `log` renders the
  chain including compaction/rewind flags.
- R4. Management commands work as specced: `list`, `log`, `show`, `rename`,
  `note`, `prune`, `rm`, `artifacts`.
- R5. Help is the product surface: no-arg `bottle` emits a ~60-line agent-tuned
  skill; every subcommand `--help` gives usage + examples + pitfalls; golden-file
  tested.
- R6. `~/.bottles` is a lazily-initialized git repo with best-effort auto-commit,
  graceful git-absent degradation, and per-store configurability.
- R7. Security posture: 0700 store dirs, `rm` prints the git-history retention
  warning, `--attach` refuses sensitive-looking filenames without `--force`, the
  central-store secret-scan gate is documented as a blocking future requirement.
- R8. Distribution: no committed binaries; `bin/bottle` shim builds via Go from
  PATH or mise fallback, caching per machine.

---

## Scope Boundaries

- Claude Code only — codex materializer, central store sync, `bottle ask`, the
  Bubble Tea TUI, pinned-worktree decants, and `--attach auto` are future phases
  (carried from origin).
- No pointer/delta storage scheme — full transcript copies; dedup is git's job.
- No sidecar-state materialization (`file-history/`, `session-env/`, `tasks/`):
  rewind-with-file-restore inside a decant is documented as unsupported, not built.
- No secret scanning in v1 (the store never leaves the machine in v1); the gate is
  specified for the central-store phase only.

### Deferred to Follow-Up Work

- Codex harness package (`harness/codex`): future iteration, same registry/refs.
- Registry merge strategy for shared remotes: central-store phase.

---

## Context & Research

### Relevant Code and Patterns

- `skills/herder/scripts/herder-spawn` — pane spawning; note it takes
  `--split right|down` (not `--pane right|below`) and **defaults to
  skip-permissions** (`--safe` opts out); `herder-fork` shows cwd-anchoring.
- `bin/ai-doctor`, `lib/common.sh` — repo bash idiom for the `bin/bottle` shim
  (`#!/usr/bin/env bash`, `set -euo pipefail`, sourcing `lib/common.sh`).
- `skills/*/SKILL.md` — skill stub format; `ai-setup` links `skills/*/SKILL.md`
  into home skill roots automatically.
- Session-format facts verified this session: `~/.claude/projects/<encoded-cwd>/`
  (encoding: non-alphanumerics → `-`, lossy — always derive, never invert);
  entry classes = tree nodes (`uuid`/`parentUuid`), stateful trailers
  (`last-prompt` w/ `leafUuid`, `mode`, `permission-mode`), operational lines
  (`queue-operation`, `file-history-snapshot` keyed by `messageId`, `ai-title`);
  compaction = `compact_boundary` (`parentUuid: null` + `logicalParentUuid`) then
  `isCompactSummary` user entry; `$CLAUDE_CODE_SESSION_ID` set in live sessions;
  `claude --resume` is cwd-scoped; foreign-written resumed OK on CC 2.1.170.

### Institutional Learnings

- No `docs/solutions/` in this repo yet.
- Repo rules (ai-config skill): no auto-commit without user ask; no absolute home
  paths in portable files (`$HOME` etc.); `bash -n` changed shell scripts; run
  `bin/ai-doctor` after changes; publish via `bin/ai-push`.

### External References

- None needed — stack is well-known (Go stdlib + cobra-style CLI), all
  harness-format questions are answered empirically or assigned to the spike.

---

## Key Technical Decisions

- Spike-first sequencing: U1 resolves the three remaining format unknowns (leaf
  selection, dangling tool_use resume behavior, multi-compact truncation) before
  the transcript package hardens its truncation contract.
- Five internal packages per origin: `transcript`, `store`, `refs`,
  `harness/claude`, `cli`. `refs` owns pure `name[@v]` parsing; resolution against
  the registry lives in `store` (refs stays dependency-free) — settles the
  boundary question from the spec review.
- `rm` confirmation: interactive y/N prompt when stdin is a TTY, `--force` flag
  for agents/scripts — agents cannot answer prompts and they are first-class
  users (per the agent-first help requirement).
- `decant --pane` passes `--safe` to herder-spawn unless `--yolo` is given, so
  permission semantics are identical across interactive and pane decants
  (herder-spawn alone would default to skip-permissions).
- v1 has zero TUI dependencies: rewind picker is numbered stdout + read prompt
  (origin decision; Bubble Tea arrives with the TUI phase).
- Registry mutations take a `flock` on a lockfile alongside the atomic
  temp-file+rename, closing the concurrent-decant lost-update gap flagged in
  review.

---

## Open Questions

### Resolved During Planning

- `refs` vs `store` boundary: refs = pure parsing; store = resolution (above).
- `rm` confirmation mechanism: TTY prompt + `--force` (above).
- `--pane` permission semantics: `--safe` by default, `--yolo` opts in (above).

### Deferred to Implementation

- What resume uses for leaf selection (`last-prompt.leafUuid` vs last tree
  entry): U1 spike answers it; dictates trailer rewrite rules in U3.
- Whether resume tolerates a transcript ending in a dangling tool_use: U1 spike;
  determines how aggressive the self-bottle trim must be in U6.
- Exact mise invocation in the shim (`mise x go@latest` vs pinned version):
  decided when writing the shim against the live machine.

---

## Output Structure

    bin/bottle                          # bash shim: build-and-exec via go/mise
    tools/bottle/
      go.mod
      cmd/bottle/main.go
      internal/
        transcript/                     # parse, classify, truncate, rewrite
        store/                          # bottle dirs, meta, registry, git substrate
        refs/                           # name[@version] parsing
        harness/claude/                 # discovery, encoding, materialize, launch
        cli/                            # command wiring + agent-first help
      testdata/                         # fixture JSONL corpus (U1)
      scripts/smoke-decant.sh           # manual live-harness smoke test (U1)
    skills/bottling/SKILL.md            # trigger-phrase stub → `bottle --help`
    docs/plans/2026-06-10-001-feat-bottle-cli-plan.md

---

## Implementation Units

### U1. Spike: fixture corpus + live-resume verification

**Goal:** Answer the three deferred format questions and harvest a fixture corpus
covering every observed entry type, so U3's truncation contract is built on
evidence rather than assumption.

**Requirements:** R1, R2 (de-risks both)

**Dependencies:** None

**Files:**
- Create: `tools/bottle/testdata/` (sanitized fixture JSONLs: plain session,
  branched session, compacted session, multi-compact session, session with
  queued messages, session ending in dangling tool_use)
- Create: `tools/bottle/scripts/smoke-decant.sh`

**Approach:**
- Copy + sanitize real sessions from the local store into fixtures (strip
  content, keep structure). If no multi-compact session exists locally, force one
  in a throwaway session.
- Smoke script: copy fixture → rewrite ids → write to a temp project dir →
  `claude --resume` → assert context recall. Run variants: truncated file,
  truncated-at-compact-boundary, dangling tool_use tail.
- Record answers (leaf selection mechanism, dangling-tool_use behavior) as
  comments in the fixtures dir README for U3/U6 to consume.

**Test scenarios:**
- Test expectation: none — this unit *produces* fixtures and a manual smoke
  script; its output is recorded findings, not shipped behavior.

**Verification:**
- Each deferred question has a recorded answer; fixture corpus contains all entry
  classes; smoke script passes on a foreign-written truncated session (or the
  fork-session fallback path is documented as required).

---

### U2. Module scaffold + bin/bottle shim

**Goal:** Compilable Go module with command skeleton, plus the shim that makes
`bottle` runnable on any machine in this repo's style.

**Requirements:** R8

**Dependencies:** None (parallel with U1)

**Files:**
- Create: `tools/bottle/go.mod`, `tools/bottle/cmd/bottle/main.go`,
  `tools/bottle/internal/cli/` (root command + subcommand stubs)
- Create: `bin/bottle`
- Test: `tools/bottle/internal/cli/cli_test.go`

**Approach:**
- Shim follows `bin/ai-doctor` idiom; resolves `go` from PATH, falls back to
  `mise x go -- go build`, errors with a one-line `mise use -g go` instruction
  when both absent. Builds into `~/.cache/bottle/` keyed on source hash, then
  execs. No absolute home paths in the script (`$HOME` only).
- Root command renders the agent-skill help on no args (content lands in U8;
  scaffold wires the mechanism).

**Patterns to follow:**
- `bin/ai-doctor` + `lib/common.sh` for the shim; standard cobra-style wiring
  (or stdlib flag if cobra feels heavy — implementer's call, the spec names
  "cobra-style" as shape, not a dependency mandate).

**Test scenarios:**
- Happy path: `bottle` with no args exits 0 and prints the skill-help skeleton.
- Happy path: unknown subcommand exits non-zero with a one-line hint.
- Edge case: shim rebuilds when a source file changes, reuses cache otherwise
  (covered by a shellcheck/bash -n pass + manual check; shim logic itself stays
  trivial enough to read).

**Verification:**
- `bin/bottle` runs from a clean checkout on this machine (no Go preinstalled,
  mise present) and prints help.

---

### U3. `transcript` package: parse, classify, truncate, rewrite

**Goal:** All JSONL surgery: entry classification, tree walk, turn enumeration,
truncation with trailer repair, session-id rewrite.

**Requirements:** R1, R2, R3

**Dependencies:** U1 (fixture corpus + leaf-selection answer), U2

**Files:**
- Create: `tools/bottle/internal/transcript/` (parser, classifier, truncate,
  rewrite)
- Test: `tools/bottle/internal/transcript/transcript_test.go` (against
  `testdata/` fixtures)

**Approach:**
- Three entry classes per origin spec: tree nodes, stateful trailers, operational
  lines. Unknown entry types pass through untouched on copy and are dropped after
  the cut (forward-compatible default).
- "User turn" = `type: user` tree node with human text (not tool_result carriers,
  not `isCompactSummary`).
- Truncation: walk `parentUuid`, hop `logicalParentUuid` at compact boundaries;
  drop trailers/operational lines past the cut; drop `file-history-snapshot` by
  `messageId`, `queue-operation` for cut messages, dangling `ai-title`/summary
  refs; rewrite final `last-prompt.leafUuid` per U1's answer.
- Rewrite: new UUID into `sessionId` on every line; `parentUuid` tree preserved
  verbatim.
- Streaming line-by-line processing (multi-MB transcripts; no whole-file DOM).

**Execution note:** Fixture-driven test-first — the truncation contract is the
riskiest logic in the project and the fixtures from U1 exist precisely to pin it.

**Test scenarios:**
- Happy path: truncate at turn N keeps through the completing assistant response
  and nothing after; resulting file parses clean.
- Happy path: session-id rewrite touches every line's `sessionId` and nothing else.
- Edge case: cut exactly at a compact boundary keeps boundary + summary intact.
- Edge case: multi-compact session truncated between boundaries stays coherent.
- Edge case: turn enumeration on a branched session lists only the live branch's
  turns (leaf-path enumeration).
- Edge case: file with queued messages — `queue-operation` lines for cut messages
  are dropped.
- Error path: malformed JSONL line → clear error naming the line number, no
  partial output written.
- Error path: empty file / file with only header lines → "no turns" error.
- Integration: truncated fixture passes the U1 smoke script against a live
  harness (manual, documented in the fixtures README).

**Verification:**
- All fixtures round-trip: parse → truncate → write → re-parse with zero dangling
  uuid references (an explicit lint the tests run on every output).

---

### U4. `refs` + `store` packages: registry, meta, git substrate

**Goal:** Durable storage: bottle dirs, `meta.json`, `registry.json` with locking
and atomic writes, name/version resolution, git substrate.

**Requirements:** R3, R6, R7 (0700)

**Dependencies:** U2

**Files:**
- Create: `tools/bottle/internal/refs/`, `tools/bottle/internal/store/`
- Test: `tools/bottle/internal/refs/refs_test.go`,
  `tools/bottle/internal/store/store_test.go`

**Approach:**
- `refs`: pure parse of `name[@version]`; name regex `[a-z0-9][a-z0-9-]*`, `@`
  reserved. No registry knowledge.
- `store`: narrow backend interface (read, write, list, atomic-swap), local-fs
  impl; all dirs 0700; registry mutations = flock + temp-file + rename; bottle
  ids 8-char base36 random; `meta.json` fields per origin (source, parent,
  inherited_lines, compaction annotations, note); decants map entries carry
  timestamps.
- Git substrate: lazy `git init` on first mutation (plain-dir fallback when git
  absent, warn once, later mutation sweeps untracked state into first commit);
  auto-commit after mutations; configurable off per store location.

**Test scenarios:**
- Happy path: create→resolve `name` (latest) and `name@2` (pinned); version bump
  on same-name create; new-name lineage records parent.
- Happy path: rename moves all versions, refuses existing target, log lineage
  survives.
- Edge case: registry with deleted parent — `log` renders `(deleted)`.
- Edge case: invalid names (`Foo`, `-x`, `a@b`) rejected with the regex in the
  error message.
- Error path: two concurrent mutations (goroutine test) — flock serializes; no
  lost updates.
- Error path: git absent — every operation succeeds, exactly one warning.
- Integration: store dir created from scratch ends up 0700 throughout; git repo
  state matches registry state after a create/rm sequence.

**Verification:**
- `store` tests pass with and without git on PATH (the git-absent suite runs with
  a scrubbed PATH).

---

### U5. `harness/claude` package: discovery, materialize, launch

**Goal:** Everything Claude-specific: find sessions, encode project dirs,
materialize decant seeds, build launch commands.

**Requirements:** R1, R2

**Dependencies:** U2, U3

**Files:**
- Create: `tools/bottle/internal/harness/claude/`
- Test: `tools/bottle/internal/harness/claude/claude_test.go`

**Approach:**
- Discovery: `$CLAUDE_CODE_SESSION_ID` for self; `--last` = newest mtime JSONL in
  the encoded project dir for a cwd; `--last` prints the chosen session's age and
  first user message before proceeding (review finding: concurrent same-cwd
  sessions make silent `--last` dangerous).
- Encoding: derive from cwd string (non-alphanumerics → `-`); never invert.
- Materialize: transcript rewrite (U3) → `mkdir -p` the encoded project dir →
  write seed file.
- Launch: returns argv + mandatory run-cwd (bottle's recorded cwd or `--cwd`);
  interactive path chdirs then execs `claude --resume <id>`; pane path shells to
  `herder-spawn` mapping `--pane right|below` → `--split right|down`, passing
  `--safe` unless `--yolo`.
- Compaction/GC detection for create-time warnings (compact markers present;
  source file missing → clear error before any write).

**Test scenarios:**
- Happy path: encoding matches the observed on-disk scheme for this repo's paths.
- Happy path: materialize writes seed under the right encoded dir with fresh id.
- Edge case: encoded project dir doesn't exist yet → created 0700.
- Edge case: `--last` with two candidate sessions → newest chosen, preview
  printed (assert on output).
- Error path: bottle cwd no longer exists → refusal naming the recorded path and
  suggesting `--cwd`.
- Error path: source session GC'd → error before anything is written.
- Integration: launch argv for pane mode includes `--safe` by default and
  `--dangerously-skip-permissions` only under `--yolo` (assert argv, not a live
  spawn; live spawn is U1's smoke script).

**Verification:**
- Unit tests green; a manual decant of a real bottle on this machine lands in a
  live session with context recall (smoke script extended to call the binary).

---

### U6. Core commands: `create`, `decant`, `rebottle`

**Goal:** The product's three load-bearing verbs, wired end-to-end.

**Requirements:** R1, R2, R3, R7

**Dependencies:** U1 (dangling-tool_use answer), U3, U4, U5

**Files:**
- Create: `tools/bottle/internal/cli/create.go`, `decant.go`, `rebottle.go`
- Test: `tools/bottle/internal/cli/create_test.go`, `decant_test.go`,
  `rebottle_test.go`

**Approach:**
- `create`: session default chain (explicit → `$CLAUDE_CODE_SESSION_ID` →
  `--last`); self-bottle trims to last completed turn (drop trailing user msg +
  assistant entries with unmatched tool_use ids — severity informed by U1);
  `--at` numbered-stdout picker + non-interactive `--at N`; `--attach` prints
  resolved paths, refuses sensitive patterns (`.env*`, `*secret*`,
  `*credential*`, `id_rsa*`, `*.pem`) without `--force`; compaction warning.
- `decant`: resolve ref → materialize → record decants-map entry (timestamped,
  pre-launch) → launch (interactive chdir+exec, or pane).
- `rebottle`: decants-map lookup → parent set → same-name version bump or
  new-name lineage; clear error + plain-create fallback hint when the session
  isn't a known decant.

**Test scenarios:**
- Happy path: create from fixture → bottle dir + meta + registry entry correct;
  decant materializes seed + registry decant entry; rebottle from that decant id
  bumps version with parent set. (End-to-end over a temp store + temp project
  dir, no live harness.)
- Happy path (Covers spec acceptance: provenance): `log` after
  create→decant→rebottle shows `@2 ← decant of @1 (session …)`.
- Edge case: self-bottle fixture ending in dangling tool_use → trimmed cut; meta
  records the cut turn.
- Edge case: `--at` on rewound/branched fixture cuts on the live branch.
- Edge case: rebottle with `--session` of a non-decant → exits with the
  documented error text.
- Edge case: create on compacted fixture → warning emitted, bottle still made;
  meta carries compaction annotation; rewind-into-parent flag set when cut ≤
  inherited prefix.
- Error path: `--attach .env` without `--force` refuses; with `--force` attaches
  and prints path.
- Error path: name collision never overwrites — same name always bumps.

**Verification:**
- The spec's core promise works on this machine end-to-end: bottle a real
  session, decant it twice, talk to both, rebottle one — `log` tells the true
  story.

---

### U7. Management commands: `list`, `log`, `show`, `rename`, `note`, `prune`, `rm`, `artifacts`

**Goal:** The full management surface over the store.

**Requirements:** R4, R7

**Dependencies:** U4 (U6 not required — these read/mutate store state only)

**Files:**
- Create: `tools/bottle/internal/cli/` (one file per command)
- Test: corresponding `_test.go` files

**Approach:**
- `list`: table of name, latest version, version count, age, note.
- `log`: provenance chain across renames; `compacted` / `rewound-into-parent` /
  `(deleted)` flags from meta.
- `show`: metadata + last 5 turns preview (`--turns N`).
- `rename` / `note`: registry-only move; meta note rewrite (the sanctioned
  mutability exception).
- `prune`: drop decants-map entries whose session files are gone; report count.
- `rm`: TTY y/N prompt or `--force`; prints git-history retention warning; whole
  name vs `@v`.
- `artifacts`: list + `--extract DIR` (default `./bottle-artifacts/<name>@<v>/`,
  never overwrite).

**Test scenarios:**
- Happy path per command against a seeded temp store (golden-ish table output
  for `list`/`log`).
- Edge case: `rm name@2` where `@2` is another bottle's parent → allowed; `log`
  shows `(deleted)`.
- Edge case: `prune` with mixed live/dead decant entries removes only dead ones.
- Error path: `rm` without `--force` on non-TTY stdin → refuses with hint (agents
  must pass `--force`).
- Error path: `artifacts --extract` onto existing files → refuses, names the
  collision.

**Verification:**
- Every command's behavior matches its spec table row; `bottle list` on an empty
  store says so politely instead of erroring.

---

### U8. Agent-first help, golden tests, `bottling` skill stub, repo wiring

**Goal:** Make the CLI self-describing for agents and integrate with the repo's
skill/link machinery.

**Requirements:** R5

**Dependencies:** U6, U7 (help documents final behavior)

**Files:**
- Create: help text alongside commands in `tools/bottle/internal/cli/`
- Create: `tools/bottle/internal/cli/help_golden_test.go` +
  `tools/bottle/internal/cli/testdata/golden/`
- Create: `skills/bottling/SKILL.md`
- Modify: `README.md` (one-line mention under Commands)

**Approach:**
- No-arg `bottle` = the lightweight skill: ~5-line concept model, command table,
  "run `bottle <cmd> --help`" — target under 60 lines.
- Subcommand help = usage + 2–3 examples + pitfalls only (self-bottle trim rule;
  rm/git-history retention; v1 is Claude-only — non-Claude agents can inspect
  and pane-decant but not self-bottle, per the review's codex-overselling
  finding).
- Skill stub: trigger phrases ("bottle this as X", "rebottle this", "decant X
  into a pane") + "run `bottle --help`" + `$CLAUDE_CODE_SESSION_ID` pointer.
  `ai-setup` links it automatically; verify with `bin/ai-doctor`.

**Test scenarios:**
- Golden: no-arg output and each subcommand `--help` match goldens; a line-count
  assertion keeps no-arg help under budget.
- Happy path: every command named in the no-arg table actually exists (help
  can't drift from wiring).

**Verification:**
- A fresh agent given only "this machine has `bottle`" can bottle, decant, and
  rebottle using help output alone (manual check); `bin/ai-doctor` reports the
  new skill linked cleanly.

---

## System-Wide Impact

- **Interaction graph:** Writes into Claude's own store (`~/.claude/projects/`)
  — seeds appear in the resume picker and are subject to Claude's GC (by design,
  documented). Shells out to `herder-spawn` (pane mode) and `git` (substrate).
  `ai-setup` picks up `skills/bottling/` automatically.
- **Error propagation:** All harness-facing failures (missing cwd, GC'd source,
  unresumable seed) surface as single-line errors with a suggested flag; nothing
  is written before validation passes (create validates source first; decant
  records the registry entry only after a successful materialize, before launch).
- **State lifecycle risks:** Registry is the single mutable contention point —
  flock + atomic rename; decant seeds are disposable by contract; bottle store
  is immutable-after-create except `note`.
- **API surface parity:** CLI flags are a contract for agents (help is product);
  goldens pin them. `--pane` permission semantics deliberately mirror
  interactive (`--safe` default) even though herder-spawn's own default differs.
- **Integration coverage:** Live-harness behavior (resume of foreign/truncated
  files) is covered by U1's smoke script, not unit tests — rerun it when Claude
  Code major versions change.
- **Unchanged invariants:** No existing repo tooling is modified; `bin/` gains
  one shim; no secrets or session data are ever committed to ai-config (bottles
  live in `~/.bottles`, outside the repo).

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Claude changes the project-dir encoding or JSONL schema | Encoding derived (never inverted); unknown entry types pass through; smoke script re-run on harness upgrades; `version` field recorded in meta for forensics |
| Resume rejects truncated/foreign files in some future version | Fallback specced (materialize + `--fork-session` native copy); if activated, capture the fork's real session id for the decants map (review finding) |
| herder-spawn flag/permission drift | Pane integration isolated in `harness/claude` launch builder; argv asserted in tests |
| Registry corruption under concurrency | flock + atomic rename + git history as recovery net |
| Go toolchain missing on a new machine | Shim's mise fallback + explicit one-line instruction; documented as implementation step 0 |

---

## Documentation / Operational Notes

- README gets a one-line `bin/bottle` mention; everything else is in-tool help
  (deliberate — help is the product surface).
- After U8, run `bin/ai-doctor` and `bash -n bin/bottle`; publish with
  `bin/ai-push` only when the user asks (repo rule).

---

## Phased Delivery

### Phase A — Evidence (U1, U2 in parallel)
Spike answers the format unknowns while the scaffold makes the repo buildable.

### Phase B — Engine (U3 → U4, U5)
Transcript surgery first (fixture-driven), then storage and harness packages.

### Phase C — Verbs (U6, then U7)
Core verbs end-to-end, then the management surface.

### Phase D — Surface (U8)
Help, goldens, skill stub, repo wiring.

---

## Sources & References

- **Origin document:** [docs/superpowers/specs/2026-06-10-bottle-design.md](../superpowers/specs/2026-06-10-bottle-design.md)
- Related code: `skills/herder/scripts/herder-spawn`, `skills/herder-fork/`,
  `bin/ai-doctor`, `lib/common.sh`
- Empirical groundwork: spike #1 pre-verified during design review (CC 2.1.170)

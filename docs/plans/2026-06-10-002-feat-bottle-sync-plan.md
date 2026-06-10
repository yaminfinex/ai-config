---
title: "feat: bottle sync — multi-machine sync via git remote"
type: feat
status: active
date: 2026-06-10
origin: docs/superpowers/specs/2026-06-10-bottle-sync-design.md
---

# feat: bottle sync — multi-machine sync via git remote

## Summary

Add a `sync` verb to the bottle CLI that synchronizes `~/.bottles` between
machines through a private git remote. The merge strategy treats the registry
as a projection: store dirs union through an ordinary git merge, then
`registry.json` is regenerated from every bottle's `meta.json` with a
deterministic collision policy (older `created` keeps the name; loser and its
same-name versions move to the first free `-2`-style suffix). `bottle list`
gains a no-network unsynced hint. All merge/regenerate logic lands in
`internal/store` (registry types are unexported there); the CLI verb stays
thin, matching every existing verb.

---

## Problem Frame

Bottles now exist on two machines with no way to share them; a shared DB
backend is explicitly premature. The store was built for this moment — it is
already a git repo that auto-commits every mutation — so sync collapses to
remote orchestration plus a registry merge strategy. The full design is in the
origin spec; this plan covers how to build it.

---

## Requirements

- R1. `bottle sync --remote <url>` configures `origin` on the store repo and
  syncs; bare `bottle sync` syncs thereafter. Re-running `--remote` replaces
  the remote. First-run setup prints the remote and a privacy reminder.
- R2. Sync algorithm: sweep dirty state → fetch → if remote empty, push and
  stop → merge the store's own branch with
  `--no-commit --allow-unrelated-histories` → regenerate `registry.json` from
  all `meta.json`s → union `decants` (ours-wins) → apply collision renames via
  the rename machinery → commit merge → push → print summary (received, sent,
  renames).
- R3. Collision policy is deterministic: a collision is two bottle ids
  claiming the same `name@version`; older `created` keeps the name (tie →
  smaller id); the loser moves — with all versions sharing its name — to the
  first free suffixed name, keeping version numbers and parent pointers.
  Both machines converge on byte-identical registries regardless of sync
  order.
- R4. Same-bottle-id-different-content merge conflicts abort with an error
  naming the id; transcript content is never auto-resolved.
- R5. `rm` propagates: a store-dir deletion merges in and regeneration drops
  the entry. Missing parents render as `(deleted)` in `log` (already true).
- R6. With a remote configured, `bottle list` appends one quiet hint line
  (e.g. `· 3 commits unsynced — bottle sync`) comparing `HEAD` against the
  last-fetched remote-tracking ref — no network call. No remote → no hint.
- R7. Sync is the only command that touches the network; failures abort
  cleanly (`git merge --abort`) leaving the store untouched. A sync
  interrupted between merge-commit and push self-heals on the next sync.
- R8. Sync hard-fails with a clear single-line error when git is absent or
  the store's git substrate is disabled (`git_auto_commit: false`) — the one
  sanctioned exception to the warn-and-continue posture.
- R9. New verb ships with agent-first help (Examples + Pitfalls), appears in
  the root help table, and is covered by goldens.

---

## Scope Boundaries

- No secret-scan gate before push — consciously relaxed from the parent
  design for v1 (user decision recorded in origin's Out of scope).
- No auto-sync, background sync, or sync-on-mutation.
- No shared database backend; no partial/per-bottle sync.
- No cross-machine locking — fetch/merge/push plus deterministic regeneration
  is the concurrency story.
- No `rm`-history expungement work beyond extending the existing documented
  caveat to cover the remote.

---

## Context & Research

### Relevant Code and Patterns

All paths relative to `tools/bottle/` (Go 1.26, stdlib only — no cobra, no
go-git):

- `internal/cli/cli.go` — hand-rolled `command` registry; new verb = one
  `live(...)` entry + `cmdSync` in a new `internal/cli/sync.go`. Root help
  table is generated from the registry (`TestRootHelpTableMatchesRegistry`).
- `internal/cli/command.go` — `deps` struct (store, stdio, `now`, seams);
  `parseFlexible` for flags after positionals. Tests build `deps` directly.
- `internal/store/git.go` — git via `os/exec`: `(*Store).git(gitPath, args...)`
  runs in `s.root` and returns `(out, err)` — reuse this for sync's
  load-bearing git calls. `autoCommit` is best-effort/void; sync must NOT use
  that posture. Lazy init uses `-c init.defaultBranch=main` but the branch is
  never referenced elsewhere — discover the current branch, don't hardcode.
  Commit identity overrides: `-c user.name=bottle -c user.email=bottle@localhost
  -c commit.gpgsign=false`.
- `internal/store/registry.go` — unexported `registry{Names map[string][]versionEntry;
  Decants map[string]Decant}`; `readRegistry`/`writeRegistry`
  (`AtomicSwap`); `mutate(commitMsg, fn)` = flock → read → fn → atomic write →
  autoCommit → unlock. Sync logic lives in `internal/store` (new `sync.go`)
  because these types are unexported.
- `internal/store/meta.go` — `Meta{Name, Version, Created, Note,
  PreviousNames, Source, Parent, ...}`; `Parent.BottleID` links by id, so
  collision renames never break lineage.
- `internal/store/ops.go` — `Rename(oldName, newName)`: per version, append
  old name to `PreviousNames`, set `Name`, rewrite meta, move registry key.
  Collision renames mirror this meta mutation. `List()` returns `[]NameInfo`
  for `bottle list`.
- `internal/cli/list.go` — tabwriter table; hint line slots in after
  `tw.Flush()`.
- `internal/store/store_test.go` — `newStore(t)`, `requireGit(t)` (skips when
  git off PATH), `gitOut(t, root, args...)`, scrubbed-PATH git-absent tests.
  No bare-remote harness yet; sync tests add one (`git init --bare` in a
  second temp dir).
- `internal/cli/help.go` + `internal/cli/testdata/golden/` — help consts,
  goldens regenerated with `go test ./internal/cli/ -run Golden -update`;
  `speccedVerbs` in `cli_test.go` must gain `"sync"`; root help has a
  <60-line budget test.

### Institutional Learnings

- No `docs/solutions/` exists yet. Prior art is the origin spec, the parent
  design (`docs/superpowers/specs/2026-06-10-bottle-design.md`), and the v1
  plan (`docs/plans/2026-06-10-001-feat-bottle-cli-plan.md`), which
  established: errors are single-line with a suggested flag; nothing is
  written before validation passes; help is product surface with goldens;
  agents are first-class users (confirmations need non-interactive escapes);
  never auto-publish (`bin/ai-push` only on user ask); run `bin/ai-doctor`
  after changes.

### External References

- None used — git plumbing is well-understood and local patterns are strong.

---

## Key Technical Decisions

- **Sync logic in `internal/store`, thin CLI verb**: registry types,
  flock/mutate, `writeMeta`, and `s.git` are all unexported store internals;
  a `store/sync.go` keeps the projection logic next to the data it projects.
- **Regeneration over merge**: `registry.json` is never 3-way merged; it is
  rebuilt from `meta.json`s after the git merge. Determinism of the rebuild is
  the convergence proof (see origin).
- **Sync uses error-returning git calls, not `autoCommit`'s warn-and-continue**:
  sync is the one command whose git operations are load-bearing; it hard-fails
  on git-absent, substrate-disabled, network, or merge errors (R8).
- **Hold the registry flock across the whole sync**: fetch → merge →
  regenerate → commit → push all happen under `lockExclusive()` so a
  concurrent `create` can't mutate the store mid-merge. The existing
  `mutate` helper commits with a fixed message after `fn`; sync needs its own
  orchestration (lock directly, commit the merge itself with a `sync: ...`
  message) rather than forcing itself through `mutate`.
- **Branch discovery, not hardcoding**: read the store's current branch
  (`git symbolic-ref --short HEAD` or equivalent) and fetch/merge/push that
  one branch (origin spec requirement).
- **Hint is store-side and best-effort**: a `Store` method computes
  ahead/behind from local refs only (`rev-list --left-right --count`); any
  error (no git, no remote, no remote-tracking ref yet) silently yields no
  hint — `list` must never get slower or noisier from sync's existence,
  except the one intended line.
- **Suffix allocation respects the name grammar**: suffixed names
  (`name-2`, `name-3`, …) already match `refs.NamePattern`
  (`[a-z0-9][a-z0-9-]*`); allocation scans existing names (post-union) for
  the first free suffix, and validates with `refs.ValidateName` defensively.

---

## Open Questions

### Resolved During Planning

- Scan gate before push: deferred entirely for v1 (user decision; recorded in
  origin spec's Out of scope).
- Where does sync logic live given unexported registry types: in
  `internal/store` (new `sync.go`), CLI stays thin.
- Behavior when git substrate is disabled or git absent: hard-fail with a
  single-line error — sync is meaningless without the substrate.

### Deferred to Implementation

- Exact git invocations for merge-state introspection (e.g. detecting the
  same-id content conflict from `git merge` output vs. `ls-files -u`):
  depends on observed porcelain behavior; the contract (abort + error naming
  the id) is fixed, the detection mechanics are not.
- Whether the unsynced hint also fires for "never pushed" via missing
  remote-tracking ref or via `rev-list` error semantics — same contract
  (hint when ahead/behind/never-pushed), mechanics chosen at implementation.
- Summary counting ("N received, M sent") — likely derived from pre/post
  bottle-id sets rather than git diff parsing; pick whichever is simpler in
  code.

---

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for
> review, not implementation specification. The implementing agent should
> treat it as context, not code to reproduce.*

```
Store.Sync(remoteURL string) (SyncReport, error)        // store/sync.go
  ensure git present + substrate enabled        else error
  if remoteURL != ""  → git remote add/set-url origin
  require origin configured                     else error("run with --remote")
  lockExclusive()                               defer unlock
  sweep: git add -A + commit if dirty           (belt-and-braces)
  branch := current branch
  git fetch origin
  if remote branch missing → push -u → report{sent: all} → done
  git merge --no-commit --allow-unrelated-histories origin/<branch>
    conflict on store/<id>/* → merge --abort → error(id)
  rebuild:
    metas := scan store/*/meta.json
    names := project(metas)                     // deterministic policy B
      collision(name@v claimed by 2 ids):
        winner = older Created (tie: smaller id)
        loser's name-group → first free suffix; meta rewrite via
        rename idiom (PreviousNames append + Name set)
    decants := union(ours, theirs)              // ours-wins
    writeRegistry(names, decants)               // atomic swap
  git add -A + commit ("sync: merge origin/<branch>")
  git push origin <branch>
  report{received, sent, renames}

cmdSync (cli/sync.go): parse --remote, call Store.Sync, print report/error.
Store.SyncStatus(): (ahead, behind int, ok bool)  // local refs only, for list
```

---

## Implementation Units

### U1. Registry projection: scan metas, deterministic rebuild, collision policy

**Goal:** Pure, deterministic regeneration of the registry's names map from a
set of `Meta`s, with policy-B collision resolution returning the renames to
apply.

**Requirements:** R3

**Dependencies:** None

**Files:**
- Create: `tools/bottle/internal/store/sync.go` (projection + collision types)
- Test: `tools/bottle/internal/store/sync_test.go`

**Approach:**
- A pure function from `[]Meta` (id + meta pairs) to a projected names map
  plus a list of rename decisions (`bottleID, oldName, newName`), with no I/O
  — this is what makes the convergence property unit-testable.
- Collision = same `name@version`, two ids. Winner: older `Created`, tie →
  lexicographically smaller id. The loser's *entire name-group* (all versions
  sharing the loser's name) moves together to the first free suffixed name;
  version numbers are preserved.
- Suffix scan considers all names post-union (including names created by
  earlier renames in the same pass) so resolution order can't oscillate;
  iterate collisions in a deterministic order (name, then version, then id).
- A second store-side function applies the projection: rewrites loser metas
  via the rename idiom (append `PreviousNames`, set `Name`) and writes the
  registry through `AtomicSwap`.

**Patterns to follow:**
- `internal/store/ops.go` `Rename` for the meta mutation idiom.
- `internal/store/registry.go` for registry types and write path.

**Test scenarios:**
- Happy path: disjoint name sets from two machines → union, no renames.
- Happy path: same name, same versions, same ids (already synced) → no-op.
- Covers independent-create collision: two `auth-expert@1`, different ids →
  newer `Created` becomes `auth-expert-2@1`, winner untouched.
- Covers divergent-rebottle collision: shared `@1`/`@2`, both sides created
  `@3` → newer `@3` (and its stacked `@4`) move to `auth-expert-2` keeping
  versions 3 and 4; parent pointers unchanged.
- Edge case: `Created` timestamps equal → smaller bottle id wins the name.
- Edge case: suffix exhaustion — `auth-expert-2` already exists as a real
  name → loser lands on `auth-expert-3`.
- Edge case: rm-vs-rebottle — parent `@2` absent from metas, child `@3`
  present with dangling parent id → projection keeps `@3`, no error.
- Determinism: project(A ∪ B) == project(B ∪ A), asserted byte-identical
  after JSON marshal, across all the above fixtures.
- Edge case: empty store → empty registry, no error.

**Verification:**
- `go test ./internal/store/` passes; the determinism property holds across
  all collision fixtures.

---

### U2. Sync orchestration: remote config, fetch/merge/push, decants union, report

**Goal:** `Store.Sync` end-to-end: remote management, locked git
orchestration, conflict abort, projection application, decants union, commit,
push, and a sent/received/renames report.

**Requirements:** R1, R2, R4, R5, R7, R8

**Dependencies:** U1

**Files:**
- Modify: `tools/bottle/internal/store/sync.go`
- Modify: `tools/bottle/internal/store/git.go` (if small helpers — current
  branch, remote get/set — fit better there)
- Test: `tools/bottle/internal/store/sync_test.go`

**Approach:**
- Precondition checks before any work: git on PATH, substrate enabled
  (`gitEnabled()`), remote configured (or `--remote` provided). Single-line
  errors with the suggested fix (e.g. "no remote configured — run: bottle
  sync --remote <url>").
- Hold `lockExclusive()` across the entire sequence; do not route through
  `mutate` (sync owns its own commit message and ordering: commit *then*
  push).
- Empty-remote fast path: push with upstream tracking, stop.
- Merge conflicts under `store/` → `git merge --abort`, error naming the
  bottle id; verify post-abort the worktree is clean.
- Decants union: read ours (pre-merge) and theirs (from the fetched ref,
  e.g. `git show origin/<branch>:registry.json`) — ours-wins per session id.
- Report: diff pre-merge vs post-merge bottle-id sets for received; ahead
  commits pushed for sent (or pre/post remote id sets — implementer's
  choice); list renames performed.
- First-run (`--remote` provided): after success, print remote URL +
  privacy reminder (the store may contain keys/PII; remote must be private).

**Execution note:** Start with a failing integration-style test using two
temp stores and a bare remote — the contract test for the whole unit.

**Patterns to follow:**
- `internal/store/git.go` `(*Store).git` for error-returning git calls and
  the identity-override flags on commits.
- `requireGit(t)` / `gitOut(t, ...)` from `store_test.go` for test plumbing.

**Test scenarios:**
- Happy path (integration): store A creates bottles, `Sync` to empty bare
  remote → pushed; store B syncs → receives all; registries byte-identical.
- Covers convergence: both stores create colliding and non-colliding bottles;
  A syncs, B syncs (merge happens on B), A syncs again → both registries
  byte-identical and renames match policy B.
- Happy path: rebottle on A, sync both ways → B sees the new version.
- Integration: `rm` on A propagates — after both sync, bottle dir and
  registry entry gone on B.
- Error path: no git on PATH (scrubbed PATH) → hard error mentioning git,
  store untouched.
- Error path: substrate disabled via `config.json` → hard error, no network.
- Error path: no remote configured, bare `Sync("")` → error suggesting
  `--remote`.
- Error path: unreachable remote URL → fetch fails, single-line error, store
  unchanged.
- Error path: forced same-id content conflict (hand-craft same dir, different
  transcript on both sides) → abort, error names the id, worktree clean
  (`git status --porcelain` empty), registry unchanged.
- Edge case: interrupted between merge-commit and push (simulate by making
  push fail once) → next sync pushes without re-merging; remote ends correct.
- Edge case: decants union — both registries carry decant entries; merged
  registry contains both; overlapping session id keeps ours.
- Edge case: `--remote` re-run with a different URL replaces origin.
- Happy path: dirty/untracked file in store root gets swept into a commit
  before merge.

**Verification:**
- Integration suite green with git present; scrubbed-PATH suite green;
  failure paths leave `git status --porcelain` empty and the registry
  readable.

---

### U3. CLI verb: `bottle sync` wiring, flags, output

**Goal:** Thin `cmdSync` exposing `Store.Sync`: `--remote <url>` flag, report
rendering, error rendering.

**Requirements:** R1, R2 (surface), R8 (error wording)

**Dependencies:** U2

**Files:**
- Create: `tools/bottle/internal/cli/sync.go`
- Modify: `tools/bottle/internal/cli/cli.go` (registry entry)
- Test: `tools/bottle/internal/cli/sync_test.go`

**Approach:**
- One `live("sync", ...)` entry; flags via `flag.FlagSet` + `parseFlexible`.
- Output: one summary line for the common case ("synced: 2 received, 1 sent")
  plus one line per rename; "already in sync" when nothing moved; first-run
  remote + privacy reminder lines.
- No confirmation prompts (sync is additive and abort-safe; nothing
  destructive to confirm), so no `--force` needed.

**Patterns to follow:**
- `internal/cli/create.go` / `list.go` for command shape, `deps` usage, and
  error conventions.

**Test scenarios:**
- Happy path: `cmdSync` with `--remote` against a bare remote (reusing store
  test plumbing) prints remote + privacy reminder + summary; exit 0.
- Happy path: second run prints "already in sync"; exit 0.
- Error path: no remote configured → exit 1, stderr suggests `--remote`.
- Error path: store error (e.g. git absent) → single-line stderr, exit 1.
- Happy path: renames render one line each, naming old → new.

**Verification:**
- `cmdSync` drives a real two-store round-trip through the CLI layer in
  tests; exit codes and stderr shapes match repo conventions.

---

### U4. Unsynced hint in `bottle list`

**Goal:** Quiet one-line hint in `list` when a remote is configured and the
store is ahead/behind/never-pushed — computed from local refs only.

**Requirements:** R6

**Dependencies:** U2 (remote config helpers)

**Files:**
- Modify: `tools/bottle/internal/store/sync.go` (`SyncStatus` — ahead/behind
  from `rev-list --left-right --count <branch>...<remote-ref>`)
- Modify: `tools/bottle/internal/cli/list.go`
- Test: `tools/bottle/internal/cli/list_test.go`,
  `tools/bottle/internal/store/sync_test.go`

**Approach:**
- `Store.SyncStatus()` returns counts + ok; *any* failure (no git, no remote,
  no remote-tracking ref) → not-ok, and `list` prints nothing extra. Never a
  warning, never an error — the hint must not make `list` noisier or slower
  in stores without a remote.
- Render after the table: `· 3 commits unsynced — bottle sync` (behind-only
  phrasing may differ slightly; keep to one line).

**Patterns to follow:**
- `internal/cli/list.go` rendering + `humanizeAge` tone; existing `list.txt`
  golden conventions.

**Test scenarios:**
- Happy path: no remote configured → `list` output byte-identical to today
  (existing `TestListTable` still passes unmodified).
- Happy path: remote configured, local ahead by N → hint line with N.
- Happy path: remote-tracking ref ahead of HEAD (behind after a fetch) →
  hint appears.
- Edge case: remote configured, fully synced → no hint.
- Edge case: git absent with a previously-configured remote → no hint, no
  warning.

**Verification:**
- `list` in a remote-less store is unchanged; hint appears/disappears
  according to ref state in an integration test.

---

### U5. Help, goldens, root table, docs

**Goal:** `sync` becomes product surface: agent-first help with Examples and
Pitfalls, root-table entry, goldens, spec/skill cross-references.

**Requirements:** R9

**Dependencies:** U3, U4

**Files:**
- Modify: `tools/bottle/internal/cli/help.go` (`syncHelp` const)
- Modify: `tools/bottle/internal/cli/cli_test.go` (`speccedVerbs` += "sync")
- Create: `tools/bottle/internal/cli/testdata/golden/sync.txt`
- Modify: `tools/bottle/internal/cli/testdata/golden/root.txt` (regenerated)
- Modify: `skills/bottling/SKILL.md` (one-line sync mention)

**Approach:**
- Help body documents landed behavior only: Examples (first-run `--remote`,
  routine `bottle sync`), Pitfalls (private remote — transcripts may contain
  secrets; `rm` does not expunge git history on the remote; collisions
  auto-rename the newer bottle to `name-2`).
- Regenerate goldens with `-update`; root help must stay under its 60-line
  budget — tighten the sync summary line if needed.

**Test scenarios:**
- Test expectation: covered by existing golden/registry drift tests —
  `TestRootHelpTableMatchesRegistry`, `TestRootHelpUnderLineBudget`, and the
  per-verb golden test gain `sync` automatically once `speccedVerbs` is
  updated.

**Verification:**
- `go test ./internal/cli/` green including goldens; root help under budget;
  `bin/ai-doctor` clean.

---

## System-Wide Impact

- **Interaction graph:** Sync takes the same flock as every mutation, so
  concurrent `create`/`rebottle`/`rm` on the same machine serialize against
  it. No other command gains network behavior; `list` gains one local-ref
  git call only when a remote is configured.
- **Error propagation:** Store-level sync errors are returned (not warned)
  and rendered by the CLI as single-line stderr + exit 1 — deliberately
  opposite to `autoCommit`'s best-effort posture, scoped to sync alone.
- **State lifecycle risks:** Partial sync is the main one — mitigated by
  doing all mutation under the flock, aborting failed merges
  (`git merge --abort`), and the commit-then-push ordering whose
  interrupted-between state self-heals on the next sync (R7).
- **API surface parity:** None — no other harness or command exposes sync;
  the bottling skill doc gets a pointer only.
- **Integration coverage:** The two-stores-and-bare-remote suite (U2) is the
  load-bearing coverage; unit tests alone cannot prove convergence or
  abort-cleanliness.
- **Unchanged invariants:** Bottle immutability (renames touch only
  `Name`/`PreviousNames`, the same fields `bottle rename` already mutates);
  registry writes stay flock + atomic-swap; every non-sync command remains
  fully offline and degrades warn-and-continue without git; `meta.json`
  remains the provenance source of truth.

---

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Registry regeneration drops information only the old registry knew (it is the projection's only non-meta input) | Decants are explicitly unioned from both pre-merge registries; names/versions are fully derivable from metas — asserted by U1 round-trip tests on real stores |
| Suffix-allocation oscillation across machines (rename on A creates a name that collides on B) | Deterministic, order-independent projection (U1 determinism property test: project(A∪B) == project(B∪A)) |
| `git merge` porcelain differences across git versions when detecting conflicts | Detection mechanics deferred to implementation; contract pinned by the forced-conflict integration test |
| Interrupted sync leaves merge state in the worktree | Abort on every failure path; tests assert `git status --porcelain` empty after each error scenario |
| Hint slows or breaks `list` on stores without remotes | `SyncStatus` is best-effort/silent; existing `list` tests must pass unmodified |

---

## Documentation / Operational Notes

- Origin spec already records the deferred scan gate and the compounded
  `rm`/git-history caveat (expunge now requires force-pushing the remote).
- `skills/bottling/SKILL.md` gains a one-line sync mention (U5).
- Repo rule: do not publish via `bin/ai-push` unless asked; run
  `bin/ai-doctor` after changes.

---

## Sources & References

- **Origin document:** [docs/superpowers/specs/2026-06-10-bottle-sync-design.md](../superpowers/specs/2026-06-10-bottle-sync-design.md)
- Parent design: `docs/superpowers/specs/2026-06-10-bottle-design.md`
- Prior plan: `docs/plans/2026-06-10-001-feat-bottle-cli-plan.md`
- Related code: `tools/bottle/internal/store/` (registry.go, git.go, ops.go,
  meta.go), `tools/bottle/internal/cli/` (cli.go, command.go, list.go,
  help.go)

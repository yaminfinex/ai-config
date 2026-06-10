# bottle sync — design

Sync all bottles between machines using the git substrate that `~/.bottles`
already is. No shared database, no new service: a private git remote is the hub,
and the registry is treated as a **projection** — regenerated from the immutable
bottles after every merge rather than merged itself.

Companion to `2026-06-10-bottle-design.md`, which anticipated this phase:
"Sharing later = add a remote... plus a registry merge strategy." This document
is that registry merge strategy.

## Decisions (settled during brainstorming)

- **Transport:** a private git remote (e.g. code.storage, GitHub private repo).
  Ambient git auth (SSH keys). Transcripts can contain keys/PII, so the remote
  must be private; first-run setup prints the remote and a privacy reminder.
- **Collision policy:** loser-gets-suffixed (policy B). Never lose data, never
  fabricate lineage, names stay stable for the older bottle.
- **Trigger:** manual `bottle sync`, plus a quiet unsynced hint in `bottle list`.
  No auto-sync: every other command stays local, fast, and offline-safe.
- **Deletions:** `rm` propagates between machines on sync. One logical store,
  two mirrors.

## Command surface

One new verb:

```
bottle sync --remote <url>   # first run: store remote as origin, then sync
bottle sync                  # thereafter: fetch, merge, regenerate, push
```

The remote lives in the store repo's git config as `origin` — no new config
file. Running `--remote` again replaces the remote.

## Sync algorithm

1. **Sweep.** Commit any untracked/dirty state in `~/.bottles` (the auto-commit
   layer should leave none; belt-and-braces).
2. **Fetch** `origin`. If the remote is empty, push and stop.
3. **Merge** the remote branch with `--no-commit --allow-unrelated-histories`.
   The store's own current branch name is used on both ends (whatever
   `init.defaultBranch` produced locally); sync pushes and merges that one
   branch and never assumes it is literally `main`. Unrelated histories cover
   the initial adoption case where both machines already have bottles.
   `store/<id>/` dirs are immutable, add-only, and
   randomly named, so they union cleanly. If two dirs conflict on the same
   bottle id with *different content* (8-char-base36 id collision —
   astronomically unlikely), abort the merge and error naming the id. Never
   auto-resolve transcript content.
4. **Regenerate the registry.** Ignore git's merge result for `registry.json`
   entirely. Rebuild the names map from every `meta.json` now on disk, applying
   collision resolution (below). Union the `decants` map from both parents'
   registries (ours-wins on a same-session-id clash, which UUIDs make
   practically impossible) — decant sessions are machine-local files, so this
   map is bookkeeping, not truth.
5. **Apply renames.** If resolution renamed any bottle, rewrite its `meta.json`
   via the existing rename machinery.
6. **Commit the merge, push.** Print a summary: bottles received, bottles sent,
   renames performed.

The registry-as-projection move is what makes this cheap: there is no 3-way
merge of a nested structure anywhere. The immutable bottles are the source of
truth; `registry.json` is re-derived. Even without the original sequence of
events, the surviving facts (`meta.json`s) are enough to re-project.

## Collision resolution (deterministic policy B)

A **collision** is two different bottle ids claiming the same `name@version`.

- The bottle with the **older `created` timestamp keeps the name**; ties break
  to the smaller bottle id.
- The **loser moves to the first free suffixed name** (`auth-expert` →
  `auth-expert-2`, then `-3`, ...), **together with its descendants**, so a
  divergent chain stays intact under one name. Version numbers are kept as-is
  (a moved `@3` stays `@3` under the new name — versions are identifiers, and
  renumbering would silently move pinned `name@v` references).
- **Provenance is untouched.** Parent pointers live in `meta.json`, so
  `bottle log` walks the true chain across the rename.

This one rule covers both collision shapes:

- **Independent creates:** both machines made `auth-expert@1` from scratch.
  Newer one becomes `auth-expert-2@1`.
- **Divergent rebottles:** both machines rebottled `auth-expert@2` into `@3`.
  The newer `@3` (and any `@4+` stacked on it) moves to `auth-expert-2`,
  keeping versions `3`, `4`, ... — its parent pointer still names
  `auth-expert@2`, which `log` shows.

Determinism is the convergence proof: both machines compute the same
resolution from the same set of bottles, regardless of sync order, so their
registries end up byte-identical without coordination.

## Deletions

`rm` propagates through the ordinary git merge: the store-dir deletion merges
in, and regeneration drops the registry entry. A bottle `rm`'d on one machine
disappears from the other on its next sync.

The existing retention caveat **compounds**: an `rm`'d transcript persists in
git history on both machines *and* the remote. The `bottle rm` warning and the
`git filter-repo` expunge procedure in the docs now apply to three repos, and
the expunge procedure must be run against the remote too (force-push).

Edge: machine A `rm`s `auth-expert@2` while machine B rebottles it into `@3`.
The merge applies both — `@2`'s dir is gone, `@3` exists with a parent pointer
to a deleted bottle. `log` already renders missing parents as `(deleted)`;
nothing new needed.

## The hint

When a remote is configured, `bottle list` compares `HEAD` against the
last-fetched remote-tracking ref — **no network call** — and appends one quiet
line when ahead, behind, or never pushed:

```
· 3 commits unsynced — bottle sync
```

No remote configured → no hint, no change to current output.

## Failure modes

- `sync` is the only command that touches the network. Offline or auth failure
  → plain error, store untouched (a failed merge is aborted cleanly with
  `git merge --abort`).
- Id-collision conflict (step 3) → abort + error; the user escalates manually.
  Expected never to happen.
- A sync interrupted between merge-commit and push just leaves the store ahead
  of the remote; the next `sync` pushes. The hint surfaces this.

## Testing

- **Unit (table-driven):** registry regeneration + collision resolution —
  independent creates, divergent rebottles, descendants following the loser,
  rm-vs-rebottle races, suffix exhaustion (`-2` also taken), created-timestamp
  ties broken by id.
- **Integration:** two temp stores and a bare repo as the remote. Create,
  rebottle, and rm on both sides; sync both (twice, to cover the
  second-machine-merges case); assert the two registries are byte-identical
  and both stores hold the union of surviving bottles.

## Out of scope

- Auto-sync, background sync, or sync-on-mutation.
- Any shared database backend (explicitly deferred by the user).
- Partial sync / per-bottle push.
- Multi-writer locking across machines — git's fetch/merge/push cycle plus
  deterministic regeneration is the concurrency story.

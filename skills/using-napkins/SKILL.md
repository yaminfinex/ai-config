---
name: using-napkins
description: >
  Per-branch working memory in `napkins/` (gitignored). Owns the full lifecycle: capture, harvest, prune. Use when the
  user says "capture this", "capture for later", "park this", "save for later", "remember this for the harvest", or any
  other intent to record todos / decisions / questions / lessons from recent conversation context without promoting them
  to permanent homes yet. Also use when the user signals the branch is wrapping up — "raising a PR", "open the PR",
  "let's PR this", "wrap up the branch", "finishing this work", "before merge", "harvest the napkin", "prune the napkin"
  — to walk the napkin, route entries to their permanent homes, and delete the directory. Other skills (handoff,
  improve-architecture, debug repros) write hand-named files into the same directory.
---

# Using Napkins

Napkins are working-tree-only scratch storage. They live in `napkins/` at the repo root and are gitignored, so they
cannot survive into a merge. This skill owns the full lifecycle:

1. **Capture** — explicit, user-triggered. Distill recent context into high-signal atoms appended to `captures.md`.
2. **Harvest** — walk the napkin before PR/merge, route each entry to its permanent home.
3. **Prune** — delete `napkins/` once harvest is done.

## Directory layout

```text
napkins/                  # gitignored — working-tree-only
  index.md                # hand-maintained, lists hand-written files + history
  captures.md             # append-only dated batches, hand-written by the agent
  <whatever>.md           # handoff docs, debug repros, etc., caller-named
  <workflow>/...          # optional subdirectories (e.g. handoff/<slug>/)
```

Flat — no `<branch>/<user>/` namespacing. Each worktree has its own filesystem and the directory is gitignored, so
collisions are impossible.

## On session start

1. If `napkins/index.md` exists, read it. It tells you which hand-written files are present.
2. If `napkins/captures.md` exists, read it (or `tail -c 20000` if long). It rehydrates atoms captured in prior turns.
3. Otherwise do nothing; files are created on demand.

## Capture

Triggered when the user says any of: "capture this", "capture for later", "park this", "save for later", "ok capture
this", "remember this for the harvest", or any equivalent intent to record recent context.

**Behavior:** read the recent conversation, distill high-signal atoms, append a single dated batch to
`napkins/captures.md`. The file is append-only — never edit older batches.

**Format:**

```markdown
## 2026-05-18 14:30

- todo: Refactor proxy.ts after merge refs: apps/web/src/shared/electric/proxy.ts:88
- decision: Use connect-only timeout why: request-wide AbortSignal kills SSE body stream mid-flight
- question: Should bridge tolerate Inngest 5xx?
- lesson: Caddy emits 502 EOF when upstream body stream closes mid-flight
- meta: capture should auto-suggest refs when a file was just edited
```

**Categories** (hints, not enforced — use judgment):

- `todo` — deferred work
- `decision` — choice plus rationale
- `question` — open item
- `lesson` / `gotcha` — durable insight
- `meta` — friction with this skill itself, or patterns worth baking in

Optional `refs:` for `file:line` pointers, optional `why:` for rationale. Keep capture low-friction; no schema beyond
this.

**Filter for signal.** Skip things the user can reconstruct from the diff, the commit message, or the conversation
transcript. The point of capture is to surface what would otherwise be lost.

If `napkins/` does not exist yet, create it before writing.

## Hand-written napkins (handoff docs, debug repros, etc.)

Other skills (or the user) may want to write a longer hand-named file — `handoff-<slug>.md`, `review.md`,
`debug/repro.md`. When that happens:

1. Ensure `napkins/` exists.
2. Write or update the file.
3. Update `napkins/index.md`:
   - Add to the `## Files` section if new.
   - Append a `## History` line with timestamp and short commit hash.

Do not list `captures.md` in `## Files` — it is the default capture sink and is always implicit.

### Index file format

```markdown
# Napkins

## Files

- [handoff-finish-extractor-rollout.md](handoff-finish-extractor-rollout.md) — partial handoff, runtime verification
  pending
- [review.md](review.md) — PR review notes, in-progress

## History

- 2026-05-18 14:30 (a1b2c3d) — Started implementation of staking tools
- 2026-05-18 15:10 (d4e5f6a) — Completed service layer, pausing for review
```

## Harvest

Triggered when the user signals end-of-branch intent — "raising a PR", "open the PR", "let's PR this", "wrap up the
branch", "finishing this work", "before merge", "harvest the napkin", "prune the napkin". Run `issue-capture` before
raising the PR so any captured todos that need a Linear ticket exist by the time the PR body is written. Linear's GitHub
integration auto-links the resulting PR by detecting `STV-N` in branch/title/body.

**Steps:**

1. **Survey.** Read `napkins/captures.md` and every file listed in `napkins/index.md`. Group capture entries by
   category.
2. **Cluster by root cause.** Before walking entries linearly, do one pass to **group entries by underlying cause**, not
   by capture category or chronology. Long-running branches often accumulate several entries that collapse to the same
   root: "ids aren't durable", "input modes aren't uniform", "server env != caller env", "this API has a silent
   failure mode", etc. A linear walk produces duplicate destinations and contradictory routing decisions for siblings of
   the same cause. The cluster pass surfaces the right *unit of work* — usually one doc edit, one issue, or one rule —
   that resolves the whole cluster at once.

   Present the clusters back to the user before routing (one line per cluster, listing the constituent capture entries),
   so they can confirm the grouping or split a cluster that's been over-merged. Single-entry "clusters" are fine and
   stay as-is.
3. **Walk clusters and route.** For each cluster, propose a destination and prompt the user `y / edit / skip`. Routing
   hints (not strict — agent uses judgment):
   - `todo` → delegate to `issue-capture` (creates a Linear issue), or skip if stale
   - `decision` → `docs/work/<topic>/...`, an ADR, the PR body, or a commit message
   - `question` → `docs/work/<topic>/...` if still open; drop if resolved
   - `lesson` / `gotcha` → `.agents/rules/<rule>.md` or `CONTEXT.md`
   - `meta` → suggested edit to this `SKILL.md`, or append to a backlog file inside this skill directory

   Mixed-category clusters are common (one lesson + one todo + one decision all rooted in the same cause). Route the
   *cluster*, not the individual entries — the destination doc/issue/rule should reflect the root cause, with the
   per-entry detail folded into it.
4. **Walk hand-written napkins.** For each file in `index.md`, ask: promote to `docs/work/<topic>/...`, fold findings
   into another doc, or drop?
5. **Prune.** Once the walk completes, delete the whole directory:

   ```sh
   rm -rf napkins/
   ```

   It is gitignored, so this is a working-tree-only delete with no commit needed.

6. **Verify.** `git status` shows no `napkins/` paths.

## Self-iteration via `meta` captures

If you hit friction using this skill — confusing instruction, missing routing destination, awkward category — capture it
as `meta`. At harvest time, `meta` entries become proposed edits to this `SKILL.md`. That's how the skill improves
without a separate review cycle.

---
name: using-napkins
description: >
  Per-branch working memory in `napkins/` (gitignored). Owns capture, harvest, prune. Use on intents like "capture this",
  "park this", "save for later", "remember this for the harvest" — recording todos / decisions / questions / lessons from
  recent conversation without promoting them yet. Also use on end-of-branch intents — "raising a PR", "open the PR",
  "wrap up the branch", "before merge", "harvest the napkin", "prune the napkin" — to route entries to their permanent
  homes and delete `napkins/`. Other skills (handoff, improve-architecture, debug repros) write hand-named files into the
  same directory.
---

# Using Napkins

Working-tree-only scratch storage in `napkins/`. Gitignored, cannot survive a merge. Lifecycle: **capture → harvest → prune**.

## Directory layout

```text
napkins/                  # gitignored
  index.md                # hand-maintained: lists hand-written files + history
  captures.md             # append-only dated batches
  <whatever>.md           # handoff docs, debug repros, etc.
  <workflow>/...          # optional subdirs
```

Flat namespacing — each worktree has its own filesystem.

## On session start

If `napkins/index.md` exists, read it. If `napkins/captures.md` exists, read it (`tail -c 20000` if long) to rehydrate prior atoms. Otherwise do nothing; files are created on demand.

## Capture

Triggers: see frontmatter. Read recent conversation, distill high-signal atoms, append one dated batch to `napkins/captures.md` (append-only — never edit older batches). Create `napkins/` first if missing.

Format:

```markdown
## 2026-05-18 14:30

- todo: Refactor proxy.ts after merge refs: apps/web/src/shared/electric/proxy.ts:88
- decision: Use connect-only timeout why: request-wide AbortSignal kills SSE body stream mid-flight
- question: Should bridge tolerate Inngest 5xx?
- lesson: Caddy emits 502 EOF when upstream body stream closes mid-flight
- meta: capture should auto-suggest refs when a file was just edited
```

Categories (hints, not enforced): `todo`, `decision`, `question`, `lesson`/`gotcha`, `meta` (friction with this skill).

Optional `refs:` for `file:line`, optional `why:` for rationale. **Filter for signal** — skip what the user can reconstruct from diff, commit message, or transcript. Capture is for what would otherwise be lost.

## Hand-written napkins

Other skills (or the user) may write a hand-named file — `handoff-<slug>.md`, `review.md`, `debug/repro.md`. When that happens:

1. Ensure `napkins/` exists.
2. Write or update the file.
3. Update `napkins/index.md` (add to `## Files`, append to `## History` with timestamp + short commit hash).

Don't list `captures.md` in `## Files` — it's the implicit default sink.

```markdown
# Napkins

## Files

- [handoff-finish-extractor-rollout.md](handoff-finish-extractor-rollout.md) — partial handoff, runtime verification pending
- [review.md](review.md) — PR review notes, in-progress

## History

- 2026-05-18 14:30 (a1b2c3d) — Started implementation of staking tools
- 2026-05-18 15:10 (d4e5f6a) — Completed service layer, pausing for review
```

## Harvest

Run `issue-capture` first so any todo-rooted Linear issues exist before the PR body is written (Linear auto-links via `STV-N` in branch/title/body).

1. **Survey.** Read `captures.md` and every file in `index.md`.
2. **Cluster by root cause.** Before walking linearly, group entries by *underlying cause*, not category or chronology. Long-running branches accumulate siblings of one root ("ids aren't durable", "server env != caller env", "this API has a silent failure mode"). Linear walks produce duplicate destinations and contradictory routes. Each cluster usually maps to one doc edit / issue / rule. Present clusters back to the user for confirm/split; single-entry clusters are fine.
3. **Walk clusters and route.** For each, propose a destination, prompt `y / edit / skip`. Routing hints:
   - `todo` → `issue-capture` (Linear issue), or skip if stale
   - `decision` → `docs/work/<topic>/...`, ADR, PR body, or commit message
   - `question` → `docs/work/<topic>/...` if open; drop if resolved
   - `lesson` / `gotcha` → `.agents/rules/<rule>.md` or `CONTEXT.md`
   - `meta` → edit to this `SKILL.md` or backlog file in this skill directory

   Mixed-category clusters are common — route the *cluster*, with per-entry detail folded in.
4. **Walk hand-written napkins.** For each file in `index.md`: promote, fold, or drop.
5. **Prune.** `rm -rf napkins/`. Gitignored, no commit needed.
6. **Verify.** `git status` shows no `napkins/` paths.

## Self-iteration

Friction with this skill → capture as `meta`. At harvest, `meta` entries become proposed edits to this file.

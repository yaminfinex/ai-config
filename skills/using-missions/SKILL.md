---
name: using-missions
description: >
  Working memory backed by the active mission under `$MISSIONS_REPO/missions/<slug>/` (resolved via the `.mission`
  marker). Owns capture and session-start rehydration. Use on intents like "capture this", "park this", "save for
  later", "remember this" — recording todos / decisions / questions / lessons from recent conversation without
  promoting them yet. Requires an active mission; refuses (with the `mish` recipe) when there is none. Other skills
  (handoff, improve-architecture, orchestrate, debug repros) write hand-named files into the same mission's
  `artifacts/` tree.
---

# Using Missions

Working memory lives in the active mission, not the working repo. A mission directory
(`$MISSIONS_REPO/missions/<slug>/`) is durable, git-shared, and driven on main — captures survive
branch deletion and worktree teardown, never pollute the code repo, and are readable by every
agent and machine on the mission. Lifecycle is just **capture**: the mission is the durable home;
final disposition belongs to mission closeout (`mish` skill), not to this one.

## Resolving the mission

`mish resolve` reads the worktree's `.mission` marker and prints the mission context as JSON.

**No marker → no capture.** Don't auto-mint and don't fall back to local scratch files. Tell the
user capture needs a mission and hand them the choice: `mish new <slug>` to mint one, or write an
existing slug into `.mission` to join one (doctrine in the `mish` skill). Missions are opt-in by
design — most work never becomes one.

## On session start

If `.mission` exists: resolve it, then read the mission's `artifacts/captures.md` if present
(`tail -c 20000` if long) to rehydrate prior atoms. Otherwise do nothing.

## Capture

Triggers: see frontmatter. Read recent conversation, distill high-signal atoms, append one dated
batch to the capture sink (append-only — never edit older batches).

**Where:** freeform — the mission's orchestrator or playbook dictates paths when there is one
(check the playbook before inventing a location). When nothing dictates, default to
`artifacts/captures.md` at the mission's artifacts root. With multiple concurrent writers, pick a
disjoint path per the `mish` artifacts doctrine and note it on the board.

Format:

```markdown
## 2026-05-18 14:30 (api@a1b2c3d)

- todo: Refactor proxy.ts after merge refs: apps/web/src/shared/electric/proxy.ts:88
- decision: Use connect-only timeout why: request-wide AbortSignal kills SSE body stream mid-flight
- question: Should bridge tolerate Inngest 5xx?
- lesson: Caddy emits 502 EOF when upstream body stream closes mid-flight
- meta: capture should auto-suggest refs when a file was just edited
```

Categories (hints, not enforced): `todo`, `decision`, `question`, `lesson`/`gotcha`, `meta`
(friction with this skill). Optional `refs:` for `file:line`, optional `why:` for rationale. The
batch header names the source repo + short sha, since the mission lives outside the repo the atoms
refer to. **Filter for signal** — skip what's reconstructable from diff, commit message, or
transcript. Capture is for what would otherwise be lost.

A `todo` that is already durable and actionable can go straight onto the mission board
(`mish backlog ... task create`) instead of the capture sink — judgment call; pull first per the
`mish` ID-race doctrine.

## Hand-written files

Other skills (or the user) may write hand-named files — `handoff-<slug>.md`, `review.md`,
`improve-architecture/<cluster>.md`. They land under the mission's `artifacts/` at whatever path
the orchestrator dictates (default: the artifacts root, or a subdir named for the workflow). No
index file to maintain — the artifacts tree and the board are the catalog.

## Git rhythm

The missions repo is shared; follow the `mish` doctrine rather than restating it here: pull before
creating, commit at the mission-subtree grain (`git -C "$MISSIONS_REPO" add missions/<slug>` —
one mission per commit), push when a unit of work lands. Capture batches ride the custody grammar
with a freeform verb:

```bash
git -C "$MISSIONS_REPO" add "missions/<slug>"
git -C "$MISSIONS_REPO" commit -m 'mission(<slug>): capture atoms from api proxy work'
```

Per-atom commits are noise; one commit per batch (or per sitting) is the grain.

## Self-iteration

Friction with this skill → capture as `meta`. When the user asks (or at mission close), `meta`
atoms become proposed edits to this file.

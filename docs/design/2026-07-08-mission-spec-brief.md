---
title: "Brief — mission spec writer (CLI + dir format + skill)"
date: 2026-07-08
status: DISPATCH BRIEF for a side agent; the grilling rulings it encodes are ratified, the
  spec it commissions is DRAFT until owner ratification.
---

# Brief: write the mission spec, beginning to end

You are writing `docs/specs/mission-spec.md` on branch `mission-spec` (your worktree),
docs-only. Deliverable: a complete spec in the house style — ubiquitous language, domain
model, invariants, expected behaviour, high-level design, acceptance scenarios — status
**DRAFT, awaiting ratification**. `docs/specs/herder-spec.md` (main, RATIFIED) is the
**style and quality bar only**; missions are completely herder-unaware, so no herder concept
may appear in the mission spec's model.

**Working mode.** The owner (bigboss on the bus) will work with you directly in your pane —
this is collaborative spec-writing, not a fire-and-forget unit. The session `tomo` holds the
boundary-grilling context; cross-pollinate over the bus when a ruling seems ambiguous rather
than guessing. Escalate contested calls to the owner; keep a running open-questions list in
the spec's tail.

## Read first, in order

1. `docs/design/2026-07-08-sessions-missions-boundaries.md` — **§6b Q11–Q17 are ratified and
   binding**; §1–§5 give the component doctrine (note: parts of §1–§4 are superseded by §6b —
   the grilling record wins wherever they disagree).
2. `docs/design/2026-07-02-missions-scenario-pack.md` — background walkthroughs (two rounds
   stale; scenarios still useful as acceptance-scenario quarry).
3. The live Backlog.md CLI (`backlog --help`; mise-installed) and this repo's `backlog/` as a
   living example of the board format and conventions.

## Binding rulings (condensed — §6b is authoritative if this list drifts)

- Mission = `missions/<slug>/{mission.md, backlog/, artifacts/}` — self-contained, moves as a
  unit. **No events.jsonl** (killed, Q15): custody/attribution discipline folds into
  conventioned git commit messages + board notes.
- Verbs: **`new` / `backlog` / `status`**, nothing else. `new` scaffolds the dir, inits the
  nested Backlog.md with pinned config (its autoCommit/remoteOperations OFF), writes the D6
  context marker. `backlog` = cwd-pinned passthrough with a small denylist (at minimum
  `init`, `config`). `status` is read-only.
- The CLI **never runs git by default**; an opt-in per-invocation auto-commit marker on write
  verbs is reserved (propose a design or an explicit deferral).
- `mission.md` frontmatter carries an advisory **`authority:`** field — an opaque label-grade
  string. Only the authority edits mission.md or restructures the board; a merge conflict on
  mission.md is by definition an authority violation and the authority's version wins.
- Board **assignee** = opaque label-grade string. Missions herder-unaware; herder may be very
  mission-aware — the asymmetry is ratified (Q17).
- One shared missions repo, env-var located (D11); board per mission always (D4);
  multi-writer doctrine: one manifest authority, everyone else works assigned tickets +
  disjoint artifact paths (boundaries §4).
- `artifacts/` is free-form; orchestrate-owned files (journal.md, playbook) are conventions of
  the orchestrate skill — the mission spec must NOT define them.

## The spec must settle

Exact mission.md format; slug rules; marker-file mechanics (D6) and env-var name + resolution
order; `status` output; the passthrough denylist's exact set + pinned Backlog.md config keys;
scaffold contents; multi-node union-merge posture; closeout/harvest commit-message
conventions (skill prose); what the companion skill says vs what the CLI does.

## Verify EARLY, before writing around it

Nested Backlog.md instances inside one shared repo (Q11's open verification item): actually
test `backlog` behavior with a `backlog/` dir nested below repo root. If it misbehaves,
**stop and escalate to the owner + tomo** — dir self-containment was ratified over the
shared-board fallback, so a workaround is a design change, not an implementation detail.

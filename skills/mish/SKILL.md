---
name: mish
description: Work with missions — durable, git-shared work homes under $MISSIONS_REPO/missions/<slug>/ driven by the `mish` CLI (new/backlog/status). Use when the user says "mint a mission", "create a mission", "mission board", "mission status", "adopt this into the mission", "harvest the results", "rename the mission", "close the mission", mentions a .mission marker or $MISSIONS_REPO, asks for a custody commit, or hits a merge conflict on mission files and needs the multi-writer resolution doctrine.
---

# mish

A mission is one directory — manifest, Backlog.md board, artifacts — shared by
plain git. `mish` scaffolds and reports; git and judgment stay with the agent.
The CLI's own help is the real surface; this skill carries only what spans git
and multiple writers.

## Start here

```bash
mish                  # concept model + verb table
mish <verb> --help    # working doctrine per verb: new, backlog, status
```

Read those first — they document the landed behavior (custody grammar, board
allowlist, status warnings, closeout checklist, rename procedure, marker
hygiene). Don't guess flags.

## When to mint a mission

Missions are strictly opt-in; most work never becomes one. Mint one when work
outgrows a single session and needs shared, durable working memory: multiple
agents or machines over days, artifacts worth keeping past a branch, a task
board other people need to read. Don't mint one for work a branch and its local
notes already cover — the missionless path costs zero, and it should stay that
way.

## Pull before you create

Slug uniqueness and Backlog.md task IDs are both checked per clone, not
globally: two unsynced nodes can mint the same slug, and can allocate the same
task ID — and the duplicate-ID case unions **silently**, with no merge conflict
to warn you. Pulling the missions repo before `mish new` and before task
creation narrows both windows; pushing when a unit of work lands does the same
for everyone else's window.

## Custody commits, worked

Subjects follow `mission(<slug>): <verb> <summary>` (see `mish --help` for the
verb vocabulary and trailers). `mish` never runs git — every commit below is
yours to make. Worked, across one mission's life:

```bash
# new — mish scaffolds; you commit the scaffold
mish new perf-regression --authority hera
git -C "$MISSIONS_REPO" add missions/perf-regression
git -C "$MISSIONS_REPO" commit -m 'mission(perf-regression): new scaffold for the Q3 perf regression hunt'

# adopt — you move the files in yourself (there is no adopt machinery),
# then commit; the summary names the source
cp -r ~/code/api/napkins/repro "$MISSIONS_REPO/missions/perf-regression/artifacts/hera/repro"
git -C "$MISSIONS_REPO" add missions/perf-regression
git -C "$MISSIONS_REPO" commit \
  -m 'mission(perf-regression): adopt repro script from api worktree napkins' \
  --trailer 'Mission-Source: api@9f31c2d' --trailer 'Mission-Agent: hera'

# harvest — copy out, never move: the mission stays self-contained. Record
# the disposition (destination repo, path, landed sha) on the producing task,
# where external effects live; that record is the diff the custody commit
# carries, keeping the harvest visible to git log -- missions/<slug>/
cp "$MISSIONS_REPO/missions/perf-regression/artifacts/hera/fix-notes.md" ~/code/api/docs/perf/
mish backlog --mission perf-regression task edit 12 \
  --comment 'Harvested: fix-notes.md -> api docs/perf, landed api@4e2a91b'
git -C "$MISSIONS_REPO" add missions/perf-regression
git -C "$MISSIONS_REPO" commit \
  -m 'mission(perf-regression): harvest fix notes to api docs/perf' \
  --trailer 'Mission-Dest: api@4e2a91b'

# rename — after the full procedure (see mish new --help and the walkthrough
# below); one commit covers the dir move, both field edits, and marker fixes
git -C "$MISSIONS_REPO" commit -m 'mission(q3-perf-hunt): rename from perf-regression to q3-perf-hunt'

# close — after the closeout checklist (see mish status --help); the diff is
# the authority's Closeout prose and status flip
git -C "$MISSIONS_REPO" commit -am 'mission(q3-perf-hunt): close completed; findings harvested to api docs/perf'
```

Subjects grep across history by slug (`git log --grep 'mission(q3-perf-hunt)'`)
and by verb (`git log --grep '): harvest '`). The vocabulary is open: use a
freeform verb when none fits, in the open, in the same grammar.

## Artifacts: disjoint paths

Every writer keeps to their own subtree under `artifacts/` — per agent or per
workstream (`artifacts/hera/…`, `artifacts/load-testing/…`). Disjoint paths are
what make concurrent writes from many nodes union cleanly. On an accidental
collision, either writer renames theirs to a disjoint path and notes the board
— no content adjudication.

## When a merge conflicts

Conflicts on mission files have fixed owners, decided in advance — never
adjudicated on content. The full taxonomy, plus two end-to-end walkthroughs
(an authority conflict on `mission.md`; a slug rename), is in
[references/multi-writer-walkthrough.md](references/multi-writer-walkthrough.md).

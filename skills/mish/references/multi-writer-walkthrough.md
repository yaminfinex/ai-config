# Missions under many writers: conflict taxonomy and walkthroughs

One shared missions repo, many nodes, many writers is the normal case. The
format is arranged so honest writers never collide: task-per-file boards,
disjoint artifact paths, and a single-authority manifest make concurrent
writes union cleanly. When a conflict does surface, its resolution was fixed
in advance — nothing is adjudicated on content, and no merge machinery is
involved: humans and agents apply the taxonomy with ordinary git.

**Never add `merge=union` gitattributes** (or any other automatic merge
driver) to a missions repo. Union merges corrupt frontmatter files, and this
taxonomy is a judgment doctrine, not a driver.

## The taxonomy

| Conflict on | Meaning | Resolution |
|---|---|---|
| `mission.md` | Authority violation by definition — only the authority edits the manifest | Authority's version wins verbatim; the other writer re-proposes as a task note (walkthrough A) |
| `backlog/config.yml` | Pinned-config drift or unauthorized tuning | Scaffold pins restored; for any other key, the authority's version wins |
| A task file | Two writers on one ticket | The task's **assignee's** version wins; the other writer re-proposes via a note on the merged task. When the conflict involves a reassignment or an authority restructuring act (reassignment and sweeps are restructuring), the **authority-side** version adjudicates instead; the displaced assignee's edits re-enter as a note |
| A task file modified on one side, moved or deleted by restructuring on the other | Modify/delete conflict (e.g. `cleanup` aged the task while an edit was in flight) | The move wins; the edit re-enters as a note on the moved/completed task |
| Duplicate task IDs after a union | **Silent — no git conflict:** two nodes each allocated the next sequential ID between syncs; the board lists one task while ID-addressed commands resolve to the other | The later-created task is renumbered by its creator (below); `mish status` surfaces the duplicate |
| An artifact path | Disjoint-path convention breached | Accidental collision: either writer renames theirs to a disjoint path and notes the board — no content adjudication |
| A whole mission dir | Same slug minted on two unsynced nodes (uniqueness is per-clone) — two missions interleaved under one path | Treated like the artifact-path breach: the later-pushed mission renames per walkthrough B; no content adjudication |

Renumbering a duplicate task ID: the later creator picks the next unused ID,
`git mv`s their task file under `backlog/tasks/` to that ID's filename, edits
the `id:` frontmatter to match, commits, and notes the change on the task.
Because the union is silent, run `mish status` after pulling — the duplicate
warning is the detection surface.

## Walkthrough A — authority conflict on `mission.md`

Mission `perf-regression`, authority `hera`. On node A, hera (the authority)
records a decision in `mission.md`. On node B, worker `toma` edits the Scope
section of the same file — an authority violation by definition, whatever the
intent. toma pushes first; hera pulls:

```bash
git -C "$MISSIONS_REPO" pull --no-rebase
# Auto-merging missions/perf-regression/mission.md
# CONFLICT (content): Merge conflict in missions/perf-regression/mission.md
```

The current authority resolves the merge; the authority's version is taken
**verbatim** — no combining with the violating edit. From hera's clone the
authority's version is the local (`--ours`) side:

```bash
git -C "$MISSIONS_REPO" checkout --ours missions/perf-regression/mission.md
git -C "$MISSIONS_REPO" add missions/perf-regression/mission.md
git -C "$MISSIONS_REPO" commit --no-edit
git -C "$MISSIONS_REPO" push
```

`--ours`/`--theirs` flip with merge direction — if the conflict surfaces on
the non-authority side first, the same rule holds with the sides swapped:
identify which side is the authority's (read the conflict hunks or
`git log --merge`) and take that side whole.

toma's change is not lost — it re-enters through the one channel non-authority
writers have, a note on their assigned task:

```bash
mish backlog --mission perf-regression task edit 12 \
  --comment 'Proposed scope addition (lost in mission.md merge): also cover the p99 tail on /search — toma'
```

(`--comment` appends; `--notes` *replaces* the whole notes field — the same
read-then-re-set caution as the references edge in `mish backlog --help`.)

The authority reads the proposal and, if accepted, makes the manifest edit
themselves.

One nuance: a `mission.md` conflict can also be the *authority's own* edits
from two nodes — authority is a label, not a single writer. The resolution
owner is the same (the current authority), but there the authority may choose
or combine versions freely; only non-authority content is restricted to
re-entering as a task note.

## Walkthrough B — rename, end to end

Renames are an authority act, not a CLI verb. Mission `perf-regression`
becomes `q3-perf-hunt`. Self-containment keeps the blast radius to exactly:
the dir name, two fields, and any markers.

```bash
cd "$MISSIONS_REPO"
git pull                          # authority habit before restructuring

# 1. pick the new slug (slug rules apply; see mish new --help) —
#    the target dir must not exist
test ! -e missions/q3-perf-hunt

# 2. move the dir
git mv missions/perf-regression missions/q3-perf-hunt

# 3. edit the two fields
#    mission.md frontmatter:      mission: q3-perf-hunt   (must equal the dir name)
#    backlog/config.yml:          project_name: q3-perf-hunt   (cosmetic)

# 4. fix or remove markers pointing at the old slug. Markers live in project
#    worktrees, usually untracked, so this step happens outside the missions
#    repo — stale ones fail loudly at resolution rather than misdirecting
echo q3-perf-hunt > ~/code/api/.mission

# 5. one rename custody commit covering the lot
git add -A missions/q3-perf-hunt
git commit -m 'mission(q3-perf-hunt): rename from perf-regression to q3-perf-hunt'
git push
```

Verify the rename landed clean:

```bash
cd ~/code/api
mish status          # resolves via the updated marker; no frontmatter/dirname warning
mish backlog board   # board intact, tasks unchanged
ls "$MISSIONS_REPO/missions/q3-perf-hunt/artifacts"   # artifacts intact

# the old slug survives only in history:
git -C "$MISSIONS_REPO" log --oneline --grep 'mission(perf-regression)'
```

The same procedure resolves the same-slug-on-two-nodes collision from the
taxonomy: the later-pushed mission renames; neither side's content is judged.

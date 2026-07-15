---
title: "Missions, artifacts, and node/hub topology — domain model"
date: 2026-07-02
status: exploration-sketch — NOT a spec, nothing herein is decided
purpose: fuller mechanism text behind the exploration corpus; superseded as working doc by
  docs/design/2026-07-02-sessions-missions-exploration-corpus.md (read that first — it carries
  the honest status, the review findings ledger, and the post-review re-thinks this doc lacks)
related:
  - docs/design/2026-07-02-session-management-architecture.md   # now read as REVEALED REQUIREMENTS, not intent
  - docs/plans/2026-07-01-002-feat-hcom-launch-substrate-plan.md # hcom launch substrate (in flight, feat/hcom-delivery-driver)
pending:
  - fold in settled hcom delivery-driver contract (§9) once the branch lands
---

# Missions & topology — domain model

> Draft for adversarial review (holes, elegance, YAGNI). Supersedes the architecture sections
> (§3c graph, §4 roles) of the session-management snapshot; that doc's pain inventory + prior-art
> survey remain valid as revealed requirements. §9 has marked PENDING fold-ins from the hcom build.
>
> **Superseded bus model:** §9's per-team ringfence is historical exploration only. Herder now
> spawns every agent on the node's global bus; mission attribution uses registry membership and tags.

## 0. Invariants (non-negotiable, everything else is)

1. **Escape hatches.** Bottom layer is plain files + git + terminals. Every layer degrades: any
   component dies → the layer below still works and is directly usable (cd, grep, git, ssh).
2. **Truth at the edge.** Sessions/work are owned where they run. Nothing central is
   system-of-record for live work.
3. **Derived things are disposable; durable things are named.** Every store declares its
   durability contract (§7). Never confuse an index with an archive.
4. **Usage-first ladder.** Build rungs that are independently valuable; each banks a primitive;
   none commits to the end-state. No component for a pain not yet felt.
5. **Env contract, not API.** Mission context propagates as environment (`MISSION_ID`,
   `MISSION_DIR`). Tool convention: *if `MISSION_DIR` set, write outputs there; else cwd.* Tools
   never talk to servers.

## 1. Revealed requirements (from usage, condensed)

- Agent comms flaky → hcom delivery drivers (in flight). Run-logs must stop being the comms substrate.
- Terminal = great control surface, bad observation surface → web surface over sessions/missions.
- Mission artifacts live as napkins pinned to worktrees → lose provenance, pollute git status,
  disconnect from sessions, no harvest/cleanup → **missions need a home separate from codebases**.
- Worktree-as-workspace is the wrong boundary → mission is the boundary object.
- Multi-machine/multi-account isolation → node/hub topology (§6).
- Free-form task state fails: agents invent schemas; invented schemas can't be visualized →
  standardize boards (Backlog.md) per mission.
- bottle unused (snapshot-as-entry-fee wrong) but its *durability* half was legitimate → archive (§7).
- Fast fork wanted (new pane, no agent round trip) — separate rung, not this doc.

## 2. Entities & authorities

One authority per entity. Cross-references by id only; no store duplicates another's data.

| Entity | What | Authority |
|---|---|---|
| **Codebase** | thing missions work ON | work repo's git. Missions reference `(repo, branch, sha)`; write only via harvest |
| **Session** | one agent conversation | harness JSONL (content); per-node registry/join table (identity) |
| **Mission** | unit of intent: N sessions, N runs, N worktrees, artifacts | missions repo: `missions/<dir>/` blobs + manifest/events |
| **Run** | one orchestrate execution | its playbook/run-log, stored in mission dir. mission:run = 1:N |
| **Napkin** | branch-local code scratch that must sit beside code | worktree, gitignored (shrinks to ~scratch scripts) |
| **Artifact** | mission output not destined for a work repo | mission dir |

**Boundary rules (anti-overlap):**
- Mission manifest = authority for mission↔session **membership**. Registry/join table = authority
  for session↔**live-handle** (name/pane/terminal). Cross-ref by `session_id`/guid.
- Product intent lives in the work repo's tracker; mission execution units live in the mission's
  backlog. One task, one home. Forward reference (→ mission id) always; backward reference
  (→ repo tracker item) detection-gated, any tracker (Backlog.md, GH issues, Linear).
- Workspace (herdr panes) = ephemeral surface arrangement. May be spawned FROM a mission
  ("resume mission" → arrange panes); missions never depend on workspaces.
- Transcripts never enter the missions repo. Missions reference sessions; archive stores them (§7).

## 3. Identity

- **Mission id**: `m-YYYYMMDD-NNN`, immutable, minted once. **Dir**: `<id>-<slug>`, slug freely
  renameable (`mission rename` = git mv + alias event). All references (commit trailers, backlog,
  events, hcom) use the id; lookup = glob `<id>-*`. Commit trailer: `Mission: m-20260702-001`
  (leaks nothing meaningful).
- **Session identity** (per hcom build, §9): registry row
  `guid ↔ label ↔ terminal_id ↔ team/hcom_dir ↔ hcom_name [↔ session_id — last column, pending]`,
  written at spawn (bus-bound from birth; SessionStart hook carries session_id — Claude and Codex).
- **Node dimension**: node = (machine × account); hcom names are further **bus-scoped** (team
  `HCOM_DIR`) → identity rows/events carry `node` (+ bus coordinate already in registry).
  `session_id` is a UUID (globally ok).

## 4. Mission dir layout & conventions

```
missions/<id>-<slug>/
  manifest.json       # mutable summary: id, slug, status, repos[{repo,branch,sha}], created
  events.jsonl        # append-only: session joins, artifact records, harvests, renames, node
  brief.md
  backlog/            # Backlog.md conventions — ALWAYS scaffolded (invariant: every mission has a board)
  runs/<run-slug>/    # playbook.md, run-log.md (thin decision journal — see below)
  artifacts/          # docs, analyses, screenshots (commit directly; partial clones keep laptops light)
  html/               # default output for html skills; served statically by hub
```

- `manifest.json` = last-writer-wins. `events.jsonl` = append-only, union-merge driver →
  multi-node writes structurally conflict-free.
- **Run-log = decision journal only.** Chatter → hcom messages. Evidence → by reference
  (`session_id` + offset) into archived transcripts. Command output does not get pasted in.
  (Also dissolves most of the secrets-in-synced-repo concern.)
- Backlog is the standardized unit schema: frontmatter parseable by the hub tailer → boards render
  in web surface without invented formats. Optional `product_ref:` field for backrefs.
- Orchestrate: repoints state files here; run without `MISSION_ID` in scope → auto-mint mission
  from run slug.

## 5. Lifecycle

- **Establish** (3 paths): `mission new <slug>` (explicit) · `mission adopt` (lazy — mints id,
  **moves** current napkins/scratch in, records current worktree+sha; the common case) ·
  auto-from-orchestrate.
- **States**: `active` → `archived` (+ physical delete). Minimal on purpose. Archived semantics
  beyond the flag: deferred.
- **Harvest = act, not state**, recorded as events (`harvested: <file> → <repo>/docs/...`).
  Repeatable. Pipeline: napkin → mission → durable docs; each promotion optional.
- **Default safe-to-delete** at every stage: unharvested archived mission dying loses nothing
  chosen to keep. Tip-delete keeps history; `git filter-repo` is the true-delete valve.
- Incomplete backlog units at mission end: die with mission or harvest into product tracker.

## 6. Topology: nodes + hub

**Node** = (machine × account): own `~/.claude`/`~/.codex`, herder registry, hcom bus, missions
clone, sessions. Fully functional offline. Two accounts on one box = two nodes.

**Hub** = provisionable HTTP service (initially co-located on the main persistent host, logically
separate): derived SQLite + web surface (SSE) + artifact static serving (own missions clone) +
drive gateway. **Rule: hub talks to its own machine's node exactly as it talks to remote nodes**
(same tailer protocol, same drive path) → "one server" MVP is the topology at N=1; adding boxes
changes nothing.

```
                 ┌── HUB (HTTP url; index disposable, archive durable) ──┐
                 │ SQLite(derived) · web+SSE · artifact serving · drive gw │
                 └───────▲───────────────────────────────┬───────────────┘
                meta+transcripts up (HTTP push)          │ drive down (ssh now; hcom relay later)
            NODE A          NODE B          NODE C ◄─────┘
         (box1/acct1)    (box1/acct2)      (box2)
            └────────────── git ──────────────┴──── GitHub origin (missions repo)
```

**Three flows, three transports:**
- **Meta up**: node daemon tails registry/events/backlog/liveness → HTTP push to hub. Append-only
  rows with ids → idempotent ingest; last-acked offset per stream → offline buffering trivial.
- **Blobs via git**: missions repo, GitHub private as origin (auth already solved everywhere; host
  is just another clone). Hub serves artifacts from its clone.
- **Drive down**: MVP = hub ssh (tailnet) → node's herder/hcom. Later = hcom relay
  (outbound-dial rendezvous; upstream hcom already ships `relay`). Transport swap, not topology
  change. Cross-node agent↔agent messaging: not wanted; hub-as-rendezvous name reserved, nothing built.

**Node daemon** (the one per-node install, alongside missions clone + hub token), three jobs:
1. tail meta up (registry, events, backlog, liveness)
2. ship transcripts to archive (§7)
3. missions git sync loop (§8)

## 7. Durability: four stores, four contracts

| Store | Contents | Contract |
|---|---|---|
| Node disk | live sessions, harness JSONL, registry | **working copy** — authoritative only while live; harness may GC |
| Missions repo (GH origin) | artifacts, manifests, events, boards | **durable** — git, multi-clone |
| **Archive** | raw session JSONL, keyed `<node>/<harness>/<session_id>.jsonl` | **durable** — the only durable home for transcripts |
| Hub SQLite | derived index over everything | **disposable** — rebuild by re-tail + re-clone |

- Archive v1: plain dir tree on hub machine, fed by node-daemon stream, backed up off-box
  (restic → B2/R2). zstd ~10× on JSONL → archive everything; retention policy deferred.
- **Admitted copy.** "Index, don't copy" was right about *entry-fee gating*, wrong about
  *durability*: metadata durability = git; content durability = archive (the legitimate half of
  bottle, kept). Sessions stop being ephemeral once the daemon runs.
- Hub machine hosts one disposable thing (SQLite) and one durable thing (archive dir). Never confuse.
- Archive write path: through hub HTTP ingest (one url, one token). Direct object-store push =
  rejected for v1 (second credential, more moving parts).
- Secrets: transcripts contain them → archive stays off-GitHub, host-local + encrypted backup.
  Synced missions repo carries decision journals + artifacts only (see §4 run-log rule).

## 8. Sync & freshness protocol

**Node daemon git loop**: fs-watch on missions clone (debounced) → `add -A; commit; pull --rebase;
push` + idle flush (~60s). Never force. Conflicts structurally rare (disjoint dirs, append-only
events, LWW manifest); retry on race.

**Hub freshness** — hub does not watch git for liveness:
1. Meta is realtime via tailer (seconds, independent of git). Git = durability/blobs channel.
2. **Events carry commit shas** → hub *knows* staleness (event sha vs clone HEAD), never guesses.
3. Pull triggers: GH webhook → hub url (push-driven) · lazy pull-on-request when surface requests
   an artifact whose event sha > checkout (correctness catch-all) · slow poll (fallback).
   Worst case: event visible instantly, body "syncing…" for seconds — never silently stale.

## 9. Session layer & hcom harmonisation — folded from `feat/hcom-delivery-driver` (2026-07-02 worktree state; branch still in flight)

**Settled contract** (per `skills/herder/references/delivery-drivers.md` + `spike-findings-hcom.md`):
- **Delivery drivers**: `resolve`/`send` behind `driver_dispatch`; exit codes 0 delivered/queued,
  1 transient, 2 refused, 64 usage; `--json` shapes byte-identical to legacy `herder-send` (goldens).
- **Selection**: `HERDER_BUS` = `auto`(default)/`herdr`/`hcom`. Auto: hcom on PATH + target resolves
  (`hcom list <name>`; `Not found` ⇒ fall back) → hcom; else herdr keystroke. **herdr fallback is
  never removed** — no-hcom machine behaves identically to today. → Invariant 0.1 satisfied by build.
- **Bus membership at birth**: launch THROUGH hcom via PATH shims → `hcom-launch` →
  `hcom <tool> --run-here --tag <role>`. hcom owns identity (`<role>-<random>`; name-pinning retired,
  W2). herder-spawn captures `team`/`hcom_dir`/`hcom_name`/`hcom_tag` into the registry.
- **Bus scope = team, not global**: `HCOM_DIR` pinned into child process env at spawn
  (`--team` → `$HERDER_TEAMS_ROOT/<name>`, else `~/.hcom`). Hard ringfence; config-dir passthrough
  keeps auth on the real per-tool dir.
- **Codex is a first-class hcom target** (live U5: mid-turn injection ~8.4s at posttooluse
  boundaries; 3-send burst coalesced atomically, ordered, zero drops; `deliver:` event = recorded ack).

**Consequences for this model:**
- Identity row becomes `(node, hcom_dir/team, hcom_name)` — names are **bus-scoped**, not
  node-scoped. §3 node dimension refines to node×bus. Registry already carries the bus coordinate.
- **Mission↔team alignment (open design choice):** `HERDER_TEAM=<mission-id>` would give
  per-mission buses natively — message streams partition by mission with zero tagging, and
  `hcom_dir` in events is mission provenance for free. Cost: standing/cross-mission agents need a
  shared or global bus; a mission-scoped bus can't reach them. Options: (a) mission = team,
  standing agents on global bus, cross-bus send via explicit `HCOM_DIR`; (b) global bus +
  `MISSION_ID` tag on messages. Decide at v1 step 2. Leaning (a) for ringfencing.
- **Still pending in branch**: `session_id` capture into the registry (hcom sees `sessionstart`
  hooks; wiring the id into the registry row is not yet evidenced) — this is the last join-table
  column; W3 (resolve guid/label via registry bus coordinate instead of probe) in flight.
- Hub tailer consumes the registry as-is (`guid ↔ label ↔ terminal_id ↔ team/hcom_name [+
  session_id when wired]`) — no new session store invented.

## 10. Explicitly deferred / dead (YAGNI ledger)

- **Search/index over sessions**: no felt pain; arrives later as byproduct of hub tailer + archive.
- **Pin/label metadata layer**: no felt pain; deferred.
- **bottle**: dead. Fork-verb rung replaces live use; archive replaces durability half;
  lineage → session layer when needed.
- **Cross-harness decant / canonical IR (casr)**: deferred until wanted in anger.
- **Cloudflare Artifacts**: shape-compatible future backend (repo-per-mission, git-clonable);
  closed beta, no viewer → design so migration = `git push` per mission. Track, don't adopt.
- **Cross-node agent messaging**: reserved name only.
- **herderd daemon**: the node daemon is NOT herder; it's dumb infra (tail/ship/sync). Coordination
  stays in skills.

## 11. Minimal v1 (build order)

1. `~/missions` clone + GH private origin; `mission new|adopt|rename` helpers; id scheme (§3);
   scaffold backlog/ always.
2. Env contract in herder-spawn/orchestrate (`MISSION_ID`/`MISSION_DIR`); orchestrate state files →
   mission dir; html skills default output → `html/`; provenance events on spawn.
3. Node daemon v1: git sync loop + events tail → hub ingest (single binary/script, systemd/launchd).
4. Hub v1 on main host: ingest → SQLite; static artifact serving; minimal web list (missions →
   board, artifacts, sessions, message stream when §9 lands).
5. Archive: daemon ships transcripts → hub dir; restic backup.

Each rung independently valuable; 1–2 useful with no daemon/hub at all.

## Unresolved questions

1. Archive ingest format: raw JSONL passthrough only, or normalize-at-edge too (per-harness
   adapters) so hub SQLite can index content? (Lean: raw first; adapters when the surface needs
   transcript rendering.)
2. Retention/GC for archive + missions repo growth (partial clones mitigate; policy TBD).
3. `mission adopt` sweep edge cases: scratch that code depends on in-place (symlink back? leave?).
4. Hub auth for web surface (tailnet-only vs token) — decides how shareable "multiplayer = send a URL" is.
5. Archived-state mechanics (move to `archive/`, hide in surface, partial-clone exclude) — deferred.
6. Mission↔team bus mapping (§9): mission=team ringfence vs global bus + message tags. Leaning
   mission=team; decide at v1 step 2.
7. `session_id` capture into the registry (§9): confirm wiring lands in the branch (it's the last
   join-table column; everything downstream assumes it).

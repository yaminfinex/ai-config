---
title: "Missions scenario pack — usage walkthroughs as adjudicator"
date: 2026-07-02
updated: 2026-07-08 (realigned to herder Go port + herder-spec draft; author feedback round 1
  recorded same day)
status: FEEDBACK ROUND 1 RECORDED — author reviewed v2 and gave direction on all scenarios +
  docket; a proposed boundary shape is drafted in
  docs/design/2026-07-08-sessions-missions-boundaries.md (brainstorm draft, not doctrine).
  Still in brainstorm mode; grilling/discussion ongoing.
purpose: the UX-grill deliverable from the exploration corpus — 15 concrete walkthroughs traced
  through the idea inventory; decisions get adjudicated by walkthrough, not philosophy
related:
  - docs/design/2026-07-08-sessions-missions-boundaries.md            # ← the live doctrine distilled from adjudication
  - docs/design/2026-07-02-sessions-missions-exploration-corpus.md   # the corpus this executes §5 of
  - docs/design/2026-07-02-missions-topology-domain-model.md          # mechanism text for ideas #1–#17
  - docs/design/2026-07-02-session-management-architecture.md         # revealed requirements + prior art
  - docs/specs/herder-spec.md (herder-spec branch)                    # herder domain model, awaiting ratification
---

# Scenario pack: sessions, missions, topology

> **How to use.** Each scenario is a concrete story grounded in a §1 pain from the corpus. The
> **trace** walks it through the idea inventory (#N refs = corpus §3). **Breaks** are beats where
> an idea fails, is unspecified, or two ideas conflict. **Adjudicates** names the decision(s) the
> scenario forces and what the walkthrough says about them. Idea numbering follows the corpus.
>
> **Validation pass required first:** these are an agent's reconstruction of the author's usage.
> Strike scenarios that don't match reality, edit beats that ring false, add missing ones. Only
> then do the adjudications count.

Legend: `[seed]` = one of the seven scenarios named in the corpus grilling agenda. `⟳ 07-08` =
realignment note — a fact that changed between drafting (07-02) and now.

---

## Author feedback round 1 — 2026-07-08

The author reviewed the full pack and gave direction (not final rulings — still brainstorm
mode). A proposed boundary shape distilling it lives in
**`docs/design/2026-07-08-sessions-missions-boundaries.md`** (brainstorm draft). Compressed
record:

- **The headline correction:** the exploration had entangled three independent tools into one
  stack. Direction: session ingestion (visibility, keyed on tool session ids, **never tied to
  herder**), missions ("better napkins" — self-contained, relying only on its own CLI + skill),
  and herder (experimental C2, composes with both, nothing depends on it) are separate
  components with upward-only optional joins. How any of this fits a hub concept is explicitly
  unsettled.
- **Scenario verdicts:** S1 confirm (pending the mission↔herder dependency answer — now given
  in the boundaries doc); S2 kill-test **NO** (napkins covers missionless scratch); S3
  **struck** (orchestrate goes mission-native; agents move files themselves); S4 low-relevance
  (read-only recovery fine; remote decant later); S5 confirmed (hub-for-multinode acceptable);
  S6 reframed (nodes are server-ish, reachability moot; drive via node-registered tunnel, not
  hub-ssh); S7 corrected (missions span nodes; **one shared missions repo**, env-var
  configured); S8 simplified (harvest = event logging, nothing more); S9 kill-test **NO** on
  search / **NO** on bottling — visibility is the real use case; S10 done; S12 confirmed; S13
  confirmed (direct naming works, proven in use); S14 corrected (evidence refs must not
  require herder — key on tool sids); S15 corrected (multi-node mission writes are normal;
  only manifest mutation needs a single authority).
- **Docket:** D1 agree (narrowed to one *manifest* authority); D2 agree (guid membership =
  herder-run missions only); D3 **deprecate bottle** (replace orchestrator refs with named
  registry sessions + herder spawn); D4 board per mission always, item moves in prose; D5
  cross-machine durability deferred; D6/D7 agree; D8 adopt demoted to "agent just does it";
  D9 no tagging needed; D10 observation first; D11 reversed — shared repo; D12 harvest is
  logging.

---

## Realignment 2026-07-08 — read this first

The pack was drafted 2026-07-02 against the corpus's ground truth. Six days of herder work
(the Go port + a spec branch) moved that ground. Three strata now, and every scenario trace
below is annotated against them:

**Shipped on main** (tools/herder, Go; skills/herder + herder-fork retired, doctrine moved to
CLI help + orchestrate):

- **hcom is THE send transport** — keystroke ring deleted; every `herder send` resolves through
  the registry, delivers on the bus, and polls for a `deliver:` receipt (delivered/queued; no
  bus-bound row → refuse, never type). Spawn prompts also ride the bus, gated on a
  child-specific bind signal. The corpus's "in flight, nearly done" hcom branch landed and went
  further than sketched.
- **Tool session_id capture is BUILT** — the corpus's spike candidate #1 (#14, "unbuilt,
  everything leans on it") shipped: the sidecar enriches `provenance.tool_session_id` from the
  child's own hcom roster row, pane-correlated, with a refuse-to-guess doctrine (ambiguous
  correlate → record nothing rather than the wrong thing).
- **Lifecycle verbs exist**: `fork` (+ `--self`, `--split`), `resume` (same guid, provenance
  `resumed_at`), `enroll` (adopt the current pane; retires stale rows), `compact '<steer>'`
  (self-only) and `compact --then '<continuation>'` — compact-then-continue past the context
  ceiling, claude-only. Idea #15 (fast fork) is done, not pending.
- **Backlog.md is bootstrapped at the repo root** as the durable cross-machine work ledger
  (git-tracked `backlog/`, 38 tasks and counting), with live doctrine: run-label ringfencing,
  units seeded on the base branch, and **the board lives on the base branch with the
  orchestrator as its only writer** — workers report on the bus, never touch the board.
- **Bottle is alive and maintained** (updated 07-08; decant is a working, central path).
  This contradicts the corpus's §1 ground truth ("bottle unused") and idea #18 ("deprecate").
  Author adjudication needed — see the docket.
- **Worktree spawn**: `spawn --worktree BRANCH` creates the worktree, spawns into it, records
  `workspace_id`/`branch` in provenance.

**Spec'd, awaiting ratification** (docs/specs/herder-spec.md on the herder-spec branch —
the domain-model change the pack must now align with):

- **Herder mints its own durable session identity: the guid.** One guid = one linear
  transcript, never deleted, tool-agnostic. The tool's session id (**sid**) is demoted to a
  mutable, late-arriving *attribute* (append-only `sids[]` history), with epistemic honesty:
  `continuity: confirmed | assumed`. Fork//clear/rewind mint a new guid with a parent edge.
- **Seats and labels**: seat = where a session runs (liveness); label = a non-expiring
  addressing lease, released only by retire or explicit transfer. States:
  seated → unseated → retired/lost; cull ≠ retire; double-seat refused; turnover (`/clear`)
  detected by the sidecar (sid changed in my seat ⇒ new session, eviction record).
- **Node = (machine, user)** with a lazily-minted `node_id` living beside the registry;
  write-authority gate (only the owning node writes); **backup ≠ sync** — the registry must
  never sit under bidirectional file sync; unknown-node rows load as read-only anomalies.
- **Teams are dropped.** One hcom namespace per node (minted `namespace_id` anchored in the
  HCOM_DIR); per-run traffic grouping uses hcom **tags**, not separate buses.
- **Non-goals that bless this corpus's lane**: multi-machine behaviour is explicitly deferred
  to "a future central-orchestrator/server design, not registry shipping or bus relay" — i.e.
  the hub exploration (#8/#9) is the named successor, with constraints (tail/derive, never sync
  registries). An **observation tier** (read-only viewport on a session) is named as a future
  third concept — the corpus's web-surface idea (#11) in embryo.

**Still ideas** (unchanged by the week): missions as an entity, mission dirs/CLI, archive of
transcripts, hub/daemon, web surface, harvest. Herder now overlaps none of them — but it
redefined the identity layer they join on.

**The single biggest consequence for this pack:** mission membership should key on the
**herder guid**, not the raw tool session_id. The guid is durable, tool-agnostic, survives sid
churn and turnover, and already exists — the corpus's "session_id capture" join dependency is
not just built, it's superseded by a better key. Scenario notes below apply this.

---

## S1 · Bug-poke becomes a mission `[seed]`

**Pain:** worktree-as-wrong-boundary; loose artifacts lose provenance.
**Ideas:** #1 #2 #3 #4 #5 #14 #17

**Trace.** You cd into a worktree of repo X, spawn one claude, poke at a flaky test. 40 minutes
in it's a real investigation: three hypotheses in conversation, a repro script and half an
analysis in `napkins/`. You run `mission adopt`. It mints `m-20260702-<rand>` (#4), creates
`~/missions/<id>-flaky-test/`, **moves** the napkin contents in, leaves symlinks behind (#5),
records `(repo, branch, sha)` in the manifest, drops a `.mission` marker in the worktree (#17).
The running agent keeps referencing `napkins/repro.sh` by its old path — symlink keeps it alive.
From here `mission log` / `mission artifact` resolve context from the marker (#3).

**Breaks.**
- The already-running session can't get `MISSION_ID` env injected (#17's own caveat: ambient env
  is spawn-time only). Marker-file cwd-resolution is the *only* mechanism that works here — env
  contract is a nice-to-have for spawned agents, not the foundation.
  `⟳ 07-08` The herder spec adopted the same doctrine at the session layer (invariant:
  "env-carried identity is birth provenance only; the registry is the sole authority") — this
  adjudication is now vindicated by shipped precedent, not just argued.
- Membership: recording *this session* as a mission member needs its identity. `⟳ 07-08`
  Largely dissolved: the herder registry now holds guid + captured tool sid for spawned agents,
  and `herder enroll` adopts an unmanaged occupant in place (PATH shims will catch hand-typed
  launches too, per spec). "Who is live in this worktree" is a registry query. What remains
  mission-side: `mission adopt` records the answering **guids** as members — a join row, not a
  capture mechanism.
- "Notify members over hcom" (#5): at adopt time the only member is the notifier. No-op in the
  common case — fine, but the sketch implies more machinery than the scenario needs.

**Adjudicates.**
- Marker-file resolution > env contract as the primary context mechanism (demotes half of #17).
  `⟳ 07-08` Strengthened — matches ratification-pending herder doctrine.
- ~~Adopt must define membership-backfill~~ `⟳ 07-08` Reframed: membership = mission-side join
  rows keyed on **herder guid**; backfill = registry query at adopt time. #14 as spec'd in the
  corpus is superseded.
- Lazy adopt with zero pre-arrangement is the make-or-break ergonomic; if adopt requires any
  setup, it inherits bottle's snapshot-as-entry-fee failure.

**Usage signal.** Count adopts vs `mission new` in the first month. If adopts dominate (expected),
the lazy path is the product; polish it first.

---

## S2 · Quick question that never becomes a mission

**Pain:** boundary question the corpus flags on #1 — "when is something a mission?"
**Ideas:** #1 (negatively) #10 #17

**Trace.** You open a claude in `~/dotfiles`, ask one question, get an answer, close it. Five
minutes. No mission is ever created; nothing scaffolds; no board exists. The transcript still
lands in the archive (#10 archives everything), so if this five-minute answer turns out to matter
next month, S9's search path can find it.
`⟳ 07-08` With PATH shims active (spec §4; staged separately), even this hand-typed session
gets a guid and registry row — identity for free, mission still rightly absent.

**Breaks.**
- If the agent emits an artifact (a quick HTML chart), the convention says "no `MISSION_DIR` →
  write cwd" (#17) — which is exactly the loose-files pollution pain the whole design exists to
  fix. Missionless artifacts have no home; the floor case regresses to today.

**Adjudicates.**
- Missions must be strictly opt-in; the missionless path must cost zero. Any design where every
  session needs a mission fails this scenario.
- Answers #1's boundary question by inversion: a mission earns existence when **a second thing**
  (second session, second artifact, a task list, or a tomorrow) needs the shared home. One
  session + zero keepable outputs = never a mission.
- Forces a decision the inventory lacks: a default scratch home for missionless artifacts
  (candidates: cwd-as-today, `~/missions/_scratch/`, or "adopt is so cheap you just adopt").

---

## S3 · Adopt mid-run `[seed]`

**Pain:** orchestrate state pinned to worktrees; §4 finding: live runs bake literal playbook paths.
**Ideas:** #5 #6 #7 #16 — vs §4 orchestrate-migration findings

**Trace.** An orchestrate run is live: `napkins/orchestrate/playbook.md` + `run-log.md`, three
phase agents whose briefs contain those literal paths. Mid-run you adopt. Files move to
`~/missions/<id>/runs/<slug>/`; symlinks left at the old paths keep the agents' baked references
working; the Backlog probe (cwd-relative, per §4) still resolves via the symlinked dir.

**Breaks.**
- File-handle edge: an agent holding an open fd on `run-log.md` keeps appending to the moved
  inode — fine same-volume, broken cross-volume. `~/missions` on an external/second volume
  silently corrupts the run. Unstated constraint: **same-filesystem or refuse**.
- The run's *own* mission-awareness: the playbook was written missionless; nothing tells the
  orchestrator "you now have a mission id" mid-flight. Provenance events for already-spawned
  agents are backfill again (same hole as S1, larger blast radius).
- Repo-level Backlog integration (#6 collision, un-reconciled): if the run was using the repo's
  board, adopt now has two boards in scope.
  `⟳ 07-08` This collision got much sharper. Backlog.md is no longer "detection-gated
  integration" — it's a live, git-tracked, repo-root ledger with doctrine: **the board lives on
  the base branch and the orchestrator is its only writer**; units are seeded before fan-out;
  workers report on the bus. A per-mission board (#6) now has a real incumbent to reconcile
  with, not a hypothetical. Options sharpen to: (a) mission board is the *same* Backlog.md
  living in the mission dir for mission-scoped work, repo board for repo-scoped work — one
  task one home; (b) missions reference repo-board units by id and own no board. The old
  "always scaffold a board per mission" default needs re-arguing against (b).

**Adjudicates.**
- Cheapest viable rule: **adopt at run boundaries only; refuse (or warn hard) mid-run** in v1.
  Symlinks make mid-run adoption *mostly* work, and "mostly" is how runs get corrupted. This
  matches value 3 (incremental) and defers the hardest edges of #5.
- If mid-run adopt is ever supported, same-volume check + orchestrator notification are the
  minimum contract.
- `⟳ 07-08` New: mission-board-vs-repo-board must be adjudicated against the shipped
  one-writer doctrine (added to docket).

---

## S4 · Laptop dies Tuesday `[seed]`

**Pain:** dead-laptop session loss; archive durability contract honesty (§4 finding).
**Ideas:** #9 #10 #14 #15 — and the YAGNI-deferred decant

**Trace.** Monday you work on the laptop node: two missions touched, six sessions. Tuesday the
laptop is dead. What survives: mission events/artifacts shipped by the daemon stream (#9) up to
its last ack; transcripts shipped to the archive (#10) up to the same point; the hub's
materialized tree + git journal hold Monday's state. From the desktop you open the hub surface,
read Monday's transcripts, see the mission boards exactly as last shipped.

**Breaks.**
- **You can read Monday's sessions; you cannot continue them.** Resuming needs harness-native
  state on a living machine — that's cross-machine decant, explicitly deferred (YAGNI ledger).
  The archive's honest v1 contract is *read-only recovery*, and nothing in the corpus says that
  plainly.
  `⟳ 07-08` Softened, not reversed: **bottle is alive** and `bottle decant` (copy a frozen
  transcript into a fresh session) is a working path today, Claude-side. Archived JSONL + decant
  ≈ continuation on another machine. Still right to *contract* v1 as read-only recovery — but
  the "continuation is far-future" framing is stale; the mechanism half-exists.
- The loss window is "since the daemon's last ship" — and per §4, daemon liveness is invisible.
  If the daemon died Sunday and you didn't notice, Tuesday's recovery is two days stale. RPO is
  meaningless without daemon-liveness surfacing (hub shows per-node last-seen).
- Anything not yet in the mission dir (un-adopted napkins, uncommitted worktree changes) is
  simply gone — correct per the design, worth stating as the incentive to adopt early.
- `⟳ 07-08` New constraint from the spec: the herder registry is node-local with a
  **backup-not-sync** doctrine and an owning-node write gate. The daemon (#9) may *tail* it
  (read-only, ship derived events) but must never sync the file itself; a hub must never write
  into node registries (unknown-node rows are read-only anomalies by spec). The corpus's
  daemon design was already tail-shaped — now it's a hard boundary, not a preference.

**Adjudicates.**
- Write the archive contract as: "transcript lines durable within N minutes of write, survive
  loss of any one machine, **read-only recovery** (continuation via decant = explicit later
  rung)". Test mechanisms against that sentence — this is the durability grill's input.
- Node liveness display is not optional polish; it's what makes the durability promise checkable.

---

## S5 · Hub-down day `[seed]`

**Pain:** escape hatches (value 1); #9's hub-mediation has never been attacked.
**Ideas:** #9 #10 #11 #12 — vs invariant "truth at the edge"

**Trace.** The hub host is down all day. On your laptop: sessions run, missions CLI works
(local clone), boards edit, orchestrate runs — the node is fully functional (truth at the edge
holds). The daemon buffers events/transcripts at its last-acked offset; when the hub returns,
everything ships. What's lost meanwhile: the web observation surface, drive-from-hub, archive
ingest (buffered, not lost), and — under #9 — **all cross-node convergence**, since the hub is
the only rendezvous.

**Breaks.**
- Under the retired git-as-bus shape, GitHub was the rendezvous and nodes converged hub-lessly.
  #9 traded that away. Hub-down means the desktop cannot see the laptop's afternoon at all. Is
  that acceptable? Probably yes *if* multi-node-same-day is rare (see S15) — but that's exactly
  the unreviewed assumption.
- Escape-hatch check: with the hub process down but its host up, can you still get at everything
  with ssh + grep + git? Only if the hub's materialized tree is a **plain browsable dir + real
  git repo** on disk, not internal state. This must be a stated requirement of #9, not an
  accident.

**Adjudicates.**
- #9's degraded mode is tolerable only with: (a) offset-buffered ship (already sketched),
  (b) hub tree = plain files + git (make explicit), (c) honest answer on multi-node concurrency
  frequency (S15). This is the sync grill's opening position.
- `⟳ 07-08` The spec *blesses this lane with constraints*: multi-machine is deferred to "a
  future central-orchestrator/server design, **not registry shipping or bus relay**". The hub
  exploration is that named successor — and the two rejected transports are now recorded
  decisions to honor, not options to revisit.

---

## S6 · Phone view and nudge `[seed]`

**Pain:** terminal = bad observation surface; value 5 (single working surface).
**Ideas:** #11 #12 #13 #14

**Trace.** Waiting for coffee, you open the hub URL on your phone. Missions list → the active
mission's board → live session stream (SSE off the daemon tail). One agent has been sitting on a
clarifying question for 20 minutes. You type a one-line reply; the hub drive gateway executes
`herder send` locally (target is on the always-on host) or over ssh (target is on the desktop).
The `deliver:` receipt comes back through the stream as your ack.
`⟳ 07-08` The drive primitive got materially stronger: sends are bus-only, registry-resolved,
and **receipt-verified on main today** (delivered/queued/refused — never keystrokes, never
guessing). The gateway's job shrank to "exec the CLI where the node is".

**Breaks.**
- Auth is blank (unresolved Q4): tailnet-only means the phone must be on the tailnet; token
  means auth design. Decides whether "multiplayer = send a URL" is real.
- Sleeping/roaming laptop: hub-ssh drive requires an inbound-reachable node. The omnara lesson
  (outbound-dial rendezvous) says the phone→sleeping-laptop nudge is **structurally out of scope
  until a relay exists** — and the spec now records "hcom relay is unused and unmodelled", so
  no relay is coming implicitly. v1 drive honestly covers: hub-host-local agents +
  wake-reachable ssh nodes.
- Attribution of your nudge in the mission record depends on the membership join (guid-keyed,
  per S1 `⟳`).

**Adjudicates.**
- Value 5 is satisfiable in v1 only for reachable nodes — state the boundary rather than imply
  phone-drives-everything.
- The *observe* half (board + stream, read-only) is independently valuable and much earlier than
  the *drive* half. Split #11 and #12 into separate rungs; don't gate the board on the gateway.
  `⟳ 07-08` Vindicated: the spec independently names an "observation tier" (read-only viewport,
  never a seat) as its own future concept — same split, derived separately.

---

## S7 · Two accounts, one box `[seed]`

**Pain:** machine/account isolation.
**Ideas:** #8 #13 — node = (machine × account)

**Trace.** Work and personal accounts on one Mac. Two nodes: separate `~/.claude`, separate
registries, separate hcom buses, separate daemons, each with its own hub token. The hub sees two
nodes and renders them distinctly. A mission belongs to whichever node's missions clone minted
it; ids are rand-suffixed (#4) so parallel minting never collides.
`⟳ 07-08` The spec formalized exactly this: **node = (machine, user)** with a lazily-minted
`node_id` that travels with the home dir, an owning-node write gate, and **namespace identity**
(a minted `namespace_id` anchored inside each HCOM_DIR — two `~/.hcom` paths on two accounts
are two universes despite identical paths). The corpus's node concept (#8) is now half-real:
the bookkeeping tier shipped into the spec; only the hub tier remains an idea.

**Breaks.**
- **One missions repo or two?** If both accounts push to one GH missions repo, the personal
  account holds a credential to the work mission corpus (and vice versa) — an isolation leak that
  contradicts the reason accounts are separate. If two repos, the hub aggregates across origins —
  fine for #9 (hub materializes per-origin trees) but never stated.
- Two daemons on one box: port/socket/launchd-label collisions are mundane but real; the daemon
  contract needs per-node addressing baked in from v1, or the N=1 shortcut hardens into "one
  daemon per machine".
- Cross-account sends on one box: buses are account-scoped; ~~the global-bus assumption in #13
  is really *per-node*-global~~ `⟳ 07-08` now literally the spec's posture: **teams dropped,
  one namespace per node**, per-run grouping via hcom tags. The pack's "rename global bus →
  node bus" adjudication landed as spec language before anyone read it — treat as settled.

**Adjudicates.**
- Missions-repo-per-account (isolation wins; hub aggregates). Make it explicit.
- ~~"Global bus" → "node bus" terminology fix~~ `⟳ 07-08` Done by the spec; cross-node =
  cross-account messaging stays not-wanted.

---

## S8 · Mission-end harvest `[seed]`

**Pain:** no harvest/cleanup; loose files outlive their usefulness.
**Ideas:** #6 #16 — lifecycle end

**Trace.** The investigation concludes. `mission harvest artifacts/analysis.md` copies it to the
work repo's `docs/solutions/` and appends a harvest event (source, target repo+path+sha). The
board has two open units: one matters long-term → harvested into the repo tracker; one dies with
the mission. `mission archive` flips status; the dir stays greppable; some week later a tip-delete
removes it, history retained, `git filter-repo` as the true-delete valve.

**Breaks.**
- Copy vs move: safe-to-delete default implies the mission stays self-contained until deleted →
  harvest = **copy + event**, never move. The corpus never says which; the walkthrough forces copy.
- Idempotency/provenance (#16's flagged thinness): re-harvest after editing the mission copy —
  overwrite? diff? And if the work repo later rebases, the recorded sha dangles — acceptable
  (event is a journal, not a live pointer), but say so.
- Backlog-unit promotion into the repo tracker re-opens the #6 collision — same un-reconciled
  seam as S3. `⟳ 07-08` Now with a concrete target: the repo's real `backlog/` ledger with its
  one-writer doctrine. Promotion = the mission side *drafts*, the repo-board owner applies —
  pleasingly, the same shape as orchestrate's "workers report, orchestrator writes the board"
  and the upstream-ticket ledger's "agents draft, never file". One doctrine, three instances.

**Adjudicates.**
- Harvest = copy + append event; mission remains self-contained until deleted.
- Archived = status flag + surface hiding only, v1. Physical archiving deferred (matches
  unresolved Q5) — this scenario shows the flag alone is enough to end the lifecycle cleanly.

---

## S9 · Find that session from a month ago (kill-test)

**Pain:** *claimed* — this is bottle's original promise and the YAGNI ledger says search has
"no felt pain". Included deliberately as a kill-test.
**Ideas:** #10 #14 #15 #18

**Trace.** "What did I decide about X in June?" Hub search over archived transcripts → hit on a
session from the (since-wiped) laptop node. Read it in the surface. If its origin node were alive
and the JSONL un-GC'd, resume natively there (drive path); otherwise read-only (S4's contract).
`⟳ 07-08` Local-history floor improved without any mission work: the herder registry is
append-only with a rotate-never-delete stance and `list --all`; sessions are "never deleted"
as *identities* (guid + label + lineage survive) even when transcripts die. Finding *that a
session existed* and *what seat/label/lineage it had* is now free; finding *what it said*
still needs the archive (#10).

**Breaks.**
- If this pain isn't real — if in a month of archive-everything you never once search it — then
  the archive's *query* half (derived SQLite over content, per-harness adapters, unresolved Q1)
  is dead weight, and archive scope shrinks to "restic for transcripts": dumb ship + compress +
  encrypt, no index. That's a dramatically smaller #10.
- `⟳ 07-08` **The bottle contradiction cuts here.** The corpus's ground truth says "bottle
  unused" and #18 says deprecate — but bottle was updated as recently as 07-08 and decant is
  live. If you've in fact been bottling sessions this week, the durability/re-entry pain is
  *felt*, and this kill-test's premise flips: the question stops being "would you ever search?"
  and becomes "is bottle's snapshot model the right shape, or the archive's index-everything?"
  Author input required.

**Adjudicates.**
- Ship the archive **without** content indexing first (raw passthrough — unresolved Q1's lean).
  Let this scenario's frequency in real usage decide whether query ever gets built. This is the
  motivation grill's cheapest-alternative answer for #10.
- `⟳ 07-08` New: adjudicate #18 (deprecate bottle) against actual current bottle usage — the
  corpus's ground-truth claim is contradicted by the repo record.

---

## S10 · Fork at the crossroads

**Pain:** bottle unused; fork/branch would be used if instant.
**Ideas:** #15 #18 #14

`⟳ 07-08` **This scenario moved from "design it" to "validate it": the fork rung shipped.**

**Trace.** Deep in a session, two plausible directions. `herder fork --self --split right` —
instant, launcher-level, no agent round-trip, no snapshot ceremony. The child gets a new guid
with `forked_from` provenance; the registry carries the lineage edge; codex forks get their
doctrine addendum re-delivered over the bus. You run both directions side by side; the loser
gets culled (unseated, resumable — cull ≠ retire per spec); the winner continues. `resume`
re-seats the same guid later; the spec's double-seat refusal prevents transcript interleaving.

**Breaks.**
- ~~The child mints a new session_id → join race~~ `⟳ 07-08` Addressed structurally: identity
  is the herder guid (minted at registration, before any sid exists); the sid arrives late and
  attaches via correlate-gated enrichment with `continuity: assumed` until confirmed. The race
  the 07-02 architecture snapshot worried about is now a modeled state, not a bug.
- ~~Lineage: v1 needs a parent column~~ `⟳ 07-08` Exists (`forked_from`/`cleared_from`, ≤1
  parent edge per session, spec-invariant). Lineage-root keying for durable metadata remains
  future — but the graph it would hang on is real.

**Adjudicates.**
- ~~Fork is a self-contained rung… strong candidate for "buildable now"~~ `⟳ 07-08` Built.
  Remaining adjudication is #18 only — and that flipped (see S9): bottle was NOT deprecated
  when fork shipped; the two coexist today. Either bottle's remaining value is real (snapshot
  as *durable named artifact*, distinct from fork's live branching) — which reshapes #10's
  archive scope — or it's inertia. Author call.

---

## S11 · Monday-morning orientation

**Pain:** "lost in workspaces"; orientation loss across panes/worktrees.
**Ideas:** #2 #3 #11 — and the incremental-path grill

**Trace.** Monday 9am. Today's reality: scan herdr workspaces, walk panes, `git status` in four
worktrees, reconstruct what Friday-you was doing. Proposed floor (no hub needed): `mission list`
reads the local clone + registry — active missions, last event per mission, live sessions per
mission, board summary. Proposed ceiling: the hub page shows the same across all nodes with
liveness.
`⟳ 07-08` The registry side of this got real: `herder list` reconciles rows against live herdr
state today, and rows carry provenance (`cwd`, `workspace_id`, `branch`, `spawned_by`, tool
sid). A `mission list` could *join registry rows by cwd/worktree to missions* with zero new
capture machinery. The spec adds label/seat/liveness/`continuity` columns.

**Breaks.**
- Orientation quality is only as good as event hygiene: if sessions don't join missions and
  events aren't emitted at spawn/adopt, `mission list` shows stale nothing.
  `⟳ 07-08` Half-resolved: the capture side exists (provenance at spawn); only the
  mission-join events are missing — and they're mission-CLI work, not herder work.

**Adjudicates.**
- ~~"hcom lands first" sequencing~~ `⟳ 07-08` Satisfied — hcom landed, herder rewrote on top,
  the churn the corpus warned about (herder-spawn rewritten by both efforts) is over. The
  mission CLI + dir format rung is now **unambiguously buildable**: its substrate (registry,
  provenance, guid identity) shipped and its spec is stabilizing. The corpus's "only unit
  buildable now" intuition is stronger than when written — with one addition: build it
  **guid-native** from day one.

---

## S12 · HTML artifact review loop

**Pain:** no home for HTML artifacts; terminal can't render them.
**Ideas:** #2 #3 #11 #17

**Trace.** An agent produces `perf-report.html`. With mission context resolved (marker/env), the
html skill writes `~/missions/<id>/html/`. Pre-hub: `open ~/missions/<id>/html/perf-report.html`
— already better than fishing it out of a worktree. Post-hub: the daemon ships it, hub serves it
statically, the URL works on the phone (S6), regeneration shows up on refresh.

**Breaks.**
- Skills today write cwd; each html-emitting skill needs the convention wired. Bare-convention
  (`if MISSION_DIR…`) scatters the rule across every skill; a `mission artifact <file>` verb (#3)
  centralizes it — skills call one command, the CLI decides placement and emits the event.
- Artifact versioning: git history of the missions repo is the answer; regenerated reports are
  commits. Adequate; no extra machinery.

**Adjudicates.**
- Prefer the CLI verb over bare convention: one integration point, provenance event for free.
  This is the strongest single justification for #3's existence.

---

## S13 · Standing agent across missions

**Pain:** §4 coherence finding — "standing agents" undefined; #13's broadcast semantics sketched.
**Ideas:** #13 #14 #1

`⟳ 07-08` The substrate question underneath this scenario got answered by reality: **teams are
dropped** (spec) — one bus per node, addressing via registry-resolved names, grouping via hcom
**tags**. The corpus's #13 debate (per-mission team buses vs global bus + membership join) is
settled in favor of the single bus; what remains is purely the mission-attribution design.

**Trace.** A long-lived triage agent runs on the node bus, not belonging to any mission. Mission
M's `mission send` expands M's members (guids → registry → bus names) — the triage agent isn't
one, so it isn't spammed: correct. You message it directly by label; it answers; the exchange
appears in the node stream.

**Breaks.**
- Attribution: the membership join maps messages → missions via sender/recipient membership.
  The triage agent's messages attribute to **no** mission — the mission-M conversation it
  participated in is invisible from M's surface view. Either membership becomes many-to-many
  with a lightweight "guest" join, or messages carry a mission tag.
  `⟳ 07-08` The tag option got cheaper and substrate-blessed: hcom tags are the spec's own
  answer for per-run grouping, and herder already records `hcom_tag` per row. A
  tag-per-mission convention (`--tag m-<id>`) is now the low-mechanism path — it was "option
  (b), rejected-leaning" in the corpus; it deserves re-adjudication on the new footing.

**Adjudicates.**
- Define: standing agent = registry session (label, guid) with zero mission memberships;
  reachable by label; mission attribution of its messages requires an explicit mechanism.
  `⟳ 07-08` Re-opened: guest-membership vs mission-tag is now a genuine two-horse race
  (tags got substrate support; the corpus's grounds for rejecting (b) — per-message tagging
  effort — largely evaporated). Pick after the identity grill, not before.

---

## S14 · Evidence reference outlives the transcript's shape

**Pain:** §4 finding — transcripts are N files (sidechains, subagents, compaction rotation);
run-log evidence-by-reference (#7) assumes stable offsets.
**Ideas:** #7 #10 — unresolved Q1

**Trace.** A run-log entry cites evidence for a decision. Two weeks later you click it in the
surface. The archive resolves the session → …which of the three files that conversation produced
(main + sidechain + post-compaction rotation)? An offset points into a file that no longer means
what it meant.

**Breaks.**
- The archive key `<node>/<harness>/<session_id>.jsonl` embeds a one-file-per-session assumption
  §4 already falsified. Ingest needs a **discovery model** (session → file set) regardless of
  everything else.
  `⟳ 07-08` The right key is now obvious: **guid**, not session_id. The registry's `sids[]`
  append-only history (spec) *is* the discovery model's spine — guid → sid history → per-sid
  file sets. Turnover/recognition semantics define exactly when a sid changes; the archive
  inherits a mapping instead of inventing one. Also note the spec's stance that `/compact` is
  a non-event (same guid, sid may re-key via recognition) — evidence refs keyed on guid
  survive compaction *by construction*.
- Offset refs are only stable against the *archived artifact*, not the live file. Evidence must
  cite the archive's coordinates (file-in-set + line), assigned at ship time — which means
  evidence-by-reference (#7) is **blocked on archive ingest existing**, and softly answers
  unresolved Q1: raw passthrough is fine, but ingest must at minimum enumerate and stably name
  the file set per guid.

**Adjudicates.**
- Evidence ref format = archive coordinates keyed on **guid**, not live-file offsets. #7 ships
  in degraded mode (guid + human hint) until #10 ingest exists.

---

## S15 · Offline on the train / do two nodes ever write one mission?

**Pain:** #9's offline semantics — flagged as never-reviewed.
**Ideas:** #9 #2 #4

**Trace.** Laptop offline for three hours; you work normally — local clone writes, events
append, transcripts accrue; daemon buffers. Reconnect: everything ships from the last-acked
offset; hub materializes; journal commits. Clean. Now the hard variant: the desktop *also*
touched the same mission during those hours. Append-only events union fine at the hub; but two
manifests changed (status, repos list) and the hub must merge — LWW-by-ship-time reintroduces
exactly the clobber the review killed, just relocated from git to the hub.

**Breaks.**
- The hard variant may be fictional. Does the author *ever* actively drive one mission from two
  nodes in the same window? Ground truth (§1) says the pains are isolation and observation — not
  concurrent multi-node authorship.

**Adjudicates.**
- Candidate simplifying invariant for the sync grill: **one active writer-node per mission**
  (advisory, surfaced in the manifest, cheap to enforce socially at N=1 users). If adopted, #9's
  merge problem collapses to append-only unions + single-writer manifests, and the comparison vs
  syncthing/per-mission-repos tilts strongly toward the simple daemon stream. If rejected, #9
  needs real merge semantics designed — the single most important fact for the sync grill to
  extract from the author.
  `⟳ 07-08` Precedent strengthened: herder itself now runs an owning-node write gate ("the
  local herder writes only as the owning node; unknown-node rows are read-only anomalies").
  One-writer-per-store is the house style. The mission-level invariant would be the same
  doctrine one level up — consistent, not novel.

---

## Cross-scenario adjudication table

Decisions that multiple walkthroughs bear on — the grills' docket, ordered by how much design
they unblock. `⟳` = status moved since 07-02.

| Decision | Scenarios | Walkthrough verdict (pending author validation) |
|---|---|---|
| One active writer-node per mission? | S15, S5, S7 | If yes (likely per §1), #9 simplifies drastically. `⟳` Strengthened: matches herder's shipped owning-node gate |
| **Mission membership keys on herder guid** `⟳ new` | S1, S6, S11, S14 | Yes — guid is durable, tool-agnostic, shipped; supersedes corpus #14's "session_id capture" spike entirely |
| **Bottle: deprecate or embrace?** `⟳ new` | S9, S10 | Corpus says unused/deprecate (#18); repo says maintained + decant live (updated 07-08). Contradiction — author must adjudicate; outcome reshapes archive scope (#10) |
| **Mission board vs repo Backlog ledger** `⟳ new` | S3, S8 | Repo-root board with one-writer doctrine now real. Re-argue "board per mission always" (#6) vs "missions reference repo-board units" |
| Archive contract wording | S4, S9, S14 | "Durable within N min, survives one machine, read-only recovery"; no content index in v1. `⟳` Note decant-continuation is nearer than assumed (bottle alive) |
| Marker-file vs env as context mechanism | S1, S3, S12 | Marker + CLI resolution primary; env is spawn-time sugar. `⟳` Vindicated by spec doctrine (env = birth provenance only) |
| `mission` CLI verb vs bare conventions | S12, S1, S8 | CLI verb wins: one integration point + provenance events for free (#3 promoted) |
| Adopt scope in v1 | S1, S3 | Run-boundary adopt only; mid-run deferred; same-fs check mandatory |
| Standing-agent attribution: guest-membership vs mission tag | S13 | `⟳` Re-opened — was leaning guest-membership; hcom tags are now the substrate-blessed grouping primitive and herder records `hcom_tag`. Two-horse race again |
| Split observe from drive in the hub rung | S6, S11 | Board+stream (read-only) is its own earlier rung; drive gateway later, reachable-nodes-only. `⟳` Vindicated: spec names an observation tier as a distinct future concept |
| Missions repo per account | S7 | Yes — isolation wins; hub aggregates origins. `⟳` "Node bus" naming settled by spec (teams dropped) |
| Harvest semantics | S8 | Copy + event, never move; archived = flag only in v1. `⟳` Board-unit promotion should mirror the shipped draft-then-owner-applies doctrine |
| ~~Fork rung independence~~ | S10, S11 | `⟳` **Shipped** (`fork --self`, resume, lineage). Off the docket; remaining question folded into the bottle row |

## Corpus drift ledger (idea # → status after 07-08)

For re-grounding the corpus doc without rewriting it — what the week did to the inventory:

- **#13 (global bus + membership join)** — substrate half **settled by reality**: teams
  dropped, one namespace per node, tags for grouping. Only mission-attribution remains open.
- **#14 (session_id capture)** — **built and superseded**: tool sid captured (correlate-gated)
  into provenance; the spec's guid is the better join key. Spike candidate #1 is moot.
- **#15 (fast fork verb)** — **shipped** (`herder fork --self`).
- **#17 (env contract)** — **half-demoted by doctrine**: spec rules env-carried identity is
  birth-provenance-only; marker/CLI resolution must carry the rest.
- **#18 (deprecate bottle)** — **contradicted**: bottle maintained and central as of 07-08.
- **#6 (board per mission)** — **incumbent appeared**: repo-root Backlog.md ledger with
  one-writer doctrine is live; must reconcile.
- **#8 (node/hub)** — node half **absorbed into the spec** (node_id, namespaces, write gate);
  hub half remains the exploration, now explicitly named by the spec as the future
  cross-machine design — with "no registry shipping, no bus relay" as recorded constraints.
- **#7 (evidence by reference)** — re-key on guid; blocked on #10 ingest (unchanged), but the
  discovery model now inherits the spec's `sids[]` history instead of inventing one.
- Corpus §1 ground-truth items now stale: "messaging flaky" (solved: bus-only + receipts),
  "compaction pain partially solved via herder-send-self" (now `herder compact [--then]`),
  "bottle unused" (contradicted), "free-form task state fails" (Backlog.md ledger live).

## What this pack does NOT cover (known gaps)

- No scenario exercises `mission rename` mechanics (#4's unsettled git-mv question) — low stakes,
  fold into CLI spec.
- No multi-user/teammate scenario — corpus scopes multiplayer to my-own-machines; revisit when real.
- Retention/GC (unresolved Q2) — needs months of real archive growth data, not a walkthrough.
- Hub auth (Q4) is surfaced by S6 but not resolved — it's a spike, not a scenario.
- `⟳ 07-08` Turnover (`/clear`) × missions: when a member session turns over, the newcomer has
  a new guid — does mission membership follow the seat, the lineage edge, or require re-adopt?
  Needs a scenario or a ruling once membership design starts. (The spec's eviction-record
  semantics give the vocabulary.)

## Next actions

1. **Author validation pass** — strike/edit scenarios; especially confirm or kill: S9's premise
   (would you actually search?) **now entangled with the bottle contradiction — have you been
   bottling?**, S15's hard variant (do two nodes ever co-write a mission?), S2's
   missionless-artifact gap.
2. Then run the **motivation grill** using the table above as priors — several kill-candidates
   (#10 query half, mid-run adopt, drive gateway timing) already have walkthrough verdicts to
   confirm or overturn.
3. ~~Spike candidate #1: session_id capture~~ `⟳ 07-08` Done by the herder work. Replacement
   first mover: **decide guid-native membership** (a paper decision, not a spike), then the
   mission CLI rung is unblocked end-to-end.
4. `⟳ 07-08` New: adjudicate mission-board-vs-repo-Backlog against the shipped one-writer
   doctrine before speccing `mission backlog/…` verbs.

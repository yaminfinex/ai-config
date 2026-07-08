---
title: "Sessions / missions / herder — boundary doctrine"
date: 2026-07-08
status: BRAINSTORM DRAFT — a proposed boundary shape distilled from the author's v2
  scenario-pack feedback. Nothing here is ratified; we are still in brainstorm mode. This is
  discussion input for a grilling session, not doctrine.
purpose: propose component boundaries and dependency directions so the discussion has a
  concrete shape to attack
related:
  - docs/design/2026-07-02-missions-scenario-pack.md                 # the walkthroughs + rulings this distills
  - docs/design/2026-07-02-sessions-missions-exploration-corpus.md   # upstream exploration corpus
  - docs/specs/herder-spec.md (herder-spec branch)                   # herder's own domain model
---

# Boundary hypotheses (brainstorm)

> The drift being corrected: the exploration entangled three independently useful tools into
> one dependency stack (missions needing session capture needing herder). The author's
> direction: **three components, separately adoptable, composing upward only.** The shape
> below is a proposal to argue with, not a settled model.

## 1. The three components

**A. Session ingestion — visibility.**
The highest-value use case (author, S9): a central index of raw tool sessions so it's easy to
see **what everyone has been working on**. Tails harness session files per node, ships them
centrally, indexes on **tool session ids**. Broadly useful in many situations; feeds
visibility, indexing, evidence-by-reference.
*Hard rule: must NOT be tied to herder* (S9, S14). Not tied to missions either.
Search/retrieval: kill-tested NO — visibility (recency/activity overview) is the product, not
querying old transcripts. Cross-machine durability: not a goal yet (D5); read-only recovery is
fine; remote decant/resume is a later, separate question (S4).

**B. Missions — "better napkins."**
A directory convention + event log + board, driven by a `mission` CLI (D7) with marker-file
context resolution (D6). Self-contained by design: **relies only on its own CLI + skill, and
as little else as possible** — no daemon, no ingestion, no herder. Usable by any agent; in
practice expected to be used mostly by orchestrator runs.
Board per mission, **always** (D4); repo-board ↔ mission-board item movement is handled in
prose at mission kickoff/closedown, not by mechanism.
Adoption and harvest are **not machinery**: an agent that wants a mission moves the files
itself and logs events (D8, S3); harvest = logging that an artifact was harvested (and deletes,
when they happen) — an action journal, nothing more (S8, D12).
Once missions ship, **orchestrate becomes mission-native** — run state lives in the mission,
and the mid-run-adopt problem never exists (S3: scenario struck).

**C. Herder — command & control (herdr + hcom).**
The experimental tier: spawn/address/observe/cull across the terminal + bus substrates, per
its own spec. Currently in test mode by exactly one person. It **may use** missions
(orchestrator runs log there) and **may enrich** ingestion (guid ↔ tool-sid joins give richer
identity over raw sessions). **Nothing below it may depend on it.**
Bottle: **deprecate** (D3). Its remaining usage is orchestrator references; replace with named
sessions in herder's registry + herder spawning them.

## 2. Dependency rules

```
            depends on / may use →
  ingestion   →  (nothing)                 ← standalone; tool session files are the interface
  missions    →  (nothing)                 ← standalone; plain files + git + skill
  orchestrate →  missions, herder          ← policy layer; mission-native once missions ship
  herder      →  missions (optional),      ← composes downward; enriches upward
                 ingestion (enrichment joins)
```

- Arrows never point up. A machine with only missions installed, or only ingestion, is a
  complete, useful installation.
- Herder's guid is a *richer overlay* available when herder manages a session — never a
  prerequisite for anything mission- or ingestion-side.

## 3. Identity doctrine

- **Universal spine: (tool, session_id).** Ingestion indexes on it; evidence references key on
  it (plus ingest-assigned file coordinates, since sessions are N files); its known warts
  (mutable, late-arriving, tool-scoped) are accepted at this layer.
- **Herder guid = enrichment.** Where herder manages the session, the registry's guid↔sid join
  upgrades identity (lineage, turnover, continuity). Guid-keyed mission membership (D2 AGREE)
  applies to herder-run missions — i.e. orchestrator runs — not to the mission format itself.
- **Mission events carry ids as attributes, not foreign keys.** A bare agent logs whatever id
  it has (tool sid from hooks env, or nothing); an orchestrated agent's events carry guid too.
  Nothing validates; visibility joins are best-effort by design.

## 4. Nodes, multi-node, hub — LEAST SETTLED

> The author is explicitly unsure how these tools fit into a hub concept at all. Everything
> in this section is hypothesis-grade; it's the first thing the grilling should attack.

- **Node = (account × machine)** (S7) — but nodes are **server-ish machines, not laptops**
  (S6): assume reachable; do not design for roaming/sleep.
- **One shared missions repo** for all nodes, location via env var (D11). A mission is used by
  many nodes as a matter of course (S7, S15): an orchestrator on node A handing node B a job
  that lands an artifact in the mission from node B is normal, not an edge case.
- **What's actually hard about multi-node writes: almost nothing** (answering S15). Events are
  append-only and artifacts land in disjoint paths — unions, no conflicts. The only real
  conflict surface is concurrent mutation of a mission's summary/manifest, so the D1 invariant
  narrows to: **one manifest authority per mission** (the orchestrating/home context; everyone
  else appends events + artifacts freely). The scary merge problems in the corpus were
  artifacts of the dead multi-writer-git-as-bus design, not of multi-node use.
- **Hub**: the later aggregation surface. Requiring a hub for multi-node visibility is
  acceptable (S5) since single-node missions dominate. **Observation first** (D10); drive
  later — and when drive comes, the preferred shape is **nodes registering at the hub over an
  outbound bidirectional channel (tunnel) for command execution**, not hub-initiated ssh (S6).

## 5. Killed / simplified this round

| Item | Ruling |
|---|---|
| Missionless-artifact scratch home (S2) | Killed — napkins already covers it |
| Mid-run adopt machinery (S3) | Killed — orchestrate goes mission-native; agents move files themselves |
| `mission adopt` as a heavy verb (D8) | Demoted — adoption is an agent doing file ops + logging, not a transaction |
| Harvest machinery: idempotency/copy-vs-move contract (S8, D12) | Demoted — harvest is an event in the log; deletes are events too |
| Archive search / content indexing (S9) | Killed — visibility, not retrieval, is the product |
| Bottle (D3) | Deprecate — replace orchestrator refs with named registry sessions + herder spawn |
| Mission tags / guest membership for standing agents (D9, S13) | Killed — direct naming works; proven in use |
| Per-account missions repos (D11) | Reversed — one shared repo, env-var configured |
| Cross-machine transcript durability as v1 contract (D5, S4) | Deferred — read-only is fine, remote decant later |
| Hub-ssh drive (S6) | Direction changed — node-registered tunnel when drive is built |

## 6. Open questions (the short list)

0. **Herder node daemon shape** — raised by the Q8/Q9 grilling; four divergent designs +
   comparison + recommendation captured in
   `docs/design/2026-07-08-herder-node-daemon-designs.md` (decision pending: recommended
   D-via-A — flock-shared writes, disposable read projection, phased).

1. **Mission ↔ herder interaction contract** (S1, S4, D2): exactly what an orchestrator writes
   into mission events (guid? seat? run refs?) — spec'd alongside the `mission` CLI, honoring
   §2 (herder writes into missions; missions define the event shapes without referencing
   herder concepts).
2. Ingestion transport: how per-node tailers ship to the central index (the visibility rung's
   one design task; must not require herder or hcom).
3. Manifest-authority mechanics: how "one manifest authority per mission" is expressed in a
   shared git repo (advisory field? orchestrator-only convention?) — small, decide during
   mission CLI spec.
4. Hub drive channel (tunnel) design — parked until after the observation rung.

## 6b. Grilling record — 2026-07-08 session (answers are the author's; supersedes parts of §4)

The boundary grilling ran nine questions before pausing. This record is the continuation
point for the next session — do not re-ask these.

- **Q1 Who is "everyone"?** → **Team. Multiplayer, not just multinode.** Author uses these
  solo across machines today but the target is team. **Transcript content is team-visible.**
- **Q2 Is "hub" one thing?** → Decomposed. **Session central store** stands alone ("anything
  session oriented can point to that"). Missions start as git + folder + a viewer. The word
  "hub" is retired; §4's hub language is superseded accordingly.
- **Q3 Realtime board via quick-db dual-write from mission CLI?** → **Rejected.** Over-indexed
  on quick (limited; no fs; github access unclear); the mission↔session join is herder's;
  realtime **rides the herder work, not the mission work**. Passive "mission UI without
  herder" deferred — trivial viewer over a github repo when wanted.
- **Q4 Active-mission surface = herder's server tier?** → **Yes.** Named the **herd server**
  (hub-and-spoke; the "future central-orchestrator/server design" the herder spec reserves).
  Session service stays dumb. Author flags: opens mission-folder-ownership-by-node questions.
- **Q5 Realtime = herd events up the spoke + git as trailing record?** → Directionally yes;
  refined by Q6/Q7.
- **Q6 `events.jsonl` as the realtime interface?** → **Rejected by author**: agents will just
  *write artifacts*; the honest primitive is watching the mission dir. `mission backlog`
  wraps backlog commands (mission = only entrypoint); non-CLI edits fall back to file-level;
  needs reconciliation anchored on git state; **snapshots, not events**; needs a just-booted
  form. events.jsonl survives as provenance journal only.
- **Q7 Sync protocol vs commit-often cheat?** → **Snapshot-overlay protocol**: idempotent
  full-replace messages `{mission, git base sha, dirty files + contents}`; read-side overlay
  at the server; overlay evaporates as git catches up. Decided by the author's piggyback
  insight: this rides the **already-existing node↔server realtime feed (the spoke)** — not a
  standalone channel, and **not part of the mission CLI** (being watched happens *to* the
  mission dir).
- **Q8 One node process or two?** → **Two, confirmed**: session shipper (ingestion's,
  universal, dumb) ≠ herder daemon (spoke). Author additions: daemon consumes hcom events +
  probably the herdr socket; will later carry **cross-node relay** and be the **inbound
  control plane** (delivery verbs only). Raised: should `herder` become a daemon+client
  dual-purpose binary — might resolve current hard parts (sidecars).
- **Q9 Sidecars retire into daemon? Write path daemon-mediated?** → Author challenged
  flock-vs-daemon reasoning and short-term complexity of "additive first"; escalated to a
  **design-it-twice pass** → `docs/design/2026-07-08-herder-node-daemon-designs.md` (four
  designs; recommendation **D-via-A** — flock-shared writes, disposable read projection,
  phased; **DECISION PENDING — this is the next session's first agenda item**).

**Not yet grilled** (the remaining tree): session service design (storage, shipping protocol,
what the team surface shows, auth beyond tailnet); mission CLI verb set + dir format + event
shapes; manifest-authority mechanics (§6.3); mission↔herder interaction contract (§6.1);
herd-server projection details; scenario-pack display doc is two rounds stale (optional).

## 7. Build order (sketch, for discussion)

1. **`mission` CLI + skill + dir format** — standalone, guid-free, sid-optional. Unblocked now.
2. **Orchestrate goes mission-native** — run state moves into missions; bottle references
   replaced (D3) with named registry sessions + herder spawn.
3. **Session ingestion → central visibility surface** — independent track, can start in
   parallel; keys on tool session ids; no herder anywhere in it.
4. **Hub observation rung** — aggregates §3's outputs; drive tunnel much later.
5. Herder continues on its own spec branch, enriching the above where present.

---
title: "Centralised cross-harness session management — high-level design (as it stands)"
date: 2026-07-02
status: design-snapshot (not implementation-ready)
purpose: handoff / explainer
related:
  - docs/plans/2026-07-01-002-feat-hcom-launch-substrate-plan.md   # hcom-owned launch substrate (validated)
---

# Session Management Architecture — design snapshot & explainer

> **How to use this doc.** This is a **handoff for a new session whose goal is to *explain* this
> architecture** to the author (Riley) — the proposed shape, the roles each tool plays, *why* each
> role exists, and the divergent options we considered. It is a design snapshot, **not** an
> implementation plan. Read top-to-bottom: §1 problem → §2 tools → §3 the model → §4 roles+why →
> §5 the big decisions → §6 the divergent exploration → §7 constraints → §8 open questions.
> Glossary at the end.

---

## 1. The problem

Build a **centralised, cross-harness, (eventually) cross-machine layer for coding-agent sessions**,
so the author can reason about and operate over sessions as **first-class objects**. Four axes:

- **Search** — find any past session across all harnesses/machines.
- **Pin / label / note** — durable curation over sessions.
- **Decant** — snapshot a session and re-materialise it as a live, running session (same or other harness).
- **Multiplayer / cross-machine** — the author has sessions across many machines; today this mostly
  means "my own sessions everywhere," not team.

"Coding agents" = Claude Code, Codex, Cursor, Aider, etc. — CLI harnesses that each **persist a
session as JSONL (or SQLite) on disk**, keyed by a harness-chosen `session_id` UUID.

**The founding realisation (why the existing `bottle` tool is "not it").** `bottle` makes a session
first-class by **copying** it into an immutable snapshot store (`~/.bottles`) — you must snapshot
before you can reference. But **sessions already are durable objects on disk in every harness.** The
field's lesson (see `cass`, §6 prior art) is the inverse: **the on-disk session is the first-class
object; you put *views* over the real population** (index, curation, portability) rather than copying
into a parallel universe. `bottle` also conflates four independent concerns (snapshot + label + pin +
resume) into one verb, and is Claude-only. So the redesign **inverts** it: index everything that
exists; make pin/label cheap metadata over live sessions; keep snapshot/decant as *one* optional
verb, not the entry fee.

---

## 2. The existing tool ecosystem (ground truth we design around)

| Tool | What it is today | Trajectory in this design |
|---|---|---|
| **hcom** | Agent-messaging substrate. A validated plan (see `related`) makes it the **launcher + binder** of every managed agent: `hcom <tool> --run-here` in a login shell binds the agent's **hooks + PTY**, assigns identity (`<role>-<random>` via `--tag`), and **delivers messages into a running agent** (local "drive"). Installs the global hooks, so it sees the live hook event stream (which carries `session_id`). **Local-only today** — cross-device relay is explicitly future. | The **substrate**: owns launch, bind, identity(name), local message delivery. Grows a **relay arm** later for cross-machine. |
| **herder / herdr** | `herdr` = the surface/multiplexer product (workspaces / panes / tabs / git-worktrees). `herder` = the skill/driver: GUID registry, `--notify` rings, briefs, send/list/wait/cull. Under the new plan, herder **delegates launch to hcom** and **captures** the hcom name (`guid ↔ hcom-name`). | herder becomes **the session graph owner** (see §4). `herdr`-the-surface is contingent local ergonomics. |
| **bottle** | Claude-only. Snapshots a session → immutable versioned artifact **with lineage**; **decants** it back into a freshly launched session (its launch shells to herder-spawn → now routes through hcom). | Folds in as **the snapshot/decant *verb*** in the Metadata layer; lifting beyond Claude = adopt a canonical IR (casr). Not its own universe anymore. |

Prior art we **borrow from, do not depend on**: `cass` (cross-harness index/search), `casr`
(canonical IR portable resume), `watchmen` (hooks+file-tail observe). See §6.

---

## 3. The model: three planes, three layers, one graph

### 3a. Three *planes* (how you touch a session)

The historical-indexer framing (cass/casr) is only **one** plane. The full system has three:

- **OBSERVE** — record what happened. Two sub-methods, complementary, lossy in *opposite* ways:
  - **hooks / file-tail** = harness *semantics* (session_id, cwd, git, tool calls, subagent/compaction
    boundaries). Loses the wire. Per-harness (no portable contract).
  - **proxy** (wire capture) = the full API bytes (system prompt, tool defs, exact context, tokens,
    cost). Harness-agnostic. Loses all harness semantics. **Decision: NOT using a proxy** — file-tail
    only. Losing exact-prompt/tool-defs/cost is acceptable.
- **CONTROL** — *drive* a session (inject a turn). Requires owning the process. **This is hcom** (local).
- **INTERCEPT/TRANSFORM** — mutate requests in-flight. Proxy-only. **Not in scope** (no proxy).

Key point that reframed the project: **cass/casr are OBSERVE-only, read-only — they can never drive a
session.** "Sessions as inert history" is a false ceiling; the *center* of the system is the CONTROL
plane (hcom), with observe/index hanging off it.

### 3b. Three *layers* (the session-management system proper), strict one-way dependency

```
Layer 2  CONTROL     drive · attach-live · inject     needs: live process (hcom) + naming + relay
              ▲ (upper needs lower; lower knows nothing of upper)
Layer 1  METADATA    pin · label · note · lineage      needs: a stable session identity
              ▲                                         (bottle's snapshot is a verb here)
Layer 0  INDEX       file-tail → normalize → search     needs: NOTHING
                                                        (== cass; works for any harness, any machine,
                                                         even dead sessions; the product on day 1)
```

- **Build bottom-up.** Layer 0 is independently valuable, low-risk, and degrades to a pure local
  SQLite. It must **never** assume Layers 1–2 exist.
- **Identity spine = `session_id`** (durable; survives the live session dying, so pins persist).
  Live-control alias = **hcom name**. The mapping is the graph (§3c).

### 3c. The graph (herder's domain) — nodes + edges

Neither hcom (per-*message*) nor the index/metadata (per-*session*) can hold **relationships between
sessions**. That per-*relationship* layer is the graph:

- **Nodes = the identity join:** `session_id ↔ hcom-name + pane ↔ surface (worktree/pane) ↔ durable
  handle`. This is what lets you go **search → drive** (map a found session back to a live handle).
- **Edges = coordination + herd-memory:** who spawned whom, notify rings, one-writer-per-worktree
  arbitration, and shared-mission state (playbook / run-log / briefs) that belongs to *the herd, not a
  member* (no single `session_id` to key it on).

### 3d. The whole thing assembled

```
   clients (CLI today · web/phone later)
        │  discover / search              │  drive
        ▼                                 ▼
   ┌────────────────────────┐      ┌──────────────────────────────┐
   │ Layer 0/1: INDEX +     │      │ herder = THE SESSION GRAPH    │
   │ METADATA               │      │  nodes: session_id↔name↔pane  │
   │ file-tail→canonical→   │◄─────┤  edges: spawn/ring/lock,      │
   │ SQLite (local floor)   │ join │         playbook/run-log      │
   │ → ship to central svc  │      │  (orchestrate = policy skin)  │
   └────────────────────────┘      └──────────────┬───────────────┘
        keyed on session_id                       │ launches THROUGH
                                                   ▼
                                       hcom = SUBSTRATE
                                       launch · bind(hooks+pty) · identity(name)
                                       · local message delivery (drive)
                                       · [future] cross-device relay
                                                   │
                                       agent (Claude/Codex/…) in a herdr pane,
                                       JSONL on disk  ─────► (tailed by Layer 0)
```

---

## 4. Roles and the *why* of each

| Component | Owns | **Why it owns it** |
|---|---|---|
| **hcom** (substrate) | launch, bind(hooks+pty), identity(name/tag), local message delivery (drive), live hook stream | **"Binding a session to the bus is the launcher's job."** Whoever launches owns the PID + PTY, so it has strictly more signal than any external wrapper — verified-send, liveness, and injection all belong where the PTY is owned. This also **solves the PTY↔session_id join at birth** (the launcher sees both). |
| **Layer 0 — Index** | file-tail → per-harness adapter → canonical `Conversation→Message→Snippet` → SQLite (local floor) → optional ship to central | Sessions **already exist on disk**; index the real population instead of copying it. **Depends on nothing** so it's the always-valuable floor and degrades to pure-local. Borrow cass's *"SQLite is truth; index/vectors/curation are rebuildable — corruption is a non-event."* |
| **Layer 1 — Metadata** | pin / label / note / lineage; **bottle's snapshot = a verb here** | Curation is **cheap metadata over live sessions**, not a reason to copy — this is the field's biggest *gap* (nobody but Droid has real pin+tag+search) and the author's clearest differentiation. Kept in a **separate db** from the rebuildable index (cass discipline). |
| **herder** (the graph) | the **session relationship graph**: identity join (nodes) + coordination/herd-memory (edges); `orchestrate` skill = policy over it | It's the **only per-relationship layer**. hcom is per-message, index/metadata is per-session; edges (topology, rings, write-locks) and herd-memory (playbook/run-log/briefs) have **no home** in either. And the join is what bridges durable `session_id` ↔ drivable `hcom-name`. |
| **herdr** (the surface) | workspaces / panes / tabs / git-worktrees; spatial "point at that one" | **Contingent local ergonomics.** Real value only for *parallel local work*; replaceable by tmux + an index UI. **Not part of the centralised spine** — this was the surprise from the divergent pass (§6): the "surface" framing is herder's *weakest* justification, the graph is its strongest. |
| **relay** (future) | cross-device transport for **drive** (remote inject) | Tailscale gives reachability but you still need an **outbound-dial rendezvous** so a machine can be driven without inbound listeners (roaming/sleep-safe). Correctly located as a **future hcom arm**, not a separate project. This is where multiplayer/remote-drive lives. |

**One-line answer to "what does herdr add if we lean on hcom?"** → herder stops being launcher+surface
(hcom took launch; tmux/the-index-UI can take the surface) and becomes **the session graph** — the
join between durable sessions and live handles, plus the coordination edges and herd-memory no
`session_id` can hold. That role is irreducible; its *implementation* can start as **a table + the
`orchestrate` skill** and only becomes a daemon at the cross-machine tier.

---

## 5. The big decisions and their *why*

1. **Invert `bottle`: index the real sessions; don't copy to make them first-class.** Copying creates
   a parallel universe that drifts from reality and gates search/pin behind a snapshot. (§1)
2. **Observe via JSONL file-tail only — no proxy.** Buys simplicity, no MITM/key-custody/SPOF. Costs
   the wire (exact prompt, tool-defs, cost). Accepted. Contain the JSONL-schema-churn risk with **thin
   per-harness adapters at the edge** → canonical schema everywhere downstream. (§3a)
3. **hcom is the launcher + binder.** Because binding is the launcher's job; this solves the identity
   join at birth and puts verified-send/liveness where the PTY is owned. (§4)
4. **herder = the graph, not the surface.** The surface (panes) is disposable ergonomics; the graph
   (join + edges + herd-memory) is the only per-relationship layer. (§3c, §4, §6)
5. **Degrade-to-local / upgrade-to-remote, at every layer.** Index: local SQLite floor → optional
   central service (two-stage: *edge* tail+normalize+buffer → *central* ingest+serve; same interface,
   only the binding changes). herder: **scales** from alias → join table → coordination → node daemon
   (§6). Transport: hcom present → bus; absent → raw tool + keystroke fallback.
6. **The relay (cross-device drive) is a future hcom arm.** Keeps local and remote drive unified as
   "hcom message delivery," with a relay hop when remote. (§4)
7. **Multiplayer, phase 1 = my-own-machines**, not team. A self-hosted central index + a relay reach
   this without per-harness cooperation (local-only harnesses like Claude Code never give cross-device
   natively). (§6 prior art)

---

## 6. The divergent exploration (options considered)

We ran a **4-way divergent design pass** on the single question *"what does herder irreducibly add if
hcom owns launch+bind+identity+messaging?"* — each design forced to a different stance **and to write
its own strongest objection**. The objections all converged, which is how we found the core.

| Design | Stance | Its own fatal admission |
|---|---|---|
| **A — Maximalist hcom (delete herder)** | herder is redundant; reduce to a launch alias. hcom + index + a thin `surf` shim. | "Delete = **relocation + scattering**." The `surf` shim, *"the moment it must verify delivery and survive id compaction, IS herder-spawn."* You don't delete the complexity, you smear it across 3 layers with no owner. |
| **B — Spatial surface only** | herder = window manager (panes/worktrees/deixis), nothing semantic. | The pane grid **contributes nothing to the centralised spine**; *"replaceable by tmux + the index UI… moat is UX polish, not architecture."* → the surface framing refutes itself. |
| **C — Orchestration brain** | herder owns the **coordination graph**: topology edges, rings, arbitration, herd-memory; hcom dumb transport, index dumb storage. | Can defend coordination as a distinct **responsibility** (neither per-message nor per-session) but **not** that it must be a separate **process** vs a sub-module + table. A *boundary* argument, not a *packaging* one. |
| **D — Per-machine node daemon (`herderd`)** | herder = the machine's node agent: owns local registry + edge tailer, does the continuous identity join, is the relay's dial-in endpoint. | *"You've invented a daemon to justify keeping herder."* The join *"could be a materialized view in the Index over (hook-stream ⊕ tailer output) with no bespoke process."* Fusing node + pane-arranger is coupling of convenience. |

**Synthesis — the four are one component at four scales, not alternatives:**

| Scale | herder is… | Design |
|---|---|---|
| 1 session | ~nothing (a launch alias + one join row) | **A** |
| N sessions, 1 machine | join table (+ optional spatial surface) | **A / B** |
| multi-agent protocol | the coordination graph (edges, arbitration, herd-memory) | **C** |
| cross-machine fleet | node + relay landing endpoint | **D** |

The **graph role is constant; its weight scales.** So the irreducible add is "owner of the session
graph," and:

- **Packaging verdict:** start as **a table (nodes+edges) + the `orchestrate` skill as policy**. Do
  **not** build `herderd` (a daemon) yet. Promote to a daemon **only at the cross-machine tier**,
  where something genuinely must hold the outbound relay connection and reconcile continuously. This
  preserves degrade-to-local.

---

## 7. Correctness constraints to bank (independent of packaging)

Every divergent design surfaced these — they're system properties, not design choices:

1. **The launch/decant join race.** `session_id` only exists once the harness writes its first JSONL
   line, but `hcom-name` + pane exist at spawn → a window where you can mis-join and drive/index the
   **wrong** session. **Decant mints a fresh `session_id`**, re-firing the race every time.
   **Resolution:** hcom must **persist its `name ↔ session_id` bind as a record the indexer consumes**
   — a deliberate, single-record carve-out of "file-tail only." It's the seam the whole system hangs on.
2. **Key Metadata on a stable *lineage-root*, not raw `session_id`.** Otherwise every resume/decant
   silently orphans pins/labels. Correctness, not optimization.

---

## 8. Open questions (for the next design pass, not the explainer)

1. **hcom bus scope:** global (simplest) vs per-worktree `HCOM_DIR` isolation. If per-worktree, the
   index needs a bus/partition provenance dimension. (From the hcom plan's own unresolved Q1.)
2. **Where does curation live in local mode** — edge-local then sync-up, or always-central-even-locally?
3. **`watch` (live view) source:** central index subscription (survives sleep, laggy) vs edge-tail
   (real-time, needs the machine up) — or both with a handoff?
4. **Index: depend on cass/casr (Rust) or reimplement their model in Go** to keep the toolchain uniform?
5. **Does immutable snapshot survive at all**, or does cheap pin+label+lineage-over-live-sessions make
   freezing redundant (label + git sha instead of a frozen copy)?
6. **Cross-harness decant target:** do you actually want to resume a Claude session *inside Codex*
   (casr's premise), or is "centralised" really *search + curate everything, resume in native harness*?

---

## Glossary

- **harness** — a coding-agent CLI (Claude Code, Codex, Cursor, Aider…). Each persists sessions on disk.
- **session_id** — the harness-chosen UUID for a conversation; the **durable identity spine**. On disk
  at e.g. `~/.claude/projects/<enc-cwd>/<uuid>.jsonl`, `~/.codex/sessions/.../rollout-*.jsonl`.
- **hcom name** — `<role>-<random>` handle hcom assigns at launch (via `--tag`); the **live-control
  alias**. Not user-pinnable; ephemeral. Routes drive/inject.
- **bind (hcom)** — hcom attaching to a launched agent's hooks + PTY; `bindings: hooks, pty`.
- **the join** — the mapping `session_id ↔ hcom-name + pane ↔ surface ↔ durable handle`. herder's nodes.
- **decant** — materialise a stored session/snapshot back into a **running** harness pane. (Distinct
  from *fork*: fork diverges the history graph; decant re-hydrates a stored point into a live process.
  The field has no other word for this.)
- **bottle / rebottle** — snapshot a session into an immutable versioned artifact with lineage /
  create a new version from a decanted session. Becomes the snapshot verb in Layer 1.
- **lineage-root** — the stable identity a chain of resumes/decants shares; the correct key for
  durable pins/labels.
- **terminal_id** — herdr's durable pane handle (survives pane_id compaction).
- **relay** — future hcom cross-device arm; outbound-dial rendezvous carrying *drive* across machines.
- **canonical schema / IR** — `Conversation → Message → Snippet` (cass) / casr's `CanonicalSession`;
  the per-harness normalization target so downstream never touches raw JSONL.

## Prior art (borrow, don't depend)

- **cass** (`coding_agent_session_search`) — read-only cross-harness (20+) index/search; file-tail →
  canonical → SQLite + Tantivy lexical + optional on-device vectors (hybrid RRF); curation in a
  **separate** db (bookmarks/tags/saved-views). *"SQLite is truth; everything derived is rebuildable."*
  **OBSERVE-only, cannot drive.** → our Layer 0/1 reference.
- **casr** (`cross_agent_session_resumer`) — canonical IR + symmetric per-provider read/write adapters
  + **native-file output** (target's own engine resumes) + atomic write/read-back safety. **No
  lineage, no context-fit** (its two punts = our value-add). → our cross-harness decant reference.
- **watchmen** (`firstbatchxyz/watchmen`) — hooks + file-tail OBSERVE stack, multi-harness, **observe-
  only**. Confirms the capture pattern; not a dependency.
- **Claude Remote Control / omnara** — the local-process + relay + thin-client web-control pattern.
  *Reference only:* Remote Control is Claude-only/closed; **omnara's OSS is abandoned** (CLI-wrapper
  became unmaintainable — the lesson: **ride stable seams (PTY, `--resume`, hooks), don't wrap the CLI**).
- **Amp / Droid** — server-native multiplayer threads (the "cloud store by construction" model);
  **traces.com** — *"Make Coding Agents Multiplayer,"* per-session publish/share (not continuous
  personal cross-machine sync). Reference points for the multiplayer axis.

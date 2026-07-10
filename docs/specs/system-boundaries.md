# System Boundaries

Date: 2026-07-10
Status: standing cross-component contract; component specs remain authoritative

This note consolidates the July 2 exploration corpus, scenario pack, domain sketch, architecture snapshot, and the July 8-9 boundary grilling. It records why the components are separate and what remains unbuilt without asking future readers to reconstruct the exploration chronology.

Normative sources:

- `docs/specs/mission-spec.md` for missions and `mish`;
- `docs/specs/session-service-spec.md` for the session service;
- `docs/specs/sesh-wire.md` for the shipper/store contract;
- `docs/specs/herder-spec.md` for herder and the node observer.

Where this map and a component spec disagree, the spec wins.

## Boundary decision

Sessions, missions, and herder solve different problems and remain independently adoptable:

```text
session service -> nothing
missions        -> nothing
orchestrate     -> missions + herder
herder          -> missions optionally + session-store enrichment
```

Dependencies never point upward. A machine with only `mish`, or only the session shipper, is complete for that component's purpose. Herder may understand missions and session-store identity; neither missions nor the session service may depend on herder.

## Components

### Session service: visibility

The session service answers: "what has everyone been working on?"

One dumb shipper per OS user tails harness transcript files and sends raw byte ranges plus identity facts to a central byte-faithful mirror. The store parses centrally, maintains an index, and serves a people-first recency page with read-only transcript drill-down.

Boundary rules:

- shipping is file-driven, never process-driven;
- nodes ship bytes and facts, never parsed events or identity verdicts;
- source files are buffers, not the archive; the mirror outlives upstream retention;
- parsing and policy live centrally so format churn is repaired by reindexing;
- no mission, herder, or hcom concept appears in the wire or node agent;
- search, write actions, live relay, and node-side parsing remain out of scope.

The adopted transport mechanics are recorded in `session-service-spec.md`: content fingerprinting, acknowledgement before cursor advance, reset on truncation, filesystem notifications as hints plus periodic rescans, and ingest deduplication.

### Missions: durable working memory

A mission is "better napkins": one self-contained directory holding intent, a nested Backlog.md board, and artifacts:

```text
missions/<slug>/
  mission.md
  backlog/
  artifacts/
```

`mish` has three verbs: `new`, `backlog`, and `status`. It never mutates git; `status` may make read-only git queries for staleness. The board is a pinned nested Backlog.md instance; the directory moves and archives as one unit.

Boundary rules:

- missions are plain files plus git and do not require a daemon or session service;
- no herder guid, seat, run id, or session foreign key appears in mission files;
- `authority` is advisory manifest-write authority and may name an agent or person;
- `owner` attributes the work to a human and is distinct from authority;
- board assignees are opaque label-grade names; richer joins happen on herder's side;
- custody and work decisions live in board notes, git history, `mission.md`, and artifacts, not an event log.

### Herder: command and control

Herder owns active agent seats, addressing, launch, delivery, observation, lineage, and culling. Its registry is the authority for herder state.

The node presence uses the observer-first shape selected by the node-daemon design pass:

- the observer watches every seated session regardless of whether it was spawned or enrolled;
- it has no privileged write path and appends observations through the same locked writer as CLI verbs;
- it is disposable, has no handoff protocol, and reconstructs state on every boot;
- local commands and reads continue when it is absent;
- a registry write daemon remains rejected.

### Orchestrate: policy

Orchestrate remains a skill rather than a service. It composes herder and missions:

- the mission board carries unit state and assignment;
- `artifacts/journal.md` carries narrative decisions and handoff context;
- the mission is the orchestration scope, so a separate execution identifier and log ledger are unnecessary;
- herder handles live agents and message delivery.

The mission format does not know about these orchestrate conventions.

## Identity and attribution

The shared low-level spine is `(tool, session_id)`, amended by content-derived file identity because one logical session can span moved, recreated, or overlapping files.

Identity joins are deliberately asymmetric:

```text
mission assignee label
        -> herder label / guid / session ids
        -> session-store transcript
```

The join is best-effort and view-time. A failure to enrich never changes the underlying mission or transcript.

`SESSION_OWNER` is the cross-surface declaration for the human behind work on shared infrastructure. The session shipper treats it as an observed fact, not authentication. On Linux it can correlate the environment to writers; Codex permits exact process/file mapping, while Claude may only be attributable at `(node, OS user, cwd)` when all visible writers agree. Personal macOS devices rely on tailnet identity and OS user without process correlation.

## Settled rulings

The exploration and grilling produced these durable decisions:

| Topic | Ruling |
|---|---|
| Product target | Team multiplayer, not merely one person across nodes. |
| Central services | Session store and future herd server are different systems; the generic "hub" concept is retired. |
| Mission board | Verbatim nested Backlog.md instance with pinned safe configuration. |
| Mission verbs | `new`, `backlog`, and `status`; no `log` verb. |
| Mission event log | Rejected; board notes, git, and journal prose carry the useful information. |
| Mission/herder contract | Missions are herder-unaware; herder may enrich opaque mission labels. |
| Session transport | Dumb byte shipper plus central mirror/index. |
| Session UI | People-first recency and read-only drill-down; no search in the first product. |
| Node attribution | Ship facts and resolve policy centrally; attribution is never authentication. |
| Node daemon | Disposable observer as a peer writer; no privileged daemon-mediated write path. |
| Realtime mission state | Future herd-server concern using idempotent mission-directory snapshot overlays, not mission dual-writes. |
| Bottle in this architecture | Not a prerequisite for sessions or missions; snapshot/decant remains a separate tool. |

## Remaining architecture work

The main unbuilt boundary is the herd-server tier. Its first server phase is gated on a dedicated design pass and includes:

- node registration and outbound spoke transport;
- active-mission snapshot overlays anchored to git base state;
- delegation of a node or mission to a human owner;
- cross-node command delivery;
- view-time joins from herder identity into the session store.

A later hot-read phase may route selected herder reads through a rebuildable daemon projection only after the legacy two-state registry view is retired. The registry file remains truth, cold reads remain the parity oracle, the observer remains disposable, and no write may route through the daemon.

That design should start from the ratified component specs and the observer-first node-daemon decision record. It should not revive the old monolithic hub, mission event log, node-side parsing, daemon-authoritative registry, or mission files containing herder ids.

## Consolidation note

The exploration, scenario, domain-model, architecture-snapshot, and boundary-grilling source records remain under `docs/design/` while backlog entries and open decisions still cite their detail. This note preserves the decisions that constrain future work; component-level contracts belong in their specs. Each source record can be retired separately only after its remaining references and load-bearing content have a named successor.

---

## Historical boundary decision record

This appendix is a non-normative historical record. It preserves the detailed rulings and open questions behind the standing boundary contract without making its former implementation inventory normative.

```yaml
title: "Sessions / missions / herder — boundaries v2 (consolidated)"
date: 2026-07-09
status: BRAINSTORM-CONSOLIDATED — the settled output of the Q1–Q20 grilling, restructured
  around rulings instead of hypotheses. Still pre-spec except where noted (mission spec in
  flight; herder phase 1a merged). Supersedes §1–§5 of the v1 doc; the grilling record itself
  (v1 §6b, Q1–Q20 with full rationale) remains the authoritative trail and continuation point.
related:
  - docs/design/2026-07-08-sessions-missions-boundaries.md        # v1 + the Q1–Q20 record
  - docs/design/2026-07-08-herder-node-daemon-designs.md          # daemon design pass + decision record
  - docs/design/2026-07-09-session-shipping-prior-art.md          # Q18 prior-art memo
  - docs/design/2026-07-08-mission-spec-brief.md                  # mission spec dispatch (in flight)
  - docs/specs/herder-spec.md (main, RATIFIED 2026-07-08)         # herder's own ground truth
```

### Boundaries v2

Three components, separately adoptable, composing upward only. Target: **team multiplayer**
(Q1) — transcript content is team-visible; nodes are server-ish machines plus personal
devices; the word "hub" is retired (Q2) in favor of two distinct central surfaces (the
session store and the herd server).

#### 1. The components

**A. Session service — visibility.** Per-node **dumb byte-shipper** (one per user): tails
harness session files with fingerprint-identity offset cursors, ships raw bytes plus four
identity **facts** — store-stamped tailnet identity (node-scoped), os user, hostname,
`SESSION_OWNER` where visible — to a central **byte-faithful mirror** + parse-on-ingest index.
Team surface: one people-first recency page (person → nodes → sessions; read-only transcript
drill-down). No search (killed, S9), no parsing on nodes, no policy on nodes, no herder or
hcom anywhere in it. Auth: a tailnet **grant/ACL scope** (not whole-tailnet), tailscaled-
injected identity only. Prior-art memo verdict: build it; adopt the filebeat/vector
mechanisms (fingerprint identity, ACK-then-advance cursors, truncation-reset, inotify-as-hint
+ rescan, source-file-as-buffer, ingest dedup by message uuid — the last is core correctness,
not polish: "append-only" has verified exceptions). Shipping is **file-driven, never
process-driven**: dead or pre-install session files ship identically; the mirror outlives the
source (30-day client cleanup) and becomes the team's durable session archive.

**B. Missions — "better napkins."** A directory convention + per-mission board + a three-verb
CLI, completely herder-unaware. `missions/<slug>/{mission.md, backlog/, artifacts/}` —
self-contained, moves as a unit. The board is a **verbatim nested Backlog.md instance**
(verified: nearest-ancestor resolution makes it self-contained; config pinned at scaffold:
autoCommit/remoteOperations/checkActiveBranches/autoOpenBrowser off). Verbs: `new` (scaffold +
D6 marker), `backlog` (cwd-pinned passthrough; denylist at least init/config), `status`
(read-only). **No events.jsonl** (strong kill, Q15): custody/attribution discipline rides
conventioned git commits + board notes. The CLI never runs git by default (opt-in
auto-commit marker reserved). `mission.md` carries advisory `authority:` (manifest-edit
rights; label-grade, may be an agent) and — pending mission-spec ratification — a separate
human **owner** signal field: *separate consumers, separate nouns*. Conflict on mission.md =
authority violation; authority's version wins. Spec in flight (mission-spec lane).

**C. Herder — command & control**, per its RATIFIED spec, plus the decided node presence:
**D-via-A, observer-first** (Q10 decision record). Phase 1a — the universal seat observer
(no write authority; confirmed-write contract; v2-states-only projection; disposable, no
handoff) — is designed, reviewed, **merged to main**, implementation task filed, errata in
the spec-steward lane. Phase 1b (spoke telemetry + inbound deliver) and phase 2 (hot reads,
behind legacy-view retirement) stay gated. The **herd server** is herder's central tier:
hub-and-spoke, owns the active-mission realtime surface (mission-dir snapshot-overlays
`{mission, git base sha, dirty files}`, idempotent full-replace, riding the spoke), the
richer identity joins, and — added by Q20 — the **node/mission delegation lease** (register
a node as delegated to a human owner; interim: static env at provisioning).

**Orchestrate** stays a skill (policy, not a component) and goes **mission-native** when
missions ship: execution state lives in the mission (the board absorbs the ledger half;
the narrative half survives as `artifacts/journal.md`, an orchestrate-owned convention
missions know nothing about). Run refs die — the run IS the mission.

#### 2. Dependency and awareness rules

```
  session service →  (nothing)      facts out, policy elsewhere
  missions        →  (nothing)      plain files + git + Backlog.md
  orchestrate     →  missions, herder
  herder          →  missions (very mission-aware, optional),
                     session store (enrichment joins)
```

- Arrows never point up. **Awareness is asymmetric by doctrine (Q17): herder may be very
  mission-aware; missions are completely herder-unaware.** Missions may carry opaque
  label-grade strings (authority, assignee, owner) that herder-side machinery resolves; no
  guid, seat, or run ref ever lands in a mission file.
- **Nodes ship facts, never verdicts** (Q20). All interpretation is view-time (display
  precedence in the store, revisable without touching nodes) or set-time in the component
  with context (herder stamps `SESSION_OWNER` from mission owner or node lease).

#### 3. Identity and attribution

- **Universal spine: (tool, session_id)** — with the prior-art amendment that file identity
  is session-uuid + content fingerprint, never path or inode.
- **Labels join the surfaces.** One vocabulary across mission surfaces and herder: opaque
  label-grade names. View-time joins (assignee → label → guid → sids → session store) are
  herder-side and best-effort by design. Supersedes D2's guid-keying.
- **`SESSION_OWNER`** (one var across all components; MISSIONS_ACTOR dead): declared at the
  root of a work tree, inherited through spawns, read by the shipper via /proc correlation
  (validated live; prior process-correlation evidence precedent). Codex: exact per-session (open rollout fd). Claude:
  (node, os-user, cwd) granularity — unanimous ⇒ stamp, else honest absence; hooks are an
  optional exactness upgrade, never a dependency. Correlations once observed are remembered
  (cursor registry) — process death never retracts a stamp. macOS: no correlation at all;
  personal devices are covered by tailnet identity. **Attribution is never authentication.**
- Upstream ask worth filing: Claude Code exposing its session id in process env deletes the
  cwd-ambiguity class.

#### 4. Killed / superseded (cumulative with v1 §5)

| Item | Ruling |
|---|---|
| events.jsonl (and `mission log`) | STRONG KILL (Q15) — journal/git/board carry it |
| Hooks as attribution bridge | Killed (Q20) — ergonomics; /proc correlation instead |
| Node-side attribution ladder | Superseded (Q20) — facts shipped, policy view-time |
| D2 guid-keyed mission membership | Superseded (Q17) — label-keyed, guid-free |
| MISSIONS_ACTOR | Dead — one var, SESSION_OWNER |
| "Hub" as a concept | Retired (Q2) — session store ≠ herd server |
| Separate execution ledger file | Decomposed (Q14) — board ledger + artifacts/journal.md |
| Run refs / run ids | Dead (Q17) — the run is the mission |
| Daemon designs B and C | Rejected (Q10) — board evidence + ratified §10 |
| OTel as transcript transport | Disqualified (prior-art memo) |

#### 5. Open items

1. **Herd-server projection details** — the last ungrilled §6b item; gated with phase 1b.
   Now also owns the delegation-lease design (Q20).
2. **Mission spec ratification** — in flight (mission-spec lane). Owner-confirmed and folded
   into the draft (2026-07-09): **SESSION_OWNER is the final cross-surface name**; mission
   `owner:` frontmatter (human attribution) is distinct from `authority:`; owner stamped
   `--owner` > SESSION_OWNER > OS user at `mission new`, echoed with its source; absence of
   SESSION_OWNER stays meaningful; git identity is suggestion-never-canonical. Read
   mechanics remain session-service business.
3. **Phase-1a implementation** — follow-up filed; errata adjudication pending
   (E-2 v2-only deviation and E-10 turnover coupling are explicit steward calls).
4. Upstream asks: Claude Code sid-in-env; the existing upstream batch is unaffected.

#### 6. Build order (updated)

1. **Mission spec → mission CLI + skill** — spec in flight now; unblocked, standalone.
2. **Session service v1** — shipper (per-user, cross-platform) + mirror + index + one page;
   mechanisms per the prior-art memo; SESSION_OWNER read is Linux-only.
3. **Herder phase 1a** (in flight on the board) → orchestrate goes mission-native once the
   mission CLI exists.
4. **Herd server** (observation first): spoke + snapshot-overlays + delegation lease +
   projection — design pass when phase 1a bakes.

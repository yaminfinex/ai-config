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

Current state: the service implementation and deployment artifacts are built. The read-only surface remains open on owner visual sign-off, and a small pre-fleet scalability/robustness batch remains on the board. The session-service plan stays live until those items close.

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

Current state: the mission format, CLI, companion skill, tests, and repository integration are shipped. The ratified mission spec is the source of truth.

### Herder: command and control

Herder owns active agent seats, addressing, launch, delivery, observation, lineage, and culling. Its registry is the authority for herder state.

The node presence uses the observer-first shape selected by the node-daemon design pass:

- the observer watches every seated session regardless of whether it was spawned or enrolled;
- it has no privileged write path and appends observations through the same locked writer as CLI verbs;
- it is disposable, has no handoff protocol, and reconstructs state on every boot;
- local commands and reads continue when it is absent;
- a registry write daemon remains rejected.

Current state: the universal observer is implemented and incorporated into the ratified herder spec. The later herd-server tier is not built.

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

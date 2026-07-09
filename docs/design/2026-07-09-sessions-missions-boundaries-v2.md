---
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
---

# Boundaries v2

Three components, separately adoptable, composing upward only. Target: **team multiplayer**
(Q1) — transcript content is team-visible; nodes are server-ish machines plus personal
devices; the word "hub" is retired (Q2) in favor of two distinct central surfaces (the
session store and the herd server).

## 1. The components

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
missions ship: run state lives in the mission (board absorbs the run-log's ledger half;
the narrative half survives as `artifacts/journal.md`, an orchestrate-owned convention
missions know nothing about). Run refs die — the run IS the mission.

## 2. Dependency and awareness rules

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

## 3. Identity and attribution

- **Universal spine: (tool, session_id)** — with the prior-art amendment that file identity
  is session-uuid + content fingerprint, never path or inode.
- **Labels join the surfaces.** One vocabulary across mission surfaces and herder: opaque
  label-grade names. View-time joins (assignee → label → guid → sids → session store) are
  herder-side and best-effort by design. Supersedes D2's guid-keying.
- **`SESSION_OWNER`** (one var across all components; MISSIONS_ACTOR dead): declared at the
  root of a work tree, inherited through spawns, read by the shipper via /proc correlation
  (validated live; TASK-045 precedent). Codex: exact per-session (open rollout fd). Claude:
  (node, os-user, cwd) granularity — unanimous ⇒ stamp, else honest absence; hooks are an
  optional exactness upgrade, never a dependency. Correlations once observed are remembered
  (cursor registry) — process death never retracts a stamp. macOS: no correlation at all;
  personal devices are covered by tailnet identity. **Attribution is never authentication.**
- Upstream ask worth filing: Claude Code exposing its session id in process env deletes the
  cwd-ambiguity class.

## 4. Killed / superseded (cumulative with v1 §5)

| Item | Ruling |
|---|---|
| events.jsonl (and `mission log`) | STRONG KILL (Q15) — journal/git/board carry it |
| Hooks as attribution bridge | Killed (Q20) — ergonomics; /proc correlation instead |
| Node-side attribution ladder | Superseded (Q20) — facts shipped, policy view-time |
| D2 guid-keyed mission membership | Superseded (Q17) — label-keyed, guid-free |
| MISSIONS_ACTOR | Dead — one var, SESSION_OWNER |
| "Hub" as a concept | Retired (Q2) — session store ≠ herd server |
| Run-log as a file | Decomposed (Q14) — board ledger + artifacts/journal.md |
| Run refs / run ids | Dead (Q17) — the run is the mission |
| Daemon designs B and C | Rejected (Q10) — board evidence + ratified §10 |
| OTel as transcript transport | Disqualified (prior-art memo) |

## 5. Open items

1. **Herd-server projection details** — the last ungrilled §6b item; gated with phase 1b.
   Now also owns the delegation-lease design (Q20).
2. **Mission spec ratification** — in flight (mission-spec lane); owner-vs-authority field
   naming and SESSION_OWNER land there mission-side.
3. **Phase-1a implementation** — task filed on run-herder-dx; errata adjudication pending
   (E-2 v2-only deviation and E-10 turnover coupling are explicit steward calls).
4. Upstream asks: Claude Code sid-in-env; (existing TASK-029 batch unaffected).

## 6. Build order (updated)

1. **Mission spec → mission CLI + skill** — spec in flight now; unblocked, standalone.
2. **Session service v1** — shipper (per-user, cross-platform) + mirror + index + one page;
   mechanisms per the prior-art memo; SESSION_OWNER read is Linux-only.
3. **Herder phase 1a** (in flight on the board) → orchestrate goes mission-native once the
   mission CLI exists.
4. **Herd server** (observation first): spoke + snapshot-overlays + delegation lease +
   projection — design pass when phase 1a bakes.

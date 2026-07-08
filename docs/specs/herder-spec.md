# Herder Spec

Status: **RATIFIED 2026-07-08** (owner walkthrough, decisions D1–D12 confirmed; D5 teams-kill
confirmed explicitly; migration dormant-default resolved under D12). This document is the
ground truth for herder's shape: ubiquitous language, domain model, expected behaviour,
high-level design, and acceptance scenarios. Implementation plans derive from it; it does not
narrate how we got here — the derivation working-memory was pruned with its branch, and this
document stands alone.

---

## 1. Purpose & scope

Herder exists because running plans across many agent sessions needs plumbing that is a
**program, not prose**. The orchestration mechanics — spawn a worker, brief it, address it,
watch it, kill it — were previously improvised per run by skills driving the substrates
directly, and were flaky in exactly the way improvised plumbing always is: unverified prompt
delivery, guessed liveness, addressing that rotted mid-run.

Two substrates each carry half of what fleet operation needs. **herdr** (terminal daemon:
workspaces/tabs/panes) is the visual surface — you watch agents work, arrange them, intervene.
**hcom** (message bus: names, hooks, events) is addressing, delivery, and status. hcom alone
would have gone most of the distance, but loses the visual control; neither alone is enough.

Herder is the **owned front door** to that capability: a Go CLI that provisions, addresses,
observes, and retires LLM agent sessions across both substrates — and any others adopted later —
closing the gaps between them consistently. It is defined by being the consistent entry point,
not by its current feature list; today that job is mostly joining and integration. The deepest
gap it closes is identity: no substrate keeps one durable handle for a conversation (pane ids
are positional, bus names churn, tool session ids are tool-scoped and mutable), so herder owns
durable, tool-agnostic identity for conversations, plus the addressing and liveness bookkeeping
around it.

In scope: identity, seats, labels, lifecycle (spawn/enroll/fork/resume/cull/retire), delivery
addressing, status observation, node/epoch bookkeeping, the registry that records it all.
Out of scope (§10): multi-machine behaviour, multi-reader observation, automatic restart policy,
run orchestration protocols (the `orchestrate` skill's domain).

## 2. Ubiquitous language

| Term | Meaning |
|---|---|
| **session** | One linear conversation transcript, tool-agnostic. What the **guid** denotes. Never deleted. |
| **guid** (herder session id) | Herder-minted surrogate key for a session. Used *instead of* tool sids because those are mutable, late-arriving, and tool-scoped. Exists before/without any tool sid; unique across tools, nodes, and time. |
| **sid** (tool session id) | A tool's native session id (claude UUIDv4, codex UUIDv7). A mutable, late-arriving, tool-scoped *attribute* of a session — never its identity. |
| **seat** | A binding of a session to the place it runs. Defines liveness. Kinds: `herdr` (terminal binding), `process` (bare process binding; headless). |
| **occupant** | The live tool process currently realising a session in a seat. Seat liveness means *occupant* liveness. |
| **vacant seat** | A surviving terminal whose occupant exited. Re-seatable in place. |
| **label** | Optional addressing lease on exactly one session. Non-expiring: released only by retire or explicit transfer — never by liveness, TTL, or co-location. |
| **lease / transfer** | The label's tenure semantics. Transfer = explicit anointing naming the current holder. Never automatic. |
| **turnover** | `/clear`: the seat survives, the session is evicted, a new session (new guid, unlabelled, unbriefed) is registered in the same seat. |
| **eviction record** | What a turnover leaves behind: who was evicted, when, who now sits there. A record, not a forwarding address. |
| **lineage** | ≤1 parent edge per session (`forked_from` ⊕ `cleared_from`), 0..\* children. |
| **brief / briefed** | The role instructions delivered to a session. A turnover newcomer is unbriefed until re-briefed. |
| **continuity** | Epistemic status of a session's transcript identity: `confirmed` (sid-verified) or `assumed` (sid-less; recorded, surfaced, never faked). |
| **recognition** | Matching a newly observed occupant to an existing session by (tool, sid, tool-native scope). |
| **reconciliation** | Re-verifying seat bindings after an epoch/node mismatch: probe, then re-stamp or unseat. Triggered, never assumed. |
| **retire** | Explicitly closing a session for good. Releases its label. Distinct from unseating. |
| **lost** | A session whose transcript is verified gone from the tool layer. Not resumable; never deleted. |
| **state dir** | `$HERDER_STATE_DIR`: herder's node-local home — the live registry file plus the `node_id` marker. One writable registry per state dir; it travels with the home directory. |
| **node** | A (machine, user) environment — the locality everything else is scoped to. Identity = a pure-random `node_id` minted lazily on the first registry write, living with the state (registry + marker). A data-model affordance: attribution + write authority, no behaviour of its own. |
| **owning node** | The node whose id the local state dir carries (marker + registry agreeing). The only node the local herder writes as. |
| **namespace** | One hcom universe = one HCOM_DIR (own db, own names, own epochs). Identity = a minted `namespace_id` anchored *inside* the dir; the path is a description, never an identity. Dormant bookkeeping; posture is one namespace per node. |
| **epoch** | One continuous lifetime of a substrate authority: hcom epoch = one db lifetime within a namespace; herdr epoch = one server lifetime. Herder mints surrogate epoch ids on first observation. |
| **registry** | Herder's append-only JSONL event log; latest-row-per-guid projection is current state. The sole seat→session authority. |
| **sidecar** | Per-occupant background process: status bridge (hcom events → herdr agent status), enrolment, name capture, turnover detection. |
| **cull** | Destroy a seat. The session goes unseated (dormant, resumable) — culling is not retirement. |

## 3. Domain model

Three core concepts. **Identity lives in the session, liveness in the seat, addressing in the
label.** (The factoring matches systemd unit/invocation/bus-name, OTP child-spec/pid/name,
k8s Deployment/Pod/Service, and hcom's own session-binding/process-binding/name triple —
herder makes hcom's epoch-scoped model durable; it does not invent an ontology.)

Cardinalities:

```
label 0..1 ──lease──→ session 1 ──seated in──→ 0..1 seat        (at an instant)
session ↔ seat: 1..* both directions over time (resume / turnover)
session lineage: ≤1 parent edge (forked_from ⊕ cleared_from), 0..* children
seat: holds ≤1 session at a time; kind ∈ {herdr, process}
```

### 3.1 Invariants

1. **One guid, one linear transcript.** Any operation that makes histories diverge — fork,
   /clear, rewind-and-continue — mints a new guid with a parent edge. A guid is never a
   container of branches.
2. **Fork/clear symmetry.** Fork copies the conversation to a new seat; /clear keeps the seat
   and evicts the conversation. Both mint a new guid. `/compact` is a non-event.
3. **Sessions are never deleted.** All lifecycle is appended state: seated, unseated, retired,
   lost.
4. **Only seated sessions can respond.** Send resolves label|guid → session → seat; unseated →
   undeliverable, reporting the unseating fact (on turnover: the eviction record).
5. **Double-seat refusal.** A session seated elsewhere is never seated again (double-resume
   interleaves transcripts). Refusal names the occupying seat and suggests fork.
6. **Label uniqueness among non-retired sessions**, enforced atomically with the write (§5.2).
7. **Label transfer is explicit.** Bare collision refuses; takeover requires naming the current
   holder; the displaced holder keeps guid and history, loses the alias, and is notified.
8. **Env-carried identity is birth provenance only.** `HERDER_GUID` seeds registration; no
   resolution path reads it afterwards. The registry is the sole seat→session authority.
9. **Uncertainty is recorded, not faked.** Sid-less sessions carry `continuity: assumed`,
   shown in `list` and warned at send time.
10. **Every record is node-attributed.** All registry rows carry the writing node's id (§6.1) —
    a zero-cost stamp that keeps attribution unambiguous if the state dir ever moves or scope
    ever widens; no behaviour hangs off it today.
11. **Epoch/node mismatch triggers reconciliation, never a verdict.** Stale bindings are
    re-verified, then re-stamped or unseated; sends against unreconciled bindings refuse.
12. **Unknown-node rows are anomalies, not peers.** The local herder writes only as the owning
    node. A row attributed to any other node id (a synced-in or hand-copied fragment) is
    flagged loudly by the loader, surfaced in `list`, and never written to or adjudicated.
    *Unknown* means no `node_registered` record for that id exists in this file: rows from a
    prior local identity (pre-`node init --new`) are history, not anomalies — their sessions
    stay resolvable and manageable, and any new row they accrue stamps the current owning
    node. Cross-node behaviour is out of scope (§10).

### 3.2 Session state machine

```
register (spawn|enroll|shim|fork|clear-newcomer) ──→ SEATED
SEATED    ──unseat (cull | crash observed | displaced)──→ UNSEATED    [idempotent]
UNSEATED  ──seat (resume | recognition)──→ SEATED                     [refuse if seated elsewhere]
UNSEATED  ──retire──→ RETIRED                                         [releases label; idempotent]
RETIRED   ──reopen (explicit, rare)──→ UNSEATED                       [always unlabelled]
UNSEATED  ──transcript verified gone──→ LOST
```

Illegal: SEATED → seated in another seat; RETIRED → SEATED directly; row deletion.
Label sub-machine (`unlabelled ⇄ labelled` via label/transfer/release-on-retire) is orthogonal.

### 3.3 Seat model

- `herdr` seat: key `terminal_id`, addressed by `pane_id` (run-scoped: stable within a server
  run, re-keyed by cross-workspace moves, reshuffled across restarts — display only, never a
  key; terminal_id survives moves but not restarts, which is the epoch machinery's job, §6.3).
  `process` seat: key pid + hcom process binding.
- **Seat liveness = occupant liveness.** Terminal presence is necessary, not sufficient: if the
  occupant exits while the terminal survives, the session is unseated (recorded) and the seat is
  vacant — re-seatable in place.
- Unseating is a recorded event appended by whichever component first observes seat death,
  never a per-caller inference.
- Every seat binding carries: kind, key, `node`, `namespace`, `hcom_epoch`, `herdr_epoch`
  (herder epoch ids, §6), `hcom_name`, and a last-confirmed timestamp.

## 4. Components — from model to machinery

A seat binding is a join row across three layers — the herdr terminal, the hcom name, the
tool's transcript — and each layer's handle expires on its own schedule. Worse, the joins
change *while no herder command is running*: an occupant `/clear`s at 2am, crashes, or a human
hand-types `claude` into a pane herder never saw. A command wrapper that learns things only
when invoked is permanently stale — the original flakiness. So the architecture is:
**commands write intent, a per-occupant observer writes observations, the registry arbitrates,
everything else is a cache.**

```
        operator / orchestrating agent
                   │ spawn · send · wait · list · cull · resume · fork · rename · retire
                   ▼
           ┌──────────────┐        every target = a session
           │  herder CLI  │        (guid | short guid | label)
           └──┬────────▲──┘
  write intent│        │ read truth
              ▼        │
        ┌──────────────┴──┐
        │ registry (JSONL)│   sole authority: guid · label · seat · lineage
        └────────▲────────┘
                 │ observations: enrol · hcom name · turnover · unseat
          ┌──────┴───────┐
          │   sidecar    │   one per occupant, forked by `herder launch`
          └─┬─────▲────┬─┘   (choke point; PATH shims route hand-typed claude/codex into it)
     status │     │    │ anchors guid at birth
     bridge ▼     │    ▼
   ┌────────┐  ┌──────────┐  ┌────────────────┐
   │ herdr  │  │   hcom   │  │ tool process   │
   │ pane = │  │ name +   │  │ transcript =   │
   │ seat   │  │ events   │  │ tool sid       │
   └────────┘  └──────────┘  └────────────────┘
     one seat binding joins all three layers — each handle expires on its own
     schedule; the registry row is the only thing that survives all of them
```

- **`herder` CLI** — the only interface; subcommands in §7. One resolver: any `<target>` means
  a *session* (guid | short-guid | label). Seats are addressable only via explicit escapes
  (`term_*`, pane id).
- **Registry** — §5. The sole seat→session authority.
- **`herder launch`** (choke point) — every managed launch flows through it; it forks the
  sidecar before exec'ing the tool.
- **Sidecar** (hidden subcommand, one per occupant) — status bridge (hcom events →
  `pane.report_agent`), enrolment + name capture, turnover detection (§8.1), guid anchoring
  (§5.3), and sid reporting: it pushes the sid it learns from the bus to the seat substrate
  (`pane.report_agent_session`), making herder its own sid reporter for the §8.3 probe.
  Exits with its occupant; distinguishes "bus errored" from "row absent" before
  counting an occupant miss.
- **PATH shims** — wrap hand-typed `claude`/`codex` into `herder launch` so hand launches join
  the same substrate (activation staged separately).

## 5. The registry

### 5.1 Shape

Append-only JSONL; **one live file per state dir** (§2: the registry plus node marker under
`$HERDER_STATE_DIR`). The file is self-describing — it contains the node record of the node
that writes it — and rows attributed to any other node are loader-flagged anomalies (§3.1-12).
Snapshot-per-event, not event sourcing: every row names the event that caused it.

**Storage decision: append-only JSONL; sqlite and hybrid rejected (§10).** The registry's
defining operations are file-shaped: file order is the authoritative order, backup is
backup-like-source, rotation archives are plain files beside the log, and the log stays
greppable/diffable in a pager. A sqlite store answers none of these better and several worse
(WAL/-shm sidecar files under a synced home dir, a whole-db corruption unit vs one quarantined
line, cgo or a multi-MB pure-Go driver on a zero-dependency binary) — and it would put herder's
durable ground truth on the same artifact class whose epoch resets herder defends against
(§6.3). sqlite's real advantages — transactional label uniqueness, torn-write safety, indexed
reads — are obtained instead by the §5.2 flock discipline, the torn-row quarantine, and the
rotation stance below.

**Growth: rotate, never delete.** When the live file grows past a threshold, it is moved to a
read-only archive beside the log and the live file is reseeded with the latest row per
non-retired guid (legal: rows are self-contained snapshots). Rows are never destroyed; archives
stay greppable; `list --all` and lineage resolution may consult archives.

**Projection.** Partition by `kind` first (absent = `session`): sessions collapse per guid,
node records per node_id, epoch records per epoch_id; rows of one kind are invisible to the
others' resolution. Within a file, later row wins — file order is authoritative; `recorded_at`
is display metadata, **never an ordering key**.

```jsonc
// session record (kind defaults to "session")
{
  "guid": "…",
  "event": "registered | seated | unseated | labelled | label_transferred
           | retired | reopened | recognised | reconciled",
  "recorded_at": "…",                       // append time — distinct from provenance.ts
  "node": "<node_id>",                       // writer attribution (§3.1-10)
  "state": "seated | unseated | retired | lost",
  "label": "…", "role": "…", "tool": "claude | codex",
  "seat": {                                  // present while seated
    "kind": "herdr | process",
    "node": "<node_id>",                     // where the seat physically is
    "terminal_id": "…", "pane_id": "…",      // kind=herdr (pane_id display-only)
    "pid": 0,                                // kind=process
    "hcom_name": "…", "namespace": "<namespace_id>",
    "hcom_epoch": "…", "herdr_epoch": "…",   // herder epoch ids
    "confirmed_at": "…"
  },
  "sids": [                                  // append-only history; newest = current
    {"sid": "…", "scope": "<project-dir | global>", "observed_at": "…",
     "source": "hook | harvest | recognition"}
  ],
  "continuity": "confirmed | assumed",       // row-scoped epistemic claim
  "lineage": {"forked_from": null, "cleared_from": null,
              "displaced_by": null, "resume_failed_from": null},
  "provenance": {                            // frozen birth record; never changes
    "mechanism": "spawn | shim | enroll | fork | clear",
    "spawned_by": "<guid | user>", "cwd": "…", "ts": "…"
  }
}

// node / namespace / epoch records
{"kind": "node", "event": "node_registered", "node_id": "…",
 "user": "…", "hostname": "…",                          // descriptive, for humans
 "recorded_at": "…"}
{"kind": "namespace", "event": "namespace_observed", "namespace_id": "…",
 "node": "…", "path": "<HCOM_DIR as seen from this node>", "recorded_at": "…"}
{"kind": "epoch", "event": "epoch_observed", "epoch_id": "…",
 "substrate": "hcom | herdr", "node": "…",
 "namespace": "<namespace_id | absent for herdr>", "fingerprint": {…}, "recorded_at": "…"}
```

Lineage double-entry: `cleared_from` on the newcomer (written once at registration) is
authoritative; `displaced_by` on the evicted session is derived convenience. Turnover appends
child-first; on disagreement the child edge wins.

### 5.2 Write discipline

- All label writes and seat transitions — and the node mint itself — execute **load → validate →
  append atomically under an exclusive registry lock** (flock). A holder-check is only a fence
  if atomic with the write. flock is a single-machine primitive: on filesystems where it cannot
  be reliably acquired (network mounts), the write refuses rather than proceeding unlocked.
- **Label uniqueness is checked over the full projection.**
- Rows carry only fields their writer owns — writers may *change* only owned fields, but every
  appended row remains a full self-contained snapshot (§5.1): non-owned fields are carried
  forward from the projection read under the same lock, and the merge lives once, in the shared
  locked append helper, not in each writer. Absence of a field in a writer's patch means
  carry-forward, never clear; clearing is itself an owned operation (unseat clears `seat{}`).
  Envelope fields — `event`, `recorded_at`, and the row-level `node` (writer attribution,
  §3.1-10) — are stamped fresh by the helper on every append and are never carried forward.
  No append may revert a concurrent unrelated write (a stale status enrichment cannot undo a
  rename; a late registration cannot mask a recognised seat's `hcom_name`; nothing resurrects
  a culled session).
- Appends are idempotent regardless of driver — the §3.2 `[idempotent]` markers are
  caller-agnostic, hook- and user-driven alike: turnover registration dedupes on (seat, new
  sid); unseat/retire/recognise are **confirmed no-ops** when the projection already shows the
  target state **and the writer's owned patch adds no new information** — no row is appended,
  and the command reports success plus the previously recorded fact (never a false negative,
  never an annotation-rewrite of why a seat died). A first verified observation on a row that
  lacks it (e.g. close-annotating a never-annotated migrated corpse after probing) IS new
  information: one annotation row is legal; owned observation fields are write-once per
  episode — later differing claims no-op, and unverifiable claims are rendered honestly,
  never recorded (§3.1-9).
- The loader **quarantines** malformed rows (skip + warn); one torn line never disables the CLI.
- Projection anomalies (two live holders of one label; one session in two seats) resolve
  deterministically and loudly — flagged conflict, never silent tie-breaking.

### 5.3 Durability

- At birth the sidecar anchors the guid in one lower layer (hcom launch tag / marker adjacent
  to the transcript) so sid↔guid and label↔guid joins are rebuildable for the current epoch.
  The node_id marker (§6.1) and namespace anchor (§6.2) are the same doctrine one level up.
- The registry file is the single durable store of guids, labels, and lineage; it is expected
  to be backed up like source. **Backup ≠ sync**: the registry is node-local state and must not
  sit under bidirectional file synchronisation (whole-file sync silently drops concurrent
  appends); the §6.1 gate catches a synced-in registry, it does not make syncing safe.

### 5.4 Migration from the v1 registry (one-shot, at first v2 write)

The pre-spec registry is status-snapshot rows (`status: active | closed`, flat `hcom_*`
fields, no kind/node/event/state/seat/sids/continuity/lineage on any row). It migrates once,
by rewrite, under the §5.2 lock:

1. **Rotate first.** The untouched v1 file becomes the first rotation archive (§5.1 growth
   stance); rows are never destroyed. The live file is reseeded with one v2 row per
   non-retired guid.
2. **Mapping.** `status: closed` ⇒ `state: retired`. `status: active` ⇒ `unseated`
   (**dormant default — migration performs no live probing**; a genuinely live occupant is
   re-seated by its sidecar's next observation, an explicit enroll, or §8.3 reconciliation).
   Migration must never seat a session verbatim: on the reference machine at spec time,
   ~28 of 34 latest-active guids were dead sessions never culled, and a storage-wave
   migration should not grow a substrate dependency just to classify them.
3. **Attribution.** Migrated rows carry the freshly-minted local node_id and a migration
   event marker, so backfill stays distinguishable from live observation. Absent-node in an
   unmigrated file is a bootstrap state, not an anomaly — §3.1-12 applies only after the mint.
4. **Vocabulary.** `tool` covers every agent kind herder launches (bash included), not only
   claude|codex. `short_guid` is derived display, never stored data. Unknown legacy keys
   (e.g. `team`) are **ignored, not quarantined** — quarantine is for malformed rows only.
5. **Sids & continuity.** `provenance.tool_session_id` seeds `sids[]` where present (~17% of
   guids on the reference machine); every other migrated session carries
   `continuity: assumed`.
6. **Namespaces.** Flat `hcom_dir` paths mint/attach namespace ids (§6.2) at migration;
   retired teams-era dirs (`~/.hcom/teams/*`) become distinct namespaces, never grandfathered
   into the default.
7. **Idempotence.** Byte-duplicate v1 rows collapse to one v2 event; re-running migration on
   an already-migrated file is a no-op.

## 6. Nodes, namespaces, epochs

Three ids with three different jobs and different maturity — motivated separately, not as one
mechanism. **Node** answers "whose record is this / who may write" (a dormant attribution
stamp). **Namespace** answers "which bus universe is this name from" (a dormant identity
anchor). **Epoch** answers "can I still trust this cached binding" (live bookkeeping, used by
reconciliation every day). The join between substrate lifetimes and herder is always an
explicit registry event, never an ambient inference.

### 6.1 Node

A node is herder's durable "here": the (machine, user) environment a registry lives in. In a
single-machine world the node is invisible — it is a **data-model affordance, not behaviour**:
its one job is attribution that survives the state dir moving (new laptop, restored backup),
plus serving as write authority. Hardware identity is deliberately NOT part of the model.

- `node_id` is a pure-random mint, created **lazily on the first registry write** (under the
  §5.2 lock; concurrent first writes converge on one id): the write appends `node_registered`
  {node_id, user, hostname} and stores the id in a marker (`$HERDER_STATE_DIR/node_id`).
  Username and hostname are descriptive, for humans. `herder node init` performs the same mint
  explicitly (idempotent); it is never a prerequisite.
- **Identity travels with the state.** Registry and marker live in the home directory, so a
  migrated home keeps its node — same node on new hardware, nothing re-keyed. A true clone
  (two copies, one node_id) is accepted residual risk: humanly obvious, repaired by
  `node init --new` on the copy; the append-only log keeps every prior row intact.
- **The gate.** Registry-writing commands require marker and registry to agree on the local
  node_id. Both absent ⇒ the lazy mint (bootstrap). Disagreeing or half-present ⇒ refuse with
  repair guidance (`herder node init`) — this is the enforcement half of backup-not-sync
  (§5.3): it catches a synced-in registry or half-copied state dir before it writes garbage.
  Single-machine safety, not a multi-node feature.

### 6.2 Namespace

One HCOM_DIR is one complete hcom universe: its own db, its own names, its own epochs. A path
cannot identify one — two machines' `~/.hcom` are different universes with identical paths — so
on first contact with an unanchored HCOM_DIR, herder mints `namespace_id`, writes it into the
dir (marker at HCOM_DIR root — survives `hcom reset`; the §5.3 anchor doctrine one level up),
and appends `namespace_observed`. Seat and epoch records reference `namespace_id`, never a
path. hcom itself has no team concept (its `tag` is a soft label on one shared bus), and herder
no longer models teams (§10); the operating posture is **one namespace per node**. Dormant
bookkeeping — the identity rule is fixed now so nothing re-keys if scope ever widens.

### 6.3 Epochs (implementation tier)

Epochs are oracle bookkeeping, not domain identity: they answer "can I still trust this cached
binding (hcom name, terminal id), or did the substrate restart under me?" — nothing more. They
appear in seat bindings and §8.3 reconciliation, never in addressing or human language.

- **hcom epoch.** One db lifetime within a namespace. Fingerprint: db birth time + inode.
  hcom names are never recycled within an epoch and are forgotten across epochs — in-flight
  send safety leans on the former and dissolves at the boundary.
- **herdr epoch.** One server lifetime (no namespace — herdr is bus-agnostic). No boot id is
  exposed; epochs are probe-inferred (a registry terminal_id unknown to the daemon implies a
  boundary). `herdr update` performs a live handoff (occupants survive); a cold restart kills
  occupants. §8.3 reconciliation makes both safe without distinguishing them a priori.
- First observation appends `epoch_observed` with a minted per-node `epoch_id`.

## 7. Command surface — expected behaviour

| Command | Behaviour |
|---|---|
| `node init [--new]` | Explicit form of the lazy node mint (§6.1); idempotent, locked. Normally never needed — the first registry write mints transparently. `--new` mints a fresh node_id (clone repair). |
| `spawn` | Register session + create seat + seat + brief (+ label). Readiness observed via the status bridge. Emits guid + label + seat. |
| `launch` | The substrate primitive spawn rides on: the §4 choke point + sidecar in the current context. Exception: claude `-p`/`--print` one-shots bypass the bus and sidecar entirely (exec the tool directly, no hcom required) — they return their answer and never become sessions. |
| `enroll` | Adopt the session found in an existing seat (same code path as sidecar enrolment). Label collisions refuse (§3.1-6). |
| `send <target>` | Resolve label\|guid → session → seat; deliver via bus; **receipt always includes the resolved guid**. Refusals: unseated (report unseating/eviction), unreconciled binding, name↔sid disagreement. Warnings: unbriefed, `continuity: assumed`. Dereference-at-issue is the race semantics. |
| `wait <target>` | Observe the seat (herdr status via bridge; process seats: bus status). |
| `compact <steer>` | Queue a steered `/compact` into the **caller's own** pane. Self-only by construction: no target argument exists; self-identity is proven via the registry before anything is typed. The one ruled exception to bus-only delivery. No registry event — `/compact` is a non-event (§3.1-2, AC-9). |
| `list` | Sessions × (label, seat, liveness, continuity). Default: seated + recently unseated; `--all` includes retired/lost (and rotation archives). |
| `resolve <target>` | Print the session's current coordinates (`--hcom-name`, `--terminal`, `--pane`, `--guid`) for composition with raw substrate commands. The general escape hatch (below). |
| `cull <target>` | Destroy the seat; session → unseated. Never touches other sessions' rows; kills the sidecar last (no post-cull resurrection). |
| `resume <target>` | Re-seat an unseated session, same guid. Refuses: already seated (names the seat, suggests fork), retired, lost. **Verifies** the tool's reported sid equals the requested sid; mismatch ⇒ turnover (new guid, `resume_failed_from`), never a same-guid re-seat. Prefers sid over hcom name as the launch vehicle. |
| `fork <target>` | Register a new session from an existing one (`forked_from`); parent undisturbed; works on unseated **and retired** parents — transcript is the substrate, and retirement closes the session's occupancy and label, never its history (§3.1-3). Only a `lost` parent refuses (transcript verified gone: no substrate). |
| `rename <target> <label>` | Mint/move a label lease. Bare collision refuses; `--take-from <holder>` is the explicit takeover: atomic transfer + notify the displaced holder. Re-minting a previously-held name warns with the predecessor. |
| `retire <target>` | Close the session for good; releases the label. `reopen` (rare, explicit) returns it unlabelled. |

**Escape hatches.** Herder wraps the identity-bearing lifecycle verbs only; the substrates keep
large command surfaces of their own (hcom transcript/term/listen/events/bundle; herdr
pane/tab/workspace ops). The policy: `herder resolve` exposes the current coordinates so any
substrate command composes — `hcom transcript $(herder resolve fix-1 --hcom-name)` — and herder
proxies a substrate command only where the registry join adds real value (candidates:
`transcript` stitched across turnovers via lineage; `term`/inject), decided by real-world
usage. Substrate-global commands (`hcom reset`, `herdr update`/`server stop`) are never
wrapped; herder observes their effects as epoch boundaries (§6.3, §8.3).

## 8. Recognition & reconciliation

### 8.1 Turnover detection (one rule, every path)

The sidecar watches its seat's sid. **Sid changed in my seat ⇒ turnover**: unseat the old
session (displaced), register the newcomer (new guid, `cleared_from`, unlabelled, unbriefed) —
both inside one lock, child-first. Exception: sid changed *with* transcript-continuity evidence
(prefix match / logical parent pointer) = the tool re-keyed the same conversation ⇒ `recognised`
row re-keys the same guid. This one rule serves spawned, shimmed, and enrolled sessions alike.

### 8.2 Recognition keys

- Key = **(tool, sid, tool-native scope)** — claude: (sid, project dir); codex: sid alone.
  A scope mismatch is a different conversation: fresh guid + collision note.
- **Seat-continuity beats sid-lookup for sid-less sessions**: a sid newly observed in a seat
  bound to a sid-less session attaches to that session (continuity upgrades assumed →
  confirmed). Sid-lookup applies only to unregistered seats.
- A sid that surfaces and *disagrees* with an assumed-continuity window ⇒ retroactive turnover:
  a `reconciled` correction row with the backdated displacement.

### 8.3 Epoch reconciliation

On any epoch/node mismatch between a seat binding and the live substrate:

1. Suspend trust in the binding; sends refuse until reconciled.
2. Probe: does the terminal exist, and does the pane's reported sid match the session's sid?
   For process seats: pid + bus status.
3. Outcomes: match ⇒ re-confirm + re-stamp epoch (`reconciled`); sid found live in a different
   seat ⇒ re-bind (seat migrated — the handoff case); neither ⇒ unseat (cold restart — the
   occupant genuinely died).

**Sid-probe precondition (normative).** herdr's per-pane sid exposure is report-only — the
substrate never scans for a sid; `agent_session` populates only when a reporter pushes it
(`pane.report_agent_session`). The probe therefore requires an active sid reporter: the herder
sidecar self-reporting the sid it already learns from the bus (preferred — no third-party
config writes, covers every tool herder launches, reuses the sidecar's ambiguity guard), or the
tool's herdr agent integration (`herdr integration install claude|codex`). Spawn and any future
doctor surface warn when neither reporter is active.

**Sid-less fallback (normative).** A probe returning no `agent_session` means *sid-less, not
dead*. Reconcile on the durable substrate key: `terminal_id` first, then a guarded
(tool, label, cwd) match that refuses on ambiguity. The outcome is re-confirmation at
`continuity: assumed` — never a fake match, never an unseat on absence-of-evidence alone; a
later sid report upgrades assumed → confirmed per §8.2.

This procedure is what makes herdr live-handoff updates, cold restarts, and hcom db resets all
safe without herder distinguishing them in advance. Reconciliation only ever adjudicates seats
recorded by the owning node; unknown-node rows (§3.1-12) are flagged, never probed or unseated.

## 9. Acceptance scenarios

Normative. Each is a high-level test case; implementation plans map suites onto them.
(Traceability: S/A refs point into the derivation docs.)

**Provisioning & addressing**

- **AC-1 spawn** — `spawn --role worker --label fix-1` yields a seated, briefed, labelled
  session; `send fix-1` delivers; receipt carries the guid. *(call-graph A)*
- **AC-2 hand launch** — a shimmed `claude` in a herdr pane appears in `list` with
  `mechanism: shim`, addressable and cullable like a spawned one. *(B)*
- **AC-3 enroll** — an unmanaged occupant adopted in place; label collision refuses. *(C)*
- **AC-4 undeliverable** — send to an unseated session refuses and reports the unseating fact.
- **AC-5 warnings** — send to an unbriefed or assumed-continuity session delivers with the
  respective warning; both states visible in `list`.

**Turnover**

- **AC-6 /clear** — occupant /clears: old session unseated keeping its label; newcomer
  registered (new guid, `cleared_from`, unlabelled, unbriefed) in the same seat; bus name churn
  absorbed. Identical behaviour for spawned, shimmed, and enrolled sessions. *(S1, S5)*
- **AC-7 eviction report** — post-turnover `send <label>` refuses with the eviction record:
  evicted guid, when, who now occupies the seat. *(S7)*
- **AC-8 fork disambiguation** — after turnover, `fork <old-guid>` forks the briefed
  conversation; `fork <new-guid>` forks the newcomer. The env pin must not graft the newcomer's
  sid onto the old guid. *(S4, A9, S22)*
- **AC-9 /compact** — no registry event; same guid, same seat. *(S10)*
- **AC-10 invisible turnover** — sid-less occupant starts over: seat continues under the same
  session with `continuity: assumed`; a later disagreeing sid produces a retroactive turnover
  correction. *(S6, A8)*

**Lifecycle**

- **AC-11 cull→resume** — cull unseats; resume re-seats the same guid in a new seat; label and
  lineage travel. *(S2)*
- **AC-12 crash** — seat death without cull converges to the same dormant state; unseating is
  recorded by the first observer. *(S9)*
- **AC-13 vacant seat** — occupant exits, terminal survives: session unseated, seat vacant,
  re-seatable in place. *(S9b, A5)*
- **AC-14 resume verification** — resume of a pruned/foreign transcript never re-seats the
  guid: tool-reported sid mismatch ⇒ new guid + `resume_failed_from`; verified-gone transcript
  ⇒ `lost`. *(S23, A10)*
- **AC-15 double-seat refusal** — resuming/recognising an already-seated session refuses,
  naming the occupying seat and suggesting fork. *(S8)*
- **AC-16 recognition** — raw `claude --resume` in an unregistered seat re-seats the existing
  guid by (sid, project dir); the same sid in a *different* project dir does not match. *(S12, S17)*
- **AC-17 retire** — retire releases the label and refuses resume; reopen returns the session
  unlabelled. *(S25, A6)*

**Labels**

- **AC-18 collision** — minting an existing non-retired label refuses — including labels held
  by unseated sessions.
- **AC-19 takeover** — `rename --take-from` transfers atomically; the displaced holder keeps
  guid/history, loses the alias, and is notified. *(S11)*
- **AC-20 re-mint** — re-minting a retired session's former label warns with the predecessor;
  historical resolution is recency-ordered, never guid-ordered.

**Nodes, namespaces, epochs**

- **AC-21 the gate + lazy mint** — the first registry write mints the node transparently
  (under the registry lock; concurrent first writes converge on one node). Thereafter, writes
  proceed only when marker and registry agree on the local node_id; disagreement or a
  half-present pair refuses with repair guidance (`herder node init`).
- **AC-22 hcom epoch reset** — after `hcom reset`, sends against pre-reset seats refuse and
  trigger reconciliation; delivery never happens on name-exists alone. *(S13, A4)*
- **AC-23 herdr cold restart** — reconciliation unseats all locally-seated herdr sessions
  (occupants died); sessions are dormant and resumable, not lost.
- **AC-24 herdr live handoff** — after `herdr update`, reconciliation re-confirms surviving
  occupants (sid probe where a reporter is active; terminal_id + guarded label/cwd match
  otherwise, re-confirming at `assumed` continuity) and re-stamps epochs; no healthy session
  is unseated. Note terminal ids may be reissued wholesale at handoff, so the fallback match
  must not assume terminal_id stability across the boundary.
- **AC-25 unknown-node rows** — a registry containing rows attributed to an unknown node id
  (a hand-copied fragment, a synced-in file) still loads: those rows are flagged anomalous in
  `list`, never written to, and never adjudicated by reconciliation; every command keeps
  working.
- **AC-26 namespace identity** — two default `~/.hcom` dirs on two machines are TWO namespaces
  despite identical paths; the anchored `namespace_id` survives `hcom reset`; name resolution
  never crosses namespaces.
- **AC-27 home migration** — a home directory moved to new hardware (registry + marker
  intact) keeps its node_id: same node, sessions resolve as local, seats reconcile to unseated
  (the occupants died in transit); nothing orphaned, nothing re-keyed.
- **AC-28 clone repair** — a cloned state dir (two copies, one node_id) is repaired by
  `node init --new` on the copy: fresh node_id for future writes; prior rows stay intact,
  attributed to the original.
- **AC-29 registry loss** — after rebuild-from-anchors, the node marker re-registers the SAME
  node_id: recovery is node-stable. *(extends AC-34)*
- **AC-30 kind partition** — node/namespace/epoch records are invisible to session resolution
  (and vice versa); a session-projection idiom over mixed rows must neither collapse them nor
  surface them as phantom sessions.

**Registry robustness**

- **AC-31 concurrent label writes** — two simultaneous claims of one label: exactly one wins;
  the loser gets the refusal; resolver and uniqueness check agree on the owner. *(S14)*
- **AC-32 stale enrichment** — a sidecar enrichment loaded before a rename cannot revert it;
  a status heartbeat cannot resurrect a culled session. *(S15)*
- **AC-33 torn row** — a malformed row is quarantined with a warning; every command still
  works. *(S16)*
- **AC-34 anchors** — with the registry lost, current-epoch sid↔guid and label↔guid joins are
  rebuildable from the birth anchors. *(S24)*

- **AC-36 v1 migration** — a v1 registry (status-snapshot rows) migrates one-shot: the v1
  file is archived untouched; closed ⇒ retired; active ⇒ unseated (dormant default, no live
  probing — no corpse is seated, and live occupants re-seat via observation/enroll/
  reconciliation); legacy keys ignored; migrated rows node-attributed and event-marked as
  migration; re-running is a no-op.

**Headless**

- **AC-35 process seat** — a headless (hcom `--headless`) session gets a `process` seat:
  liveness from pid/bus status; `wait`/`cull` degrade to bus-only; `send` unaffected; the
  double-seat guard verifies pid + epoch (not the vacuous terminal predicate). *(S19b)*

## 10. Non-goals (recorded decisions, not omissions)

- **Multi-machine / cross-node behaviour** — deliberately out of scope. When it comes, it
  rides a future central-orchestrator/server design, not registry shipping or bus relay
  (hcom relay is unused and unmodelled). Herder keeps only zero-cost residue: the node
  attribution stamp (§3.1-10), the §6.1 gate, and the namespace anchor.
- **Teams** — dropped from the model. hcom has no team concept (its `tag` is a soft label on
  one shared bus); herder's former HCOM_DIR-per-team construct is retired. Posture: one
  namespace per node; per-run traffic grouping uses hcom tags. Removal-day requirements
  (recorded from the live survey, 2026-07-08): the spawn hook template and the orchestrate
  skill stop advertising `--team`; legacy `team` row keys keep parsing (§5.4); the tag
  replacement needs spawn-time tag propagation, tag-filtered list/events ergonomics, and a
  name-collision story — hard message isolation is explicitly not reproduced.
- **sqlite / hybrid registry store** — considered and rejected (§5.1). A rebuildable
  projection cache remains a compatible future optimization because JSONL stays the log of
  record.
- **One-shot ask of an unseated session** — future direction: a single question to a dormant
  session (an ephemeral seat, or fork-ask-discard); semantics designed when needed. Until
  then §3.1-4 (only seated sessions respond) stands.
- **Observation tier** — a read-only viewport onto a seated session (tmux-attach analog) is a
  future third concept; it never creates a seat; double-seat refusal is a single-writer rule.
- **Restart/reconcile policy** (systemd `Restart=` analog) — herder is operator-driven; if ever
  added it hangs off the session or a supervisor concept, never the seat.
- **Label TTLs / liveness-coupled labels; full bitemporal schema; registry daemon** —
  considered and rejected; correction rows + flock are the right-sized mechanisms.

## 11. Decisions embedded in this spec (ratification checklist)

Ratifying this spec ratifies these. Flag any line to reopen it.

| # | Decision | Spec'd as |
|---|---|---|
| D1 | guid = session (one linear transcript) | §3.1-1 |
| D2 | Label stays with the evicted session at /clear; transfer explicit | §3.1-7, AC-6 |
| D3 | Review amendments A1–A12 adopted wholesale | §§3–8 throughout |
| D4 | Node = lazily-minted node_id living with the state (registry + marker, travels with the home dir); marker↔registry gate enforces backup-not-sync; hardware identity excluded; clone = accepted residual risk repaired by `--new` | §6.1, AC-21, AC-27..29 |
| D5 | Namespace identity = minted namespace_id anchored inside the HCOM_DIR; path is per-node description; one namespace per node; teams dropped | §6.2, AC-26, §10 |
| D6 | Epochs are implementation-tier oracle bookkeeping: herder-minted per-node ids; derive + probe; reconcile-never-invalidate | §6.3, §8.3 |
| D7 | Single-machine only: cross-node behaviour out of scope, deferred to a future central-orchestrator design; node attribution kept as a zero-cost stamp; unknown-node rows are read-only anomalies | §3.1-12, §10, AC-25 |
| D8 | Headless: full schema now (pid/node/epochs), degraded command support | §3.3, AC-35 |
| D9 | Registry: one live JSONL per state dir; kind-partitioned projection; flock write discipline; backup-not-sync; sqlite/hybrid rejected with recorded rationale; rotate-never-delete growth stance | §5, §10 |
| D10 | One resolver: targets are sessions; seats via explicit escapes; `resolve` + a minimal usage-driven proxy set as the substrate escape hatch | §4, §7 |
| D11 | Sid probing requires an active reporter (sidecar self-report preferred over tool integrations); sid-less panes reconcile via terminal_id-then-guarded-match at `assumed` continuity, never unseat on absence of evidence | §4, §8.3, AC-24 |
| D12 | v1 registry migrates by one-shot rewrite-with-archive at first v2 write: closed→retired, active→unseated (dormant default, no live probing — never verbatim, never a corpse seated), absent-node legal only pre-mint, legacy keys ignored, tool vocabulary widened beyond claude\|codex | §5.4, AC-36 |

Process notes outside the spec: where the distilled glossary lands (CONTEXT.md home) and which
gaps ride which branch belong to the implementation plan, not this document. Open naming
decision (carried from the pre-ratification walkthrough): a more descriptive term for "epoch";
cosmetic, may be settled by any implementation wave that touches the vocabulary.

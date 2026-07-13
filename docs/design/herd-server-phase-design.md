# Herd-server phase — design

Status: DRAFT — produced by a dedicated design unit; adversarial review and
owner ratification pending. Decision blocks (DECISION-n) are design-level
recommendations; §7 lists the choices that are genuinely the owner's and are
**not** decided here. Spec status lines are owner territory; §8's amendments
are proposals only, none exercised.

Evidence base, cited throughout by path + section:

- `docs/specs/system-boundaries.md` — the standing cross-component contract
  this design implements one phase of;
- `docs/specs/herder-spec.md` (RATIFIED) — registry shape, write discipline,
  observer invariants, non-goals;
- `docs/design/2026-07-08-herder-node-daemon-designs.md` — the D-via-A
  decision record whose phase gates this design fills in;
- `docs/design/2026-07-12-sesh-store-served-distribution.md` — the adjacent
  central tier (owner-ratified); exposure and operations pattern precedent;
- `docs/specs/sesh-wire.md` and `docs/specs/session-service-spec.md` —
  ratified transport mechanics reused here as *pattern*, never as shared
  system;
- `docs/design/grok-first-class-design.md` §DR-2 — the receipt-state-machine
  doctrine this design generalizes to the fleet tier.

## 0. Ratified constraints (not relitigated)

This phase is bounded by rulings already made. A design that breaks any of
these is dead on arrival, and this one is built to be checkable against them:

1. **Phase 1b scope**: outbound node registration and spoke telemetry (bus
   events, registry deltas, mission-directory snapshots), inbound delivery,
   mission-directory snapshot overlays, and human delegation. **Phase 2** may
   add hot herder reads only after legacy-view retirement.
   (`docs/specs/system-boundaries.md` §Remaining architecture work;
   `docs/design/2026-07-08-herder-node-daemon-designs.md` §Decision record.)
2. **The file is truth.** The registry file remains the sole seat→session
   authority; cold reads remain the parity oracle; liveness without an
   appended row is advice. (`docs/specs/herder-spec.md` §5, §3.1;
   node-daemon decision record, memo-derived invariant 5.)
3. **The observer is disposable and holds no write authority.** No registry
   write routes through the observer or any daemon; observer liveness is
   never a precondition for any verb. (`docs/specs/herder-spec.md` §3.1-13,
   §3.1-14, §10.)
4. **No write routes through the server tier.** The herd server may own
   truths of its own tier (defined in §2.3) but never holds or mediates
   authority over any node's truth — registry, mission files, or transcripts.
5. **Session store ≠ herd server**; the generic "hub" stays retired.
   (`docs/specs/system-boundaries.md` §Settled rulings.)
6. **Dependencies never point upward**: session service → nothing; missions →
   nothing; herder → missions optionally + session-store enrichment. The herd
   server is herder's central tier and inherits herder's position in that
   graph. (`docs/specs/system-boundaries.md` §Boundary decision.)
7. **Nodes ship facts, never verdicts**; interpretation is view-time and
   revisable centrally. (`docs/specs/system-boundaries.md` §Identity and
   attribution; `docs/specs/session-service-spec.md` I1.)

Equally dead on arrival, per the same record: a monolithic hub, a mission
event log, node-side parsing verdicts, a daemon-authoritative registry, and
herder identifiers inside mission files.

## 1. Shapes compared (DECISION-1)

Four real shapes, each capable of carrying the phase-1b duties, compared on
where they put the new central tier and how the spoke reaches it.

### Shape A — standalone herd server, observer-carried spoke

A new, separate service: one Go binary, its own tailnet node identity, its own
data directory, joined by every fleet node's observer dialing outbound. The
observer (already the per-node daemon per phase 1a) gains the spoke duties the
node-daemon decision assigned it: outbound telemetry, inbound delivery
execution, overlay production. The server mirrors what nodes ship, owns the
server-tier truths (delegation, command queue), and serves the team view.

Strengths: implements the ratified component split literally ("session store
and future herd server are different systems"); the spoke terminus is the
component the decision record already placed it in; every exposure/operations
question has an owner-ratified precedent to copy (tsnet day 1, capability
grants, data-dir backup, staging-then-rename release — the distribution
design's pattern). Weaknesses, honestly: the most new code of the four (server
skeleton, wire, storage); a second central service to host, back up, and ask
the tailnet admin about; observer restart makes the spoke momentarily dark
(acceptable: spoke darkness is a display state, never a capability loss — §3.2).

### Shape B — store-colocated: herd capability on the session-store listener

Graft the herd-server routes onto the existing session-store process — one
central binary, one listener family, one data dir, one backup drill, possibly
one capability with more verbs.

Strengths: one host, one admin ask, reuse of the store's listener/auth
middleware and operational tooling; cheapest path to "something running".
Weaknesses: this is the retired hub in operational clothing. It couples two
systems' deploy cadence, failure domain, and versioning (a herd-server deploy
restarts transcript ingestion); it puts herder-registry parsing inside the
component whose boundary rule is "no mission, herder, or hcom concept appears
in the wire or node agent" (`docs/specs/system-boundaries.md` §Session
service) — the rule names the wire and node agent, but the spirit is that the
session service stays herder-free, and colocation erodes exactly that; and it
contradicts a settled ruling outright ("Central services: session store and
future herd server are different systems", §Settled rulings). Reversing a
ratified ruling is not this design's call, and nothing here needs it.

Verdict: rejected as *system* colocation. Salvage: full **pattern** reuse
(tsnet node + capability grant + data-dir discipline + store-served binary
distribution for the herder CLI itself later), and **host co-residency stays a
pure ops choice** — the two services may share a machine without sharing a
process, listener, capability, or data dir (§7, owner).

### Shape C — transport reuse: the spoke rides the session-shipping wire

No new transport: the registry log and the node-side delivery journal are
append-mostly files, so let the sesh shipper tail them as additional file
classes; the herd server becomes a reader of the session store's mirror.

Strengths: the transport correctness this phase needs — fingerprint identity,
ACK-then-advance cursors, truncation reset, at-least-once with ingest dedup —
is already ratified *and implemented* (`docs/specs/sesh-wire.md`); telemetry
would cost near-zero node work; mirror durability comes free. Weaknesses: it
violates the session service's boundary rules verbatim ("no mission, herder,
or hcom concept appears in the wire or node agent") and its frozen wire
(distribution design preamble: "Frozen and untouched: wire v1"); it makes the
session service load-bearing for command and control, so fleet operation would
require adopting the session service — breaking independent adoptability in
practice even though no code arrow points upward; it carries no inbound path
at all (the sesh wire is ship-up/read-down for transcripts), so delivery would
still need a new channel and the shape only halves the problem; registry
rotation *reseeds* the live file (`docs/specs/herder-spec.md` §5.1 "Growth"),
which presents as a new fingerprint generation every rotation — mirror churn
the transcript wire never has to absorb; and mission snapshots are not append
files at all.

Verdict: rejected as a system. Salvage: adopt the **mechanics as pattern** in
the spoke wire (§3.2) — they are the best-tested transport idioms this
codebase owns.

### Shape D — busless peer mesh: no central tier

No server: every node exposes a read-only tailnet endpoint; the herder CLI
federates fleet reads client-side; delivery goes node-to-node.

Strengths: no new central service, no server storage, no registration
protocol; the write path is untouched by construction. Weaknesses: the
delegation lease has no home — it was assigned to the herd server precisely
because it is team-level state that belongs to no single node
(`docs/specs/system-boundaries.md` §Historical boundary decision record,
components §1C); the product
target is team multiplayer (`docs/specs/system-boundaries.md` §Settled
rulings), and a mesh gives no durable, always-on team surface — personal
devices sleep; every node running an inbound listener multiplies the exposed
surface and the admin grants by N; and the only existing mesh transport
candidate, hcom relay, is recorded as "unused and unmodelled"
(`docs/specs/herder-spec.md` §10) and was already rejected as a spine in the
node-daemon pass (its design C). The always-on rendezvous the mesh lacks is
exactly what a server *is*.

Verdict: rejected. Salvage: the posture it forces on nodes is kept as
doctrine — **nodes stay outbound-only; no fleet feature may require a node to
run an inbound listener** (§3.2).

### DECISION-1 — standalone herd server (Shape A)

**DECISION: Shape A.** A standalone herd server as its own system, spoke
carried by the node observer per the node-daemon decision record's phase-1b
assignment, borrowing the distribution design's exposure/operations pattern
and the sesh wire's transport mechanics as idioms — never sharing process,
listener, capability, wire, or data dir with the session store.

Why: it is the only shape that satisfies the settled rulings without
amendment (B contradicts one, C violates two boundary rules, D cannot house
delegation or the team surface), and its honest cost — more new code, a second
service to operate — is exactly the cost the rulings already accepted when
they split the store from the herd server. The mitigations are pattern reuse
and small phasing (§9: observation first, delivery second, overlays and
delegation after).

## 2. Architecture of the chosen shape

```text
fleet node                                    herd server (own tsnet node)
──────────                                    ──────────────────────────────
registry (JSONL, truth) ◀── CLI verbs         spoke ingest listener
   │ tail                    (flock, §5.2)      │ PUT telemetry ──▶ mirrors (bytes)
   ▼                                            │ POST commands ◀── operators
node observer ══ outbound dial ════════════▶    │ long-poll fetch ──▶ dispatch
   │  ├─ telemetry: registry bytes,             ▼
   │  │  bus events, overlays, journal        server log (JSONL, server truth:
   │  ├─ fetch: inbound deliver commands        registrations, leases, commands,
   │  └─ execute: local `herder send` only      receipt journal)
   ▼                                            ▼
delivery journal (node file, shipped up)      projections (disposable, rebuilt
mission dirs (read-only scan)                   on boot) ──▶ view surface (humans,
                                                view-time joins into session store)
```

### 2.1 Components

- **Herd server** — one Go binary, one service, its own tailnet node identity
  and capability (names: owner, §7). Listeners follow the store's precedent of
  splitting ingest from human surface; the split and ports are implementation
  detail. It mirrors node telemetry, owns server-tier truths, serves the team
  view, and performs view-time joins into the session store as an ordinary
  read client.
- **Node spoke** — lives inside the node observer daemon (ratified placement:
  node-daemon decision record, "Phase 1b"). Outbound-only dialer. Its death is
  a local non-event; a node without a running observer is simply dark at the
  server.
- **Failure-domain fence (architecture constraint, not an implementation
  hint).** The installed observer is a single synchronous
  reconnect/observe/sweep loop whose backoff and heartbeat freshness are
  driven by sweep outcomes (`tools/herder/internal/observercmd/`). The spoke
  duties — server network I/O, file shipping, mission scans — must run in
  goroutines independent of that loop, connected only by bounded queues:
  file-tail streams take backpressure through their cursors (the file is the
  buffer, so queues never grow); level-state coalesces under backpressure
  (only the newest snapshot per key is ever queued). A slow scan, a wedged
  server connection, or a full queue may never block, delay, or fail the
  registry observation loop or its heartbeat; spoke goroutine teardown and
  restart lose no required facts (files and ACKed cursors are the durable
  state). Server outage degrades exactly the spoke — nothing else — and that
  is an acceptance criterion (§9 U-CORE), not an aspiration.
- **Inbound execution** — structurally delivery-only (§3.3): the node contains
  no command interpreter; the only thing the spoke does with an inbound
  envelope is map it, by code, onto the local send path.

### 2.2 New code territory

Server code and the spoke module are new territory beside the existing
observer (`tools/herder/internal/observercmd/` and siblings). Nothing in this
phase modifies the session service (`tools/sesh/`), the mission CLI
(`tools/mish/`), or hcom internals. The registry writer package is consumed
read-only by the spoke (tailing) and normally by the local verbs it invokes.

### 2.3 Server storage: three classes, three rules

1. **Mirrors** (byte-faithful copies of shipped node files: registry live log
   and rotation archives, node delivery journals). Never authority — the node
   file remains truth; the mirror is the durable archive that outlives node
   retention, exactly the session-store stance
   (`docs/specs/system-boundaries.md` §Session service: "source files are
   buffers, not the archive").
2. **Server truths** (node registrations observed, delegation leases, the
   command queue and server-side receipt journal). These are truths *of the
   server tier* — they describe team-level facts no node owns — and are the
   only things the server is authoritative for. Stored as append-only JSONL
   with latest-row projection, the registry doctrine applied at the server
   tier (`docs/specs/herder-spec.md` §5.1); backup rides the data dir like the
   store's.
3. **Projections** (fleet view, mission board view, join caches). Disposable;
   rebuilt on boot from classes 1–2; never backed up; never load-bearing for
   correctness.

### 2.4 Version skew

Fleet nodes upgrade at different times. The spoke wire is versioned from day
one; the server parses registry bytes centrally with the loader's stance
(unknown keys ignored, malformed rows quarantined —
`docs/specs/herder-spec.md` §5.2, §5.4), so registry format churn is repaired
by reindexing the mirror, never by touching nodes — the session-service lesson
applied to herder's own format.

## 3. Pinned semantics

### 3.1 Node registration (DECISION-2)

**DECISION: registration is an outbound, fact-bearing FIRST-BINDING — the
server records identity, never assigns it, and never lets two stamped
identities share one claimed node_id.**

- **Identity**: `node_id` is minted locally, lazily, and lives with the node's
  state (`docs/specs/herder-spec.md` §2 "node", §6.1). The server's
  registration row records the id it was shown. There is no server-issued
  node identity and no enrollment ceremony.
- **Mechanics**: the spoke registers on every connect (level-triggered);
  first contact creates the row. Payload is facts only: `node_id`, hostname,
  OS user, herder build version, namespace/epoch anchors as locally observed,
  and the set of streams this node ships. Tailnet identity is stamped
  server-side from the connection (WhoIs), never claimed in the payload —
  fact precedence is store logic (`docs/specs/session-service-spec.md` §3
  identity facts).
- **First-binding, not blind upsert.** A copied state dir is a modeled
  condition, not a hypothetical: herder documents `node init --new` as the
  clone repair (`tools/herder/internal/nodecmd/node.go`), and a clone that
  missed repair presents a duplicate `node_id`. Rules:
  - first contact **binds** `node_id` ↔ stamped tailnet identity;
  - the same `node_id` from the **same** identity refreshes normally;
  - the same `node_id` from a **different** identity is **refused and
    quarantined**: the conflicting registration is flagged loudly, no stream
    from the new identity is accepted under that `node_id`, and command
    dispatch to that `node_id` suspends until an operator resolves it — the
    registry's own anomaly doctrine ("unknown-node rows are anomalies, not
    peers", `docs/specs/herder-spec.md` §3.1-12) applied at the server tier.
    Merging two real nodes' streams or delivering one node's commands to its
    clone is the failure this rule exists to make impossible.
- **Succession (legitimate host move)**: the state dir travels with the home
  directory by design (`docs/specs/herder-spec.md` §2 "state dir"), so a
  moved node presents its old `node_id` from a new tailnet identity. That is
  the quarantine case above until an operator with the appropriate grant
  executes an explicit **re-bind** at the server, which records succession
  `{node_id, from identity, to identity, at, by}` as server truth. Until
  re-bound, the moved node is refused — visibly, never silently. The
  clone-repair path stays what it is today: `node init --new` mints a new
  identity that registers as a brand-new node.
- **One active spoke channel per node_id.** The server accepts one live spoke
  session per bound `node_id` (newest connect wins; the older channel is
  closed politely). Two live spokes alternating under one binding — the
  same-identity clone case — surfaces as an incarnation flip-flop anomaly
  (§3.4's incarnation fencing makes it visible) and is flagged, never
  silently interleaved.
- **Lifecycle**: nodes are never deleted, mirroring "sessions are never
  deleted" (`docs/specs/herder-spec.md` §3.1-3). Staleness is displayed
  (last-contact timestamp), never inferred into a verdict. A re-minted node
  registers as new; the old row goes permanently stale; the server never
  merges node identities — succession annotations link them for display only.
- **Admission**: holding the spoke capability on the tailnet grant *is* the
  admission control, exactly the store's posture; tightening is a grant edit,
  not a server change. Registration is attribution, never authentication
  (`docs/specs/system-boundaries.md` §Identity and attribution) — but the
  binding rule above means attribution conflicts fail closed for streams and
  commands rather than being displayed-and-accepted.

### 3.2 Spoke transport: streams, reconnect, replay (DECISION-3)

**DECISION: outbound-only spoke; three stream classes with per-class
sequencing — explicit file-generation identity with overlap validation for
file tails, epoch-keyed dedup for bus events, incarnation-fenced replacement
for level state; ACK-then-advance cursors; at-least-once shipping.**

Posture first, because it shapes everything: **nodes are outbound-only**. No
fleet feature may require a node to run an inbound listener. The spoke dials
the server; inbound work rides responses to node-initiated requests
(long-poll or stream — implementation detail). A dark spoke parks the node:
the server shows last-known-at-T, never fake-live (the node-daemon pass's
honesty rule, kept from its design C analysis).

Three stream classes, because the payloads have three different shapes of
truth:

1. **File-tail streams** — the registry live log, its rotation archives, and
   the node delivery journal (§3.3). Sequence = (file generation, byte
   offset). The sesh wire's identity trick — filename UUID plus a fingerprint
   that only exists past a 1 KiB head window (`docs/specs/sesh-wire.md` §File
   Identity) — does **not** transfer here: registry live files have no
   filename UUID, rotation reseeds the live file in place under the same
   name, and a young file or journal is below any fingerprint window exactly
   when identity matters most. So identity is made **explicit instead of
   content-derived**: every shipped file carries a self-identity header — a
   `file_generation` UUID stamped as its first record at creation and at
   every rotation-reseed (both happen under the registry write lock, so the
   stamp is atomic with file birth; requires the registry-format amendment
   §8-A6; the delivery journal is a new format and simply starts with one).
   Wire identity = (node, file class, file_generation), immutable and
   available from byte 0. The head fingerprint is kept as a **cross-check**
   on reconnect, never as identity.

   Replay rules, adopted from the frozen wire by reference where they
   genuinely apply, defined explicitly where they do not:
   - **ACK-then-advance** and **durable ACK = fsynced mirror bytes**: adopted
     verbatim (`docs/specs/sesh-wire.md` §Invariants).
   - **Reconnect**: the node asks the server's high-water per
     file_generation (the recovery-lookup idiom) and resumes from it.
   - **Byte-overlap validation after ACK loss**: when the node's cursor is
     ahead of the server's high-water (an ACK was lost), the node re-ships
     from the server's high-water; before appending, the server verifies the
     re-shipped overlap window byte-matches its mirror. At-least-once
     shipping plus overlap validation replaces the sesh tuple-dedup summary.
   - **Overlap mismatch or in-generation size regression** (source below
     cursor within one file_generation): within a generation, files are
     append-only by contract — rotation is the only sanctioned truncation
     and it mints a new generation — so either signal means in-place
     mutation or corruption. The server **quarantines the generation**:
     mirrored bytes are preserved untouched, ACKs stop, the anomaly is
     flagged for operator repair. Never a silent reset, never an overwrite —
     the store's preserve-conflicting-histories stance
     (`docs/specs/sesh-wire.md` §File Identity) applied with herder's
     loud-anomaly doctrine.
   - **Rotation**: the reseeded live file is a new generation and ships as a
     new unit; the pre-rotation archive (carrying its own generation header)
     ships once, immutable. The server's parsed projection dedups rows
     across generations by content — rows are self-contained snapshots — so
     rotation costs bytes, never correctness.
2. **Bus-event streams** — hcom events, keyed (namespace_id, epoch_id,
   event_id) per the epoch model (`docs/specs/herder-spec.md` §6.2–6.3).
   Cursor = last-ACKed event id per (namespace, epoch); an epoch change is a
   legitimate new stream, never a replay anomaly; at-least-once, server dedup
   on the triple.
3. **Level-state streams** — mission overlays (§3.4) and node status.
   Idempotent full-replace snapshots keyed per subject, ordered by an
   incarnation-fenced token `(spoke incarnation, counter)` — §3.4 defines the
   token and its recovery rules, because a "node-minted monotonic counter"
   alone cannot survive a disposable observer's rebuild. No replay exists or
   is needed: on reconnect the node re-emits current state per the server's
   retained-key listing (§3.4).

**Reconnect**: the node asks the server for its high-water per stream (the
recovery-lookup idiom from the sesh wire) and resumes from there. A server
that lost state answers zero and is re-filled to the extent nodes retain —
which for the registry is everything (rotate-never-delete), for bus events is
the epoch's retention, and for level state is always (it is current state).
The server's data-dir backup, not node retention, is the real durability
story, same as the store's.

**Parsing**: nodes ship bytes and facts; the server parses centrally,
quarantines malformed rows exactly like the local loader, and rebuilds its
parsed projection by reindexing mirrors. No node-side parsing verdicts.

**Locality**: the spoke never blocks local operation. Observer death, server
death, or network partition degrade exactly one thing — fleet visibility and
fleet delivery — and repair by reconnect + replay with no operator ceremony.

### 3.3 Inbound delivery and receipts (DECISION-4)

**DECISION: structurally delivery-only inbound; node-local resolution; a
node-owned durable delivery journal; a per-command delivery mode —
at-most-once by default, at-least-once by explicit choice — with
"indeterminate after claim" as a first-class terminal state. Exactly-once is
explicitly NOT claimed against the installed bus.**

- **Command model**: a typed envelope `{command_id (server-minted, unique),
  target node_id, verb, payload}`. The phase-1b verb set is exactly one verb:
  **deliver** `{target session (label | guid), text, sender attribution,
  optional deadline, mode: at-most-once | at-least-once}`. There is no shell,
  no argv passthrough, no interpreter on the node: the spoke maps the
  envelope by code onto the local send path — remote sends can never be more
  permitted than local ones (the delivery-only-by-structure framing the
  decision record kept from its design C).
- **Resolution is node-local.** The node resolves the target against its own
  registry with its own refusal semantics (`docs/specs/herder-spec.md` §7
  `send`, §3.1-4/5/11). The server never resolves against its mirror —
  server-side resolution is the resolver-drift failure the node-daemon pass
  rejected. Server pre-checks against its projection are advisory warnings to
  the submitting operator, never verdicts.
- **What the installed bus can evidence** (verified design input, not an
  assumption): `hcom send` returns no message id, and delivery receipts carry
  no message correlate — the local send engine
  (`tools/herder/internal/send/hcom.go`) exists in its current shape
  precisely because of this: it serializes a snapshot → send → receipt-wait
  window under an inter-process lock and reports
  `delivered | queued | not_joined | send_failed`, where `delivered` means "a
  strictly-newer receipt appeared inside my serialized window". No durable
  artifact ties a specific bus message to a specific caller. Therefore no
  design can honestly promise exactly-once delivery, or receipt-correlated
  dedup across a crash, on the installed bus — and this design does not.
- **The delivery journal is node truth, not observer state.** It lives in the
  herder state dir beside the registry, appended with fsync discipline. The
  spoke is merely its usual writer, exactly as the observer is an ordinary
  peer writer of the registry (`docs/specs/herder-spec.md` §3.1-13); any
  later spoke incarnation — or a CLI verb — can read and recover it. This
  keeps the observer *process* disposable: kill it at any point and every
  journal state below remains truthful. The journal carries a self-identity
  header (§3.2) and ships upstream as a file-tail stream, so **receipts
  replay losslessly by construction**: a receipt that raced a disconnect
  arrives when the journal bytes do.
- **Execution protocol**, per fetched command, in order:
  1. dedup: `command_id` already journaled → no new attempt (redispatch is
     re-offering, never re-execution by itself);
  2. fence: a journaled fence row for `command_id` → refuse, report fenced;
  3. deadline: past deadline by the node clock → journal a fence row
     (reason: expired), never execute;
  4. attempt-open: `{command_id, attempt n, envelope hash, opened_at}`
     appended and fsynced **before** any execution;
  5. execute: invoke the local send path;
  6. attempt-close: the local verdict appended verbatim.
- **Crash truth table** — the observer must be killable at any point with
  truth intact, so the gap is modeled, not papered over:
  - killed before attempt-open: nothing happened; a later redispatch finds no
    journal entry and executes normally;
  - killed between attempt-open and attempt-close: the send may or may not
    have reached the bus — **indeterminate**. Recovery (the next spoke
    incarnation, or a CLI recovery verb over the same journal) resolves it by
    the command's mode: **at-most-once** (default) → append
    `indeterminate` (terminal; never re-executed; ships upstream with its
    evidence: attempt opened at T, no outcome); **at-least-once** → open
    attempt n+1 (duplicate delivery is possible, and every attempt is
    journaled and visible, never silent);
  - killed after attempt-close: the outcome is durable and ships on replay.

  At-most-once is the right default here: the payloads are instructions to
  live agents, where a duplicated instruction is worse than a stranded one,
  and the bus doctrine is already "never blind-resend". At-least-once is the
  submitter's explicit opt-in for idempotent payloads.
- **Receipt state machine** (server-side, per command_id, strictly monotonic,
  duplicates recorded never regressed — the doctrine of
  `docs/design/grok-first-class-design.md` §DR-2 generalized):

  ```text
  accepted ──► dispatched ──► claimed ──► concluded(outcome)   (terminal)
      │             │             └─────► indeterminate        (terminal;
      │             │                      at-most-once crash gap, evidence shown)
      │             ▼
      ├──► cancel_requested | expiry_reached      (NON-terminal: stop offering)
      │             │
      └─────────────┴──► cancelled | expired      (terminal; only once FENCED)
  ```

  - **accepted** — durably journaled in the server log before any dispatch or
    any acknowledgement to the submitter.
  - **dispatched** — handed to a node fetch response; may repeat; repeats are
    journaled, never assumed delivered.
  - **claimed** — the node's shipped journal shows the attempt-open row.
  - **concluded** — the node's journal shows the outcome, surfaced verbatim:
    the local send verdict with its resolved guid when resolution succeeded,
    or the local refusal named (unseated with eviction record, unreconciled
    binding, unknown target — the local vocabulary, unmodified).
  - **indeterminate** — the node's journal shows an attempt-open resolved
    under at-most-once with no outcome; terminal; displayed with its
    evidence, never as failure or success. Resubmission is an explicit
    operator act minting a new command_id — the fleet-tier analog of "read
    the pane before retrying".
- **Cancel and expiry cannot lie** (fencing): `cancel_requested` (submitter)
  and `expiry_reached` (server clock) immediately stop the envelope from
  being offered in any future fetch — but they are **non-terminal**, because
  a dispatch may already be in flight. The server finalizes to `cancelled` /
  `expired` only when no claim can ever arrive: either the command was never
  dispatched, or the node has **fenced** it — on its next contact the spoke
  is told which in-flight command_ids are cancel/expiry-pending and journals
  a fence row for each it has not claimed, reporting the fence upstream.
  Fence-vs-claim races resolve deterministically by journal append order:
  whichever row landed first wins. Deadlines are enforced at both edges (the
  server stops offering by its clock; the node fences at claim time by its
  clock); skew therefore can only make expiry more conservative, and a node
  whose clock ran behind may still execute — in which case evidence
  dominates, below.
- **Evidence dominates bookkeeping.** A claim or outcome arriving for a
  command in `cancel_requested` / `expiry_reached` moves it to `claimed` /
  `concluded`: the cancellation simply failed, and the display says so
  ("cancel requested at T; executed anyway at T′"). If server bookkeeping
  ever finalized early against later journal evidence, the display reconciles
  to the evidence and flags the disagreement loudly — the server never
  defends a false terminal state. This is legal monotonicity because
  cancel_requested/expiry_reached are defined as non-terminal.
- **Honesty rules** (doctrine): a delivery claim is never stronger than its
  correlated evidence chain; "no receipt yet" is displayed as *unknown /
  node dark*, which is a display state, not a verdict; timeouts never
  fabricate failure or success; nothing is blindly re-sent — redispatch only
  re-offers an envelope the journal dedups, and re-execution happens only
  under an explicit at-least-once mode. Where a seat has a full ack-chain
  receipt machine, "delivered" surfaces that chain; where local semantics top
  out at the installed engine's vocabulary, the server reports that verdict
  verbatim (`delivered` meaning window-correlated receipt, `queued` meaning
  submitted-no-receipt) and never upgrades it. The server's vocabulary is the
  minimum of the node's, never the maximum.
- **Upstream option, recorded not assumed**: if the bus ever returns a
  durable per-send message id (or accepts a client-supplied idempotency key),
  the crash gap closes — attempt-open records the correlate, recovery can
  distinguish sent from not-sent, `indeterminate` collapses into
  `concluded`/safe-retry, and exactly-once *effect* becomes claimable. That
  is an upstream bus change; whether to file the ask is an owner decision
  (§7-8). Nothing in phase 1b depends on it.

### 3.4 Mission overlay reconciliation (DECISION-5)

**DECISION: overlays are idempotent full-replace photographs keyed
(node, mission directory), anchored to a git base commit; ordered by an
incarnation-fenced (spoke incarnation, counter) token; cross-node same-slug
is surfaced, never merged; nothing ever reconciles back into mission
files.**

- **Payload**: `{mission slug + directory path, git base commit sha, the set
  of dirty/untracked files under the mission directory with full contents,
  captured_at, ordering token}` — the settled ruling's shape verbatim
  (`docs/specs/system-boundaries.md` §Settled rulings: "idempotent
  mission-directory snapshot overlays, not mission dual-writes"). Payloads
  carry an explicit size ceiling and an honest `truncated` marker when a
  mission exceeds it — owning the ceiling is the lesson from rejecting
  payload-capped transports, inverted.
- **Ordering token: (spoke incarnation, counter), incarnation-fenced at the
  server.** A disposable observer cannot carry a durable counter — anything
  generation-bearing living only in observer-owned state would make that
  state load-bearing, which is exactly the forbidden shape. So: the spoke
  mints a fresh random **incarnation id at every boot** (ephemeral by
  design, nothing persisted); within an incarnation the counter is a plain
  in-memory monotonic. The **server** orders incarnations per node by
  first-observed succession (server truth, recorded at connect) and fences:
  a snapshot from the node's newest incarnation always supersedes any
  retained snapshot from an earlier incarnation regardless of counters; a
  snapshot from a superseded incarnation is discarded. A rebuilt observer at
  counter 0 therefore beats a retained counter 100 — stale mission content
  can never be resurrected by a restart.
- **Reconnect recovery, both loss directions**:
  - *observer state lost (restart/rebuild)* — on connect under a new
    incarnation, the server returns its **retained-key listing** for this
    node (every (mission directory) key it holds). The node re-emits current
    state for every retained key — a fresh overlay where the directory
    exists, a **tombstone re-emission** where it does not — plus overlays
    for any new keys its scan finds. Retained keys are marked
    *pending-refresh* on the server from the moment the new incarnation is
    observed until their re-emission arrives, so the display never presents
    a superseded-incarnation snapshot as current;
  - *server state lost* — the retained-key listing is empty; the node ships
    its current scan; the view rebuilds from level state alone. Nothing can
    be resurrected because nothing was retained.
- **Production**: observer-side, strictly read-only — read-only git queries
  only (the `mish status` precedent, `docs/specs/mission-spec.md`), no locks,
  no writes, ever. Discovery is by node-configured mission roots, best-effort:
  a mission outside every root simply has no overlay, and **absence of an
  overlay means "no realtime view", never "no mission"**.
- **Server view** = git base (fetched read-only from the repo remote when the
  server has access, else absent) + overlay applied on top. Enrichment is
  view-time and best-effort; a failed base fetch degrades the display to
  overlay-plus-metadata — a failure to enrich never changes the underlying
  truth (`docs/specs/system-boundaries.md` §Identity and attribution).
- **Reconciliation rules**:
  - per (node, mission directory) key: within the current incarnation,
    highest counter wins and lower or equal counters are discarded
    (idempotent replace); across incarnations, the fencing rule above is
    absolute;
  - an overlay with an empty dirty set replaces prior state — clean is also a
    state;
  - a mission directory observed absent produces a tombstone overlay
    (observed-absent at T); history is retained; the display ages it out;
  - the same slug from two nodes or two directories is never merged: distinct
    rows, grouped by slug, flagged loudly — the registry's projection-anomaly
    doctrine (deterministic and loud, never silent tie-breaking,
    `docs/specs/herder-spec.md` §5.2) applied to missions;
  - there is no conflict resolution because there is no second writer: the
    overlay is a photograph of the mission directory, not a replica of it.
    The server never writes a mission file, a board, or a repository.
- **Boundary check**: production is herder-side reading, the allowed
  direction (herder may be very mission-aware; missions stay herder-unaware).
  No server or herder concept lands in any mission file; the mission format
  is untouched.

### 3.5 Human delegation (DECISION-6)

**DECISION: a delegation lease is server-tier truth with label-lease tenure
semantics — explicit grant, explicit transfer naming the current holder,
explicit release, no expiry — consumed downstream only as advice.**

- **Shape**: `{subject, holder: human label-grade name, granted_by, since}`.
  One tenure vocabulary across the system: like a herder label, a lease is
  released only by explicit release or transfer naming the current holder —
  never by liveness, TTL, or inference (`docs/specs/herder-spec.md` §2
  "label", "lease / transfer").
- **Subject identity — composite, because a bare slug aliases what the
  overlay model keeps distinct.** Mission identity is the slug in the
  ratified mission spec, but slug uniqueness is per-clone directory
  existence and a rename is a directory rename
  (`docs/specs/mission-spec.md` §2 "slug", §4.3): two nodes can legitimately
  hold different missions under one slug — exactly the case §3.4 refuses to
  merge — so a slug-keyed lease would silently attribute both. Subjects are
  therefore:
  - **node** — by `node_id` (under §3.1's binding rules);
  - **mission** — composite `(node_id, mission directory path)`, the
    overlay's own key, with the slug carried as descriptive display. A
    directory move or slug rename **orphans** the lease: orphaned leases
    stay visible as orphaned and are never auto-reattached; re-grant is an
    explicit act, consistent with lease tenure never being inferred;
  - **slug-group** (explicit variant, never a default) — a lease declared
    over "every mission currently bearing slug S". It attaches at display
    time to whatever rows currently carry the slug, and the display always
    shows its current capture set; a rename detaches a mission from the
    group, and a later slug reuse is captured by it — that reuse hazard is
    precisely why group scope must be an explicit declaration by the
    grantor, marked as such wherever it renders.
- **Meaning**: responsibility and attribution routing for team views, and a
  `SESSION_OWNER` source. **Never authentication, never access control**
  (`docs/specs/system-boundaries.md`: attribution is never authentication).
  Holder names are opaque label-grade strings; joins from them are
  best-effort and view-time, per the label doctrine.
- **Write path**: leases are written at the server via an authenticated
  server verb. Who may grant, transfer, and release is admission policy —
  owner territory (§7). The interim mechanism stays valid and unchanged:
  static `SESSION_OWNER` at provisioning
  (`docs/specs/system-boundaries.md` §Historical boundary decision record,
  components §1C).
- **Downstream effect**: the node's spoke receives its own delegation state
  on connect and caches it as advice. At spawn, the launch choke point may
  stamp `SESSION_OWNER` with pinned precedence: explicit env already present
  → wins; else mission `owner:` where the launch is mission-scoped; else the
  node's delegation lease; else honest absence (absence stays meaningful —
  `docs/specs/mission-spec.md` §SESSION_OWNER). Stamping is birth provenance,
  not registry truth (`docs/specs/herder-spec.md` §3.1-8); a dark spoke means
  stale advice and blocks nothing.
- **View-time**: the session store's owner-precedence display logic is
  unchanged; the herd server may use leases as one more fact source in its
  own views and its store joins — read-only, best-effort.

## 4. Independence and awareness audit

Per-component consequences of this phase, checked against the dependency
graph (`docs/specs/system-boundaries.md` §Boundary decision):

| Component | Change in this phase | Adoption coupling after this phase |
|---|---|---|
| Session service | **None.** Wire frozen, shipper untouched, spec untouched. | Complete without herder, unchanged. The herd server may join into the store's read surface as an ordinary client; a fleet without the session service loses enrichment only. |
| Missions | **None.** No file-format change, no new verb, no awareness added. | A machine with only `mish` remains complete. Overlays read mission directories from the herder side — the allowed direction. |
| Herder (node) | Observer gains phase-1b duties: spoke telemetry, inbound deliver execution, overlay production. | Every local verb works with the server absent, forever. The server is enrichment, team surface, and delivery rendezvous — never a precondition. |
| Herd server | New component (this design). | Depends on herder nodes for its content; may read missions state via overlays and the session store via its read surface. Nothing depends on it. |
| Orchestrate | **None.** Stays a skill; may later choose to use fleet delivery. | Unchanged. |

Direction check: every arrow this phase adds points from herder's tier toward
missions or the session store, and every one of those is a read. No arrow
from missions or the session service toward herder exists or is created; no
mission or session-service artifact learns any herder or server concept.

## 5. Phase 2 — hot reads, explicitly gated

What phase 2 is, per the ratified record: **local** herder reads (`list`,
`wait`, a future `watch`) answered from the node daemon's in-memory
projection under the mode-shim discipline — barrier protocol for
read-your-writes, `source` + `adjudicated_at` stamps on hot answers, `--cold`
escape forever (`docs/design/2026-07-08-herder-node-daemon-designs.md`
§Design D, §Decision record).

What phase 2 is **not**: server-served reads replacing local reads. The herd
server's views of remote nodes are display-tier advice permanently; no local
verb ever resolves against the server; the server is not a read path, it is a
viewport.

**The gate.** No hot-read work may begin until all of these hold:

1. **Legacy-view retirement — landed, so the precondition is verification,
   not work.** Retirement of the legacy two-state registry view has already
   shipped on main (commits `8af91d2` "retire legacy registry state view"
   and `75ab144` "teach four-state session vocabulary": read paths consume
   the four-state machine — seated / unseated / retired / lost — and v1
   status survives only in migration compatibility). The precondition is
   therefore a standing **verification** that this stays true: no read path
   consumes a two-state view, asserted by test and grep, and the daemon's
   projection is only ever built against the four-state machine — the
   decision record's memo-derived invariant 3.
2. **Cold parity harness over a test-only seam**: a parity harness needs a
   hot answer to compare, and nothing before phase 2 may serve one — so the
   harness exercises a **test-only projection seam**: the incremental
   projection builder a future hot daemon would serve, instantiated
   in-process by the harness (never wired to any verb, socket, or view) and
   compared against cold file reads over identical registries, including
   adversarial interleavings, in CI; parity failures block merge. Cold
   reads from the file remain the parity oracle permanently — the harness
   is a standing tax accepted knowingly (Design D's own stated price), not
   scaffolding to delete later.
3. **Phase 1b baked**: the observer-with-spoke has run in live herds long
   enough to demonstrate disposability under kill-and-rebuild with zero
   correctness loss, per its ACs.
4. **Spec amendment ratified**: the mode shim changes read semantics, which
   is spec territory; the owner ratifies the amendment before implementation
   (§8-A4).

Until all four hold, every read stays cold, and nothing in phase 1b is
allowed to create a dependency that would make phase 2 harder to refuse: the
spoke reads files and substrates directly and owns no projection any verb
consumes.

## 6. Failure honesty (summary)

| Failure | Node behavior | Server display | Repair |
|---|---|---|---|
| Observer/spoke dead | All local verbs unaffected (ratified invariant) | Node parked: last-known-at-T, never fake-live | Observer restart; boot catch-up sweep + stream replay from ACKed cursors |
| Server dead / unreachable | Nothing blocked; spoke retries with backoff; commands cannot be submitted | — | Server restart; nodes re-register and resume; mirrors re-fill from node retention |
| Server data dir lost | Nothing | Rebuilt from re-shipped node state; server truths (leases, command history) lost to the backup horizon — backup is the durability story, as with the store | Restore data-dir backup, then replay |
| Node re-minted (`node init --new`) | New node_id registers fresh | Old node permanently stale; optional succession annotation | Operator annotates at server if desired |
| Registry rotation mid-ship | Rotation is local and unaffected | New file_generation stamped and shipped as a new unit; archive ships once; parsed rows dedup by content | Automatic |
| Command to a dark node | — | `accepted`, dispatch pending, honest age shown; `expired` if deadline passes unclaimed | Node reconnects and fetches, or submitter cancels |
| Receipt raced a disconnect | Outcome sits in the durable node journal | `claimed`, awaiting journal bytes | Journal stream replay delivers the receipt losslessly |
| Observer killed between attempt-open and attempt-close | Journal shows an open attempt; recovery resolves by mode (§3.3) | at-most-once: `indeterminate` with evidence; at-least-once: attempt n+1 visible | Operator resubmits explicitly (new command) if needed |
| Cancel/expiry raced an in-flight dispatch | Node fences unclaimed ids on next contact; a landed claim executes | `cancel_requested`/`expiry_reached` (non-terminal) until fenced; late evidence dominates and is flagged | Automatic — fence handshake (§3.3) |
| Spoke wedged / slow scan (server up) | Registry observation loop and heartbeat unaffected (§2.1 fence); level-state coalesces | Streams age honestly | Spoke goroutine restart; cursors resume |
| Observer rebuilt while server retains overlays | New incarnation; re-emits per retained-key listing, incl. tombstones | Old-incarnation snapshots fenced, keys pending-refresh until re-emission | Automatic (§3.4) |
| Duplicate node_id (clone missed `node init --new`) | The refused clone's spoke gets a loud refusal | Quarantine: no stream accepted, dispatch suspended, anomaly flagged | Clone repair (`node init --new`) or explicit succession re-bind (§3.1) |
| Mission dir moved/deleted | — | Tombstone overlay (observed-absent), aged out of active display | Next scan of the new location produces a fresh overlay |

## 7. Owner decisions required

None of these are exercised in this design; each blocks only the unit that
needs it (§9 maps them).

1. **Naming**: the server's tailnet node hostname, ACL tag, and capability
   name + verb set. Precedent to match or diverge from:
   `sesh` / `tag:sesh` / `infinex.xyz/cap/sesh` with verbs `ship`,`read`
   (`docs/design/2026-07-12-sesh-store-served-distribution.md` §1). A
   parallel shape would be a short node name, matching tag, and a capability
   under `infinex.xyz/cap/` with verbs separating node telemetry+fetch,
   command submission, and human view — but naming is owner-ruled, not
   proposed here.
2. **Exposure and grants**: which tailnet principals hold each verb. Command
   submission is the fleet's remote-control surface and deserves the
   narrowest grant; telemetry and view can start team-wide per the store's
   posture. Also: whether the herd server's node is added to the session
   store's `read` grant for view-time joins.
3. **The tailnet-admin ask**: a second tagged node implies a second one-time
   ask (tag + grant + one reusable key + expiry-disable, the shape already
   approved for the store), or a consolidated ask covering both central
   nodes. Same reusable-key vs OAuth-client choice as the store's DP-7.
4. **Hosting**: co-residency with the session-store host (pure ops; systems
   stay separate per DECISION-1) vs a separate host; backup drill ownership
   for the server data dir.
5. **Delegation write policy**: who may grant, transfer, and release
   delegation leases (§3.5).
6. **Delivery attribution policy**: whether sender attribution on inbound
   deliver is display-only in phase 1b or constrained per-principal from the
   start (§3.3).
7. **Spec homing**: whether the ratified form of this design becomes a new
   component spec or an amendment section of `docs/specs/herder-spec.md`;
   status lines are owner territory either way.
8. **Optional upstream bus ask**: `hcom send` returning a durable per-send
   message id (or accepting a client-supplied idempotency key) would close
   the delivery crash gap and collapse `indeterminate` (§3.3's recorded
   upstream option). Phase 1b is designed to not depend on it; whether the
   ask is worth filing upstream is the owner's call.

No upstream asks are *required* by this design; item 8 is the one optional
ask it records.

## 8. Proposed spec amendments (proposals only, clearly marked)

Each is drafted as an amendment for owner ratification; nothing below edits a
ratified document.

- **A1 — `docs/specs/herder-spec.md` §10 (multi-machine non-goal)**: re-scope
  the non-goal. Cross-node behaviour enters scope solely as the herd-server
  tier per this design; registry shipping between nodes and bus relay stay
  rejected. The node-attribution stamp (§3.1-10) is promoted from zero-cost
  residue to load-bearing for spoke attribution — its recorded purpose.
- **A2 — `docs/specs/herder-spec.md` §3.1 + §4 (observer duties)**: the
  observer definition gains the spoke duties (outbound telemetry, inbound
  delivery execution, overlay production), with the disposability and
  no-write-authority invariants restated *over* the new duties; one new
  invariant: "no write of any node truth routes through the herd server; the
  server holds no authority over registry, mission, or transcript state."
- **A3 — `docs/specs/herder-spec.md` §7 (verbs)**: new verb sketches — spoke
  status/config on the node; fleet-targeted send and delegation verbs against
  the server. Verb names land with the owner's naming ruling (§7-1).
- **A4 — `docs/specs/herder-spec.md` §8/§9 (phase-2 mode shim + ACs)**: the
  hot-read mode shim (barrier, stamps, `--cold`) as a gated amendment whose
  ratification is itself precondition 4 of §5; new acceptance scenarios:
  spoke replay after server loss; delivery dedup under redispatch; no
  `delivered` without node-journal evidence; overlay tombstone; observer
  disposability under spoke load.
- **A5 — `docs/specs/system-boundaries.md` §Remaining architecture work**:
  point the herd-server paragraph at the ratified form of this design as its
  named successor; status and wording owner-ruled.
- **A6 — `docs/specs/herder-spec.md` §5.1 (registry file self-identity)**:
  the registry gains a `kind: file` self-identity record — a
  `file_generation` UUID stamped as the first row of the live file at
  creation and at every rotation-reseed, under the write lock — the spoke's
  wire identity for file-tail streams (§3.2). Bookkeeping-kind like node and
  epoch records: invisible to session resolution, one more row the loader
  partitions away. Existing files without a header get one stamped at their
  next locked write (their pre-header bytes ride the first generation).
- **`docs/specs/session-service-spec.md`, `docs/specs/sesh-wire.md`,
  `docs/specs/mission-spec.md`**: **no change**, deliberately — the §4 audit
  is the evidence. If the owner wants a cross-reference, the store design's
  "informational, non-contract" note pattern is the precedent.

## 9. Appendix — implementation task captures (filed-ready)

Sequenced; each independently shippable and abandonable, matching the phasing
doctrine of the node-daemon decision record. Every unit carries the standing
docs discipline: its doc rows are acceptance criteria, not follow-ups.

### U-CORE — herd server skeleton + registration + registry telemetry

- **Type**: build. **Depends on**: owner rulings §7-1..4 (naming, grants,
  ask, hosting).
- **Territory**: new server code; a spoke module inside the observer; no
  changes to `tools/sesh/`, `tools/mish/`, hcom, or any registry write path.
- **Settled by this design**: DECISION-1 (standalone), DECISION-2
  (first-binding registration, quarantine, succession re-bind, one active
  spoke channel), DECISION-3 (stream classes; explicit file_generation
  identity per §8-A6; ACK-then-advance; overlap validation; quarantine on
  in-generation mutation; outbound-only posture), §2.1 failure-domain fence,
  §2.3 storage classes, §2.4 version-skew stance.
- **AC sketch**: (1) a node registers on first spoke connect; reconnect from
  the same identity refreshes; the same node_id from a different tailnet
  identity is refused and quarantined (no streams accepted, dispatch
  suspended, anomaly flagged); an explicit succession re-bind lifts the
  quarantine and records the succession; a re-minted node appears as a new,
  never-merged node; (2) registry live log + rotation archives mirror
  byte-faithfully keyed by file_generation header identity, with
  ACK-then-advance cursors and reconnect overlap validation; kill -9 of
  observer or server at any point loses nothing after reconnect (replay
  test with injected failures, including lost-ACK re-ship); (3) rotation
  mid-ship stamps and ships a new generation with no duplicate parsed rows;
  in-generation size regression or overlap mismatch quarantines the
  generation, preserves mirrored bytes, and stops ACKs; (4) server parse
  quarantines malformed rows and reindexing rebuilds the projection from
  mirrors alone; (5) all local herder verbs pass their existing ACs with the
  server absent; (6) failure-domain fence under fault injection: a stalled
  server connection, a wedged spoke goroutine, or a slow scan never delays
  the registry observation loop or heartbeat beyond its normal cadence
  bound, and spoke teardown/restart loses no facts; (7) docs rows: server
  README + system-boundaries cross-ref (owner-gated wording).

### U-DELIVER — inbound delivery + receipt machine

- **Type**: build. **Depends on**: U-CORE; owner ruling §7-2/6 (command
  grant, attribution policy).
- **Territory**: spoke fetch/execute path; node delivery journal; server
  command queue + receipt journal; the local send path is *consumed*, never
  modified.
- **Settled by this design**: DECISION-4 in full — single deliver verb,
  node-local resolution, the journal as node truth with attempt-open /
  attempt-close protocol, delivery modes with at-most-once default and
  `indeterminate` as a first-class terminal state, no exactly-once claim on
  the installed bus, cancel/expiry fencing handshake, evidence-dominates
  rule, vocabulary-minimum rule.
- **AC sketch**: (1) redispatch under a durable journal never re-executes by
  itself (dedup-by-command_id with forced redispatch); at-most-once: a kill
  between attempt-open and attempt-close surfaces `indeterminate` with its
  evidence and is never re-executed; at-least-once: recovery re-executes as
  attempt n+1, every attempt visible; (2) no receipt state ever regresses; a
  receipt racing a disconnect arrives via journal replay; (3) `concluded`
  verdicts are reported only from node-journal outcomes, verbatim — a wedged
  node yields `dispatched`/unknown, never a verdict — and no verdict is ever
  stronger than the local engine's own (`delivered` | `queued` | refusal);
  (4) local refusals surface verbatim with resolved-guid correlation where
  applicable; (5) cancel/expiry: a cancel racing an in-flight dispatch
  finalizes only after the node fences it; a claim landing first beats the
  fence and the display shows "cancel requested, executed anyway"; nothing
  finalizes `expired` unfenced; fence-vs-claim ordering is deterministic by
  journal append order (race test); (6) the node contains no path by which
  an inbound envelope reaches a shell or any verb other than send.

### U-OVERLAY — mission-directory snapshot overlays

- **Type**: build. **Depends on**: U-CORE.
- **Territory**: observer scan module + level-state stream; server overlay
  store + view. Mission files, `tools/mish/`, and the mission spec are
  untouchable.
- **Settled by this design**: DECISION-5 in full — payload shape anchored to
  git base sha, full-replace idempotence, the (spoke incarnation, counter)
  ordering token with server-side incarnation fencing, retained-key recovery
  listing with tombstone re-emission, tombstones, never-merge cross-node
  slugs, read-only production, size ceiling with honest truncation.
- **AC sketch**: (1) overlay round-trip: dirty mission dir → server view
  equals base + dirty contents; (2) ordering: within an incarnation,
  out-of-order counters never regress the view; across incarnations, a
  rebuilt observer at counter 0 supersedes a retained counter 100, and
  stale-incarnation snapshots are discarded (both directions tested); (3)
  reconnect recovery: retained keys are pending-refresh from new-incarnation
  contact until re-emitted; a retained key absent from the current scan gets
  a tombstone re-emission — stale mission content is never displayed as
  current after a rebuild; server state loss rebuilds the view from one full
  scan; (4) clean-state and absent-dir snapshots replace and tombstone
  respectively; (5) two nodes with one slug render as flagged distinct rows;
  (6) a strace-level check (or equivalent test) that production performs no
  write and no non-read git subcommand against the mission dir; (7) payloads
  over the ceiling arrive marked truncated, never silently clipped.

### U-DELEGATE — delegation leases + SESSION_OWNER advice

- **Type**: build. **Depends on**: U-CORE; owner ruling §7-5.
- **Territory**: server lease verbs + store; spoke advice cache; the launch
  choke point's env stamping. No session-service or mission change.
- **Settled by this design**: DECISION-6 in full — lease tenure semantics,
  composite mission subject (node_id + directory; slug descriptive) with
  orphan-on-move/rename, slug-group as an explicit marked variant,
  attribution-never-authentication, stamping precedence (explicit env >
  mission owner > node lease > honest absence), advice-tier distribution.
- **AC sketch**: (1) grant/transfer/release follow label-lease rules
  (transfer names the holder; bare collision refuses); (2) subject identity:
  a mission lease never attaches across nodes or directories sharing a slug;
  a directory move/rename orphans the lease, displayed as orphaned and never
  auto-reattached; a slug-group lease renders its current capture set and is
  visibly marked group-scoped; (3) spawn stamping honors the pinned
  precedence, including absence; (4) a dark spoke serves stale advice and
  blocks nothing; (5) leases never gate any verb anywhere (grep-level and
  test-level assertion); (6) interim static-env provisioning keeps working
  unchanged.

### U-GATE — phase-2 prerequisites (enabler, not hot reads)

- **Type**: enabler. **Depends on**: none of the above (may proceed in
  parallel); blocks any future hot-read unit.
- **Territory**: a standing verification of the already-landed four-state
  invariant; the test-only projection seam and its hot/cold parity harness
  in CI. Explicitly does **not** implement hot reads, the mode shim,
  `watch`, or any serving path for the seam — the seam is instantiated only
  by tests.
- **Settled by this design**: §5's four gate preconditions, with
  legacy-view retirement recognized as landed on main (`8af91d2`,
  `75ab144`) and converted to verification; the parity harness runs against
  the test-only seam; the harness is a permanent tax, not scaffolding;
  `--cold` is forever.
- **AC sketch**: (1) a standing assertion (test + grep across verbs) that no
  read path consumes a two-state view and v1 status appears only in
  migration compatibility — codifying the landed invariant so it cannot
  silently regress; (2) the test-only projection seam exists, is wired to
  no verb, socket, or view (asserted), and the parity harness exercises it
  in CI over recorded and fuzzed registries, failing the build on any
  hot/cold divergence; (3) a written go/no-go checklist instantiating §5's
  four preconditions exists for the owner to exercise when phase 2 is
  proposed.

<!-- Provenance: design record, 2026-07-12. Design only; implementation is staged separately (§Staging). -->
# Grok as a first-class herder/hcom family — design

Status: implemented design record; home/binary portions superseded by the 2026-07-14 owner amendment below
Subject: Grok Build against herder + hcom (original characterization used CLI 0.2.93)

> **Owner amendment — 2026-07-14 (binding):** Grok seats now use the live default
> `~/.grok` home and the vendor-installed, vendor-updated executable. Herder no longer
> pins `GROK_HOME`, seeds or rewrites a Grok home, suppresses updates, accepts a pinned
> binary/version set, or executes the binary for version/capability gating. Launch walks
> `PATH`, skips herder's own shims, and selects the first vendor executable. The hcom MCP
> server is registered through a seat-worktree `.grok/config.toml` project layer using the
> characterized `[mcp_servers.hcom]` surface; launch refuses `--cwd` so that config directory
> and Grok's effective cwd cannot diverge. The owner's `~/.grok/config.toml` remains untouched.
> The identity-env
> allowlist, credential-by-name/process-env boundary, and
> `GROK_CLAUDE_HOOKS_ENABLED=0` launch override remain binding. Session discovery and
> lifecycle evidence now read `~/.grok/sessions`. Conflicting home, update, binary, and
> version-pin language below records the original design and is superseded by this ruling;
> its transport, receipt, identity, lifecycle, and observability decisions remain current.

Evidence base (cited throughout by path + section):

- `docs/grok-integration-characterization.md` — the settled delivery record: capability
  matrix, mechanism matrix ("Full delivery mechanism evidence"), monitor/MCP probes,
  session-file layout, identity hazards.
- `docs/design/grok-onboarding-memo.md` — onboarding survey, herder touchpoint gap list,
  and the post-demo ERRATUM.
- `docs/design/grok-demo-report-2026-07-12.md` — isolated pane demo: environment truths,
  hook-exit-0-without-registration falsification.

## 1. Settled ground (binding; not relitigated here)

| Constraint | Source |
|---|---|
| Delivery architecture is **monitor wake + MCP fetch/ack/pending/send**. PTY paste is owner-rejected as the delivery mechanism. Passive hook stdout, stop-hook exit-2 stderr, and `hcom term inject` are proven dead surfaces. | characterization "Decision", "Capability matrix", "Mechanism matrix" |
| The Claude-hook shortcut is falsified end-to-end: all four Claude-compatible hooks exit 0 against real hcom 0.7.23 yet create **no roster row and no bus name**. Registration and delivery must both be first-class. Hook exit 0 must never be equated with anything. | onboarding memo ERRATUM; demo report "hcom findings and memo contradiction" |
| Raw-agent login shells reset `HOME` and drop spawn-time env; child-side env injection is required. In herder panes `hcom` resolves to `tools/herder/shims/hcom` (routes through `herder hook`). Grok 0.2.93's settings-level `env.HCOM` override is ineffective; only a direct child env export reaches the real binary. | demo report "Initial containment incident", "hcom findings" |
| Update suppression: `--no-auto-update` and `[cli] auto_update = false`, both proven. Unsuppressed, the CLI auto-downloaded 0.2.99 and repointed `~/.local/bin/grok`. | demo report "Isolated retry construction", "Live vendor-state contamination" |
| Auth is the xAI API key via process env inheritance, referenced **by variable name only** — never in argv, config, registry, logs, doctrine, or reports. | demo report "Authorization and credential handling" |
| Permission mapping: normal autonomy → `--always-approve`; `--safe` stays ask-mode. `bypassPermissions` is **not ruled in** (owner decision, §10). | onboarding memo "Owner answers required" Q4; demo report |
| `--model` passthrough is supported; there is **no default model pin** (owner decides later, §10). | onboarding memo Q5 |
| Monitor semantics: an idle session is woken by a monitor line as a synthetic notification turn; a mid-turn line is buffered to the turn boundary; monitors never preempt a running tool. Monitor and MCP were proved **separately** — the correlated end-to-end receipt machine is the open work this design closes. | characterization "Delivery evidence" |

## 2. Architecture overview

Four cooperating parts per Grok seat, plus one durable store. Names used throughout:

- **Spool** — the seat's durable message journal (append-only JSONL under the herder
  state dir, keyed by seat GUID). The single source of truth for the receipt state
  machine. Survives every process in this diagram.
- **Binder** — a per-seat herder-owned daemon. The only spool writer, enforced by an
  exclusive per-seat OS lock with generation fencing (DR-2). Binds the seat to hcom
  (`hcom start` for identity, anonymous committed-cursor `hcom events --full` drains
  for inbound pickup, `hcom send` for outbound), owns the receipt state machine,
  serves a per-seat unix socket to the tap and the MCP server.
- **Tap** — `herder grok tap --seat <guid>`: the command Grok itself runs as a
  persistent monitor. A dumb pipe: connects to the binder socket and prints exactly the
  lines the binder hands it. Prints nothing else, ever.
- **Bus MCP server** — `herder grok mcp`: a Grok-spawned stdio MCP server exposing
  `fetch_message`, `ack_message`, `list_pending`, `send_message`. A thin adapter that
  forwards every operation to the binder over the same socket. Scope honesty: the
  characterization proved generic MCP request/response tool transport in Grok 0.2.93
  (characterization "MCP probe") — these four operations are **new bridge code** riding
  that transport, not individually characterized vendor capabilities.

```text
hcom bus (events store: id-addressed, non-destructive to anonymous reads)
   │  anonymous `hcom events --full` DRAIN, gated by journal-derived cursor (id > C)
   │  (anonymous `--wait` edge-trigger between empty drains — latency only)
   ▼
binder ──────────────► spool (append-only journal; fsync before wake)
   │ per-seat unix socket
   ├─────────────► tap (Grok persistent monitor) ──► one compact wake line
   │                                                  └─► synthetic notification turn
   └─────────────◄ bus MCP server ◄── Grok model calls fetch / ack / pending / send
```

Doctrine delivered at launch via `--rules` instructs the model to start the tap as a
persistent monitor on its first turn, to fetch by id when a wake line appears, to call
`list_pending` at session start and after any recovery, and to ack only after
processing. The spool makes every hand-off recoverable: any process here can die and
restart without losing a message or fabricating a receipt.

---

## DR-1 — Bridge ownership: herder owns the family; hcom is used only through its generic agent surface

**DECISION.** The Grok family is owned end-to-end by herder: launch, registration,
delivery bridge, receipts, lifecycle, observation. hcom is consumed exclusively through
its public generic verbs, executed by the binder, a herder process:

- `hcom start` — bus identity (adhoc registration; `--as` reclaim on restart);
- `hcom events --full` — **the inbound pickup source of record, as an anonymous
  non-wait DRAIN loop.** The events store is id-addressed, SQL-queryable, and — for
  identity-free invocations — non-destructive, with each `--full` row carrying the
  raw full envelope (event id, sender, text, intent, thread, mentions, scope,
  `delivered_to`, `reply_to`/`reply_to_local`). Verified against installed hcom
  0.7.23 on isolated scratch buses, as corrected by independent adversarial
  reproduction (§Design-time verification, V1–V4). The pickup contract, exactly:

  1. **Drain** (the only durable primitive): anonymous `hcom events --full` — no
     `--wait` — paged **oldest-first above the cursor**. hcom's snapshot query is
     `ORDER BY id DESC LIMIT 20` by default, so a naive `id > C` drain returns only
     the **newest** 20 rows; deriving C from that page skips every older matching row
     forever (reproduced with a 25-message backlog: 20 returned, C advanced past all
     25, repeat empty, five lost — V9). The drain query therefore selects its page
     itself, via the reproduction-proven shape:

     ```sql
     id IN (SELECT id FROM events_v
            WHERE type='message' AND id > <C>
              AND EXISTS (SELECT 1 FROM json_each(msg_delivered_to)
                          WHERE value='<seat>')
            ORDER BY id ASC LIMIT 20)
     ```

     The subquery controls page **membership only** — it does not control the order
     hcom emits the page (the outer snapshot query emits id-descending; the CLI's
     later sort is by timestamp, not id). Therefore, after parsing a drain page, the
     binder **MUST sort the rows by numeric event id ascending before any journal
     append**, then append+fsync in that order; only then is every crash-derived
     prefix cursor safe. Without the sort, a crash after fsyncing the first emitted
     (highest-id) row derives C past the rest of the page and strands it forever
     (reproduced: page emitted `[42,40,…,4]`, crash after journaling id 42, replay
     derived C=42, ids 4..40 permanently lost — V9). Set C to the max id of the
     fully-journaled page; repeat until a page comes back empty. A merely large
     `--last N` is **not** a fix for page loss — it moves the threshold without
     removing it. `msg_delivered_to` is
     hcom's own canonical routing snapshot: it covers direct, thread, tag, and
     broadcast fanout exactly as hcom routed them and excludes the sender — a
     mention-or-broadcast predicate is wrong (it returns the seat its **own**
     broadcasts, and thread fanout keeps the sender in `mentions`; both self-delivery
     bugs reproduced, V3).
  2. **Edge trigger** (latency optimization only, zero correctness weight): after a
     drain returns empty, block on the same anonymous query with `--wait`. On any
     wake **or timeout**, return to step 1. `--wait` is structurally unfit as the
     durable primitive: it looks back only 10 seconds and then initializes a private
     cursor to the database's current global max — a matching row older than 10s is
     silently unreachable (reproduced: an 11s-aged message timed out; V4). Only the
     drain reads from the binder's cursor, so backlog of any age is picked up.
  3. **Identity-free reads, explicitly scrubbed**: every read invocation (drain and
     wait) runs with **no hcom identity** — no `--name`, and an environment scrubbed
     of the native router's identity inputs, concretely `HCOM_PROCESS_ID` and
     `CODEX_THREAD_ID` in 0.7.23 (the scrub list is version-pinned and revisited on
     any hcom upgrade). A read under a registered identity triggers hcom's post-dispatch
     pending-message delivery: it appends formatted unread messages to stdout
     (corrupting the machine-read output) and advances the seat's internal cursor
     (reproduced: unread 2 → 0 from a named query; V1). Anonymous `--wait` polls
     internally at ~500ms rather than holding a per-identity endpoint — accepted;
     correctness never rides on the wait path. `--full` is mandatory on every
     invocation: the plain output strips `scope`, `delivered_to`, and reply fields,
     and no `--json` flag exists (V2). The raw full envelope is journaled as
     returned — including both `reply_to` and `reply_to_local` — never reconstructed.
- `hcom send --name` — outbound: the one identity-bearing verb the binder runs.
  Because any named command may trigger hcom's post-dispatch pending delivery onto
  stdout, the binder treats send output defensively (exit code plus tolerant parse)
  — and an implicit drain there is harmless to pickup correctness, which rides
  exclusively on the anonymous drain and the journal-derived cursor, never on hcom's
  internal per-identity cursor.

`hcom listen` is **explicitly rejected as a pickup source**: hcom advances the
instance's internal `last_event_id` before printing, outside binder control — a
crash between hcom's internal advance and the binder's fsync would silently lose the
message on restart (`hcom start --as` preserves that cursor) — and its JSON emits only
`{from,text}`, without the event id, intent, or thread the receipt machine keys on.
Both defects confirmed by direct probe (§Design-time verification, V5). Because the
committed cursor lives with the spool and the events store is non-destructive, no
hcom-internal cursor state can lose a message for this design.

No `hcom grok` launcher is required or waited for. Grok's accidental Claude-hook
compatibility is structurally disabled (DR-3) so no half-integration path exists.

**Which `hcom` binary.** In herder panes, `hcom` on PATH resolves to
`tools/herder/shims/hcom`, which routes hook calls through `herder hook` — not the raw
binary the earlier characterization exercised (demo report "hcom findings"). The
design retires this ambiguity on both sides: no Grok-side process ever calls hcom
hooks at all (Claude-compat hooks are disabled, DR-3), and the binder resolves the
real hcom binary deterministically at seat setup (recorded in seat state) rather than
trusting ambient PATH semantics inside a pane. Every bus interaction for a Grok seat
therefore goes through exactly one recorded binary, invoked by exactly one process.

The binder is started by `herder launch grok` in the seat's pane environment (as the
existing status sidecar is: `tools/herder/internal/launchcmd/launch.go`,
`startSidecar`), detached into its own session, logging only to files under the herder
state logs dir. A small supervision loop (self re-exec with capped backoff) restarts it
on crash; every restart replays the spool before touching hcom (DR-2 recovery).
Registry records binder pid + socket path so `herder` can report bridge health.

**Silence rule.** The binder and tap never write to the pane or to Grok's context
except the compact wake/recovery lines defined in DR-2 — every monitor line becomes a
model notification turn (characterization "Recommended architecture"). All diagnostics,
supervision chatter, and errors go to per-seat log files. The hermetic gate suite
enforces this with a zero-byte-output test (§8, T17).

**Alternatives considered.**

- *Upstream hcom grok launcher (`hcom grok --run-here`, matching claude/codex/gemini in
  `launchcmd.Run`).* Rejected for now: hcom 0.7.23 has no grok row (onboarding memo
  "Current launch and binding contract"), its native delivery model (hook-stdout
  injection + blocking Stop-hook poll) is structurally incompatible with Grok
  (characterization "Why hooks cannot deliver messages"), and waiting on upstream
  blocks the family indefinitely. An upstream ask remains open as a labeling/receipt
  refinement (§10), not a dependency.
- *Lean on Grok's Claude-hook compatibility for registration and let herder add only
  delivery.* Falsified: hooks exit 0 and no roster row is created (memo ERRATUM). Even
  when an earlier probe did observe registration, the row was `tool: claude` with a
  directory-keyed identity and subagent-stops-parent hazards (characterization
  "Existing hcom interaction"). Dead either way.
- *Bridge inside hcom's PTY-wrap / `term inject` plumbing.* PTY delivery is
  owner-rejected; `term inject` has no port for a pane hcom did not spawn
  (characterization §4).
- *Binder-less design: Grok polls MCP on a schedule.* This is mechanism B2
  (characterization "Recommended delivery taxonomy") — functional but model-driven
  latency, and it surrenders the push-like wake that was the point of the monitor
  probes. Kept only as the implicit degraded behavior when the wake channel is down
  (doctrine still mandates `list_pending` at deterministic points).

**hcom-side "delivered" honesty.** Herder's spool receipt is the **only**
authoritative delivery signal for Grok seats; every herder surface (send verification,
registry, observer) reports from the spool. hcom's own unread counters for the seat
are meaningless for Grok rows and documented as non-authoritative: the binder's
reads are anonymous and never consume them, while its named sends may incidentally
clear them via hcom's post-dispatch delivery — so they can drift upward or vanish,
correlated with nothing. Three non-blocking upstream niceties remain (owner
decision, §10): a roster tool label for generically-started identities (an adhoc
`hcom start` row labels itself from the detected calling environment, not the
represented tool — §Design-time verification, V6); a machine-readable no-delivery
read mode (so daemon reads would not depend on identity scrubbing, and blocking
reads would not need the anonymous ~500ms poll); and a native non-destructive
fetch/commit API that would let the unread markers track reality.

## DR-2 — The receipt state machine

This is the piece the evidence record never assembled: monitor wake and MCP ops were
proved separately (characterization "Delivery evidence"), never as one correlated
system. This section defines it completely.

### States

Per inbound message id, strictly monotonic (duplicates recorded, never regress):

```text
queued ──► surfaced ──► fetched ──► acked            (terminal: DELIVERED)
   │            │            │
   └────────────┴────────────┴─────► undeliverable   (terminal: seat retired unacked)
```

- **queued** — the binder has appended a full journal record (hcom event id, sender,
  intent, thread, payload, payload hash, timestamps) and fsynced it. This happens
  **before any wake emission and before any inference is possible** — pending ids
  persist before inference, always. The binder's **committed cursor is derived, not
  stored**: it is the maximum hcom event id present in the journal, recomputed by
  replay. A message is "picked up" if and only if it is durably queued; there is no
  separate cursor record to fall out of sync.
- **surfaced** — at least one *surfacing event* is journaled: either (a) a wake line
  handed to the tap, or (b) the id returned in a `list_pending` or `fetch_message`
  MCP response. (b) is the recovery-equivalent of a wake and is the sanctioned
  re-listing path after any restart. Each surfacing event is journaled individually
  (kind, timestamp, tap generation).
- **fetched** — the full payload was served over the seat's MCP channel. Idempotent:
  repeat fetches return the same payload and journal a repeat marker.
- **acked** — `ack_message(id)` accepted over the seat's MCP channel. Terminal.

### The delivery definition (the only one)

> **delivered(id)** ⇔ the seat's journal shows `queued → surfaced → fetched → acked`
> for that id, where the ack arrived over the seat's own MCP channel.

Nothing weaker is ever reported as delivered by herder or surfaced as delivered
through any herder-owned view of hcom state. Specifically **not** delivered: journal
append (report "queued"), wake emission (wake ≠ injection — Grok buffers mid-turn
lines to the boundary and the bridge cannot observe injection), fetch without ack,
hook exits of any kind, hcom pickup, or any hcom-native marker. The causal chain gives
the correlation the receipt needs: an id can only be acked if it was fetched, only
fetched if it was surfaced by this seat's bridge, only surfaced if it was durably
queued.

**Enforcement at the MCP boundary:**

- `fetch_message(id)` for an id never queued in this seat's spool → error (prevents
  cross-seat and fabricated-id confusion).
- `ack_message(id)` without a prior journaled fetch → rejected with an instructive
  error ("fetch before ack"). An ack straight off a wake line would otherwise claim
  delivery of a payload the model never read (wake lines carry routing + hash only).
- Both operations are idempotent on repeats; repeats are journaled.

### Wake and recovery line formats

One line per message, routing data only — no payload (quoting/size safety; the payload
travels over MCP). Format per characterization "Recommended architecture":

```text
HCOM id=<id> from=<sender> intent=<intent> thread=<thread> h=<hash8>
```

On tap (re)connect while unacked messages exist, the binder emits a single recovery
line instead of replaying per-message wakes (prevents duplicate-wake floods after
restarts):

```text
HCOM_RECOVER pending=<n>
```

Doctrine: on `HCOM_RECOVER`, call `list_pending` and work the ids. These two are the
only line shapes the tap ever prints.

### Nudge policy (bounded, journaled, never blind)

A wake may go unfetched for a legitimate reason: the message landed mid-turn and is
buffered to the boundary (characterization "Persistent monitor: interactive TUI").
The binder is therefore turn-aware before it nudges: it reads the session's
`events.jsonl` phase records (characterization "Session files") and only counts
fetch-latency while the session is idle. If a surfaced id stays unfetched past the
idle threshold, the binder re-emits the wake line — at most N times (default 2) with
backoff, each journaled as a surfacing event — then declares the wake channel suspect
and degrades (below). No hcom-level resend is ever triggered by this machinery;
"queued is never blindly resent" holds end-to-end.

### Failure and recovery matrix

| Scenario | Behavior |
|---|---|
| **Binder crash/restart** (any point) | Supervisor restarts it; it acquires the seat lock (below), replays the journal, derives the committed cursor, reclaims its bus identity (`hcom start --as <name>`), reopens the socket, and re-queries `hcom events` from the derived cursor. The crash windows, walked: *(a) crash after events query, before journal append+fsync* — the cursor never advanced (it is derived from the journal), and the events store is non-destructive, so the re-query returns the same message again; at-least-once into the spool, deduped by event id. *(b) crash after append, before wake* — replay finds the id queued-but-unsurfaced; `HCOM_RECOVER` covers it. *(c) crash after wake* — unacked ids are re-surfaced via a single `HCOM_RECOVER` on tap reconnect, never re-woken individually. No hcom-internal cursor participates in correctness at any point (DR-1). |
| **Duplicate wake** (nudge, tap restart race) | States are monotonic; double fetch is idempotent; one delivery results. |
| **Out-of-order fetch/ack** across ids | Allowed. Ids are independent; ordering is advisory (wake-line order, `list_pending` queue order). No global ordering requirement. |
| **Auth/rate failure (429) mid-turn** after fetch, before ack | Id remains `fetched`, journal intact (persisted before inference). Doctrine: at the start of any turn following an error, call `list_pending`. The idle-aware nudge also re-surfaces. Delivery completes on the retried turn. API-key auth does not self-refresh — repeated auth failures degrade the seat honestly (DR-5) rather than looping. |
| **Tap death** (monitor stopped, EOF on socket) | Binder marks the seat **wake-degraded** in the registry; messages continue to queue durably. Recovery: the model restarts the tap (doctrine: "if your monitor task is not running, restart it"), or the heavy path below. |
| **Compaction** | Monitor survival across compaction is an explicitly open question (characterization "Open questions" #2) — probe P5 (§9) resolves it before the transport unit is accepted. Defensively the design already covers both answers: if the monitor dies, tap-death handling applies; if it survives, nothing changes. Doctrine additionally mandates `list_pending` after any compaction the model is aware of. |
| **Resume** (`--resume <sid>`) | Same seat, same session id, same spool, same bus name (binder persists or reclaims via `--as`). The relaunch re-passes doctrine and the boot arming prompt (DR-3); first turn restarts the tap and calls `list_pending`, which re-surfaces everything unacked. |
| **Fork** (`--resume <sid> --fork-session`) | New seat row with lineage, **fresh spool**, new bus name, freshly armed doctrine. Unacked messages stay with the parent seat; nothing migrates silently. |
| **Seat cull/retirement with unacked ids** | Ids move to `undeliverable` (terminal, journaled). Registry records the count; herder send verification for those messages reports undelivered honestly. |
| **Wake channel down + seat idle, extended** | Last-resort heavy path, off by default and config-gated: quiesce and relaunch with `--resume <sid>` plus the arming prompt (mechanism B3, characterization "Recommended delivery taxonomy"). Default posture is honest degradation: the registry shows wake-degraded + pending count; a human or orchestrator decides. |

### Outbound

`send_message` (MCP) → binder → `hcom send --name <busname> ...` → hcom's actual
result (message id or error) returned verbatim in the tool result and journaled for
audit. Outbound needs no receipt machinery beyond that: the model sees the real
outcome synchronously and decides about retries itself.

### Reporting vocabulary (what herder may say, when)

| Report | Meaning | Trigger |
|---|---|---|
| `queued` | Durably journaled for the seat | journal append + fsync |
| `delivered` | The definition above — nothing weaker | `acked` |
| `undeliverable` | Terminal non-delivery | seat retired with id unacked |
| `wake-degraded` | Seat reachable only via recovery listing | tap down / nudges exhausted |

### Persistence format

Append-only JSONL journal per seat (`<herder-state>/grok/<seat-guid>/journal.jsonl`),
single writer (binder), fsync on the records that gate external claims (`queued`,
`acked`). State is derived by replay; a periodic snapshot record bounds replay cost.
This matches the house pattern (events.jsonl everywhere) and makes every recovery path
above a pure replay.

**Single-writer is enforced, not asserted.** A registry pid/socket record is
observation, not exclusion — a slow old binder can overlap a supervisor restart.
Therefore:

- The binder holds an **exclusive per-seat OS lock** (`flock` on a seat lock file) for
  the entire time it may touch the journal, the hcom identity, or the socket. A
  restarting binder blocks on the lock until the predecessor releases or is killed;
  two live binders for one seat are structurally impossible.
- Each lock acquisition increments a durable **generation number** (journaled). The
  tap and MCP server learn the current generation at socket handshake, and **every
  subsequent request is generation-fenced**: an operation stamped with a stale
  generation is rejected with a reconnect error, never executed. A tap or MCP
  connection that straddles a binder restart therefore cannot produce duplicate
  surfacing records, and an ack accepted by a dying generation cannot be re-presented
  by the next one — the journal (fsynced under the lock) is the arbiter either way.
- The deliberate dual-binder race is a gate test (T23, §8).

## DR-3 — Launch contract

**DECISION.** `herder spawn --agent grok` becomes a first-class family with a
herder-owned launch path that does **not** route through `hcom <tool> --run-here`
(there is no such tool row; DR-1). `launchcmd` gets a grok branch that prepares the
seat, starts the binder, and execs Grok directly with a fully explicit child
environment and argv.

**Launch sequence** (ordering matters — the bus name must exist before doctrine can
name it):

1. Generate seat GUID + session UUID (v7; `--session-id` requires a valid unused UUID —
   characterization "Launch"). Record both in the registry *before* launch.
2. Start the binder. It acquires the seat lock, then the bus identity (`hcom start`;
   the adhoc path assigns the name — role-tag ergonomics are probe P1, §9),
   writes it to the seat state, opens the socket. Launch blocks briefly on this.
3. Compose the doctrine block (below) with the assigned bus name, seat GUID, session
   id, MCP tool names, and the tap command line.
4. Exec Grok with the explicit child environment and argv (below).
5. Turn 1 (driven by the boot arming prompt) starts the tap and calls `list_pending`;
   the tap's socket connect flips the seat to wake-armed.

**Child environment — herder-owned exec env, not a wrapper script.** The demo proved
spawn-time env does not survive the raw pane's login shell (demo report "Initial
containment incident") and that only direct child env export works (settings-level
`env` overrides are ineffective in 0.2.93). The demo closed this with a throwaway
wrapper script; the first-class mechanism is the launch process itself: `herder
launch grok` runs *inside* the pane (it is the pane command), so it sets the
environment in its own process and execs Grok — exactly how `launchcmd.Run` already
works for other families. No generated wrapper file exists to drift, leak into
`ps`/argv, or tempt a credential write. Explicitly exported: `GROK_HOME`, `HCOM_DIR`,
herder state/socket vars. Explicitly **not** handled: the xAI API key — it is
inherited by name from the ambient profile environment; herder verifies set/unset
existence pre-launch (never the value) and refuses launch with a clear error if unset.

**GROK_HOME: a dedicated herder-owned home.** `GROK_HOME` is pinned to a
herder-managed directory (e.g. `<herder-state>/grok-home`), seeded once with a
controlled `config.toml`:

```toml
[cli]
auto_update = false

[compat.claude]
hooks = false
```

plus the global-scope bus MCP server registration (`herder grok mcp`). Rationale over
the alternative (pin to real `~/.grok`, the `PinConfigDir` pattern):

- The accidental Claude-hook path — the falsified half-integration and the
  subagent/identity hazards it carries (characterization "Existing hcom interaction")
  — is disabled *structurally*, without herder mutating the human's `~/.grok` config.
- Auth is env-key (settled), so no auth state needs to live in the home; the usual
  reason to share the real home is void.
- Sessions land in a layout the observer fully owns (`GROK_HOME/sessions/...`,
  DR-5), cleanly separated from the human's own Grok sessions.
- Update state is controlled; the demo showed the unsuppressed CLI rewriting
  `~/.local/bin/grok` under the user (demo report "Live vendor-state contamination").

Cost accepted: a human resuming a herder Grok session by hand must set `GROK_HOME`;
`herder resume` does it for them.

**Doctrine via `--rules`.** The only proven launch-time bootstrap surface
(characterization: `--rules` WORKS; passive hook output cannot carry doctrine). The
GrokBootstrapBlock contains: assigned bus name and addressing rules; the instruction
to start `herder grok tap --seat <guid>` as a persistent monitor immediately and
restart it if ever missing; the wake/recovery line contract and the
fetch-then-ack-after-processing discipline; `list_pending` at session start, after
`HCOM_RECOVER`, after any error or compaction; the subagent prohibition (DR-4); the
credential rule (never print or persist key material); and the silence expectation
(no speculative bus chatter). Resume and fork relaunches re-pass the doctrine; probe
P6 (§9) verifies `--rules` behavior on `--resume` before the wording is frozen.

**Initial prompt rides the receipt machine.** The task prompt is **not** pasted and
**not** put in argv. `herder spawn --prompt` enqueues it as the seat's first spool
message before Grok starts; the argv carries only a constant one-line boot arming
prompt ("Start your monitor per your rules, then list pending messages and proceed.").
Turn 1 arms the bridge and pulls the real task through fetch/ack — so even the initial
prompt gets a real delivery receipt, and large/multiline prompts avoid argv quoting
entirely. Grok's positional initial-prompt argv is probe P3; if absent, the fallback
is herder's existing raw-agent boot-paste (sigil `❯`, onboarding memo touchpoint #11)
carrying **only the constant arming line, never message content** — flagged to the
owner (§10) since it grazes the PTY rejection even though it is launch arming, not
message delivery.

**Flag mapping and refusals.**

| herder intent | Grok argv |
|---|---|
| normal autonomy | `--always-approve` |
| `--safe` | no permission flag (Grok's native ask-mode default) |
| `--model X` | `--model X` |
| always | `--no-auto-update`, `--no-subagents`, `--session-id <uuid>`, `--rules <doctrine>` |
| resume | `--resume <sid>` (+ doctrine + arming prompt) |
| fork | `--resume <sid> --fork-session` (+ fresh doctrine + arming prompt) |

Passthrough args (`--extra-arg`) that collide with the contract are **refused with an
error, never silently reconciled**: `--session-id`, `--resume`, `--fork-session`,
`--rules`, `--permission-mode`, `--always-approve`, `--model` (when the first-class
flag was also given), any auto-update flag, any arg that would re-point
`GROK_HOME`/`HOME`, and **anything that would re-enable or define subagents**
(`--agents`, or any negation of `--no-subagents`) — the subagent prohibition is a
soundness condition (DR-4), not a preference. `bypassPermissions` is not mapped
anywhere pending the owner ruling (§10).

**Version gate — the resolved binary is gated, not merely recorded.** Recording the
version after boot is observation; the launch contract needs exclusion. The box
reality makes this concrete: the characterized binary is 0.2.93, but PATH already
resolves to an **uncharacterized 0.2.99** — the documented auto-update repointed
`~/.local/bin/grok` (demo report "Live vendor-state contamination"; resolved versions
re-verified at design time, §Design-time verification V7). Therefore:

- Family config carries a **supported-version set and/or an explicit pinned binary
  path** (the characterized 0.2.93 binary initially). Launch resolves the binary,
  reads its version, and **refuses to launch** any version outside the supported set —
  before the seat is activated, with an error naming the resolved path, the version,
  and the supported set. No silent fallback, no launch-then-warn.
- Capability follows the same gate: flags the contract depends on (`--no-subagents`,
  `--session-id`, `--rules`, `--no-auto-update`) are part of the version's
  characterization; a new version enters the supported set only via a characterization
  pass, not by assumption (characterization risk #8: pin to version/capability, not
  screen text).
- The isolated live smoke (§8) runs **the same binary resolution the launch path
  uses**, so the gate battery and ordinary launches exercise one binary, never two.
- Launch always passes `--no-auto-update` and the seeded config carries
  `auto_update = false`, so the gated binary cannot drift under a running family.

## DR-4 — Identity and lifecycle

**DECISION.** Seat identity binds exclusively on **preassigned session id + process
and pane evidence** (Grok pid in the seat's pane, session directory
`GROK_HOME/sessions/<urlenc-cwd>/<sid>/` appearing at boot). Directory/cwd-keyed
identity is prohibited in every code path — probes saw a later same-cwd session
silently claim an existing identity (characterization "Bus-join hypothesis…identity
hazards").

**Parent/subagent separation.** A Grok subagent is its own session (own uuid-v7) whose
`SessionEnd` stopped the parent's bus instance in probes (characterization "Event
census", subagent hazard). Beyond the lifecycle hazard, subagents threaten the receipt
machine itself: the MCP tool surface carries no proven caller-session identity (the
characterized `tools/call` envelope shows name/arguments/progress metadata only), so a
subagent that can reach the seat's MCP tools could fetch and ack a parent message the
parent model never saw — a **false delivered receipt**. Doctrine cannot close that;
only an enforced boundary can. The guards, all enforceable:

1. The lifecycle hazard's vehicle — Claude-compat hooks — is disabled outright (DR-3),
   so no subagent lifecycle event can reach hcom at all.
2. Registry lifecycle transitions for the seat require **process-level evidence**
   (Grok pid exit, pane death, binder socket state) — never session events.
3. **First-class seats launch with subagents disabled**: `--no-subagents` ("Disable
   subagent spawning") is verified present in the characterized 0.2.93 CLI
   (§Design-time verification, V8), is in the always-argv (DR-3), and any passthrough
   that would re-enable or define subagents is refused. The flag is part of the
   version capability gate: a resolved binary without it fails the gate and does not
   launch.

Stated plainly: **without an enforced subagent boundary — `--no-subagents`, or a
future OS/cryptographic caller-identity boundary on the MCP channel — the delivered
predicate of DR-2 is unsound**, because ack-authorship cannot be attributed to the
parent model. No first-class seat runs without one of those in force. Doctrine still
tells the model not to delegate bus work, but it carries zero soundness weight. T16
(§8) tests this boundary as a **rejection** contract, not a journaling one.

**Resume** re-enters the same seat: same GUID, same session id, same spool, same bus
name (binder persists across the Grok restart, or reclaims with `hcom start --as`).
Doctrine and monitor are re-armed by the relaunch flags + arming prompt (DR-3);
`list_pending` on turn 1 closes any gap that opened while down.

**Fork** creates a new seat: new session id — which `--fork-session` assigns rather
than accepting preassignment, so the launch captures it post-boot from the session
directory / session listing and binds it with process/pane evidence before the seat is
declared bound. New spool, new bus name, registry lineage (forked-from GUID + parent
sid). Parent's unacked messages never migrate.

> **Erratum (U3 implementation, 2026-07-13).** The "assigns rather than accepting
> preassignment" premise above is falsified by the pinned 0.2.93 CLI itself:
> `--fork-session` "create a new session ID instead of reusing the original
> (optionally set via `--session-id`)", and `-s, --session-id` "With `--resume`/
> `--continue`, only valid together with `--fork-session` (names the forked
> session)". U3 therefore forks with a herder-minted UUIDv7 preassigned via
> `--resume <parent> --fork-session --session-id <fresh>`, run through the same
> cwd-agnostic `sessions/*/<sid>` collision check as launch, with process/pane
> AND session-directory evidence required before bound. This is a strict
> tightening — preassignment is DR-4's preferred identity model; the post-boot
> capture path described above is superseded. All three vendor flags remain
> refused as user passthrough in every mode (herder-owned mode data only).
> Authorized on the U3 thread; evidence quoted in the U3 DONE record.

## DR-5 — Observability honesty

**DECISION.** Every observation surface reports only what its evidence supports, with
the source labeled.

- **Transcript** = `GROK_HOME/sessions/<urlenc-cwd>/<sid>/chat_history.jsonl`, located
  by the seat's session identity. The hook-advertised `transcriptPath` is never used —
  it is absent at session start and points at `updates.jsonl`, not the transcript
  (characterization "Session files", §3). The observer gets a Grok transcript adapter
  for this layout; with the herder-owned `GROK_HOME` (DR-3) the path is fully
  determined by seat state.
- **Live status:** herdr has no Grok integration target, so herdr-reconciled
  `live_status` stays **`unknown`** — exactly as the demo row honestly showed (demo
  report "Pane and typed-task evidence"). No status is synthesized from weak evidence.
  Herder MAY enrich the row from the session's `events.jsonl` phase records
  (`turn_started`, `phase_changed`, tool/permission events — real on-disk evidence,
  characterization §3), but only as an explicitly labeled secondary source (e.g.
  `status(grok-events): tool_execution`), never mapped into herdr's native status
  vocabulary or into `unknown`'s place.
- **Registry rows** say `tool: grok` with real capability flags reflecting proven
  bridge state: `bus: bound` (binder registered on hcom), `wake: armed|degraded|down`
  (tap socket state), `pending: <n>` (unacked spool count). A row never implies bus
  capability the bridge has not proven — the falsified-registration lesson (memo
  ERRATUM) generalized: presence of machinery is not capability.
- **hcom-native markers** for Grok rows are documented as pickup-only (DR-1); herder
  send verification reads the spool, not hcom state.

---

## 8. Test and gate plan (contracts the implementation units must ship)

Hermetic first: the state machine, binder, tap, and MCP server are testable with a
**mock Grok** — a scripted process that consumes wake lines and drives MCP client
calls per scenario — plus a stub hcom (or isolated `HCOM_DIR` bus). No inference in
the gate battery. One isolated live smoke proves the real thing end-to-end.

Receipt state machine cases (each is an explicit named test):

- **T1 initial delivery** — spawn prompt enqueued pre-boot, surfaced on turn 1 via
  `list_pending`, fetched, acked; herder reports queued → delivered.
- **T2 idle delivery** — queued → wake → fetch → ack while idle.
- **T3 busy-turn delivery** — wake emitted mid-turn; fetch happens post-boundary; no
  nudge fires (turn-aware idle detection honors `events.jsonl` phase).
- **T4 duplicate wake** — nudge or tap restart causes a second wake; double fetch;
  exactly one delivered.
- **T5 duplicate ack** — idempotent; state unchanged; repeat journaled.
- **T6 out-of-order** — fetch/ack id 5 before id 3; both deliver independently.
- **T7 ack-before-fetch rejected** — instructive error; no delivered claim.
- **T8 foreign/unknown id fetch rejected.**
- **T9 binder crash before wake** (journaled, unsurfaced) — restart replays;
  `HCOM_RECOVER`; re-list; delivered. Also: crash after events query, before journal
  append — the derived cursor never advanced, the events store is non-destructive, so
  the restart re-query returns the message; no loss, no duplicate delivery.
- **T10 binder crash after wake, before fetch** — restart does not re-wake per id;
  single `HCOM_RECOVER`; delivered via re-list.
- **T11 tap death** — seat flips wake-degraded; messages keep queueing; tap restart →
  `HCOM_RECOVER` → drained.
- **T12 mid-turn 429/auth failure** — fetched-not-acked persists; idle-aware nudge or
  next-turn `list_pending` completes delivery; zero hcom-level resends.
- **T13 compaction** — policy test for whichever behavior probe P5 establishes
  (monitor survives → no-op; dies → tap-death path), plus doctrine-mandated re-list.
- **T14 resume** — same seat/sid/spool/name; re-armed; pending re-listed; no receipt
  regression.
- **T15 fork** — fresh spool and name; parent's unacked ids stay put; lineage
  recorded; no cross-seat fetch possible (T8 applies across the pair).
- **T16 subagent boundary (REJECTION test)** — launch argv provably contains
  `--no-subagents`; every passthrough form that would re-enable or define subagents
  (`--agents`, negations) is refused at spawn; an MCP request fenced to a stale
  generation or bearing non-seat session evidence is **rejected, never executed or
  journaled as accepted**; synthetic subagent session events never transition the
  seat.
- **T17 silence gate** — idle binder+tap emit zero bytes on the model-facing channel
  over an extended window; diagnostics appear only in files.
- **T18 reporting gate** — `delivered` is claimable only from `acked`; wake emission,
  fetch, hcom pickup each provably insufficient (assert on the reporting API, not
  logs).

Launch/lifecycle contract tests:

- **T19 passthrough refusals** — every colliding `--extra-arg` from the DR-3 list is
  refused with a targeted error.
- **T20 version/capability gate** — a resolved binary outside the supported set is
  **refused before seat activation** (error names path, version, supported set); the
  supported-set config and pinned-path knob are honored; `--no-auto-update` always
  present and seeded config carries `auto_update = false`; the smoke asserts its
  binary is the launch-path-resolved binary, not a separately pinned one.
- **T21 child environment** — launched Grok's `/proc` env shows the pinned
  `GROK_HOME` et al. despite the login-shell reset (integration test in a real pane);
  no secret name's *value* appears in argv, generated files, registry, or logs.
- **T22 identity binding** — a second Grok session in the same cwd cannot claim the
  seat (no cwd-keyed path exists to exercise).
- **T23 dual-binder race** — two binders deliberately started for one seat: exactly
  one holds the seat lock and serves; the loser blocks or exits without touching
  journal, hcom identity, or socket; tap/MCP connections straddling the generation
  change are fenced with reconnect errors; the journal shows no duplicate surfacing
  and no ack accepted by a stale generation.

hcom surface contract tests — run against the **real installed hcom binary** on an
isolated bus (not the stub), because they pin the exact 0.7.23 behaviors the pickup
contract was corrected for:

- **T24 stale backlog beyond the wait lookback** — a message delivered while the
  binder is down, aged well past 10 seconds, is picked up by the restart drain (the
  `--wait` path alone provably cannot return it; the drain must).
- **T25 identity-free reads** — drain and wait invocations run with no `--name` and
  a scrubbed environment; their stdout is exactly the `--full` event rows (no
  post-dispatch message delivery appended); the seat's hcom unread state is unchanged
  by reads.
- **T26 self-delivery exclusion** — the seat's own broadcast and its own
  thread-fanout sends never enter its spool (the `msg_delivered_to` predicate
  excludes the sender); a peer's broadcast, thread, tag, and direct sends all do.
- **T27 backlog beyond the snapshot page, with a mid-page crash under hostile
  ordering** — more than 20 matching messages queue while the binder is down, with
  **equal/non-monotonic timestamps forced** on the page (a naturally-ordered run
  passes accidentally, because timestamps usually rise with ids, and pins nothing);
  the binder is crashed after K < page-size rows have been fsynced; on restart the
  drain journals **every id and payload exactly once**, in ascending id order,
  across multiple pages — pinning both the oldest-first paged membership query and
  the mandatory binder-side id sort before append.

**Live smoke (one, isolated, gated):** real Grok 0.2.93, throwaway `HOME`/`GROK_HOME`/
`HCOM_DIR`/herder state, owner-authorized spend. Proves bidirectionally: an inbound
message reaches `delivered` through the full correlated chain, and an outbound
`send_message` lands on the isolated bus with hcom's receipt. This is the acceptance
gate for the launch unit (§11, U2), run under the activation flag. It does not by
itself declare the family working — that happens only at the activation change after
the lifecycle and observer units land (§11, "Activation is its own gate").

## 9. Implementation probes (facts to nail before/while building; not owner-level)

- **P1** `hcom start` identity ergonomics for a daemon binder: per-start name/tag
  control (the adhoc path assigns a random name — V6) and `--as` reclaim behavior
  across binder restarts. (Registration itself, envelope, and pickup are settled —
  §Design-time verification.)
- **P2** — *resolved at design time.* `hcom listen` is disqualified as pickup
  (destructive internal cursor, `{from,text}`-only JSON) and anonymous `hcom events
  --full` draining is qualified as the source of record, with `--wait` demoted to a
  post-drain edge trigger and reads required to be identity-free; see §Design-time
  verification V1–V5 as corrected by independent reproduction. No implementation
  probe remains.
- **P3** Grok TUI positional initial-prompt argv: does it start turn 1 unattended?
  (Fallback path and its owner flag: DR-3.)
- **P4** Bus MCP server child environment: does Grok pass `GROK_SESSION_ID` (or
  equivalent) to MCP server children? If not, the server identifies its seat by
  parent-process walk against the registry (process evidence, consistent with DR-4);
  seat files by cwd are prohibited.
- **P5** Monitor survival across compaction and `--resume`; monitor auto-stop
  threshold under line volume (characterization "Open questions" #2–3).
- **P6** `--rules` on `--resume`: accepted? duplicated into context? (Doctrine
  re-arm wording depends on it.)
- **P7** Wake-line rate/size tolerance of the monitor channel (flood behavior feeds
  the nudge caps).

## 10. Owner decisions required

1. **`bypassPermissions`** — normal → `--always-approve` and `--safe` → ask-mode ship
   as designed; whether any herder mode may map to Grok's `bypassPermissions` changes
   the security boundary and stays unmapped until ruled (onboarding memo Q4).
2. **Default model pin** — none in this design; `--model` passes through. Owner
   decides any pin after scored trials (onboarding memo Q5, ranked task 3).
3. **Live-smoke inference spend** — the §8 smoke consumes xAI quota; each
   implementation unit that runs it needs spend authorization.
4. **Upstream hcom asks (conditional, non-blocking)** — the review-directed
   escalation ("no adequate pickup surface → blocking owner decision before
   implementation") was evaluated and **averted**: the events surface passed
   design-time verification as the pickup source of record (§Design-time
   verification), so implementation is not blocked on upstream hcom. What remains is
   file-or-hold niceties: (a) a `grok` tool label for generically-started identities
   so the hcom roster can say what herder's registry says (adhoc rows currently label
   from the detected environment — V6); (b) a machine-readable **no-delivery read
   mode** for daemon consumers, removing the need for identity scrubbing and the
   anonymous ~500ms wait poll (V1/V4); (c) a native non-destructive fetch/commit API
   so hcom's unread markers could track reality instead of being documented as
   non-authoritative for Grok rows (DR-1).
5. **Boot-arming fallback (conditional)** — only if probe P3 falsifies the argv boot
   prompt: approve one constant arming line via the existing composer boot-paste at
   launch (never message content), or require waiting for an alternative. Flagged
   because PTY paste was rejected for *delivery*; this would be launch arming.

## 11. Staging (mergeable units, territory fences, gates)

Transport ships first; the shim ships last — a hand-typed `grok` shim routing into a
nonfunctional family is forbidden until the live smoke is green.

| # | Unit | Territory (fence) | Gate |
|---|---|---|---|
| U1 | **Transport core**: spool journal + state machine, binder (incl. supervision, seat lock/generation fencing, hcom generic binding), tap, bus MCP server, `herder grok <tap\|mcp\|bridge>` subcommands. No spawn wiring — nothing user-reachable changes. | New internal package(s) only (e.g. `tools/herder/internal/grokbridge/`) + `herder grok` command registration. | T1–T18 + T23 hermetic (mock Grok + isolated bus); T24–T27 against the real hcom binary; probes P1/P4/P5/P7 answered and recorded. |
| U2 | **Launch contract, behind an activation gate**: `launchcmd`/`spawncmd` family wiring, version/capability gate, dedicated `GROK_HOME` seeding, session preassign + verify, doctrine composition, flag mapping + refusals, boot arming prompt. `--agent grok` refuses with a clear "family not activated" error unless the explicit activation config/env is set — the smoke runs gated. | `launchcmd`, `spawncmd` grok branches; `grokbridge` consumed, not modified. | T19–T22 + the **isolated live smoke** (owner spend, §10.3) run under the activation flag. |
| U3 | **Lifecycle & identity**: resume/fork/cull paths, subagent guards, registry capability flags, wake-degraded reporting. | `lifecyclecmd`, registry schema additions. | T14–T16 + resume/fork live re-check ridealong on the U2 smoke pattern. |
| U4 | **Observer & transcript**: `chat_history.jsonl` adapter, `events.jsonl` labeled enrichment, honest-unknown reconciliation. | `observercmd` + transcript plumbing. | Adapter tests against recorded session fixtures; no synthesized status (assert `unknown` preserved). |
| U5 | **Shim/setup/doctor/docs**: `grok` PATH shim, ai-setup/ai-doctor coverage, user docs. | shims + setup/doctor scripts + docs. | Ships only after U2's live smoke is green; recursion/shadowing checks (memo "Minimal first-class diff" #6). |

U1 → U2 strictly ordered; U3 and U4 can proceed in parallel after U2; U5 last. Each
unit is independently mergeable behind its fence, and cross-family adversarial review
plus the full repository gate battery apply to every behavior diff (per house rules;
see task acceptance criteria in the backlog).

**Activation is its own gate.** U2's green smoke does **not** declare the family
working — it proves transport + launch under the activation flag. `--agent grok`
becomes available by default only in a small **activation change after U3 and U4
merge** (it may ride U5): removing the gate requires resume/fork/cull, subagent
guards, wake-degraded reporting (U3) and the honest transcript/status adapter (U4) to
be present, so no user ever reaches a spawnable family whose lifecycle or observation
contracts don't exist yet. Until that change, the family is explicitly experimental
and opt-in.

## 12. Design-time verification

Empirical checks run while writing this design (2026-07-12), against installed hcom
0.7.23 and the two Grok binaries on the box. Isolation: hcom probes used a throwaway
scratch `HCOM_DIR` bus with two throwaway adhoc identities (nothing touched the live
bus); Grok probes were `--version`/`--help` only — no session, no inference, scratch
`HOME`/`GROK_HOME`, no writes to `~/.grok`. Reproduction: create a scratch
`HCOM_DIR`; register two identities with `hcom start`; send one direct message and
one broadcast between them; interleave `hcom list` (unread counts), `hcom events`
queries, and one `hcom listen`.

V1–V4 were first probed while drafting, then **corrected by an independent
adversarial reproduction** (isolated buses, `env -i` identity scrubbing, real 0.7.23
binary). The entries below state the corrected findings; where the original probe
overclaimed, the correction is spelled out.

- **V1 — `hcom events` is non-destructive for ANONYMOUS reads only.** The original
  probe (a `--name`d query; unread `+1` unchanged) was condition-dependent luck. The
  reproduction showed that a read under a registered identity triggers hcom's
  post-dispatch pending delivery: formatted unread messages are appended to stdout
  and the identity's internal cursor advances (unread observed going 2 → 0 from a
  named query). Non-destructiveness — and clean machine output — hold only for
  identity-free invocations, which DR-1 therefore mandates and T25 enforces.
- **V2 — the full envelope requires `--full`; there is no `--json` flag.** `hcom
  events --full` rows include the event `id`, `type`, `ts`, and `data` with `from`,
  `text`, `intent`, `mentions[]`, `thread`, `scope`, `delivered_to[]`, and both
  `reply_to` (string) and `reply_to_local` (numeric). Plain `hcom events` output is
  streamlined and strips `scope`, `delivered_to`, and reply fields (and, for raw-SQL
  queries, `mentions`). The original entry's "`--json` rows" phrasing was wrong.
  DR-1 mandates `--full` on every invocation and journals the raw envelope.
- **V3 — committed-cursor queries work; the routing predicate must be
  `msg_delivered_to`.** `--sql "id > <C> AND EXISTS (SELECT 1 FROM
  json_each(msg_delivered_to) WHERE value='<seat>')"` returns exactly what hcom
  routed to the seat (direct, thread, tag, broadcast), in id order, excluding the
  sender. The original mention-or-broadcast predicate was reproduced returning the
  seat its **own** broadcast (`scope:"broadcast"` events match `msg_scope` regardless
  of recipient), and thread fanout keeps the sender in `mentions` — both would feed a
  seat its own outbound as inbound. T26 pins the corrected predicate.
- **V4 — `--wait` blocks and returns FUTURE matches only; it cannot serve backlog.**
  Reproduction: a message sent ~1s after an anonymous `--wait` started was returned
  in ~1.5s; a message aged 11 seconds timed out (`--wait 2`, exit 1) despite matching
  the SQL. Cause: the wait path looks back only 10 seconds, then initializes a
  private cursor at the database's current global max — not at the caller's `id > C`
  bound — so older matching rows are unreachable, and repeating the wait does not
  help. The original entry ("returned immediately when a match already existed")
  had not tested blocking on aged rows. Hence DR-1: drain is the source of record;
  `--wait` is an edge trigger between empty drains; T24 pins the stale-backlog case.
- **V5 — `hcom listen` is destructive and lossy, confirming its disqualification.**
  `listen --json` returned `{from,text}` only — no id, intent, or thread — and
  consumed the unread state (the `+1` cleared), i.e. the instance cursor advanced
  outside caller control.
- **V6 — adhoc `hcom start` registers, with an environment-guessed label.** On a bare
  scratch bus, `hcom start` created a named roster row immediately; its tool label
  reflected the detected calling environment, not the tool the identity represents —
  the basis of the roster-label upstream ask (§10.4a).
- **V7 — the PATH binary is uncharacterized.** `command -v grok` resolves to a 0.2.99
  binary (`b1b49ccb71`); the characterized 0.2.93 (`f00f96316d`) remains at its
  pinned downloads path. The DR-3 version gate is live on day one.
- **V8 — the subagent boundary flag exists in the characterized version.** Grok
  0.2.93 `--help` lists `--no-subagents` ("Disable subagent spawning") and
  `--agents <JSON>` ("Inline subagent definitions as JSON") — the former is the DR-4
  enforcement vehicle; the latter is on the DR-3 refusal list.
- **V9 — snapshot `events` pages newest-first, capped at 20 by default; the paged
  subquery fixes membership, not emission order.** From independent reproduction
  against installed 0.7.23 (source-confirmed: `ORDER BY id DESC LIMIT last_n`,
  `last_n` defaulting to 20): with 25 direct messages queued to one seat, a naive
  `id > C` drain returned only the newest 20; deriving C from that page and
  repeating returned zero rows, permanently skipping the oldest five. The corrected
  oldest-first paged shape (`id IN (SELECT id … ORDER BY id ASC LIMIT 20)`, DR-1
  step 1) returned 20 then 5 — all 25 exactly once. A larger `--last N` only moves
  the loss threshold. A follow-up reproduction with equal timestamps forced on the
  page showed the outer query still **emits** the selected set id-descending
  (`[42,40,…,4]`); a crash after fsyncing only the first emitted row derived C=42
  and stranded ids 4..40. **Binder-side ascending-id sorting before any journal
  append is therefore part of the correctness contract**, not a nicety. T27 pins
  both facts.

---

## Addendum — owner rulings (2026-07-13)

1. **bypassPermissions**: no mapping ruled in; ships as designed (normal →
   `--always-approve`, `--safe` → ask-mode). Standing default unless the owner
   explicitly rules otherwise.
2. **Default model**: RULED — the production default model is **Grok 4.5**. The launch
   contract resolves the exact model identifier from the CLI's model list at
   implementation time and pins it as the family default; `--model` passthrough still
   overrides per-spawn. The live smoke runs the pinned model.
3. **Inference spend**: blanket-approved for all implementation-unit smokes in this
   design's staging plan; spend is not a per-unit consideration.
4. **Upstream hcom asks**: HOLD — do not file. Watch item instead: upstream PR
   aannoo/hcom#81 (open) adds a first-class `hcom grok` launcher with hooks, resume/
   fork, and transcript parsing — but its delivery strategy is plain-text PTY body
   injection + Enter, the mechanism this design's provenance explicitly rejected for
   delivery. If that PR merges, the activation unit must ensure hcom's injection path
   and this design's binder NEVER both deliver to one seat (double-delivery race);
   the roster-label nicety (a) may arrive for free with it.
5. **Boot-arming fallback**: PRE-APPROVED — if the argv boot-prompt probe fails, one
   constant arming line via composer boot-paste at launch is authorized (launch
   arming only; never message content).

<!-- Provenance: design record, 2026-07-12. Design only; implementation is staged separately (§Staging). -->
# Grok as a first-class herder/hcom family — design

Status: proposed design, pending adversarial review
Subject: xAI Grok CLI 0.2.93 (`f00f96316d`) against herder + hcom 0.7.23

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
- **Binder** — a per-seat herder-owned daemon. The only spool writer. Binds the seat to
  hcom (`hcom start` / `hcom listen` / `hcom send`), owns the receipt state machine,
  serves a per-seat unix socket to the tap and the MCP server.
- **Tap** — `herder grok tap --seat <guid>`: the command Grok itself runs as a
  persistent monitor. A dumb pipe: connects to the binder socket and prints exactly the
  lines the binder hands it. Prints nothing else, ever.
- **Bus MCP server** — `herder grok mcp`: a Grok-spawned stdio MCP server exposing
  `fetch_message`, `ack_message`, `list_pending`, `send_message`. A thin adapter that
  forwards every operation to the binder over the same socket.

```text
hcom bus
   │  hcom listen --json (binder pickup)
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
its public generic verbs — `hcom start` (identity), `hcom listen --json` (inbound
pickup), `hcom send --name` (outbound) — executed by the binder, a herder process. No
`hcom grok` launcher is required or waited for. Grok's accidental Claude-hook
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

**hcom-side "delivered" honesty.** The binder picking a message off `hcom listen` must
not let anything report end-to-end delivery. Whether hcom marks a listened-to message
as read/delivered in its own bookkeeping is unverified (probe P2, §9). Until proven
otherwise: herder's spool receipt is the **only** authoritative delivery signal for
Grok seats; every herder surface (send verification, registry, observer) reports from
the spool; and family documentation states that hcom-native read/delivery markers for
Grok rows are pickup markers, not delivery. If hcom's semantics cannot be kept honest
by configuration, an upstream ask is filed (owner decision, §10).

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

- **queued** — the binder has appended a full journal record (message id, sender,
  intent, thread, payload, payload hash, timestamps) and fsynced it. This happens
  **before any wake emission and before any inference is possible** — pending ids
  persist before inference, always.
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
| **Binder crash/restart** (any point) | Supervisor restarts it; it replays the journal, reconciles pickup cursor against hcom, reclaims its bus identity (`hcom start --as <name>`), reopens the socket. Unacked ids are re-surfaced via `HCOM_RECOVER` on tap reconnect — not re-woken individually. Crash *between hcom pickup and journal append* is closed by ordering: the binder journals before advancing its hcom read cursor, so an unjournaled message is picked up again (at-least-once into the spool; ids dedupe). |
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
2. Start the binder. It acquires the bus identity (`hcom start`, tag-derived name),
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
| always | `--no-auto-update`, `--session-id <uuid>`, `--rules <doctrine>` |
| resume | `--resume <sid>` (+ doctrine + arming prompt) |
| fork | `--resume <sid> --fork-session` (+ fresh doctrine + arming prompt) |

Passthrough args (`--extra-arg`) that collide with the contract are **refused with an
error, never silently reconciled**: `--session-id`, `--resume`, `--fork-session`,
`--rules`, `--permission-mode`, `--always-approve`, `--model` (when the first-class
flag was also given), any auto-update flag, and any arg that would re-point
`GROK_HOME`/`HOME`. `bypassPermissions` is not mapped anywhere pending the owner
ruling (§10).

**Version pinning.** Launch always passes `--no-auto-update`; the seeded config
carries `auto_update = false`; the launched binary's reported version is recorded in
the registry at boot. Tests key on version/capability, not screen text
(characterization risk #8). An optional config knob may pin an explicit binary path;
default is PATH resolution.

## DR-4 — Identity and lifecycle

**DECISION.** Seat identity binds exclusively on **preassigned session id + process
and pane evidence** (Grok pid in the seat's pane, session directory
`GROK_HOME/sessions/<urlenc-cwd>/<sid>/` appearing at boot). Directory/cwd-keyed
identity is prohibited in every code path — probes saw a later same-cwd session
silently claim an existing identity (characterization "Bus-join hypothesis…identity
hazards").

**Parent/subagent separation.** A Grok subagent is its own session (own uuid-v7) whose
`SessionEnd` stopped the parent's bus instance in probes (characterization "Event
census", subagent hazard). Three independent guards:

1. The hazard's vehicle — Claude-compat hooks — is disabled outright (DR-3), so no
   subagent lifecycle event can reach hcom at all.
2. Registry lifecycle transitions for the seat require **process-level evidence**
   (Grok pid exit, pane death, binder socket state) — never session events.
3. Doctrine forbids subagents from operating the bus MCP tools; the MCP server journals
   whatever session evidence accompanies each call, so a violation is visible in the
   journal rather than silently impersonating the seat.

**Resume** re-enters the same seat: same GUID, same session id, same spool, same bus
name (binder persists across the Grok restart, or reclaims with `hcom start --as`).
Doctrine and monitor are re-armed by the relaunch flags + arming prompt (DR-3);
`list_pending` on turn 1 closes any gap that opened while down.

**Fork** creates a new seat: new session id — which `--fork-session` assigns rather
than accepting preassignment, so the launch captures it post-boot from the session
directory / session listing and binds it with process/pane evidence before the seat is
declared bound. New spool, new bus name, registry lineage (forked-from GUID + parent
sid). Parent's unacked messages never migrate.

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
  `HCOM_RECOVER`; re-list; delivered. Also: crash before journal append → hcom
  re-pickup (cursor not advanced); no loss, no dup delivery.
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
- **T16 subagent lifecycle** — synthetic subagent session events never transition the
  seat; MCP call with subagent session evidence is journaled as such.
- **T17 silence gate** — idle binder+tap emit zero bytes on the model-facing channel
  over an extended window; diagnostics appear only in files.
- **T18 reporting gate** — `delivered` is claimable only from `acked`; wake emission,
  fetch, hcom pickup each provably insufficient (assert on the reporting API, not
  logs).

Launch/lifecycle contract tests:

- **T19 passthrough refusals** — every colliding `--extra-arg` from the DR-3 list is
  refused with a targeted error.
- **T20 update suppression + version record** — `--no-auto-update` always present;
  seeded config carries `auto_update = false`; version captured at boot.
- **T21 child environment** — launched Grok's `/proc` env shows the pinned
  `GROK_HOME` et al. despite the login-shell reset (integration test in a real pane);
  no secret name's *value* appears in argv, generated files, registry, or logs.
- **T22 identity binding** — a second Grok session in the same cwd cannot claim the
  seat (no cwd-keyed path exists to exercise).

**Live smoke (one, isolated, gated):** real Grok 0.2.93, throwaway `HOME`/`GROK_HOME`/
`HCOM_DIR`/herder state, owner-authorized spend. Proves bidirectionally: an inbound
message reaches `delivered` through the full correlated chain, and an outbound
`send_message` lands on the isolated bus with hcom's receipt. This is the acceptance
gate for the launch unit (§9) — the first moment `--agent grok` may be called working.

## 9. Implementation probes (facts to nail before/while building; not owner-level)

- **P1** `hcom start` from a non-AI daemon process: name assignment, tag control,
  `--as` reclaim behavior, what tool label the roster records.
- **P2** `hcom listen --json` pickup semantics: message id fields, cursor behavior,
  and whether pickup flips any hcom-side read/delivered marker (feeds DR-1 honesty;
  escalates to §10 only if dishonest and unconfigurable).
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
4. **Upstream hcom asks (conditional)** — file or hold: (a) a `grok` tool label for
   generically-started agents so the hcom roster can say what herder's registry says;
   (b) deferred/suppressible delivery markers if probe P2 shows `listen` pickup
   overstates delivery and no configuration corrects it.
5. **Boot-arming fallback (conditional)** — only if probe P3 falsifies the argv boot
   prompt: approve one constant arming line via the existing composer boot-paste at
   launch (never message content), or require waiting for an alternative. Flagged
   because PTY paste was rejected for *delivery*; this would be launch arming.

## 11. Staging (mergeable units, territory fences, gates)

Transport ships first; the shim ships last — a hand-typed `grok` shim routing into a
nonfunctional family is forbidden until the live smoke is green.

| # | Unit | Territory (fence) | Gate |
|---|---|---|---|
| U1 | **Transport core**: spool journal + state machine, binder (incl. supervision + hcom generic binding), tap, bus MCP server, `herder grok <tap\|mcp\|bridge>` subcommands. No spawn wiring — nothing user-reachable changes. | New internal package(s) only (e.g. `tools/herder/internal/grokbridge/`) + `herder grok` command registration. | T1–T18 hermetic (mock Grok + isolated bus); probes P1/P2/P4/P5/P7 answered and recorded. |
| U2 | **Launch contract**: `launchcmd`/`spawncmd` family wiring, dedicated `GROK_HOME` seeding, session preassign + verify, doctrine composition, flag mapping + refusals, boot arming prompt. | `launchcmd`, `spawncmd` grok branches; `grokbridge` consumed, not modified. | T19–T22 + the **isolated live smoke** (owner spend, §10.3). `--agent grok` is declared working here or not at all. |
| U3 | **Lifecycle & identity**: resume/fork/cull paths, subagent guards, registry capability flags, wake-degraded reporting. | `lifecyclecmd`, registry schema additions. | T14–T16 + resume/fork live re-check ridealong on the U2 smoke pattern. |
| U4 | **Observer & transcript**: `chat_history.jsonl` adapter, `events.jsonl` labeled enrichment, honest-unknown reconciliation. | `observercmd` + transcript plumbing. | Adapter tests against recorded session fixtures; no synthesized status (assert `unknown` preserved). |
| U5 | **Shim/setup/doctor/docs**: `grok` PATH shim, ai-setup/ai-doctor coverage, user docs. | shims + setup/doctor scripts + docs. | Ships only after U2's live smoke is green; recursion/shadowing checks (memo "Minimal first-class diff" #6). |

U1 → U2 strictly ordered; U3 and U4 can proceed in parallel after U2; U5 last. Each
unit is independently mergeable behind its fence, and cross-family adversarial review
plus the full repository gate battery apply to every behavior diff (per house rules;
see task acceptance criteria in the backlog).

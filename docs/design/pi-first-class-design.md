<!-- Provenance: design record, 2026-07-14. Design only; implementation is staged separately (§Staging). -->
# Pi as a first-class herder/hcom family — design

Status: proposed design; pending adversarial design review
Subject: `@earendil-works/pi-coding-agent` 0.80.6 against herder + hcom 0.7.23

Evidence base (cited throughout by path + section):

- `docs/design/pi-demo-report-2026-07-13.md` — the settled characterization record:
  installation provenance, managed-home mapping, offline/telemetry behavior, the
  extension-lifecycle probes, session model, provider routing, earned-clause table.
  Double-reviewed; this design does not re-derive or contradict it. Where it left an
  explicit evidence gap, this design designs conservatively and registers the
  assumption (§7) for the implement units to verify.
- `docs/design/grok-first-class-design.md` — the house pattern for a family design and
  the source of the proven hcom 0.7.23 pickup contract (its DR-1 drain contract and
  design-time verification V1–V9), the launch-contract shape (its DR-3), the identity
  rules and fork-preassignment erratum (its DR-4), the observability-honesty rules
  (its DR-5), and the staging/activation discipline (its §11).
- Grok family activation and hardening evidence (hcom 0.7.23; recorded in the grok
  program's backlog notes and review threads; mechanism-level facts restated inline
  where cited): the one-shot `hcom start` placeholder latch and its de-placeholder
  seam; the CLAUDE*/tool-signal hook-install hazard and its both-surface
  neutralization; ambient-PATH hcom resolution breaking under a cwd-sensitive shim;
  status-op-authoritative bind capture; cull row-stop + read-back confirmation;
  credential presence checked by name in a fresh non-interactive login-shell
  environment.

## 1. Settled ground (binding; not relitigated here)

| Constraint | Source |
|---|---|
| Pi is **fully herder-managed**: a dedicated managed home per seat under the herder state root. Pi does not consume `PI_HOME`; herder defines the concept and translates it into `HOME`, `PI_CODING_AGENT_DIR`, `PI_CODING_AGENT_SESSION_DIR`, and the four XDG roots. | owner ruling; demo "Managed home and state model" |
| Binding is a **native TypeScript Pi extension** — no external bridge process. The probe-proven inject path is `pi.sendUserMessage(...)` producing an `input` event with `source=extension` and a turn that runs to `agent_settled`. | owner ruling; demo "Binding fork", injection probe |
| Offline/update suppression: `PI_OFFLINE=1` (couples the version-check skip) plus `PI_TELEMETRY=0`. `PI_SKIP_VERSION_CHECK=1` alone is too narrow. Inference is not gated by offline mode (strace-backed for one Anthropic call; per-provider residual-network checks remain integration-test work). | demo "Startup network and update behavior" |
| Credentials: **one provider per seat**, routed by environment, referenced **by name only** — never in argv, config, registry, logs, doctrine, or reports. A cross-provider model change is a controlled relaunch with a re-filtered environment. | owner ruling; demo "Provider routing and least privilege" |
| Pinned install integrity: explicit version plus tarball and CLI-entry hashes, verified with the demo's audit commands at provision time. A per-launch binary hash gate is **not earned**. | demo "Installation provenance", clause table |
| **Every** Pi invocation receives the managed environment — `--help` creates mutable state even though `--version` does not. Installer checks run inside a scratch home. | demo "Startup network and update behavior" |
| Per-launch config rewrite is **not earned**: settings are seeded once; environment flags provide stable startup suppression. Pi has no observed config-drift surface. | demo clause table |
| The `/proc` post-spawn environment ceremony is **CONDITIONAL, not settled**: retain a one-time post-spawn environment assertion until herder's actual pane-spawn path for Pi is characterized as env-preserving direct-exec. This design carries the conditionality forward (§DR-3, §10 activation unit); it is not resolved on paper. | owner ruling; demo clause table |
| Pi sessions are versioned JSONL trees: header carries format version, session UUID, timestamp, cwd, optional parent-session reference; `--fork` creates a parent-linked file; `--session-dir`/`PI_CODING_AGENT_SESSION_DIR` force the root. | demo "Session compatibility" |
| hcom 0.7.23 pickup contract (proven for grok, adopted verbatim): anonymous `hcom events --full` oldest-first paged drain above a journal-derived cursor with mandatory binder-side ascending-id sort before append; `--wait` demoted to an edge trigger; identity-free reads with a scrubbed environment; `msg_delivered_to` as the routing predicate; `hcom listen` rejected. | grok design DR-1 + V1–V5, V9 |

## 2. Architecture overview

Grok needed four cooperating parts (binder daemon, tap, MCP server, spool) because
Grok cannot host managed code. Pi can: it loads TypeScript extensions in-process and
exposes lifecycle and injection primitives (demo "Binding fork"). The Pi topology is
therefore two parts plus the durable store, and **no long-lived herder process exists
outside Pi itself**:

- **Spool** — the seat's durable message journal (append-only JSONL under
  `<HERDER_STATE_DIR>/pi/<seat-guid>/journal.jsonl`), same house pattern as grok's.
  Single source of truth for delivery state; survives every process here.
- **Extension** — the herder-managed Pi extension installed in the seat's
  `agent/extensions/`. The binder-owner: claims the seat on `session_start`, drives
  the inbound loop, injects via `pi.sendUserMessage`, translates Pi lifecycle events
  into seat status, releases idempotently on `session_shutdown`. Lives and dies with
  the Pi process; herder supervises Pi, not the extension.
- **Bus ops** — `herder pi bus <claim|drain|wait|pending|send|status|retire>`:
  short-lived, bounded CLI invocations the extension (and herder itself) makes. All
  hcom mechanics — identity, de-placeholder, drain paging/sorting, journal
  append+fsync, cursor derivation, outbound send — live in this one Go
  implementation, sharing the contract code and pinned tests already proven for grok
  (`tools/herder/internal/grokbridge/`). No daemon, no socket: every process in the
  bus path is a bounded child invocation.

```text
hcom bus (events store: id-addressed, non-destructive to anonymous reads)
   │  anonymous `hcom events --full` DRAIN via `herder pi bus drain`
   │  (bounded `bus wait` child between empty drains — latency only)
   ▼
spool (append-only journal; fsync before any injection)
   ▲                                        │
   │ append (bus ops, under per-op flock)   │ read pending
   │                                        ▼
herder pi bus ops ◄── spawn/collect ── extension (inside the Pi process)
                                          │  pi.sendUserMessage(...)   [probe-proven]
                                          │  lifecycle events agent_start/…/agent_settled
                                          ▼
                                        Pi turn → provider inference
outbound: model runs `herder pi send` (doctrine-mandated wrapper)
          → journaled + trimmed `hcom send --name <busname>`
```

Doctrine and the initial task prompt ride the spool: launch enqueues them before Pi
boots, and the extension injects them through the same receipt machinery as every
later message — so even the boot prompt gets a real delivery record, and argv carries
no prompt content.

---

## DR-1 — Binding ownership: the extension is the binder; bus mechanics live in one herder-owned implementation

**DECISION.** The Pi family is owned end-to-end by herder: install, launch,
registration, delivery, receipts, lifecycle, observation. hcom is consumed exclusively
through its public generic verbs (`hcom start`, anonymous `hcom events --full`,
`hcom send --name`), and **only** from inside `herder pi bus` operations — never
directly from TypeScript, never from ambient PATH.

The settled binding decision (native extension, no external bridge process) fixes
*who owns the seat*: the extension, in-process. This DR fixes *where the bus
mechanics run*, which the demo left open ("extension execution API or a carefully
scrubbed child process" — demo "Binding fork", evidence table). The fork:

1. **Pure-TypeScript bus mechanics inside the extension.** The extension itself runs
   the drain SQL, the paging/sorting contract, journal fsync, cursor derivation, and
   identity scrubbing. Rejected: this duplicates, in a second language, exactly the
   contract that took four adversarial review rounds to get right for grok
   (oldest-first paged membership, mandatory ascending-id sort before append, wait
   lookback limits, identity-free reads — grok design V1–V5/V9), and it would need a
   second full contract-test battery. Node's stdlib also lacks `flock`, weakening the
   single-writer story.
2. **A long-lived transport child** spawned by the extension (stdio-streamed drain
   loop holding the seat lock). Rejected for now: it reintroduces a persistent
   process outside Pi's turn machinery — adjacent to the external-binder shape the
   demo rejected — for a latency benefit the bounded `bus wait` invocation already
   provides. Revisit only if per-op invocation overhead is measured to matter.
3. **Short-lived `herder pi bus` invocations, extension-driven loop.** ADOPTED. The
   extension owns timing and lifecycle gating; each invocation performs one atomic,
   flock-serialized operation against the spool and/or hcom and exits. The proven Go
   contract code is reused; the pinned real-hcom tests carry over; the TypeScript
   surface stays a thin adapter (spawn op, parse NDJSON, inject, report).

This does not reopen the settled binding fork: injection, lifecycle observation,
seat claiming, and restart behavior all live in the extension; no process outside
Pi's tree persists between operations.

**The pickup contract is inherited, not re-derived.** Inbound pickup is the grok
DR-1 contract verbatim: anonymous `hcom events --full`, oldest-first paged
`id IN (SELECT … WHERE id > C … ORDER BY id ASC LIMIT 20)` membership subquery,
mandatory ascending-id sort before journal append, cursor derived as the max id of
the fully-journaled page, `--wait` only as an edge trigger between empty drains,
`msg_delivered_to` as the routing predicate, `hcom listen` rejected. Those behaviors
are pinned against installed hcom 0.7.23 by the existing grok contract tests; the Pi
unit reuses that code and re-points the same pins (§8, T15). The scrub list and the
contract are version-pinned and revisited on any hcom upgrade, exactly as for grok.

**Identity invocation hardening is designed in from day one** (grok learned these
post-activation; Pi ships with them):

- **Allowlist-built environment for every hcom invocation.** hcom 0.7.23 keys a
  claude-hook-install-and-exit-1 path off `CLAUDE*`/`CLAUDECODE` tool signals
  (suppressed only by launched/adhoc signals), and its identity routing reads
  ambient `HCOM_PROCESS_ID`/`CODEX_THREAD_ID`. The grok binder originally
  scrub-listed `os.Environ()` and was caught inheriting the launching pane's
  signals; the recorded hardening direction is allowlists on security boundaries.
  `herder pi bus` therefore constructs the hcom child environment from an explicit
  allowlist (`HOME`, `PATH` floor, `HCOM_DIR`, and nothing tool-signal-shaped),
  regardless of what its own process inherited. Pi's managed process environment is
  itself allowlist-built (DR-3), so no `CLAUDE*` signal should exist to leak — the
  allowlist makes that true even for hostile or foreign launch contexts (T13).
- **Pinned absolute hcom binary.** Live grok evidence: resolving `hcom` through
  ambient PATH hit a cwd-sensitive version-manager shim and failed `hcom start`
  from inside worktrees. Seat provisioning resolves and records the absolute real
  hcom binary once; every bus op invokes that recorded path, never PATH (T14).
- **Placeholder de-latch.** Grok activation evidence on hcom 0.7.23: a one-shot
  generic `hcom start` leaves the roster row as a `new` placeholder that hcom
  finalizes `launch_failed` at ~30 s, after which sends exclude the row. The proven
  seam: one **identified read-only** follow-up operation de-placeholders the row
  (pinned there as: no second identity, no unread-state advance, single roster row,
  send-accepted). `herder pi bus claim` performs identity acquisition as
  `hcom start` (or `--as` reclaim) **plus** that de-placeholder read, as one
  operation, and only then reports the seat bound (T12).

**Outbound.** The model sends through `herder pi send` (doctrine-mandated), which:
journals the outbound intent, executes `hcom send --name <busname>` with the pinned
binary and allowlisted env, **scrubs the seat's provider credential from the child
environment** (demo extension rule 8 — hcom does not need it), returns hcom's actual
result, and **trims stdout to the send receipt line**. The trim matters: any named
hcom command may trigger post-dispatch pending delivery, appending other messages'
bodies to stdout — for Pi that stdout lands in the model's tool result, creating a
second, uncontrolled delivery path for content the extension will also inject
(duplicate context) — the same context-hygiene hazard grok closed with first-line
trimming. Pickup correctness is unaffected either way (it rides the anonymous drain
and journal cursor, never hcom's per-identity cursor), so an incidental drain by a
raw `hcom send` a model runs anyway is harmless to correctness, merely unhygienic;
doctrine directs all sends through the wrapper (T16, T25).

**hcom-side "delivered" honesty.** As for grok: the seat's spool is the only
authoritative delivery record; hcom's unread counters for Pi rows are documented as
non-authoritative (anonymous reads never consume them; wrapper sends may
incidentally clear them). The roster tool label for a generically-started identity
reflects the detected calling environment, not `pi` (grok design V6); the registry's
`tool: pi` row is authoritative, and the upstream label nicety stays on the same
HOLD the owner already ruled for grok.

**Alternatives considered** (beyond the three-way fork above): an upstream `hcom pi`
launcher — hcom 0.7.23 has no pi row and waiting on upstream blocks the family;
RPC-mode external controller as binder — explicitly rejected by the demo (weaker
session-transition access, second crash protocol; demo "Binding fork: Decision");
transcript scraping — rejected by the demo (the extension API removes the need).

## DR-2 — Inbound delivery state machine and recovery

Grok's receipt machine needed model-visible fetch/ack because delivery ran through a
wake line and an MCP fetch the bridge could not correlate with injection. Pi's
extension **is** the injector and observes the turn lifecycle in-process, so the
machine is shorter and needs no model-side protocol — but the same honesty rules
apply: nothing is reported that the evidence does not show.

### States

Per inbound message id, strictly monotonic (duplicates recorded, never regress):

```text
queued ──► injected ──► delivered            (terminal)
   │            │
   └────────────┴──────► undeliverable       (terminal: seat retired first)
```

- **queued** — a `herder pi bus drain` invocation appended the full journal record
  (hcom event id, sender, intent, thread, payload, payload hash, timestamps) and
  fsynced it, in ascending-id order per the inherited contract. Happens before any
  injection is possible. The committed cursor is derived from the journal by replay,
  never stored separately.
- **injected** — the extension called `pi.sendUserMessage(...)` with the message
  envelope and observed Pi's `input` event with `source=extension` for it
  (probe-proven pair — demo injection probe). The extension records the injection
  through `herder pi bus` (journaled, fsynced). Once injected, the content is in the
  session's context and — per the session model — persisted in the session JSONL
  (assumption A3, §7: the probe showed the input event; its persistence as a session
  entry is API-inventory, verified in U1).
- **delivered** — a subsequent `agent_settled` was observed in the same session
  after the injection was journaled. `agent_settled` is probe-proven as the
  turn-completion signal for an injected message (demo: the injected turn ran to
  `agent_settled`). Terminal.

### The delivery definition (the only one)

> **delivered(id)** ⇔ the seat's journal shows `queued → injected → delivered` for
> that id, where *injected* required the observed `source=extension` input event and
> *delivered* required a later `agent_settled` in the same session.

What this claims, exactly: the message content entered the session as a user-visible
turn input and the agent subsequently completed at least one full turn over a context
containing it. What it does **not** claim: anything about the reply content — the
demo did not capture the reply of the injected turn, and this design does not
manufacture that evidence. Herder reports `delivered` with precisely the above
meaning; nothing weaker (journal append, sendUserMessage call without the input
event, injection without a settle) is ever reported as delivered (T26).

### Injection policy

- **Idle-gated.** The demo proved injection into an **idle** session; streaming
  delivery options for `sendUserMessage` are API-inventory only (demo evidence
  table). Until the steering probe passes (assumption A2), the extension injects
  only when no turn is active — it observes `agent_start`/`agent_end`/
  `agent_settled` in-process, which is strictly stronger evidence than grok's
  on-disk phase inference. Messages arriving mid-turn queue durably and inject at
  the settle boundary (T3).
- **Batched.** All pending ids inject as one composed user message, one envelope
  block per message (id, sender, intent, thread, then body — formatted to match
  hcom's native delivery style). One turn per batch keeps context and turn count
  proportionate to load; each id gets its own journal transitions (T4).
- **No blind re-injection.** An id that reached *injected* is never re-injected on
  the strength of a missing settle alone: its content is already in the session
  context and persists across resume via the session JSONL. Recovery uses a nudge
  turn instead (below).

### Failure and recovery matrix

| Scenario | Behavior |
|---|---|
| **Pi process exit or crash** (any point) | The extension dies with Pi — that is the design, not a failure of it (demo "Restart, crash, and message recovery"). Herder records the exit from process/pane evidence and relaunches via resume (DR-4). On the new `session_start` the extension reclaims the seat, replays the journal, and drains. Walked windows: *(a) crash after drain query, before journal fsync* — cursor never advanced; the non-destructive events store returns the rows again; deduped by event id. *(b) crash after queue, before inject* — replay finds queued-not-injected ids and injects them (exactly the demo's pending-replay clause). *(c) crash after inject, before settle* — replay finds injected-unsettled ids; the extension injects one **nudge** message ("possibly unprocessed messages: <ids> — they are in your context; review and continue"), whose settle delivers them. At-least-once into context, with duplicate-safe framing, per the demo's stated preference for at-least-once over loss. |
| **Turn aborted after injection** (user interrupt, provider error) | Id stays *injected*. Any later settle in the session delivers it; if the seat idles with injected-unsettled ids past a threshold, the extension issues the same nudge turn. No hcom-level resend ever fires. |
| **Extension handler throws** | Probe-proven containment: Pi emits `extension_error` and keeps serving (demo extension-lifecycle probe). The failing extension reports the error to seat diagnostics (log file) and the seat status degrades honestly (DR-6); Pi is not killed for it. |
| **Duplicate drain rows / replayed events** | Journal is id-keyed; monotonic states; duplicates journaled as repeat markers, never re-injected (T5). |
| **Second Pi process on the same seat** (operator error, restart race) | `herder pi bus claim` refuses when a live claimant exists: the claim file records pid + process-start evidence; a claim is stale only when that process is provably gone. Per-op flock serializes journal writers regardless, so even a pathological overlap cannot interleave appends (T10). |
| **Session switched/replaced inside Pi** (new/switch/fork from within the TUI) | The extension treats every `session_start` as a rebinding event (demo extension rule 6): re-claim, re-verify session identity against seat state, replay pending. A session the seat does not recognize flags the seat for reconciliation rather than guessing (DR-4). The shutdown→reload→start replacement sequence is API-inventory, not probed (assumption A4). |
| **Seat cull/retirement with undelivered ids** | Ids move to `undeliverable` (terminal, journaled); registry records the count; send verification reports those messages honestly as undelivered. Cull follows the proven row-stop + read-back-confirm pattern from grok activation: stop the bus row, confirm its absence by read-back, then tear down the pane and seat processes (T24). |
| **Wake latency when idle** | Between empty drains the extension runs one bounded `herder pi bus wait` child (anonymous `--wait` edge trigger; correctness never rides it — inherited grok V4). On wake or timeout it returns to the drain. No daemon exists to die; the failure mode of a stuck `wait` child is a timeout and a fresh drain. |

### Reporting vocabulary

| Report | Meaning | Trigger |
|---|---|---|
| `queued` | Durably journaled for the seat | journal append + fsync |
| `delivered` | The definition above — nothing weaker | settle observed after journaled injection |
| `undeliverable` | Terminal non-delivery | seat retired with id short of delivered |
| `inject-degraded` | Extension cannot currently inject (extension error, no session) | extension diagnostics / claim state |

### Persistence format

Append-only JSONL journal per seat (`<HERDER_STATE_DIR>/pi/<seat-guid>/journal.jsonl`),
fsync on the records that gate external claims (`queued`, `injected`, `delivered`),
state derived by replay, periodic snapshot records to bound replay cost — the house
pattern, shared with the grok spool implementation. Writers are `herder pi bus`
invocations only, serialized by per-op flock on the seat lock file; the extension
never writes the journal from TypeScript.

## DR-3 — Launch contract

**DECISION.** `herder spawn --agent pi` becomes a first-class family with a
herder-owned launch path in `launchcmd` (joining `claude|codex|gemini|grok`), execing
the pinned Pi CLI directly with a fully explicit, allowlist-built child environment
and argv. Nothing routes through an `hcom <tool>` launcher (none exists for pi).

### Provisioning (once per pinned version, not per launch)

1. **Pinned install.** `herder pi install` (provisioning surface) installs
   `@earendil-works/pi-coding-agent` at the pinned version into an immutable shared
   prefix `<HERDER_STATE_DIR>/pi/install/<version>/`, using the demo's isolated
   `env -i` npm procedure (scratch HOME, isolated npm cache — demo "Reproducible
   scratch layout and audit commands"), then verifies: package version, tarball
   SHA-256 `2a77634640b2d86d90d24087bb67559ecf2366e0fb52a42c55eed416147da411`,
   registry integrity, and CLI-entry SHA-256
   `af302f231437eaf6f37691bce4b34234fcb626bcb5eb3910d4fc3f6519bf78ca` for the initial
   pin 0.80.6. Mismatch refuses provisioning with the observed and expected values.
   No per-launch hash gate (not earned — demo clause table). Version verification
   reads the installed `package.json` rather than executing `pi --version`, so the
   gate itself creates no state; any check that must execute Pi runs inside a
   scratch home (demo: even `--version` is run in scratch as an artifact check).
2. **Node runtime pin.** The install records the absolute Node binary used
   (observed floor `>=22.19.0`; demo provenance table). Launch uses the recorded
   runtime, not ambient PATH — the same determinism rule as the pinned hcom binary
   (DR-1).
3. **Version gate at launch.** Family config carries the supported-version set
   (initially `{0.80.6}`). Launch resolves the seat's install, reads its recorded
   version, and refuses anything outside the set with an error naming the install
   path, version, and supported set. A new Pi version enters the set only via a
   characterization pass (extension API compatibility is exactly the surface that
   can drift), never by assumption. `PI_OFFLINE=1` on every launch means the gated
   install cannot drift under a running family.

### Seat construction (per seat, at spawn)

`PI_HOME` is a herder concept translated per the demo's proven mapping:

```text
PI_HOME = <HERDER_STATE_DIR>/pi/<seat-guid>
HOME=$PI_HOME/home
PI_CODING_AGENT_DIR=$PI_HOME/agent
PI_CODING_AGENT_SESSION_DIR=$PI_HOME/sessions
XDG_CONFIG_HOME=$PI_HOME/xdg/config   (+ cache/data/state)
```

Seeded at seat provisioning, before first launch:

- `agent/settings.json` — owner-controlled seed, telemetry off; **seed once, never
  rewritten at launch** (per-launch rewrite not earned — demo clause table).
- `agent/extensions/<herder-hcom-extension>` — the managed extension, copied from
  the version-matched artifact herder ships (Pi loads TypeScript directly — demo
  "Binding fork"). The extension version is recorded in seat state; a
  version-mismatched extension refuses to claim rather than half-binding.
- `agent/models.json` — only if the seat's provider/model requires a custom entry;
  never contains secrets (demo state table).
- Seat bus state: recorded absolute hcom path, bus name, journal, claim file.

Project `.pi/` resources stay untouched in the workspace, subject to Pi's own trust
lifecycle (demo: they are project content, not seat state).

### Launch sequence (ordering: the bus name must exist before doctrine can name it)

1. Mint seat GUID; resolve session identity per DR-4. Record both in the registry
   before launch.
2. Acquire the bus identity via `herder pi bus claim` (identity + de-placeholder as
   one op — DR-1); persist the bus name in seat state. Restarts reclaim with `--as`.
3. Compose the doctrine message (bus name, addressing rules, `herder pi send`
   mandate, credential rule: never print or persist key material, duplicate-replay
   framing, silence expectation) and enqueue it as spool message #1; enqueue the
   task prompt (`--prompt`) as spool message #2. Argv carries no prompt content —
   large/multiline prompts avoid argv entirely, and both messages get real delivery
   receipts through DR-2, mirroring grok's spool-borne initial prompt.
4. Exec Pi (recorded Node runtime + pinned `dist/cli.js`) inside the pane with the
   allowlist-built environment and explicit argv.
5. `session_start` fires in the extension: claim verify, journal replay, drain,
   batch-inject doctrine + task. Spawn's bind capture is **status-op-authoritative**:
   spawn confirms the seat via a `herder pi bus status` read-back of seat state (the
   grok activation lesson — never infer bind from hcom row-state transitions), and a
   no-bind within the window hard-fails the spawn with confirmed cleanup, never a
   silent green.

### Child environment — allowlist, not inheritance

The launch process (running inside the pane, as `launchcmd` does for other
families) **constructs** the child environment rather than filtering its own:

- The `PI_HOME` mapping above; `PATH` floor containing the recorded Node runtime
  and herder shims; `HCOM_DIR`; herder seat/state variables (`HERDER_STATE_DIR`,
  seat GUID, bus name for the wrapper).
- `PI_OFFLINE=1`, `PI_TELEMETRY=0` — required on every invocation (settled).
- **Exactly one provider credential, by name**, per DR-5. Herder verifies presence
  by name pre-launch — in the environment the pane process actually receives, not
  the CLI caller's (grok activation lesson: interactive-shell exports do not reach
  non-interactive spawn chains; the check must be a fresh-pane-truth check) — and
  refuses launch with a cause+remedy error if absent. Values are never logged,
  never asserted beyond nonempty.
- Nothing else. No `CLAUDE*`/tool signals can exist in the seat by construction
  (DR-1 relies on this and re-enforces it with its own allowlist).

**The `/proc` ceremony, carried conditionally as ruled.** Whether the pane-spawn
path delivers this constructed environment intact to the Pi process is exactly the
uncharacterized link (demo clause table: CONDITIONAL). Until herder's actual Pi
pane-spawn path is characterized as env-preserving direct-exec, every launch
performs a **one-time post-spawn assertion**: read the live Pi process environment
(`/proc/<pid>/environ`) and verify the managed mapping (variable names and managed
paths; never credential values). Assertion failure is a launch failure with
teardown, not a warning. The activation unit (§10) owns producing the
characterization evidence; only after it shows direct-exec preservation may the
ceremony be removed, as its own reviewed change. This design does not resolve the
conditional on paper.

### Flag mapping and refusals

| herder intent | Pi argv / mechanism |
|---|---|
| always | explicit session selection per DR-4; `--session-dir` implied by env; no prompt in argv |
| `--model X` | Pi model selection for the pinned provider (exact argv per the pinned version's CLI; recorded at implementation) |
| resume | exact session selection (`--session`/`--session-id` family — demo session table) |
| fork | `--fork` with parent session (demo session table) |
| autonomy modes | **unmapped pending characterization** — the demo did not characterize Pi's interactive approval surface; probe A6 (§7) answers it; any bypass-like mapping is an owner decision (§9), per the grok precedent |

Passthrough args that collide with the contract are **refused with an error, never
silently reconciled**: anything selecting or re-pointing sessions or session
directories, `HOME`/state-root re-points, offline/telemetry/update-behavior
overrides, credential or auth-file arguments, extension-path arguments, and
`--no-session` (a first-class seat is always a durable session; DR-4 depends on it).
The refusal list is finalized against the pinned version's full CLI surface in the
launch unit and pinned by test (T20), exactly as grok's T19.

## DR-4 — Identity, sessions, lifecycle

**DECISION.** Seat identity binds on **seat GUID + process/pane evidence + session
identity**, never on cwd. Pi's session files are cwd-labeled in their headers, and
herder forces the session root per seat, so no cwd-keyed claim path may exist in any
code path (the grok DR-4 rule, inherited).

**Session identity: preassign if the pinned CLI allows it; otherwise
extension-published capture.** The demo proved exact resume (`--session`,
`--session-id`), forking (`--fork`, parent-linked), and forced session roots — but
did not probe whether a **new** session's UUID can be preassigned at launch. The
grok fork erratum is the precedent in both directions: preassignment is the
preferred identity model, and vendor flag surfaces can turn out to support it on
inspection. Resolution order, decided here:

1. Probe the pinned CLI for new-session preassignment (P1, §7). If supported,
   launch mints a UUIDv7, records it pre-launch, and verifies it post-boot — the
   grok model.
2. If not supported, the extension **publishes** the session identity: on
   `session_start` it reads the live session UUID from its extension context
   (API-inventory surface, assumption A5) and writes it to seat state through a bus
   op; spawn's status-op read-back binds it with process/pane evidence. Fallback if
   A5 also fails: sid-glob discovery under the seat's forced `sessions/` root —
   viable only because the root contains exactly this seat's sessions.

Either way the binding requires process/pane **and** session evidence before the
seat is declared bound; a same-cwd or same-directory session can never claim an
existing seat (T22).

**Resume** re-enters the same seat: same GUID, same spool, same bus name
(`--as` reclaim), exact session selection. `session_start` replay (DR-2) closes any
gap that opened while down. Herder-initiated restart after a crash is a resume.

**Fork** creates a new seat: new GUID, fresh spool, new bus name, registry lineage
(forked-from GUID + parent session UUID); Pi's `--fork` provides the parent-linked
session file (demo session table). The parent's undelivered ids never migrate.
Whether `--fork` composes with session-id preassignment follows the P1 probe; the
grok erratum pattern (preassigned fork id, collision-checked) is adopted if the
surface allows.

**In-TUI session mutation** (user or model switches/creates sessions inside a
running Pi): every `session_start` is a rebinding event (demo extension rule 6). The
extension compares the live session identity against seat state; a recognized
session (the seat's own, or its declared fork/resume target) rebinds and replays; an
unrecognized one puts the seat into an explicit `session-drift` state visible in the
registry — pending work stops injecting until reconciled — rather than silently
adopting an identity (the falsified-registration lesson generalized: presence of a
session is not seat identity).

**Cull/retire**: row-stop + read-back confirm (proven live in grok activation), then
process/pane teardown, undelivered ids to `undeliverable`, seat state retained for
audit. Registry lifecycle transitions require process-level evidence (pid exit, pane
death), never session events.

**Subagents.** Pi's extension API inventories tool/subagent-adjacent events, but the
demo recorded no subagent lifecycle hazard and no subagent kill-switch flag. Unlike
grok, Pi's delivery receipts do not depend on model-side ack authorship — delivery
is extension-observed — so a subagent cannot forge a delivered receipt. The residual
risks are context/credential shaped (a child inherits the provider key: inherent,
demo-documented) and identity-shaped (a subagent session must not rebind the seat —
covered by the session-drift rule above). Probe P4 (§7) inventories Pi's actual
subagent surface at the pinned version; if a disable flag exists, the launch unit
adds it to the always-argv as hardening, with a design note, not a soundness
requirement.

## DR-5 — Multi-provider surface and least privilege

**DECISION.** A seat declares its provider explicitly at spawn; herder filters the
environment to exactly that provider's credential; provider changes are supervised
relaunches. Nothing guesses.

**Spawn syntax.** `herder spawn --agent pi --provider <family> [--model <id>]`.

- `--provider` is **required** (no default pending the owner ruling, §9). The
  provider table is family config, initially exactly the demo-proven rows:

  | Provider family | Credential name routed | Demo evidence |
  |---|---|---|
  | `anthropic` | `ANTHROPIC_API_KEY` | success (demo provider table) |
  | `openai` | `OPENAI_API_KEY` | success |
  | `xai` | `XAI_API_KEY` | success |

  Unknown provider → refusal naming the supported set. New rows enter via
  characterization, not assumption.
- `--model` passes through to Pi. Herder does not maintain a model catalog and does
  not validate model↔provider pairing beyond what Pi itself enforces; a
  wrong-provider model fails at Pi/provider level with its own error. There is no
  model-prefix guessing map: convenience inference that silently picks a credential
  is exactly the class of reconciliation the house refuses. Default model per
  provider: owner decision (§9), grok precedent (owner pinned grok-4.5 after
  design).
- The registry row records `provider: <family>` and the requested model.

**Least-privilege filtering at exec.** The DR-3 allowlist includes exactly the one
credential name from the provider table — by name, value never inspected beyond
nonempty, never logged. Pi's tools and extension children inherit the Pi process
environment (demo: "a seat must receive only the credential required for its
selected provider"), so the one-key rule is the whole containment story inside the
seat; the two herder-controlled child paths that don't need the key (`herder pi
bus`, `herder pi send`) additionally scrub it (DR-1).

**Cross-provider change = controlled relaunch** (settled). Herder-side: a relaunch
op that retires the running process (resume semantics, same seat), rebuilds the
environment for the new provider, and relaunches into the same session. Whether the
same conversation is *coherent* across a provider change is Pi's business (its
sessions record model changes — demo session families); herder's contract is only:
never two provider credentials in one process environment, ever. Extension-side:
`model_select` is observed (API inventory); an in-process model change that crosses
provider families is flagged to the registry as a provider-drift warning. It cannot
succeed at inference (the credential is absent) — the demo's least-privilege
observation — but the flag makes the failure legible instead of mysterious.
Credential-name mapping stays per-harness (the demo's Codex `CODEX_API_KEY` lesson):
the table above maps names for **Pi**, and no name aliasing for other harnesses
leaks into a Pi seat.

## DR-6 — Observability, sesh, and honesty

**DECISION.** Every observation surface reports only what its evidence supports,
with the source labeled — grok DR-5, applied to Pi's surfaces.

- **Transcript** = the seat's session JSONL under `$PI_HOME/sessions/`, located by
  session UUID from seat state. The observer gets a Pi adapter for the JSONL tree
  format (header + parent-linked entries — demo "Session compatibility"). Entries
  are id/parent-id linked (branching), so the adapter renders the active branch and
  labels branch points rather than flattening silently.
- **sesh integration.** Pi is the friendly case sesh was shaped for: the adapter
  indexes the session header (format version, session UUID, timestamp, cwd,
  parent-session reference), uses the session UUID as the stable session
  identifier, and records fork lineage from the parent-session link — no SQLite, no
  scraping (demo: sesh "does not need SQLite knowledge or transcript scraping").
  Bus reconciliation state stays in the spool, never solely in a session file a
  user can branch or replace (demo "Session compatibility", closing rule).
- **Live status:** herdr has no Pi integration target, so herdr-reconciled
  `live_status` stays `unknown` — never synthesized. The extension publishes
  lifecycle-derived status to seat state (`idle`, `turn-active`, `tool-running`
  from the probe-proven event stream), and herder surfaces it as an explicitly
  labeled secondary source (`status(pi-ext): …`), never mapped into herdr's native
  vocabulary — the honest-unknown principle, which held under mutation in the grok
  observer unit.
- **Registry rows** say `tool: pi` with capability flags reflecting proven state:
  `bus: bound` (claim + de-placeholder completed), `pending: <n>` (queued/injected
  not yet delivered, exact counts), `inject: ready|degraded`, `provider: <family>`,
  and `session-drift` when DR-4 flags it. A row never implies capability the seat
  has not proven.
- **Diagnostics** (extension errors, bus-op failures, nudge history) go to seat log
  files under the seat dir, never to the pane or the model context (T25).

---

## 7. Assumption register (evidence gaps → verify in the implement units)

Every entry is conservative in the design above and carries a named verification.
None may silently become load-bearing beyond its stated fallback.

| # | Assumption / gap | Design posture | Verify |
|---|---|---|---|
| A1 | **Reply-content capture**: the demo validated injection to `agent_settled` but did not capture the reply. | `delivered` claims turn completion over a context containing the message — nothing about the reply (DR-2). | U1 probe: capture the injected turn's reply via the extension event/message stream; if capturable, add reply-hash journaling as an audit nicety (not a delivery precondition). |
| A2 | **Steering/mid-stream delivery**: `sendUserMessage` delivery options are API-inventory only. | Idle-gated injection; mid-turn arrivals hold to the settle boundary. | U1 probe: exercise streaming delivery options; if proven, a later unit may relax the idle gate as its own reviewed change. |
| A3 | **Injected input persists in the session JSONL** (crash/resume durability of injected content). | DR-2's no-re-inject rule leans on it; the nudge path is the safety net either way. | U1 probe: inject, then inspect the session file for the entry. |
| A4 | **Session replacement sequence** (shutdown → reload → start) is inventory, not probed. | Every `session_start` is a rebinding event; unrecognized sessions go to `session-drift`, never adopt. | U1/U3 probe: in-TUI new/switch/fork while bound. |
| A5 | **Extension can read the live session UUID** from its context. | Used only if P1 (preassignment) fails; sid-glob fallback behind it. | U1 probe alongside P1. |
| A6 | **Pi's interactive approval/autonomy surface** is uncharacterized. | Autonomy mapping left unmapped; seat runs Pi defaults until characterized (DR-3). | U2 probe: pinned-version approval surface inventory; owner ruling for any bypass-like mode (§9). |
| A7 | **TUI-mode extension parity**: probes ran in RPC mode; docs state the same extension contract loads in tui/rpc/json/print. | Design assumes parity for lifecycle + injection only (the documented contract), nothing UI-dependent. | U1's first TUI-mode extension smoke — before anything else builds on it. |
| A8 | **Extension child-process env**: bus ops assume the extension can spawn children with a controlled environment. | DR-1's allowlist is constructed by the Go side regardless; the TS side only needs to spawn at all. | U1 unit test in TUI mode. |
| P1 | **New-session UUID preassignment** at launch (and composition with `--fork`). | DR-4 resolution order: preassign if supported, else A5 publication, else sid-glob. | U2 probe against the pinned CLI (`--help`/docs inspection first; scratch-home execution if needed — managed env always). |
| P4 | **Subagent surface inventory** at the pinned version. | No soundness dependency (DR-4); disable flag adopted as hardening if present. | U2 probe. |
| P5 | **Per-provider residual network** under `PI_OFFLINE=1` (strace-proven for one Anthropic call only). | Offline flags required regardless; claim scoped to the demo's one-provider evidence. | Activation-unit integration check per activated provider. |

Scratch probes that require running the Pi binary happen inside the implement units
under managed scratch environments (settled: every invocation gets the managed env);
probes that require inference additionally need the owner spend ruling (§9).

## 8. Test and gate plan (contracts the implementation units must ship)

Hermetic first: the state machine, bus ops, and extension logic are testable with a
mock Pi event stream (a scripted harness driving the extension's handler surface)
plus isolated `HCOM_DIR` buses; the drain-contract pins run against the **real
installed hcom binary**. No inference in the gate battery; one isolated live smoke
per gated stage.

Delivery state machine and transport:

- **T1 initial delivery** — doctrine + task enqueued pre-boot; injected on first
  `session_start`; delivered on settle; herder reports queued → delivered.
- **T2 idle delivery** — queued → batch-injected → settled while idle.
- **T3 busy-turn hold** — arrival mid-turn queues; injection happens only at the
  settle boundary; no mid-turn `sendUserMessage`.
- **T4 batch injection** — N pending ids, one injected message, per-id journal
  transitions, per-id delivered on the settle.
- **T5 duplicate drain rows** — id-keyed dedupe; repeat markers journaled; single
  injection.
- **T6 crash before inject** — restart replay injects queued ids exactly once.
- **T7 crash after inject, before settle** — no re-injection; single nudge turn;
  delivered on its settle.
- **T8 extension handler failure** — `extension_error` contained; diagnostics to
  file; seat flips `inject-degraded`; Pi process untouched.
- **T9 whole-process crash → herder restart** — resume relaunch; claim reclaim
  (`--as`); replay; pending drains and delivers; no receipt regression.
- **T10 seat claim exclusivity** — second live claimant refused; stale claim (dead
  pid) taken over; concurrent bus ops serialized by flock with no journal
  interleaving.
- **T11 journal ordering** — ascending-id sort before append under hostile page
  ordering; crash after partial page fsync loses nothing (the inherited V9 pins,
  re-run through the Pi op path).
- **T12 identity claim + de-placeholder** — one roster row, no second identity, no
  unread-state advance, send-accepted immediately after claim; a claim left as a
  bare one-shot start provably gets finalized `launch_failed` by hcom (the hazard
  pinned, not just the fix).
- **T13 identity env allowlist** — bus ops invoked from an environment carrying
  hostile `CLAUDE*`/`CLAUDECODE`/`HCOM_PROCESS_ID`/`CODEX_THREAD_ID` still bind
  adhoc, never trigger hcom hook installation, and never route through a foreign
  identity.
- **T14 pinned hcom binary** — bus ops use the recorded absolute path; a
  cwd-sensitive PATH shim in the environment is provably not consulted.
- **T15 drain contract vs real hcom 0.7.23** — stale backlog beyond the wait
  lookback; >20-message backlog across pages with mid-page crash; identity-free
  reads leave unread state untouched; self-delivery exclusion via
  `msg_delivered_to`. (The grok T24–T27 pins, exercised through the shared
  implementation from the Pi op entry points.)
- **T16 outbound wrapper** — journaled send; stdout trimmed to the receipt line
  even when hcom appends post-dispatch pending bodies; provider credential absent
  from the wrapper's hcom child env; hcom's real result returned verbatim.

Launch/lifecycle/observation contracts:

- **T17 child environment** — allowlist-only construction: exactly one provider
  credential by name; `PI_OFFLINE=1`/`PI_TELEMETRY=0` present; managed mapping
  correct in the live process env (`/proc`, one-time post-spawn assertion —
  conditional clause active); no credential value in argv, files, registry, or logs.
- **T18 managed env on every invocation** — installer artifact checks and any
  probing execution run inside scratch/managed homes; a test asserts no writes
  outside the provided roots (the demo's `--help`-writes-state hazard, pinned).
- **T19 install gate** — version + tarball + CLI-entry hash verification; refusal
  on mismatch names observed/expected; supported-version set enforced at launch;
  no per-launch hash gate exists.
- **T20 passthrough refusals** — every colliding passthrough from the DR-3 list is
  refused with a targeted error (finalized against the pinned CLI surface).
- **T21 provider filtering** — unknown `--provider` refused naming the set;
  cross-provider credential never present; provider relaunch rebuilds the env;
  in-process cross-provider `model_select` flags provider-drift.
- **T22 identity binding** — session evidence + process/pane evidence both required
  before bound; a second session in the same cwd cannot claim the seat; no cwd-keyed
  path exists to exercise.
- **T23 resume/fork** — resume: same seat/spool/name, exact session, replay. Fork:
  new seat/spool/name, lineage recorded, parent's pending stays put.
- **T24 cull** — row-stop + read-back absence confirm; pending →
  `undeliverable` with exact counts; teardown on process evidence.
- **T25 silence and hygiene** — the extension writes nothing into model context
  except the defined injection envelope and nudge formats; bus ops and diagnostics
  emit zero bytes to the pane; logs only to seat files.
- **T26 reporting gate** — `delivered` claimable only from settled-after-injected;
  journal append, sendUserMessage without the input event, and injection without a
  settle each provably insufficient (asserted on the reporting API).
- **T27 sesh/observer adapter** — header index (UUID, cwd, parent link) against
  recorded session fixtures, including a branched session; herdr `live_status`
  stays `unknown` under mutation; extension status labeled `status(pi-ext)`.

**Live smokes (isolated, gated, owner spend per §9):** the launch unit's smoke
proves one provider end-to-end under the activation flag: spawn → status-op bind →
doctrine + prompt delivered (T1 shape, real inference) → outbound send lands on an
isolated bus → cull with row-absence confirm. The activation unit's smoke is the
real `herder spawn --agent pi` path (not a direct-launch stand-in — the grok
activation lesson: the spawn path hid a full unit's worth of integration gaps),
repeated per activated provider.

## 9. Owner decisions required

1. **Default provider and default models.** `--provider` ships required with no
   default; no per-provider default model is pinned. Owner may pin either after
   trials (grok precedent: model pinned by ruling post-design).
2. **Inference spend** for implement-unit probes and smokes (per-provider). The
   grok blanket approval was scoped to that design's staging; Pi needs its own.
3. **Autonomy mapping** once probe A6 inventories Pi's approval surface — in
   particular whether any herder mode may map to a bypass-like Pi mode (grok
   precedent: no bypass mapping ruled in).
4. **Provider activation set**: which of anthropic/openai/xai activate at the
   activation unit (each adds a credential precondition and a smoke).
5. **Version-pin refresh policy**: 0.80.6 is the characterized pin; adopting a newer
   Pi requires a re-characterization pass (extension API + offline/state behavior).
   Owner sets the cadence/appetite; the design only requires that the gate exists.

## 10. Staging (mergeable units, territory fences, gates)

Same discipline as the grok program: transport first, activation last and separate,
the shim never routes into a nonfunctional family, each unit independently
reviewable behind its fence. Cross-family adversarial review and the full repository
gate battery apply to every behavior diff (house rules).

| # | Unit | Territory (fence) | Gate |
|---|---|---|---|
| U1 | **Transport core + extension**: spool/state machine on the shared journal code, `herder pi bus` ops (claim/de-placeholder, drain, wait, pending, send, status, retire), the TypeScript extension (lifecycle handlers, idle-gated batch injection, replay, nudge), `herder pi send` wrapper. Extraction of the shared drain/journal contract code out of `grokbridge` into a shared internal package is **in this unit's fence and must keep the entire grok battery green unchanged** — any grok behavior diff is a stop-and-flag. Nothing user-reachable changes. | New internal package(s) (e.g. `tools/herder/internal/pibridge/` + shared contract package) + `herder pi` command registration + extension artifact in-repo. | T1–T16, T25, T26 hermetic (mock Pi event harness + isolated bus); T15 against real hcom 0.7.23; grok battery green post-extraction; assumptions A1–A5, A7, A8 verified and recorded (scratch managed envs; inference-bearing probes under the §9.2 ruling). |
| U2 | **Install + launch contract, behind an activation gate**: pinned installer + hash verification, seat/managed-home provisioning and seeding, allowlist env construction, provider table + filtering, flag mapping + refusals, spool-borne doctrine/prompt, status-op bind capture with hard-fail cleanup, conditional `/proc` assertion. `--agent pi` refuses with a family-not-activated cause+remedy error unless the explicit activation config/env is set. | `launchcmd`/`spawncmd` pi branches + `herder pi install`; `pibridge` consumed, not modified. | T17–T21 + probes P1/P4/A6 answered and recorded + the isolated **live smoke** (one provider, §9.2 spend) under the activation flag. |
| U3 | **Lifecycle & identity**: resume/fork/cull/relaunch-on-provider-change, session-drift handling, registry capability flags (`bus`, `pending`, `inject`, `provider`), retirement reporting. | `lifecyclecmd`/`cullcmd` pi branches, registry schema additions. | T9, T22–T24 + resume/fork live re-check riding the U2 smoke pattern. |
| U4 | **Observer, transcript & sesh**: session-JSONL adapter (header index, branch-aware rendering), sesh identifier/lineage wiring, labeled `status(pi-ext)` enrichment, honest-unknown reconciliation. | `observercmd` + transcript/sesh plumbing. | T27 against recorded fixtures; `unknown` preserved under mutation. |
| U5 | **Shim/setup/doctor/docs**: `pi` PATH shim (no-vendor-fallback + escape hatch, per the grok shim pattern), ai-setup/ai-doctor family checks (report-only, isolated probe roots), managed-home and family docs. | shims + setup/doctor scripts + docs. | Ships only after U2's live smoke is green; shadowing/recursion checks; doctor probes prove no live-state writes (T18 posture). |
| A | **Activation unit** (own change, last): flip the default. | Activation config + any final wiring. | Hard ACs below. |

U1 → U2 strictly ordered; U3 and U4 parallel after U2; U5 after U2's smoke;
activation strictly last.

**Activation hard ACs** (the grok activation lessons, made first-class here rather
than discovered live):

1. A real end-to-end `herder spawn --agent pi` passes **through the spawn path** —
   pane creation, env delivery, bind, doctrine + prompt delivery, outbound, cull
   with row-absence confirm — per provider in the owner-ruled activation set.
2. **Credential precondition by name, fresh-pane truth**: the provider key is
   present nonempty by name in the environment a freshly spawned pane actually
   receives (non-interactive login-shell chain), not in any long-lived session's
   stale env.
3. **Status-op-authoritative liveness**: bind and health claims come from seat-state
   read-backs; no green without a confirmed bound seat; no-bind hard-fails with
   confirmed cleanup.
4. **Placeholder latch covered**: the claim op's de-placeholder behavior verified
   against the live hcom version in use at activation (re-verified if hcom was
   upgraded since U1's pins).
5. **`/proc` conditional resolved with evidence**: the activation run characterizes
   the actual pane-spawn path for Pi. If it is env-preserving direct-exec, the
   ceremony's removal is authorized as a follow-up reviewed change; if not, the
   assertion stays. Either way the clause stops being conditional — by evidence,
   not by paper.
6. Per-provider offline residual-network integration check (P5) for each activated
   provider.

Until the activation change, the family is explicitly experimental and opt-in.

## 11. Earned-clause disposition (carried forward from the demo)

The demo's clause verdicts (demo "Earned launch-contract clauses"), with where each
lands in this design:

| Clause | Demo verdict | Design disposition |
|---|---|---|
| Dedicated managed `PI_HOME` concept | Required | DR-3 seat construction (exact demo mapping) |
| Managed environment on every invocation | Required | DR-3 allowlist + T18 (installer/probes included) |
| `PI_OFFLINE=1` | Required | DR-3 always-env + activation AC 6 per-provider check |
| `PI_TELEMETRY=0` | Required | DR-3 always-env + seeded settings |
| Provider-specific environment filtering | Required | DR-5 + T17/T21 |
| Provider pin per seat | Required | DR-5 (relaunch on cross-provider change) |
| Pinned package version and integrity | Required at install/provision | DR-3 provisioning + T19 |
| Per-launch binary hash gate | Not earned | Not designed; immutable install + provision-time verification only |
| Per-launch config rewrite | Not earned | Not designed; settings seeded once (DR-3) |
| Per-launch `/proc` environment ceremony | **Conditional** | Carried conditionally: one-time post-spawn assertion every launch (DR-3) until activation AC 5 characterizes the pane-spawn path; resolution only by that evidence |
| Native managed extension | Required | DR-1/DR-2 (the binder-owner) |
| External binder process | Not earned | Not designed; DR-1 fork explicitly keeps all persistent logic inside Pi or in bounded CLI ops |
| Pending-message replay on every start | Required | DR-2 recovery matrix (session_start replay + nudge) |
| Exact resume/fork integration | Required | DR-4 + DR-6 (sesh lineage) + T23 |

## 12. Design-time verification note

Per the docs-only constraint of this unit, **no new probes of the Pi binary or of
hcom were run while writing this design**. Every behavioral claim cites either the
double-reviewed demo report, the grok design's independently reproduced hcom 0.7.23
verification (V1–V9), or mechanism-level grok activation evidence. Where the demo's
evidence basis was API/documentation inventory rather than probe, the claim is
registered in §7 with a conservative posture and a named verification owner. The
first implement unit (U1) begins by discharging the §7 register — in particular A7
(TUI-mode extension parity), which everything else builds on.

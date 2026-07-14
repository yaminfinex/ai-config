<!-- Provenance: design record, 2026-07-13. Design only; implementation is staged separately (§Staging). -->
# Pi as a first-class herder/hcom family — design

Status: accepted design — five adversarial fix rounds (round 1: sixteen items
across two independent reviews; round 2: nine incumbent items; round 3: five
consolidated residuals; round 4: launch-attempt fencing, the honest three-part
plaintext invariant, full worst-case spool reserve with the nudge budget, and
the rearm assignment sweep; round 5: attempt-scoped child process identity and
the K=1 repeat-marker bound), dual-APPROVEd and merged at round 5; this text
additionally carries an owner-invoked fresh-eyes amendment (round 6:
target-scoped external lane, local-id namespace, auth-precedence register
demotion, and a consistency sweep; round 7: gated child record, A10
cross-provider exploitability probe, renew pinned token-lane; round 8: the
external lane is operator-capability-bearing — location checks demoted to
defense-in-depth after the brokered-launch counterexample; round 9: explicit
stdin presentation — herder never auto-acquires operator authority from
caller-controlled context, and the same-UID concession is stated as
load-bearing with owner sign-off; round 10, 2026-07-14: the owner
**default-homes ruling** (standing-orders 20.8) — the per-seat managed home
and the pinned isolated install dissolve to the **live default Pi home and
the vendor-updated default install with a recorded vendor version**;
credential scoping in launch env construction is retained, the DR-2
delivery/authority machinery stands unchanged per the keep-custom decision
(`docs/design/2026-07-14-hcom-native-pi-characterization.md`), and every
property the ruling weakens is recorded for sign-off in §12 item 9), pending
re-certification on the amendment diff
Subject: `@earendil-works/pi-coding-agent` against herder + hcom 0.7.23 —
characterized at 0.80.6; deployed vendor-updated at the default install per
the 2026-07-14 ruling

Evidence base (cited throughout by path + section):

- `docs/design/pi-demo-report-2026-07-13.md` — the settled characterization record:
  installation provenance, managed-home mapping, offline/telemetry behavior, the
  extension-lifecycle probes, session model, provider routing, earned-clause table.
  Double-reviewed; this design does not re-derive or contradict it. Where it left an
  explicit evidence gap, this design designs conservatively and registers the
  assumption (§10) for the implement units to verify.
- `docs/design/grok-first-class-design.md` — the house pattern for a family design and
  the source of the proven hcom 0.7.23 pickup contract (its DR-1 drain contract and
  design-time verification V1–V9), the launch-contract shape (its DR-3), the identity
  rules and fork-preassignment erratum (its DR-4), the observability-honesty rules
  (its DR-5), and the staging/activation discipline (its §11).
- `docs/design/2026-07-14-hcom-native-pi-characterization.md` — the hcom-native
  Pi integration probe record. Its decision ("keep the custom DR-2 inbound state
  machine; the Pi design stands unchanged") is the standing delivery ruling this
  amendment does not reopen: delivery is orthogonal to home location.
- Grok family activation and hardening evidence (hcom 0.7.23; recorded in the grok
  program's backlog notes and review threads; mechanism-level facts restated inline
  where cited): the one-shot `hcom start` placeholder latch and its de-placeholder
  seam; the CLAUDE*/tool-signal hook-install hazard and its both-surface
  neutralization; ambient-PATH hcom resolution breaking under a cwd-sensitive shim;
  status-op-authoritative bind capture; cull row-stop + read-back confirmation;
  credential presence checked by name in a fresh non-interactive login-shell
  environment.

## 1. Settled ground (binding; not relitigated here)

Rows superseded by the 2026-07-14 default-homes ruling are rewritten in place;
the round-10 header entry and §12 item 9 record what changed and what it costs.
The demo report's findings remain valid characterization evidence throughout —
what changed is the deployment posture ruled on top of them.

| Constraint | Source |
|---|---|
| Pi seats run against the **live default Pi home and the vendor-updated default install**: `HOME` is the operator's real home; Pi's agent dir, session root, and XDG roots resolve to their defaults. The former per-seat managed home (the `PI_HOME` translation under the herder state root) is **dissolved**. Ruling context, binding: single-purpose machines; ringfencing expressly not required; the claude/codex live-home fleet norm extends to Pi; seat-scoped behavior deltas ride **launch env only**, never owner config writes. The family remains herder-owned end-to-end (DR-1) — what dissolves is state isolation, not lifecycle authority. Herder's own seat state (spool, journal, reservation/activation records) stays under the herder state root as before. | owner ruling 2026-07-14 (standing-orders 20.8; supersedes the earlier managed-home ruling); demo "Managed home and state model" retained as characterization evidence |
| Binding is a **native TypeScript Pi extension** — no external bridge process. The probe-proven inject path is `pi.sendUserMessage(...)` producing an `input` event with `source=extension` and a turn that runs to `agent_settled`. | owner ruling; demo "Binding fork", injection probe |
| Offline/update suppression: `PI_OFFLINE=1` (couples the version-check skip) plus `PI_TELEMETRY=0`. `PI_SKIP_VERSION_CHECK=1` alone is too narrow. Inference is not gated by offline mode (strace-backed for one Anthropic call; per-provider residual-network checks remain integration-test work). | demo "Startup network and update behavior" |
| Credentials, **env-channel scoped**: herder routes **one provider per seat**, by environment, referenced **by name only** — never in argv or in anything herder writes (registry, logs, doctrine, reports, seat state). A cross-provider model change is a controlled relaunch with a re-filtered environment. Under the default-homes ruling this claim scopes the **herder-routed channel only**: Pi's own resolution can also reach credential-bearing owner config in the live home — the auth store **and** models/custom-provider config (DR-5 delta, §12 item 9a). | owner ruling; demo "Provider routing and least privilege"; owner ruling 2026-07-14 for the channel scoping |
| Install integrity under the ruling: the install is the **vendor-updated default**; herder records the **observed vendor version** at provision and at every launch — no hash gate, no supported-version refusal. The demo's 0.80.6 tarball/CLI-entry hashes remain as characterization provenance, not a gate. Version-drift consequences are an owner-signed delta (§12 item 9). | owner ruling 2026-07-14; demo "Installation provenance" retained as evidence |
| Every **seat launch** receives the herder-constructed environment (env deltas + exactly one named provider credential — DR-3). The former every-invocation scratch-home ceremony dissolves with the pinned installer; Pi's `--help`-creates-state behavior now writes ordinary default-home state (delta recorded, §12 item 9). | owner ruling 2026-07-14; demo "Startup network and update behavior" retained as evidence |
| Herder writes **no owner Pi config, ever**: the former settings seeding dissolves with the managed home; startup suppression rides `PI_OFFLINE=1`/`PI_TELEMETRY=0` in the launch env (seat-scoped deltas ride launch env only). The one herder-owned artifact in the default home is the managed extension in `agent/extensions/` (the native-extension binding is settled; hcom's own native Pi integration uses the same surface), version-recorded and inert without seat launch-env coordinates (DR-3). | owner ruling 2026-07-14; demo clause table |
| The `/proc` post-spawn environment ceremony is **CONDITIONAL, not settled**: retain a one-time post-spawn environment assertion until herder's actual pane-spawn path for Pi is characterized as env-preserving direct-exec. This design carries the conditionality forward (§DR-3, §13 activation unit); it is not resolved on paper. | owner ruling; demo clause table |
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
- **Extension** — the herder-managed Pi extension installed once in the default
  home's `agent/extensions/` (shared across seats and the owner's interactive Pi
  runs under the default-homes ruling; it activates only under seat launch-env
  coordinates and is inert otherwise — DR-3). The binder-owner: activates the seat's ownership epoch on
  `session_start`, runs the **inbound driver** (the specified drain/wait loop —
  DR-2), injects via `pi.sendUserMessage`, translates Pi lifecycle events into seat
  status, releases idempotently on `session_shutdown`. Lives and dies with the Pi
  process; herder supervises Pi, not the extension.
- **Bus ops** — `herder pi bus
  <reserve|activate|rearm|renew|drain|wait|pending|send|status|retire>`:
  short-lived, bounded CLI invocations. Caller attribution follows DR-2's
  lanes: the operator-capability external lane runs reserve, rearm, and
  cull-driven retire; the extension's seat-token lane runs activate, drain,
  wait, pending, renew, and extension-initiated retire; the model
  deliberately reaches only the `herder pi send` wrapper and read-only status. All hcom mechanics — identity + de-placeholder,
  drain paging/sorting, journal append+fsync, cursor derivation, outbound send —
  live in this one Go implementation, built on a transport-neutral extraction of
  the contract primitives proven for grok (reuse boundary fixed in DR-1). Every
  mutating op carries and is fenced by the seat's ownership epoch (DR-2). No
  daemon, no socket: every process in the bus path is a bounded child invocation.

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
3. **Short-lived `herder pi bus` invocations, driven by the extension's inbound
   driver.** ADOPTED. The loop itself is a specified component — DR-2 "The inbound
   driver" defines its states, child discipline, cancellation, failure
   containment, and the runtime assumption (A9) it stands on — not prose about the
   extension "owning timing". Each invocation performs one atomic, flock-serialized
   operation against the spool and/or hcom and exits. The proven contract
   primitives are reused per the reuse boundary below; the pinned real-hcom tests
   carry over; the TypeScript surface stays a thin adapter (spawn op, parse
   NDJSON, inject, report).

This does not reopen the settled binding fork: injection, lifecycle observation,
seat claiming, and restart behavior all live in the extension; no process outside
Pi's tree persists between operations.

**Reuse boundary — fixed here, not decided in U1 while coding.** "Reuse the proven
contract code" is made precise: what is extracted into a transport-neutral shared
internal package is the **contract primitives only** — `--full` event-envelope
parsing, the oldest-first paged membership drain loop, the mandatory ascending-id
sort before append, identity-environment allowlist construction, pinned-binary
resolution, and the append/fsync/replay journal primitives — together with the
pinned real-hcom contract tests, re-homed onto that package so grok and Pi exercise
one implementation of the hcom 0.7.23 behaviors. What is **not** reused: grok's
journal state types (`queued/surfaced/fetched/acked`), its receipt machine, and its
binder socket-generation fencing — those encode grok's binder/tap/MCP topology. Pi
gets its own state adapter (`queued/injected/delivered`, ownership epochs — DR-2)
over the shared primitives; grok's existing adapter stays where it is. The
extraction must leave the entire grok battery green unchanged (§13, U1 fence).

**The pickup contract is inherited, not re-derived.** Inbound pickup is the grok
DR-1 contract verbatim: anonymous `hcom events --full`, oldest-first paged
`id IN (SELECT … WHERE id > C … ORDER BY id ASC LIMIT 20)` membership subquery,
mandatory ascending-id sort before journal append, cursor derived as the max id of
the fully-journaled page, `--wait` only as an edge trigger between empty drains,
`msg_delivered_to` as the routing predicate, `hcom listen` rejected. Those behaviors
are pinned against installed hcom 0.7.23 by the existing grok contract tests; the Pi
unit reuses that code and re-points the same pins (§11, T15). The scrub list and the
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
  regardless of what its own process inherited. Pi's seat process environment is
  itself launch-constructed (DR-3), so no `CLAUDE*` signal should exist to leak — the
  allowlist makes that true even for hostile or foreign launch contexts (T13).
- **Pinned absolute hcom binary.** Live grok evidence: resolving `hcom` through
  ambient PATH hit a cwd-sensitive version-manager shim and failed `hcom start`
  from inside worktrees. Seat provisioning resolves and records the absolute real
  hcom binary once; every bus op invokes that recorded path, never PATH (T14).
- **Placeholder de-latch, with write-ahead reservation.** Grok activation evidence
  on hcom 0.7.23: a one-shot generic `hcom start` leaves the roster row as a `new`
  placeholder that hcom finalizes `launch_failed` at ~30 s, after which sends
  exclude the row. The proven de-latch is one **identified read-only** operation —
  exactly `hcom list --name <name> --json`, the smallest identified read-only hcom
  command, proven in the grok binder to stabilize the process-bound row without
  delivering or acknowledging pending messages (`grokbridge` `bindIdentity`);
  T12 re-pins that exact argv for Pi — U1 does not invent a different follow-up.
  `herder pi bus reserve` performs identity acquisition as `hcom start` (or `--as`
  reclaim) plus that de-latch read. **Crash atomicity is write-ahead:** the
  reservation record — including the bus name the retry will reclaim by — is
  persisted and fsynced under the seat lock *before* the identity invocation runs,
  so death between `hcom start` and the de-latch (or between either and a
  name-persist) cannot lose the reclaim key — **in the preferred shape**, where
  the name is herder-minted up front and passed via `--as`, and the guarantee of
  no second row is claimed for that shape only. Whether the pinned hcom mints a
  fresh identity from `--as` (versus only reclaiming) is register P2 (§10). The
  reclaim-only **fallback has a real window**, stated honestly: a random-name
  `hcom start` that dies before name capture leaves an orphan placeholder whose
  name nobody holds, and until hcom's ~30 s finalizer that row **may be inside
  tag/broadcast routing fanout** — a message routed to it in that window would
  never reach the live seat's spool. The P2 probe therefore must also establish
  whether a never-de-latched placeholder is routable at all. If it is not, the
  fallback (journal a `reserving` marker, retry mints fresh, orphan ages out
  inert) stands. If it **is** routable, the retry must identify and stop
  candidate orphans before minting fresh — roster diff of placeholder-state rows
  created since the `reserving` marker, stopped via hcom's row-stop — and if the
  pinned surface cannot support reliable identification-plus-stop, the fallback
  is **blocked for a design delta** before U1 completes; U1 does not improvise a
  reconciliation (T12).
- **Explicit minimal environment for every extension-spawned bus op.** Pi's own
  process environment necessarily carries the seat's provider credential (DR-5),
  and the extension lives inside that process. The extension therefore never lets
  a bus-op child inherit its environment: every `herder pi bus` invocation it
  spawns receives an explicitly constructed env object containing only the
  recorded absolute herder binary's needs — seat/state coordinates
  (`HERDER_STATE_DIR`, seat GUID, ownership epoch) and a minimal PATH floor — with
  **no provider credentials and no tool signals**, and the binary is invoked by
  its recorded absolute path. T13 and T17 assert this against the bus-op process
  itself, not just against the hcom grandchild (assumption A8, strengthened, §10).

**Outbound.** The model sends through `herder pi send` (doctrine-mandated), which:
journals the outbound intent, executes `hcom send --name <busname>` with the pinned
binary and allowlisted env, **scrubs the seat's provider credential from the hcom
child's environment** (demo extension rule 8 — hcom does not need it; the helper
process itself, being a model tool child, inherits the seat env inside the accepted
model-tool boundary — DR-5), returns hcom's actual result, and **trims stdout to
the send receipt line**. The trim matters: any named
hcom command may trigger post-dispatch pending delivery, appending other messages'
bodies to stdout — for Pi that stdout lands in the model's tool result, creating a
second, uncontrolled delivery path for content the extension will also inject
(duplicate context) — the same context-hygiene hazard grok closed with first-line
trimming. Pickup correctness is unaffected either way (it rides the anonymous drain
and journal cursor, never hcom's per-identity cursor), so an incidental drain by a
raw `hcom send` a model runs anyway is harmless to correctness, merely unhygienic;
doctrine directs all sends through the wrapper (T16, T25).

`herder pi send` and the read-only `herder pi bus status` are the **only two
deliberately model-reachable** bus surfaces (the grok precedent: the model reaches
send, nothing else). Every other op — reserve, activate, drain, wait, pending,
retire — is control-plane and capability-gated (DR-2 "Seat ownership"): knowing the
seat coordinates that necessarily sit in the environment is not sufficient to
invoke them.

**hcom-side "delivered" honesty.** As for grok: the seat's spool is the only
authoritative delivery record; hcom's unread counters for Pi rows are documented as
non-authoritative (anonymous reads never consume them; wrapper sends may
incidentally clear them). The roster tool label for a generically-started identity
reflects the detected calling environment, not `pi` (grok design V6); the registry's
`tool: pi` row is authoritative, and the upstream label nicety stays on the same
HOLD the owner already ruled for grok.

**Alternatives considered** (beyond the three-way fork above): the native `hcom pi`
launcher/extension — hcom 0.7.23 ships one, and it was probed head-on: its receipt
is the `sendUserMessage` call, not a settled turn, it has no ownership epochs,
driver lease, capability lanes, or herder lifecycle authority, and a better native
receipt would add none of those — kept as compatibility evidence and a reference
implementation, ruled out as the production delivery boundary
(`docs/design/2026-07-14-hcom-native-pi-characterization.md`, the standing
keep-custom decision);
RPC-mode external controller as binder — explicitly rejected by the demo (weaker
session-transition access, second crash protocol; demo "Binding fork: Decision");
transcript scraping — rejected by the demo (the extension API removes the need).

## DR-2 — Inbound delivery state machine and recovery

Grok's receipt machine needed model-visible fetch/ack because delivery ran through a
wake line and an MCP fetch the bridge could not correlate with injection. Pi's
extension **is** the injector and observes the turn lifecycle in-process, so the
machine is shorter and needs no model-side protocol — but the same honesty rules
apply: nothing is reported that the evidence does not show.

### Seat ownership: reservation, activation, epochs, capability

Seat ownership is three distinct facts, established in order, never conflated:

1. **Reservation (prelaunch, herder-side).** `herder pi bus reserve` establishes
   the **bus identity only**: write-ahead reservation record, `hcom start`, the
   pinned de-latch read (DR-1). A reserved seat has a name and a stable roster row
   — and no runtime, no injection capability, no claim to liveness.
2. **Runtime activation (extension-side, at `session_start`).** `herder pi bus
   activate` records the live Pi pid + process-start evidence + the session UUID
   (DR-4) and increments the seat's **monotonic ownership epoch**, persisted and
   fsynced under the seat lock. Activation verifies and rotates the **control
   capability** (full lifecycle below). Each restart, resume, and in-TUI rebind
   activates a fresh epoch; retirement runs the two-phase `retiring` → `retired`
   fence (recovery matrix).
3. **Bind readiness.** The seat is *bound* if and only if (a) a status read-back
   returns the current runtime epoch, and (b) the **inbound driver is armed** —
   it has completed its first drain at that epoch AND its liveness lease is
   fresh (bounded age; the driver spec below) — armed is a decaying lease, never
   a latched historical fact.
   Spawn's bind capture, `herder` status surfaces, and activation AC 3 (§13) all
   key on this definition — never on the roster row or the de-latch alone, which
   prove reservation, not a live seat.

**Epoch fencing.** Every mutating bus op carries the epoch it was issued under.
Each op takes the seat flock, re-reads the persisted epoch **and lifecycle phase**,
and **rejects** — before touching the journal or hcom — any mutation carrying a
stale epoch, any non-retirement mutation while the seat is `retiring`, and every
mutation after the terminal `retired` record. A wait/drain child spawned under a superseded epoch — a prior session, a
prior process, a pre-retirement loop — is thereby discarded structurally, not by
politeness; the extension additionally cancels such children on rebind, but
correctness never depends on the cancellation winning the race (T31, T32).

**Control capability — two authority lanes, full lifecycle.** Seat coordinates
(state dir, seat GUID) necessarily sit in the seat's environment, which model tool
code inherits — so coordinates must not be sufficient to drive the control plane.
Control-plane authority comes in exactly two lanes, and every op declares which
lane(s) accept it:

- **Token lane** (the live extension): ops issued from inside the Pi process —
  drain, wait, pending, **renew**, rotate-on-activate, extension-initiated
  retire — present a capability token over the bus-op child's **stdin** (never
  argv, never env), verified against a stored hash under the seat lock. The
  enumeration is authoritative: `renew` in particular is token-authenticated —
  an unauthenticated renew would let any same-UID process keep a dead driver's
  lease green, defeating the progress-attested lease outright (T34i).
- **External lane** (operator-capability-bearing): reserve (pre-exec),
  **pre-exec rekey** (`rearm`), and cull/retire require the **operator
  capability** — a fleet-level credential minted by herder outside any seat
  (lifecycle below), presented over the bus-op child's stdin exactly like the
  seat token and verified against a hash in family state. There is **no
  tokenless lane**. Location-based origin checks cannot carry this boundary:
  ancestry does not survive reparenting (double-fork/setsid sheds the target
  pid), and cgroup membership is location, not causal origin — an unprivileged
  target tool can broker its own lifecycle op through any same-UID
  process-launch broker (`systemd-run --user herder pi bus cull <own seat>`
  lands the op in a fresh transient-unit cgroup, passing any
  membership check without a single privileged write). Authority therefore
  rides possession of the capability — **presented explicitly by the caller,
  never acquired by herder**. The composition rule that makes this hold: any
  acquisition herder automates from caller-controlled context, a brokered
  command can cause (a broker inherits or trivially sets `HOME`, so an
  auto-resolving herder would fetch the credential *for* the broker). Herder
  therefore **never locates or reads the operator credential itself**; the
  caller pipes it to the op's stdin (`herder pi bus cull <seat>
  < ~/.config/herder/pi-operator` from a human shell; an orchestrator reads
  its own config and pipes — normal live-`HOME` agent behavior). A brokered
  bare command arrives with **empty stdin** and is refused. The boundary
  statement, honest and exact: the supported surface refuses **naive/direct
  self-operation** — the own-cgroup belt, the empty-stdin refusal, and no
  auto-acquisition anywhere in herder — while a model that deliberately
  *composes* broker plus credential-path redirect is executing **deliberate
  credential acquisition**, the same conceded cooperative same-UID class as
  absolute-path theft and journal forgery (fleet-wide, predating this family;
  the grok control plane ships under it — Threat model below). The claim is
  precisely: **no herder surface auto-acquires operator authority from
  caller-controlled context** — not that brokered launches are refused as a
  class. **Defense in depth retained:** the per-seat cgroup, the gated
  record-before-exec, and the cgroup-empty quiesce sweep all stay (A11) — as
  process accounting and straggler-kill correctness, and as a belt: an op
  whose own cgroup **is** the target seat's cgroup is refused even with a
  valid capability (there is no legitimate operate-your-own-seat-from-inside
  case) — but none of it is the authorization boundary. The lane stays
  **target-scoped in intent**: the fenced threat is the target seat's own
  model *naively* driving its own control plane. A herder-spawned
  orchestrator pipes the capability from its own live-home config and
  operates every seat but its own refused-by-cgroup one — the fleet's
  operating norm; a herder Pi seat's doctrine and default context carry no
  capability and no path to one short of the conceded acquisition class. A
  target model *asking* a sibling to operate its seat rides that same
  conceded in-band path. `activate` is **never** in this lane: activation is
  always seat-token-authenticated and always extension-side, because only the
  extension can truthfully publish the live pid/session runtime facts — a
  capability holder outside the process cannot, so a capability-bearing
  external activate still does not exist.

**Operator capability lifecycle — specified, not left to U1:**

- **Mint (human-shell bootstrap).** Minted once at family provisioning
  (`herder pi operator init`, a step of the same owner-run, non-seat setup
  that provisions the Pi family), **interactively** — the command prompts for
  confirmation on a terminal, so a brokered non-interactive invocation fails
  on the missing interaction, not on environment heuristics. The mint writes
  the hash into family state and the plaintext credential to a 0600 file the
  **operator chooses and owns** (conventionally
  `~/.config/herder/pi-operator`); herder records only the hash, never the
  location. Re-bootstrap after loss is the same owner-run command. The
  seat-cgroup refusal remains as **belt**, relabeled as such — it is not the
  boundary. The former managed-home belt refusal **dissolved with managed
  homes**: under the default-homes ruling every seat shares the operator's
  live home, so a home-based origin check no longer distinguishes anything —
  and the conventional credential path now sits inside that shared home
  (delta recorded, §12 item 9; reading it remains the conceded
  deliberate-acquisition class).
- **Presentation — never resolution.** Herder has **no code path that locates
  or reads the operator credential**. Every external-lane op takes the
  capability from **its own stdin, supplied by the caller**; empty or invalid
  stdin refuses with a cause+remedy error naming the explicit-presentation
  requirement. The material never lands in argv, env, logs, or seat state.
- **Rotation.** `herder pi operator rotate` requires the **old capability on
  stdin** (plus the same interactive confirmation as mint): new material is
  minted, the family-state hash swaps atomically under the family lock, and
  old material is invalid from that instant. A brokered rotate fails on empty
  stdin. Rotation cadence is owner policy; loss or suspected exposure →
  rotate, nothing else to clean up.
- **Verification.** Ops verify the presented capability against the
  family-state hash under the family lock before touching any seat, then
  proceed under the seat lock as before.

Token lifecycle, every branch specified:

1. **Trust root (first activation).** Launch — herder code running *before* Pi
   execs, inside an open launch attempt (protocol below) — mints the initial
   token, persists its hash in seat state, and writes the plaintext to a
   one-time, attempt-keyed **bootstrap file** (0600, seat dir). The extension
   consumes it (read + unlink) during its first `session_start`, **before any
   model turn has ever run in this process**, and presents it to `activate`. A
   missing or already-consumed bootstrap at first activate is a hard launch
   failure with cleanup — never a fall-through to tokenless activation. The
   bootstrap file's exposure window (exec → session_start) is protected only by
   the cooperative same-UID model, and the threat-model section says so.
2. **Rotation on every activation.** `activate` verifies the presented token
   against the stored hash under the lock, then rotates: a fresh token returns to
   the extension on the op's stdout pipe, and the new hash is stored atomically.
   A same-process rebind (in-TUI session switch, same extension instance) is the
   ordinary case: present current token, rotate, fresh epoch.
3. **The plaintext invariant — three parts, each implementable.** A bootstrap
   file written during a live session would be a designed plaintext-authority
   handoff sitting in a path the model's tools can read and race through the
   supported token lane — the trust root reopened, not protected. The invariant
   is therefore stated in the form the happy path can actually satisfy (a green
   launch necessarily has the bootstrap on disk across exec → `session_start`,
   while the process is live but pre-model):
   (i) no plaintext capability material is ever **written** while a seat
   process lives — `herder pi bus rearm` is a **pre-exec rekey** that
   hard-refuses if the recorded Pi process is alive (pid + start-time);
   (ii) a bootstrap file may **exist** only in the pre-model window after exec,
   and is consumed and unlinked before any model turn runs in that process;
   (iii) after the first successful activation, no plaintext capability
   material exists on disk for the remaining life of that process.
   A pre-opened-and-unlinked inherited descriptor would eliminate window (ii)
   entirely and is a welcome U1 tightening if Pi's exec path supports it — but
   the invariant above is the designed, testable contract (T34d).
4. **Extension reload with memory lost, Pi alive.** Whether reload preserves
   module memory is registered in A4's probe scope; if the new instance has no
   token, it cannot activate in-band — by design, and it makes **no attempt**:
   a tokenless instance is provenance-indistinguishable from a model tool
   child, so it stays control-plane inert like any other unauthenticated
   caller (DR-3 predicate note). `control-degraded` is instead **derived,
   write-free, from the authenticated side's silence**: every signal of seat
   health is token-gated (lease renewal and every token-lane journal write —
   T34(i)), so token loss shows up as the one thing no tokenless caller can
   forge — **absence**. `herder pi bus status` derives it as: current
   activation's recorded process **alive** (pid + start-time) AND the
   progress lease expired AND **no authenticated token-lane record since**
   (an authenticated `driver-degraded` record proves the token is still
   held and caps the state at driver-degraded instead) AND that silence has
   persisted past a **control-loss escalation threshold** — pinned in family
   config, sized above one lease TTL plus the driver supervisor's full
   bounded backoff, so a hung-but-token-holding driver either recovers or
   writes its authenticated degrade record before escalation ever fires.
   Detection latency is therefore **bounded but not immediate** (TTL +
   threshold), stated honestly; nothing is journaled to produce the state
   and no tokenless write path exists (T34(e)).
   Recovery is a **controlled relaunch**, not a
   live handoff — executed under the launch-attempt protocol below: open the
   attempt, quiesce and terminate the exact recorded process (model tool code
   dies with it), attempt-keyed pre-exec rekey, relaunch via the standard
   resume path (DR-4: same seat, spool, session);
   the new extension consumes the bootstrap at `session_start` before any model
   turn, exactly the sound first-boot pattern. No stored-hash replacement is
   ever accepted from inside a live seat without the current token, and no
   channel that exists as a model-readable filesystem object is used for
   capability handoff at any point after model execution has begun.
5. **Dead-process takeover — the exact crash-restart sequence.** The
   launch-attempt protocol (below), external-lane: (i) the caller — presenting
   the **operator capability** (DR-2 lanes) — opens the attempt under the seat
   lock, verifying the recorded process
   is provably gone (pid + start-time); (ii) the attempt-keyed pre-exec rekey
   mints the fresh hash + bootstrap and prepares the fresh epoch; (iii) relaunch
   execs Pi; (iv) the new extension performs a normal **token-authenticated**
   activation presenting the bootstrap token and the attempt generation,
   consuming the attempt and publishing the new pid/session evidence itself.
   The external lane never publishes runtime facts and never activates; the old
   plaintext token died with the old process, and nothing needs it.
6. **Cull, graceful or offline.** External cull needs **no channel to the
   extension and no seat token**: it is external-lane, authorized by the
   operator capability. It writes the durable
   `retiring` phase under the lock (below), which every token-lane op observes as
   a fence — a live extension's in-flight ops are rejected from that moment and
   the extension stands down; a dead or hung extension simply never contends.
   Cull then executes the retirement sequence itself as external-lane ops,
   killing the Pi process on process evidence where needed. The extension is a
   convenience participant in retirement, never a dependency.

**Launch-attempt protocol — one durable owner for every (re)launch.** The
check-then-launch interval (observe process gone → rekey → exec → activate) is
its own race surface: two restarts can both observe a dead process, rekey
competing tokens, and the loser's cleanup could tear down the winner. Per-op
flock and the live-process refusal do not fence that interval; a durable
attempt record does. Every launch — first boot, controlled relaunch, crash
restart — runs the same protocol:

1. **Open the attempt.** Under the seat lock: verify the phase permits (not
   `retiring`/`retired`; no open attempt, or the open attempt is superseded per
   rule 4), then persist a **`starting` phase record** carrying a fresh
   monotonic **attempt generation** plus the launcher's pid/start-time and an
   activation deadline. From this record on, the attempt owns the seat's launch
   path; competing opens are refused under the lock.
2. **Quiesce, exactly — every recorded process identity.** For a relaunch: with
   the attempt open (so no competitor can slip in behind the kill), stop every
   process the seat has recorded — the activation record's pid + start-time
   **and any prior attempt's recorded child** (step 3) — an explicit
   no-more-turns mechanism: SIGTERM-then-KILL addressed to each **exact
   recorded pid + start-time**, never a name or a guess — then a **final sweep
   of any remaining members of the seat's cgroup** (catching reparented
   stragglers no pid record names) — and wait until each exact identity is
   provably gone and the cgroup is empty. First boot skips this step by
   verifying no recorded process exists and the seat cgroup is empty.
3. **Rekey once, attempt-keyed; gate the child.** The pre-exec rekey
   runs inside the attempt: the bootstrap file, the stored hash, the prepared
   epoch, and every cleanup artifact are **keyed by the attempt generation**.
   The child is **gated**: it is created without executing Pi (fork before
   exec, or spawned suspended), its **pid + start-time is recorded into the
   open attempt and fsynced under the seat lock, and it is placed into the
   per-seat cgroup, before its exec is allowed to proceed** — so no live
   target process ever exists ahead of its durable record, and the
   exec→activate interval has an owner-of-record from the child's first
   instruction. The extension's activation presents the
   bootstrap token **and the attempt generation**, and `activate` consumes the
   attempt (`starting` → active) only if it is still the highest, unconsumed
   generation.
4. **Supersede, never share — and never orphan.** A competing launcher may
   supersede an open attempt only after its activation deadline has passed
   with no consumption, by opening a strictly higher generation (whose rekey
   replaces hash and bootstrap). An activation presenting a superseded
   generation is refused. The superseding attempt's quiesce (step 2) kills the
   superseded attempt's **recorded child** by exact pid + start-time — a slow
   launcher's Pi that execed but never activated is terminated, never left
   running as a same-UID orphan holding the seat's provider
   credential outside bind readiness, epoch fencing, and cull's reach. Cleanup
   is generation-scoped: an abandoned or losing attempt's teardown can remove
   **only artifacts and the process keyed to its own generation** — it
   structurally cannot touch the winner's bootstrap, hash, epoch, or process
   (T34f races this, including the exec-not-yet-activated branch).

Prompt-induced tool code that knows every environment variable in the seat can
satisfy neither lane **naively, for its own seat**: it has no seat token, no
operator capability arrives on its ops' stdin by default (herder never
auto-acquires one on any code path), and the direct in-cgroup path is
belt-refused regardless. Deliberately composing a broker with a
credential-path redirect is the conceded deliberate-acquisition class, not a
defeated boundary (T34 tests each branch above). The deliberately model-reachable surfaces
remain exactly `herder pi send` and read-only `status` (DR-1). This is an in-band
boundary within the house's cooperative same-UID trust model — see "Threat model"
below for what it does and does not claim.

### States

Per inbound message id, strictly monotonic (duplicates recorded, never regress):

```text
queued ──► injected ──► delivered                    (terminal)
   │            │
   │            ├──────► undeliverable(stalled)      (terminal: nudge budget
   │            │                                     exhausted — nudge policy)
   └────────────┴──────► undeliverable               (terminal: seat retired
                                                      first)
```

One honesty note on the `stalled` edge: terminality is monotonic, so a settle
that races budget exhaustion and loses leaves an actually-processed message
permanently reported `stalled` — the report errs toward under-claiming
delivery, never over-claiming it, and that direction is the deliberate choice.

- **queued** — a `herder pi bus drain` invocation appended the full journal record
  (hcom event id, sender, intent, thread, payload, payload hash, timestamps) and
  fsynced it, in ascending-id order per the inherited contract. Happens before any
  injection is possible. The committed cursor is derived from the journal by replay,
  never stored separately.
- **injected** — a **durable injection record** exists: the extension called
  `pi.sendUserMessage(...)` with the message envelope, observed Pi's `input` event
  with `source=extension` for it (probe-proven pair — demo injection probe), and
  the injection record was journaled and fsynced through a bus op. The state means
  the record, not the call: between the input event and the fsync there is a crash
  window in which content has entered the session but no durable record says so —
  that window is an explicit at-least-once duplicate window, handled in the
  recovery matrix, never assumed away. Content durability in the session JSONL
  itself is assumption A3 (§10), which the nudge policy below is conditioned on.
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
- **Batched, bounded.** Pending ids inject as one composed user message, one
  envelope block per message (id, sender, intent, thread, then body — formatted to
  match hcom's native delivery style), **bounded by configured count and byte
  caps** per batch; the remainder stays queued and injects at the next settle
  boundary. A bus flood therefore costs bounded context per turn, never an
  unbounded composed message (T4, T35). The spool itself is **hard-bounded, by
  prospective admission rather than by reactive state or rejection records** (a
  rejection record per flooded message would itself grow the append-only journal
  without bound, and a quota checked only *after* an append is a bound already
  crossed):

  - **Prospective admission.** The drain op computes each record's size *before*
    appending and admits, in ascending-id order, only the prefix of the page
    that fits under the admission cap; the rest is **deferred without cursor
    advancement** — the cursor is derived as the max id of fully-journaled rows,
    so deferred rows sit untouched in hcom's non-destructive, id-addressed
    events store and are re-drained after drain-down. No append ever crosses
    the cap, at 63 MiB or anywhere else.
  - **Bounded record size by construction.** A message whose payload would
    exceed the per-record cap is journaled **oversize**: envelope + payload hash
    only, with the payload fetched from the events store by id at injection
    time (an anonymous id-addressed read — the proven non-destructive surface).
    Every journal record therefore has a proven maximum size; no unbounded
    record class exists.
  - **Reserved state headroom — the full worst-case sum, both classes.**
    Everything that must append after admission stops has reserved space, and
    the reserve is enumerated, not gestured at. **Per-id class:** live ids are
    capped, and each live id has a *provably* bounded record set — the state
    transitions (queued/injected/delivered/undeliverable), repeat markers at
    **at most K = 1 live repeat-marker record per id** (a duplicate observation
    past K updates nothing and is refused/deduped without an append — the
    pre-compaction bound is finite by construction, not only after snapshot
    folding), and nudge records under the per-id **nudge budget** (nudge
    policy) — so the
    per-id reserve is id cap × the enumerated per-id record maximum × max
    state-record size. **Fixed class:** the records retirement and control
    require regardless of load — the `retiring` fence, the **one**
    deferred-summary record, the `retired` fence, activation/epoch/lease
    records, and a bounded count of snapshot records — each of bounded size
    and fixed count, summed at worst case. The enumeration is **closed under
    adversarial callers**: no journal record class is writable by an
    unauthenticated caller — tokenless or invalid-token ops of any
    provenance are refused **without any append** (T34a, T34e spoof
    branch) — so nothing outside the two authenticated lanes can grow
    either reserve class. The hard cap = admission cap +
    per-id reserve at worst case + fixed reserve at worst case, so
    cull-while-paused with **every reserve class at its worst case** still
    completes `retiring` → summary/marks → `retired` without crossing the
    bound (T33); prompt snapshot/segment rotation (a rotated segment whose ids
    are all terminal is deleted per retention config) keeps steady state well
    under it.
  - **Back-pressure state.** At the admission cap the seat flips `spool-quota`
    with an honest backlog count (anonymous count query); draining resumes when
    deliveries bring live-pending under a low-watermark. Default caps, pinned
    in family config and tuned in U1: 256 live-pending ids, 64 MiB admission
    cap plus computed reserve, low-watermark at half the id cap.

  Honest deferral over silent exhaustion, with the bound enforced before every
  write rather than observed after it (T35).
- **No blind re-injection — of durably injected ids.** An id with a **durable
  injected record** is never re-injected on the strength of a missing settle
  alone; recovery for those uses a nudge turn (below). Ids caught in the
  input-event-before-fsync crash window have no durable record and **are**
  re-injected on replay — deliberately, as the chosen at-least-once posture — with
  the **same visible envelope (same bus id, same payload hash)**, so a model that
  already saw the first copy can recognize the repeat, and doctrine says so (T30).

### The inbound driver (specified, not narrated)

All post-boot delivery rides this loop, so it is specified as a component, not as
prose about the extension "owning timing". One driver instance per activation
epoch, started by the extension after `activate` succeeds:

```text
armed := first drain at this epoch completed AND lease established
loop:
  drain until an empty page                 (bus drain; epoch + token via stdin)
  while undelivered pending ids exist:
    if turn active: park until agent_settled   (no wait child while parked)
    batch := next pending ids under the count/byte caps (injection policy)
    pi.sendUserMessage(batch envelope)
      → await the matching source=extension input event
      → journal `injected` for the batch's ids (bus op, fsync)
    await agent_settled → journal `delivered` for those ids
    drain once (pick up arrivals routed during the turn); remaining pending
      ids loop back for the next bounded batch
  spawn ONE bounded `bus wait` child        (anonymous --wait edge trigger)
  on child exit (wake, timeout, error) OR cancellation → back to drain

lease checkpoints (†): completed drain page · injected-record fsync ·
delivered record · completed (empty) wait cycle · bounded park re-check.
Lease renewal happens ONLY at (†). No independent renewal timer exists.
```

The loop **is** the delivery path end to end: drain → pending selection → bounded
batch → injection → input-event correlation → durable injected record → settle →
delivered record → residual drain — a message can sit `queued` only while a batch
ahead of it is in flight or the turn is parked, never because no code path leads
from the spool to `sendUserMessage`.

- **Child discipline.** At most one wait child exists per driver; it is spawned
  asynchronously (never blocking a Pi lifecycle handler), tracked by pid+promise,
  and always reaped (awaited) after kill or exit — no zombie accumulation. The
  child is cancelled (SIGTERM, then reap) on `agent_start` (a turn began; the
  settle boundary triggers the next drain anyway), on `session_shutdown`, on
  rebind (new `session_start`), and on retirement. Cancellation is a latency
  courtesy; a child that outlives it is fenced by its stale epoch (above) and its
  output is discarded.
- **Failure containment.** A wait child that errors or times out just returns the
  loop to drain. Repeated spawn/drain failures back off boundedly and then flip
  the seat `driver-degraded` (registry-visible: pickup is halted and reported as
  halted — undrained messages wait safely in hcom's non-destructive events store,
  not in a fictional queue) while Pi itself stays healthy; recovery is driver
  restart at the same epoch or seat relaunch.
- **Supervision and liveness — `armed` is a lease, not a latch.** A first drain
  proves the driver *was* alive; status must prove it *is*. Two mechanisms, both
  required: (a) the driver's top-level task runs under a supervisor in the
  extension — an unexpected resolve or rejection of the driver promise is
  journaled to diagnostics and restarted with bounded backoff at the same epoch,
  degrading honestly when the backoff exhausts (silent top-level exit cannot
  leave a green status); (b) a **progress-attested lease**: renewal is emitted
  **exclusively from the bounded checkpoints of the supervised state machine**
  — after a completed drain page, an injected-record fsync, a delivered record,
  a completed (empty) wait cycle, and, during an active turn, a bounded park
  re-check (the park is not one indefinite await; it is a re-check cycle with
  the turn identity recorded). Every await in the driver is deadline-bounded —
  raced against a timeout that returns control to the state machine — so a
  never-resolving promise returns the driver to a checkpoint, and a driver that
  is truly stuck anywhere stops reaching checkpoints and therefore stops
  renewing. **No independent renewal timer exists anywhere in the design**: an
  extension whose event loop and other timers are perfectly healthy cannot keep
  the lease fresh on behalf of a hung driver, because nothing outside the loop
  body can renew. The carrier is explicit: checkpoints that already execute a
  journal-writing op (drain page, injected fsync, delivered record) piggyback
  renewal on that op; the two checkpoints with no natural journal write — the
  completed empty wait cycle and the bounded park re-check — invoke the
  dedicated token-lane **`bus renew`** op (in the §2 inventory). The renewal
  op validates the reported state against the journal under the seat lock
  before accepting. `herder pi bus status` derives
  `driver: armed` from the progress-record age against a TTL sized above the
  largest legitimate checkpoint interval (wait-child timeout and park re-check
  period included, pinned in family config); the lease attests **driver
  progress, never timer liveness** (T29). Bind readiness (above) requires the
  fresh progress record, not the historical first drain.
- **Correctness split.** Only the drain is durable (journal + derived cursor);
  the wait child is the inherited anonymous `--wait` edge trigger with zero
  correctness weight (grok V4). A dead driver loses no messages — the events
  store is non-destructive and the next drain picks up from the cursor.
- **Runtime assumption, gated first.** That a Pi extension may run this loop —
  long-lived async work across turns, child-process spawn with explicit env,
  cancellation, reaping, in **TUI mode** — is exactly assumption **A9 (§10)**, and
  it is U1's **first gate**: no other U1 work builds on an unverified driver. The
  probe: TUI seat, isolated bus, seat idle across at least two full `--wait`
  timeout cycles (including a long-idle soak, 10+ minutes), then a real message
  delivered end-to-end without restart; then extension reload, session shutdown,
  and a forced loop failure while Pi lives (T28, T29). If A9 is falsified, the
  fallback shape is a **herder-supervised waiter**: a herder-side edge-trigger
  process wakes the extension, which still performs all drains and injection —
  the extension remains the binder-owner; only the blocking wait moves out. That
  fallback is a design change requiring a delta review, not a silent U1 swerve.

### Nudge policy (conditional on A3, explicitly)

The nudge turn — "possibly unprocessed messages: <ids> — they are in your context;
review and continue" — is an **id-only** reference. It is safe if and only if
injected content is durably part of the session (survives resume via the session
JSONL): that is precisely assumption A3. The dependency is explicit:

- **A3 verified** → id-only nudge as described; no content re-carriage.
- **A3 falsified** → the nudge must re-carry content: injected-unsettled ids are
  re-injected in full with the same visible envelope (id + payload hash), i.e.
  the duplicate-window posture becomes the recovery posture too.

U1 verifies A3 before the nudge wording is frozen; the reporting rules (`delivered`
only on settle-after-durable-injection) hold under either outcome.

**The nudge budget — nudges are bounded per id, structurally.** Without a cap, a
repeatedly aborting or crash-looping seat appends nudge records indefinitely for
an injected-unsettled id that never terminates — an unbounded record class on a
non-terminal id whose segment can then never rotate out (the spool bound depends
on this not existing). Each id therefore carries a **nudge budget** (default 3,
pinned in family config): each nudge journals against it, and on exhaustion the
id transitions to terminal **`undeliverable(stalled)`** — journaled, counted in
the registry's pending/undeliverable reporting, honest about what happened (the
content reached the session context per its injected record; confirmed delivery
never followed). Terminalizing restores the bounded per-id record set the
reserve arithmetic (injection policy) and segment rotation both depend on. A
human or orchestrator seeing `stalled` ids decides about re-sending; the seat
never loops.

### Failure and recovery matrix

| Scenario | Behavior |
|---|---|
| **Pi process exit or crash** (any point) | The extension dies with Pi — that is the design, not a failure of it (demo "Restart, crash, and message recovery"). Herder records the exit from process/pane evidence and relaunches via resume (DR-4). On the new `session_start` the extension activates a fresh epoch, replays the journal, and drains. Walked windows: *(a) crash after drain query, before journal fsync* — cursor never advanced; the non-destructive events store returns the rows again; deduped by event id. *(b) crash after queue, before inject* — replay finds queued-not-injected ids and injects them (exactly the demo's pending-replay clause). *(c) crash after the input event, before the injection record's fsync* — no durable injected record exists; replay treats the id as queued and **re-injects it with the same visible envelope (same id, same payload hash)** — the chosen at-least-once duplicate window, doctrine-framed so the model recognizes repeats (T30). *(d) crash after the durable injection record, before settle* — replay finds injected-unsettled ids; the extension issues one **nudge** turn per the nudge policy, whose settle delivers them. At-least-once into context, with duplicate-safe framing, per the demo's stated preference for at-least-once over loss. |
| **Turn aborted after injection** (user interrupt, provider error) | Id stays *injected*. Any later settle in the session delivers it; if the seat idles with injected-unsettled ids past a threshold, the extension issues the same nudge turn. No hcom-level resend ever fires. |
| **Extension handler throws** | Probe-proven containment: Pi emits `extension_error` and keeps serving (demo extension-lifecycle probe). The failing extension reports the error to seat diagnostics (log file) and the seat status degrades honestly (DR-6); Pi is not killed for it. |
| **Duplicate drain rows / replayed events** | Journal is id-keyed; monotonic states; a duplicate journals at most one repeat marker per id (K = 1 — further duplicates deduped without append), never re-injected (T5). |
| **Second Pi process on the same seat** (operator error, restart race) | `herder pi bus activate` refuses when a live activation exists: the activation record carries pid + process-start evidence, and it is stale only when that process is provably gone. A successful activation increments the epoch, so ops from the superseded process are rejected by the fence regardless of scheduling. Per-op flock serializes journal writers under any overlap (T10). |
| **Session switched/replaced inside Pi** (new/switch/fork from within the TUI) | The extension treats every `session_start` as a rebinding event (demo extension rule 6): re-activate (fresh epoch), re-verify session identity against seat state, replay pending. A session the seat does not recognize flags the seat for reconciliation rather than guessing (DR-4). The shutdown→reload→start replacement sequence is API-inventory, not probed (assumption A4). |
| **Rebind with an in-flight wait/drain child** | The prior epoch's wait or drain child may wake after the new activation. Its epoch is stale: any mutation it attempts is rejected under the lock and its output is discarded; the new driver's own drain picks the messages up from the cursor. Cancellation on rebind is attempted but carries no correctness weight (T32). |
| **Seat cull/retirement with undelivered ids** | Two-phase, crash-recoverable, in order: (1) persist the durable **`retiring`** phase under the seat lock — from here competing activations and all **non-retirement** mutations are rejected, while retirement's own steps remain permitted and idempotent, so a crash at any later point is recovered by re-entering `retiring` and completing (T31); (2) cancel and reap the driver's in-flight children (a live extension observes `retiring` and stands down; a dead one never contends); (3) stop the bus row and read-back confirm its absence (the proven grok activation pattern); (4) **final accounting, bounded** — the routed-but-undrained backlog is accounted **without materializing it on disk**: what fits under the spool's admission cap is drained normally (envelope records, oversize rules applying); the remainder — which under a quota-paused flood may be arbitrarily large — is captured by an anonymous count + id-range query over the events store and journaled as **one bounded summary record** (count, id range, query predicate), so the undeliverable accounting is exact per-id for journaled rows and exact by count/id-range for deferred rows, with per-id detail recoverable from the id-addressed events store on demand — never a snapshot of an incomplete journal, and never an unbounded journal either (the exact-counts lesson, T33); (5) mark all non-delivered journaled ids `undeliverable` (terminal, journaled, exact counts to the registry, deferred summary included in the reported total); (6) append the terminal **`retired`** record **last** — only now is every mutation rejected outright; (7) tear down pane and processes on process evidence (T24). |
| **Wake latency when idle** | The inbound driver's bounded `bus wait` child (anonymous `--wait` edge trigger; correctness never rides it — inherited grok V4). On wake or timeout the driver returns to drain. No daemon exists to die; a stuck `wait` child is a timeout, a fence-discarded straggler, and a fresh drain. |

### Reporting vocabulary

| Report | Meaning | Trigger |
|---|---|---|
| `queued` | Durably journaled for the seat | journal append + fsync |
| `delivered` | The definition above — nothing weaker | settle observed after journaled injection |
| `undeliverable` | Terminal non-delivery (`stalled` variant: nudge budget exhausted) | retirement sequence step 5; nudge-budget exhaustion |
| `inject-degraded` | Extension cannot currently inject (extension error, no session) | extension diagnostics / activation state |
| `driver-degraded` | Inbound pickup halted; reported from supervision or stale lease | backoff exhausted, top-level exit, or lease expiry |
| `control-degraded` | Live extension lacks the capability token; recovery is a controlled relaunch (attempt-protocol pre-exec rekey — never a live handoff) | status-derived, write-free: live recorded process + expired lease + no authenticated token-lane record, persisting past the escalation threshold (DR-2 item 4) |
| `spool-quota` | Draining paused at quota; backlog deferred in hcom, counted | quota check at drain |

### Persistence format

Append-only JSONL journal per seat (`<HERDER_STATE_DIR>/pi/<seat-guid>/journal.jsonl`),
fsync on the records that gate external claims (`queued`, `injected`, `delivered`),
state derived by replay, periodic snapshot records to bound replay cost — the house
pattern, built on the shared journal primitives (DR-1 reuse boundary). The seat dir
also holds, maintained only under the seat flock: the write-ahead reservation
record (bus name), the activation record (pid, process-start evidence, session
UUID), the monotonic ownership epoch, and the control-capability hash. Writers are
`herder pi bus` invocations only, serialized by per-op flock; the extension never
writes any of it from TypeScript.

**Two id spaces, never mixed.** Bus-routed messages are keyed by their hcom
event id; **local-origin records** — the boot doctrine, the spool-borne task
prompt, and any other herder-originated seat message that never transited the
bus — are a **distinct id class in their own namespace** (e.g. `local:<n>`).
Local ids are **excluded from cursor derivation** (the committed cursor is the
max fully-journaled *bus* event id, and only that) and **excluded from bus-id
dedupe** (a genuine bus event whose numeric id happens to equal a local
sequence number is a different key entirely). Their delivery receipts ride the
same queued → injected → delivered state machine unchanged. Without this
separation, U1 could mint local ids high (poisoning the cursor past real
boot-window traffic) or low (deduping genuine early events to death) — both
are structurally impossible with disjoint key spaces.

## DR-3 — Launch contract

**DECISION.** `herder spawn --agent pi` becomes a first-class family with a
herder-owned launch path in `launchcmd` (joining `claude|codex|gemini|grok`), execing
the provisioning-recorded vendor Pi entry point directly with a fully explicit,
constructed child environment
and argv. Nothing routes through an `hcom <tool>` launcher (hcom 0.7.23 does ship
a native `hcom pi` launcher — characterized and ruled out for production seats;
the keep-custom decision, evidence base above).

### Provisioning (vendor install resolved and recorded — not pinned)

The former machinery here — pinned install into an immutable
`<HERDER_STATE_DIR>/pi/install/<version>/` prefix, tarball + CLI-entry hash
verification, Node runtime pin, supported-version launch gate — is **dissolved
by the 2026-07-14 default-homes ruling** (default install location,
vendor-updated). What replaces it:

1. **Vendor install, default location.** Pi is installed and updated by its
   vendor mechanism at the default install location — the operator's normal
   `@earendil-works/pi-coding-agent` install. Herder does not install, pin,
   hash-verify, or version-gate it. `herder pi provision` (the provisioning
   surface) resolves the vendor Pi entry point normally — PATH/shim resolution
   semantics are decided in the implement unit and documented, the same pattern
   the grok default-homes unit follows — records the resolved path and the
   **observed vendor version** in family state, and refuses only when no Pi is
   resolvable at all (cause+remedy naming the vendor install step). The demo's
   0.80.6 tarball hash
   `2a77634640b2d86d90d24087bb67559ecf2366e0fb52a42c55eed416147da411` and
   CLI-entry hash
   `af302f231437eaf6f37691bce4b34234fcb626bcb5eb3910d4fc3f6519bf78ca` remain in
   this document as characterization provenance only.
2. **Version recording at launch.** Every launch re-reads the installed version
   (from the installed `package.json` — reading it creates no state) and records
   it in the seat's registry row and journal. There is no supported-version
   refusal: vendor updates land when the vendor lands them (the claude/codex
   fleet norm the ruling extends). Compatibility protection moves to the binding
   surface: the managed extension records the Pi extension-API surface it was
   built against and **refuses to claim** on mismatch — the seat flips a legible
   incompatibility state and the launch hard-fails with a cause+remedy error —
   rather than half-binding against a drifted API. The drift consequence
   (characterization pins can be silently invalidated between launches by a
   vendor update) is stated honestly in §12 item 9, not papered over.
3. **Extension install** (once per extension version): the managed extension is
   installed into the default home's `agent/extensions/` — the same surface
   hcom's native Pi integration uses — with its version recorded in family
   state. It activates only per the **activation predicate below** and is
   provably inert in the owner's interactive Pi runs (T18 tests the predicate,
   not just the smoke). This is the one
   herder-owned artifact in the default home; herder writes no other owner Pi
   state (§1).
4. **Operator capability mint** (once per family): `herder pi operator init`,
   run from the same owner-run non-seat context as provisioning, per the DR-2
   operator-capability lifecycle. External-lane ops fail closed with a
   cause+remedy error until it exists.

**The extension activation predicate — specified, not asserted.** The shared
extension loads into every Pi process in the home, including the owner's
interactive runs, so "inert without seat coordinates" is load-bearing and gets
an exact predicate. On `session_start` the extension activates **iff both
of**:

1. the **complete** seat coordinate tuple is present in its process
   environment — `HERDER_STATE_DIR` **and** the seat GUID variable (the exact
   variable names are fixed in U1 and recorded in family docs; the tuple is
   closed, not open-ended); **and**
2. the tuple resolves to existing herder seat state whose **open launch
   attempt records this very process** — the DR-2 gated-child record's pid +
   start-time equals the extension's own process identity — proceeding to
   bootstrap consumption and token-authenticated `activate`. A same-process
   rebind with the token held is authenticated by that token per DR-2
   lifecycle item 2 and does not re-evaluate the environment.

**The memory-lost reload (DR-2 item 4) is deliberately NOT a predicate
branch.** A reloaded, tokenless extension instance is
provenance-indistinguishable from a model tool child: both descend from the
same Pi process, so **no process-identity proof can separate them**, and any
diagnostic write authorized by such a proof would be a tokenless
seat-mutation path a model tool could take verbatim (exec `herder pi bus
activate` from an in-seat shell). The reloaded instance therefore behaves
exactly like every other unauthenticated caller: **fully inert on the
control plane** — no activation attempt, no write, nothing. Detection of the
lost token is **external and write-free**, from the authenticated side's
silence: DR-2 item 4's lease-decay derivation. There is **no code path by
which any tokenless caller — whatever its provenance — causes any journal
append or seat-status transition** (T34(a), T34(e) spoof branch).

The predicate is evaluated **before the bootstrap file is touched**: a process
that fails it never reads or unlinks a bootstrap, so an ambient-coordinate
interloper cannot consume an in-flight launch's trust root even accidentally.
Every failure mode is **inert, fail-closed, and silent in-process**: no
coordinates, a **partial** tuple, a tuple that does not resolve, no open
attempt, an open attempt recording a **different** process
(the stale `HERDER_*` exports of an owner shell that previously operated
seats — the ambient case), or a live activation with the token lost (the
reload case above) all behave identically — no seat claim, no
`activate`, no bus
ops, no seat-state or journal writes, no bootstrap read/unlink, and zero bytes
to the model context or pane (T25). Silent non-activation cannot mask a broken
seat launch: the noisy signal lives on the launch path, where spawn's
status-op bind capture hard-fails on no-bind with confirmed cleanup (launch
sequence step 5). T18 pins the predicate branch by branch.

### Seat construction (per seat, at spawn)

There is no per-seat Pi home. The former `PI_HOME` translation (per-seat `HOME`,
`PI_CODING_AGENT_DIR`, `PI_CODING_AGENT_SESSION_DIR`, XDG roots under the herder
state root) is dissolved by the ruling; the demo's mapping remains valid
evidence that those variables work, and herder deliberately does not set them.
A seat is exactly:

- **Herder seat state** under `<HERDER_STATE_DIR>/pi/<seat-guid>/`: recorded
  absolute hcom path, reservation record, journal, activation/epoch records
  (DR-2 persistence) — unchanged; this is herder state, not Pi state.
- **Pi state = the live default home**, shared with every other seat and with
  the owner's interactive Pi use: default agent dir, default session root,
  default XDG roots. Consequences, stated rather than assumed away:
  - Sessions land in Pi's **shared default session root**. Seat↔session binding
    rides the recorded session UUID plus process evidence (DR-4); the former
    forced per-seat session root — and with it the sid-glob discovery
    fallback — dissolves (DR-4; delta in §12 item 9).
  - **No owner-config writes**: no `agent/settings.json` seeding (env flags
    carry startup suppression), no `agent/models.json` writes (a
    provider/model needing a custom entry is owner-managed state; an absent
    entry fails at Pi level with Pi's own error).
  - The **extension** is the shared default-home install (provisioning item 3);
    the seat records the extension version it launched against, and a
    version-mismatched extension refuses to claim rather than half-binding.
  - The owner's own **user-level resources** in the default home (their
    extensions, tools, settings) load into seat processes as a matter of
    course — owner-trusted per the fleet norm the ruling extends, recorded as
    a delta (§12 item 9).

Project `.pi/` resources stay untouched in the workspace (demo: they are project
content, not seat state) — but **herder seats do not load them until the trust
surface is characterized**. A workspace-local `.pi/` can carry executable
resources (tools, extensions) that would load into a process holding the seat's
provider credential and control coordinates; the demo characterized the state
layout, not the trust lifecycle's behavior under autonomous launches. Until probe
P6 (§10) characterizes the installed CLI's trust controls, launch must ensure project
`.pi` resources are not loaded (trust withheld / disabled by an **enforceable**
mechanism of the installed surface, verified in U2), and the seat runs on the
default home's user-level resources only (owner-trusted — above). **Falsification branch, explicit as A9's:** if P6
finds no enforceable suppression surface in the installed CLI, that is a design
delta — U2 neither invents cwd/resource isolation nor quietly weakens the
default; the delta decides among blocking activation on the gap, an upstream
ask, and owner-ruled acceptance. Per-workspace relaxation is an owner decision
(§12), made on the characterization evidence — never a silent default.

### Launch sequence (ordering: the bus name must exist before doctrine can name it)

1. Open the seat's **launch attempt** (DR-2 launch-attempt protocol — every
   subsequent step, including the capability bootstrap, is attempt-keyed). Mint
   seat GUID; resolve session identity per DR-4. The GUID is always recorded in
   the registry pre-launch; the session UUID is recorded pre-launch only on
   DR-4's preassignment branch — on the extension-published capture branch it
   is recorded at activation, when it first exists (the sid-glob branch is
   dissolved — DR-4).
2. Reserve the bus identity via `herder pi bus reserve` (write-ahead reservation,
   `hcom start`, pinned de-latch — DR-1). Reservation proves a roster row exists;
   it claims nothing about a live seat. Restarts reclaim by the reserved name.
3. Compose the doctrine message (bus name, addressing rules, `herder pi send`
   mandate, credential rule: never print or persist key material, duplicate-replay
   framing, silence expectation) and enqueue it as **local-origin** spool record
   `local:1`; enqueue the task prompt (`--prompt`) as `local:2` (the distinct id
   class of DR-2's persistence rules — never bus event ids, never in cursor
   derivation or bus dedupe). Argv carries no prompt content — large/multiline
   prompts avoid argv entirely, and both messages get real delivery receipts
   through DR-2, mirroring grok's spool-borne initial prompt.
4. Exec Pi (the provisioning-recorded vendor entry point) inside the pane with the
   constructed environment and explicit argv, **gated**: the child's pid +
   start-time is recorded into the open attempt (fsynced under the seat lock)
   and the child is placed into the per-seat cgroup **before its exec
   proceeds** — DR-2 launch-attempt protocol step 3; no live seat process ever
   precedes its record.
5. `session_start` fires in the extension: `bus activate` (fresh epoch, process +
   session evidence, capability hash — DR-2 "Seat ownership"), journal replay,
   driver start and first drain, batch-inject doctrine + task. Spawn's bind capture
   is **status-op-authoritative on the bind-readiness definition**: spawn confirms
   the seat via a `herder pi bus status` read-back showing the current runtime
   epoch AND an armed inbound driver (the grok activation lesson — never infer
   bind from hcom row-state transitions, and reservation alone is not a live
   seat); a no-bind within the window hard-fails the spawn with confirmed cleanup,
   never a silent green.

### Child environment — allowlist, not inheritance

The launch process (running inside the pane, as `launchcmd` does for other
families) **constructs** the child environment rather than filtering its own:

- `HOME` = the operator's live home (the default-homes ruling): **no Pi
  state re-points** — no `PI_CODING_AGENT_DIR`, no
  `PI_CODING_AGENT_SESSION_DIR`, no XDG overrides; Pi resolves its defaults.
  Plus a `PATH` floor and herder shims; `HCOM_DIR`; herder seat/state
  variables (`HERDER_STATE_DIR`, seat GUID, bus name). Of these, the
  activation predicate keys on **exactly** `HERDER_STATE_DIR` + the seat
  GUID variable (the closed tuple — DR-3 predicate); the bus name is
  **wrapper-only** (`herder pi send` addressing) and carries no activation
  weight.
- `PI_OFFLINE=1`, `PI_TELEMETRY=0` — retained as **seat-scoped launch-env
  deltas**, the mechanism the ruling expressly permits. Their meaning under
  vendor-updated installs is narrower than before and stated exactly: they
  suppress in-process update checks and telemetry for the seat, so the
  version recorded at launch stays true for that process's life. They do not
  — and are not meant to — keep the vendor install from updating between
  launches.
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
(`/proc/<pid>/environ`) and verify the constructed environment (the env-delta
flags, exactly the one named provider credential by name, the seat coordinates,
and the **absence** of Pi state re-point variables; never credential values).
Assertion failure is a launch failure with
teardown, not a warning. The activation unit (§13) owns producing the
characterization evidence; only after it shows direct-exec preservation may the
ceremony be removed, as its own reviewed change. This design does not resolve the
conditional on paper.

### Flag mapping and refusals

| herder intent | Pi argv / mechanism |
|---|---|
| always | explicit session identity per DR-4 under Pi's **default** session root — no `--session-dir` and no session-root re-point on any path (the refusal list below fences passthrough attempts; DR-3 seat construction fences herder's own env); no prompt in argv |
| `--model X` | Pi model selection for the seat's provider (exact argv per the installed version's CLI; recorded at implementation) |
| resume | exact session selection (`--session`/`--session-id` family — demo session table) |
| fork | `--fork` with parent session (demo session table) |
| autonomy modes | **unmapped pending characterization** — the demo did not characterize Pi's interactive approval surface; probe A6 (§10) answers it; any bypass-like mapping is an owner decision (§12), per the grok precedent |

Passthrough args that collide with the contract are **refused with an error, never
silently reconciled**: anything selecting or re-pointing sessions or session
directories, `HOME`/state-root re-points, offline/telemetry/update-behavior
overrides, credential or auth-file arguments, extension-path arguments, and
`--no-session` (a first-class seat is always a durable session; DR-4 depends on it).
The refusal list is finalized against the full CLI surface of the vendor version
current at the launch unit and pinned by test (T20), exactly as grok's T19; a
later vendor version can introduce flags the list does not know (the §12 item 9
drift delta — new flags are not auto-refused).

## DR-4 — Identity, sessions, lifecycle

**DECISION.** Seat identity binds on **seat GUID + process/pane evidence + session
identity**, never on cwd. Pi's session files are cwd-labeled in their headers, and
under the default-homes ruling **all seats and the owner's interactive use share
one default session root** — so no cwd-keyed and no root-keyed claim path may
exist in any code path (the grok DR-4 rule, inherited and tightened: the shared
root makes location-based claims strictly less meaningful than before).

**Session identity: preassign if the installed CLI allows it; otherwise
extension-published capture.** The demo proved exact resume (`--session`,
`--session-id`), forking (`--fork`, parent-linked), and forced session roots — but
did not probe whether a **new** session's UUID can be preassigned at launch. The
grok fork erratum is the precedent in both directions: preassignment is the
preferred identity model, and vendor flag surfaces can turn out to support it on
inspection. Resolution order, decided here:

1. Probe the installed CLI for new-session preassignment (P1, §10). If supported,
   launch mints a UUIDv7, records it pre-launch, and verifies it post-boot — the
   grok model.
2. If not supported, the extension **publishes** the session identity: on
   `session_start` it reads the live session UUID from its extension context
   (API-inventory surface, assumption A5) and writes it to seat state through a bus
   op. spawn's status-op read-back binds it with process/pane evidence. The
   former third fallback — sid-glob discovery under a forced per-seat
   `sessions/` root — is **dissolved by the default-homes ruling**: it was
   viable only because that root contained exactly this seat's sessions, and
   the shared default root contains every seat's and the owner's sessions, so
   a glob cannot identify this seat's new session. If P1 and A5 both fail,
   session identity is **blocked for a design delta** — no unit improvises
   discovery against the shared root (§12 item 9).

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

**Cull/retire**: the two-phase fenced retirement sequence of DR-2's recovery
matrix — durable `retiring` first, then child cancel/reap, row-stop + read-back
row-absence confirm (proven live in grok activation), **bounded final accounting
for exact counts**, undeliverable marking, terminal `retired` last, process/pane
teardown.
Cull is external-lane (operator-capability-authorized): it needs neither the seat token nor a live
extension (DR-2 lifecycle item 6). Seat state is retained for audit. Registry
lifecycle transitions require process-level evidence (pid exit, pane death),
never session events.

**Subagents.** Pi's extension API inventories tool/subagent-adjacent events, but the
demo recorded no subagent lifecycle hazard and no subagent kill-switch flag. Unlike
grok, Pi's delivery receipts do not depend on model-side ack authorship — delivery
is extension-observed — so a subagent cannot forge a delivered receipt. The residual
risks are context/credential shaped (a child inherits the provider key: inherent,
demo-documented) and identity-shaped (a subagent session must not rebind the seat —
covered by the session-drift rule above). Probe P4 (§10) inventories Pi's actual
subagent surface at the installed version; if a disable flag exists, the launch unit
adds it to the always-argv as hardening, with a design note, not a soundness
requirement.

## DR-5 — Multi-provider surface and least privilege

**DECISION.** A seat declares its provider explicitly at spawn; herder filters the
environment to exactly that provider's credential; provider changes are supervised
relaunches. Nothing guesses.

**Spawn syntax.** `herder spawn --agent pi --provider <family> [--model <id>]`.

- `--provider` is **required** (no default pending the owner ruling, §12). The
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
  provider: owner decision (§12), grok precedent (owner pinned grok-4.5 after
  design).
- The registry row records `provider: <family>` and the requested model.

**Least-privilege filtering at exec.** The DR-3 allowlist includes exactly the one
credential name from the provider table — by name, value never inspected beyond
nonempty, never logged. Pi's tools and extension children inherit the Pi process
environment (demo: "a seat must receive only the credential required for its
selected provider"). Scrub claims, stated exactly: bus-op children **spawned by
the extension** are built with an explicit env excluding the credential (DR-1);
the model-run `herder pi send` helper, by contrast, is a tool child like any
other — it **inherits** the seat's provider key, sits inside the accepted
model-tool credential boundary (threat model), and excludes the key only from the
**hcom child it spawns** (asserted in T16 on the hcom child, not on the helper).
An extension-registered send tool that would close even that inheritance is a
possible refinement behind a tool-registration probe, not a shipped claim.

**Credential-bearing owner config under the default-homes ruling — the env
channel is the scoped channel; every other source is owner state.** Pi
resolves credentials from an explicit CLI key, `agent/auth.json`, environment
variables, or custom-provider (models) config (demo "Provider routing") —
**four sources, each dispositioned, none omitted**: the explicit CLI key is a
herder-controlled surface (herder never passes one, and credential/auth-file
passthrough arguments are on the DR-3 refusal list); the environment is
herder-constructed (below); the auth store **and** custom-provider/models
config are **owner state in the live home**, and both are open channels under
the ruling. Under the managed home this design required the store
credential-empty at launch, digest-checked it at bounded runtime checkpoints,
and terminated the seat on drift (and the seeded `models.json` was
herder-controlled). That contract **dissolves with the managed home**: these
files are now the owner's live config in the shared default home. The owner
may legitimately populate them at any time (interactive `/login`; a custom
provider entry for their own use), so a launch gate on their contents would
refuse seats because of ordinary owner action, and terminate-on-drift would
kill healthy seats when the owner logs in — machinery that polices the
owner's own state is not retained. What remains is stated exactly, delta
included:

- **Env-channel scoping — retained by the ruling.** The launch env carries
  exactly one provider credential, by name (the DR-3 construction; T17, T21).
  Through the environment, a cross-provider switch still cannot obtain a
  credential. This property is unweakened — and it is the **only**
  single-provider claim this design makes anywhere; every "one provider per
  seat" statement in this document means the herder-routed env channel.
- **Owner-config channel honesty — the delta, owner-signed (§12 item 9a).**
  Whatever credentials the owner's live auth store or custom-provider/models
  config hold are reachable by every seat process through Pi's own resolution
  order — **in-band, through the vendor's normal credential lookup, with no
  deliberate acquisition required**. On a machine where those files carry
  other providers' credentials, single-provider-per-seat is a policy honored
  on the env channel only, not a property of the seat. The design does not
  claim otherwise anywhere. Whether Pi actually prefers a file-sourced
  credential over the env credential, and whether in-process cross-provider
  selection succeeds — from either file source — is register **A10** (§10) —
  retained to **size** this delta for the owner, no longer to calibrate a
  termination machinery.
- **Tightening where the surface allows (P7, re-scoped).** If the installed
  CLI offers a **per-invocation** surface that disables credential-bearing
  file sources — auth-store reads, auth mutation, and custom-provider
  credential config — (env flags or argv switches: seat-scoped launch-env
  deltas, the mechanism the ruling permits), launch pins it and those
  channels close for seats without touching owner state. A surface covering
  only some sources closes only those, **stated per-source, never rounded up
  to "closed"**. If no such surface exists, the delta stands as ruled; there
  is no fallback machinery to reintroduce.

**Cross-provider change = controlled relaunch** (settled). Herder-side: a relaunch
op that retires the running process (resume semantics, same seat), rebuilds the
environment for the new provider, and relaunches into the same session. Whether the
same conversation is *coherent* across a provider change is Pi's business (its
sessions record model changes — demo session families); herder's contract is only:
never two provider credentials in one process environment, ever. Extension-side:
`model_select` is observed (API inventory); an in-process model change that crosses
provider families is flagged to the registry as a provider-drift warning, making
the outcome legible either way. Through the **environment** such a switch cannot
obtain a credential (the env filter is the demo's least-privilege observation);
through the **owner's live store** it can, whenever that store holds such a
credential — the design does **not** claim cross-provider inference "cannot
succeed" in-process; it claims exactly the contract above: env-channel scoping,
store-channel delta stated and owner-signed (§12 item 9), P7 tightening if the
surface exists. Credential-name mapping stays per-harness (the demo's
Codex `CODEX_API_KEY` lesson):
the table above maps names for **Pi**, and no name aliasing for other harnesses
leaks into a Pi seat.

## DR-6 — Observability, sesh, and honesty

**DECISION.** Every observation surface reports only what its evidence supports,
with the source labeled — grok DR-5, applied to Pi's surfaces.

- **Transcript** = the seat's session JSONL under Pi's default session root
  (shared under the default-homes ruling), located by
  session UUID from seat state — the UUID, never a root scan, is the locator. The observer gets a Pi adapter for the JSONL tree
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
  lifecycle-derived status to seat state: `idle` and `turn-active` from the
  **probe-proven** agent start/end/settled stream; `tool-running` only from tool
  events, which were **API-inventory in the demo, not probe-proven** — that label
  ships only after U1 observes real tool events, and until then the surface simply
  omits it (same evidence class the demo's review corrected on injection: never
  promote inventory to proven by paraphrase). Herder surfaces all of it as an
  explicitly labeled secondary source (`status(pi-ext): …`), never mapped into
  herdr's native vocabulary — the honest-unknown principle, which held under
  mutation in the grok observer unit.
- **Registry rows** say `tool: pi` with capability flags reflecting proven state:
  `bus: reserved|bound` (bound = the DR-2 bind-readiness definition: current
  runtime epoch read back + driver armed; reserved = roster row only),
  `pending: <n>` (queued/injected not yet delivered, exact counts),
  `inject: ready|degraded`, `driver: armed|degraded` (lease-derived — DR-2),
  `spool: ok|quota`, `control: ok|degraded`,
  `provider: <family>`, and `session-drift` when DR-4 flags it. (The former
  `auth: ok|drift` flag is removed with the DR-5 store contract — it policed
  what is now the owner's live store.) A row never
  implies capability the seat has not proven.
- **Diagnostics** (extension errors, bus-op failures, nudge history) go to seat log
  files under the seat dir, never to the pane or the model context (T25).

## 9. Threat model (house-inherited; stated, not invented here)

Herder families — this one, grok, and every other — run under the house's
**cooperative same-UID trust model**: every process in a seat (Pi, its tools, bus
ops, herder itself) shares one OS user, and a same-UID actor that writes seat state
out-of-band (forging journal records, activation files, or the capability hash
under the seat dir) is **out of scope for this design**, exactly as it is for the
grok family and the rest of the fleet. Changing that would be a house-wide
platform decision (separate UIDs, kernel-enforced boundaries), not a per-family
one, and this design deliberately does not attempt it unilaterally.

What this design does enforce, inside that model, is the **in-band boundary**: the
control plane is not reachable through the interfaces the model actually has —
its context, its tools' argv/env inheritance, and the seat coordinates that
necessarily appear in the environment. That is the DR-2 control capability
(memory-held token, stdin-delivered, hash-verified under the lock) plus the
deliberate reachability split (send + read-only status, nothing else). Stated
plainly: the boundary defends against prompt-induced misuse of herder's own
surfaces, not against arbitrary same-UID code, and every honesty claim in DR-2/DR-6
is scoped accordingly. Two residuals of the capability lifecycle are inside the
same concession and named here rather than hidden: the bootstrap file's
rekey-to-`session_start` window — which necessarily **straddles exec** (the
process is live but pre-model) and is bounded by the three-part plaintext
invariant (DR-2 lifecycle item 3: written only into a seatless window, existing
only until consumption before any model turn, absent for the rest of the
process's life), so **this seat's model can never be the reader**, and the
residual reader is hostile same-UID code from outside the seat during that
pre-model window; and the external lane's **operator capability**, whose
boundary is explicit possession, stated exactly: the supported surface refuses
**naive/direct self-operation** — no capability in any seat's env or herder
seat state, and no herder-issued context names its location; **no herder
surface auto-acquires operator authority from caller-controlled context** (the
caller pipes the credential to stdin, or the op refuses on empty stdin); and
the direct in-cgroup path is belt-refused. One honesty correction under the
default-homes ruling: the earlier "not in any seat's home" clause no longer
holds — every seat's `HOME` **is** the operator's live home, so the credential
file sits inside it at an owner-chosen (conventionally well-known) path, and
the former managed-home belt refusal is gone (§12 item 9). Reading that file
by path remains the conceded deliberate-acquisition class below; the boundary
(possession + no auto-acquisition) is unchanged, but the acquisition the
concession covers got mechanically easier, and sign-off covers that.
What it does **not** claim: that brokered launches are refused as a class. A
model that deliberately composes a broker with a credential-path redirect —
or reads the credential file by absolute path, or exfiltrates it — is
executing **deliberate credential acquisition**, the same house-wide
cooperative same-UID out-of-band class as journal forgery, conceded once,
fleet-wide, predating this family (the grok control plane ships under the
same model), and made **load-bearing for lifecycle authority here** (owner
sign-off covers this explicitly — §12 item 8). Sanctioned use is the fleet's
norm: operator shells and live-home orchestrator seats pipe the capability
from their own config and operate other agents' lifecycles through the herder
CLI; a target model persuading a sibling to operate its lifecycle rides that
same conceded path. All of it is exactly as strong as the cooperative model
it lives in.

---

## 10. Assumption register (evidence gaps → verify in the implement units)

Every entry is conservative in the design above and carries a named verification.
None may silently become load-bearing beyond its stated fallback.

Under the vendor-updated ruling there is no pinned version: every probe result
below is evidence about the vendor version **observed and recorded at probe
time**, and a vendor update re-opens the probes whose surfaces it touches (the
§12 item 9 drift delta). "The installed CLI" below means exactly that.

| # | Assumption / gap | Design posture | Verify |
|---|---|---|---|
| A1 | **Reply-content capture**: the demo validated injection to `agent_settled` but did not capture the reply. | `delivered` claims turn completion over a context containing the message — nothing about the reply (DR-2). | U1 probe: capture the injected turn's reply via the extension event/message stream; if capturable, add reply-hash journaling as an audit nicety (not a delivery precondition). |
| A2 | **Steering/mid-stream delivery**: `sendUserMessage` delivery options are API-inventory only. | Idle-gated injection; mid-turn arrivals hold to the settle boundary. | U1 probe: exercise streaming delivery options; if proven, a later unit may relax the idle gate as its own reviewed change. |
| A3 | **Injected input persists in the session JSONL** (crash/resume durability of injected content). | The id-only nudge is safe **only if A3 holds** — the nudge policy (DR-2) is explicitly conditional on this verification's outcome; if falsified, nudges re-carry content with the same envelope. | U1 probe (before the nudge wording freezes): inject, then inspect the session file for the entry; resume and confirm the content survives. |
| A4 | **Session replacement sequence** (shutdown → reload → start) is inventory, not probed; so is whether extension reload preserves module memory. | Every `session_start` is a rebinding event; unrecognized sessions go to `session-drift`, never adopt. Memory-lost reload is designed for either way (`control-degraded` → controlled relaunch under the launch-attempt protocol, DR-2 — never a live handoff); the probe determines how often that path fires. | U1/U3 probe: in-TUI new/switch/fork while bound; extension reload with token-retention check. |
| A5 | **Extension can read the live session UUID** from its context. | Used only if P1 (preassignment) fails; **no fallback behind it** — the former sid-glob fallback dissolved with the per-seat session root (DR-4), so P1 and A5 both failing blocks session identity for a design delta. | U1 probe (an extension-surface question, answerable in U1's harness) — run regardless of P1's outcome so the fallback provably exists before U2 resolves P1, which is a U2 CLI probe. |
| A6 | **Pi's interactive approval/autonomy surface** is uncharacterized. | Autonomy mapping left unmapped; seat runs Pi defaults until characterized (DR-3). | U2 probe: installed-version approval surface inventory; owner ruling for any bypass-like mode (§12). |
| A7 | **TUI-mode extension parity**: probes ran in RPC mode; docs state the same extension contract loads in tui/rpc/json/print. | Design assumes parity for lifecycle + injection only (the documented contract), nothing UI-dependent. | U1's first TUI-mode extension smoke — before anything else builds on it. |
| A8 | **Extension child-process control**: the extension can spawn bus-op children with an **explicitly constructed env object** (no inheritance — DR-1: no provider credential, no tool signals), feed stdin (capability token), kill, and reap them. | Every extension→bus-op spawn uses the explicit-env + stdin-token shape; T13/T17 assert against the bus-op process itself. | U1 unit test in TUI mode, asserting the bus-op child's actual environ and stdin handling. |
| A9 | **Inbound driver runtime viability**: a Pi extension may run long-lived async work across turns in TUI mode — the DR-2 driver loop with child spawn, cancellation, reaping. All post-boot delivery rides this. | The driver is specified as a component (DR-2); **U1's first gate** — nothing else in U1 builds on an unverified driver. Falsification triggers the herder-supervised-waiter fallback via delta review, never a silent swerve. | U1 FIRST-GATE probe: TUI seat, isolated bus, idle across ≥2 full `--wait` timeout cycles + 10-minute soak, real end-to-end delivery without restart; then extension reload, session shutdown, forced loop failure while Pi lives (T28, T29). |
| P1 | **New-session UUID preassignment** at launch (and composition with `--fork`). | DR-4 resolution order: preassign if supported, else A5 publication; both failing blocks for a design delta (sid-glob dissolved — DR-4). | U2 probe against the installed CLI (`--help`/docs inspection first; execution probes under the seat launch env shape). |
| P2 | **`hcom start --as <name>` fresh-mint behavior** — and, if reclaim-only, **whether a never-de-latched placeholder is routable** (tag/broadcast fanout) before the ~30 s finalizer. | Write-ahead reservation prefers herder-minted `--as` (no-second-row claimed for that shape only); the reclaim-only fallback requires proven non-routability, or identification-plus-stop of candidate orphans, or it **blocks for a design delta** (DR-1). | U1 probe on an isolated scratch bus, both questions. |
| P3 | *(retired during drafting — number retained so later probe cross-references stay stable; no open question lives here.)* | — | — |
| P4 | **Subagent surface inventory** at the installed version. | No soundness dependency (DR-4); disable flag adopted as hardening if present. | U2 probe. |
| P5 | **Per-provider residual network** under `PI_OFFLINE=1` (strace-proven for one Anthropic call only). | Offline flags required regardless; claim scoped to the demo's one-provider evidence. | Activation-unit integration check per activated provider. |
| P6 | **Project `.pi` trust surface**: what mechanism the installed CLI offers to withhold/disable project-resource loading, and what an autonomous launch does by default. | Herder seats must not load project `.pi` resources until characterized (DR-3); per-workspace relaxation is an owner decision (§12). **Falsification branch:** no enforceable suppression surface → design delta (block activation / upstream ask / owner ruling), never U2 improvisation (DR-3). | U2 probe against the installed CLI in a scratch workspace carrying decoy `.pi` resources. |
| P7 | **Owner-config channel tightening surface** (re-scoped by the default-homes ruling): does the installed CLI offer a **per-invocation** way — env flag or argv — to disable the credential-bearing file sources for one process: auth-store reads, `/login`/auth mutation, **and** custom-provider/models credential config? | Env-channel scoping ships regardless (DR-5). Whatever the surface covers is pinned as a seat-scoped launch-env delta and closes exactly those sources for seats without touching owner state — **per-source, never rounded up to "closed"**. Uncovered sources stand as the ruled delta (§12 item 9a) — the former detect/terminate machinery is not reintroduced. | U2 probe against the installed CLI, per source. |
| A10 | **File-source-vs-env resolution on a live seat** (re-scoped): the demo enumerated Pi's credential sources against *empty* stores/config; under the default-homes ruling the auth store **and** custom-provider/models config are the owner's live files and may legitimately hold other providers' credentials. The questions, per source: does Pi prefer a file-sourced credential over the managed env credential, and does in-process cross-provider selection from that source succeed? | The DR-5 delta conservatively assumes file-sourced credentials are usable by the seat. The probe **sizes the delta for owner sign-off** (§12 item 9a): proof that a source is ignored while an env credential exists, or that cross-provider selection from it is hard-blocked, shrinks the stated delta **for that source**; any weaker result — including "env wins same-provider collisions" — leaves it stated at full width. No termination machinery hangs on this any more. | U2 probe riding P7, per source: with the managed env key present, place an **alternate-provider** credential in a scratch stand-in of each file source (auth store; custom-provider/models entry) and attempt selection + inference on that provider; record each result against the delta statement. |
| A11 | **Per-seat cgroup scope availability — for accounting, not authorization**: cgroup v2 per-seat scopes on the deployment platform (seat processes placed into a dedicated cgroup at the launch gate; membership readable from `/proc/<pid>/cgroup`). Authorization does **not** ride this — cgroup membership is location, not causal origin (a same-UID launch broker exits it freely); the external lane rides the operator capability (DR-2 lanes). | Used for: the cgroup-empty quiesce sweep (reparented-straggler kill correctness), process accounting, and the defense-in-depth belt refusal of direct in-target-cgroup callers. **If falsified:** quiesce degrades to recorded-pid-only kills with the straggler residual named in the registry — a design delta on the quiesce contract only; the authorization boundary is unaffected. | U1 probe on the real spawn path: launched seat lands in its scope; the sweep sees a double-forked descendant; the belt refusal fires for in-cgroup callers. |

Probes that require running the Pi binary happen inside the implement units under
the seat launch-env shape (env deltas + scoped credential — DR-3); default-home
state writes from probe runs are ordinary under the ruling, and probes that would
mutate owner-meaningful state (auth store, settings) use scratch stand-ins for
that state rather than the live files. Probes that require inference additionally
need the owner spend ruling (§12).

## 11. Test and gate plan (contracts the implementation units must ship)

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
- **T5 duplicate drain rows** — id-keyed dedupe; at most one repeat marker per
  id (K = 1), a duplicate flood past K appending nothing; single injection.
- **T6 crash before inject** — restart replay injects queued ids exactly once.
- **T7 crash after inject, before settle** — no re-injection; single nudge turn;
  delivered on its settle.
- **T8 extension handler failure** — `extension_error` contained; diagnostics to
  file; seat flips `inject-degraded`; Pi process untouched.
- **T9 whole-process crash → herder restart** — resume relaunch; identity reclaim
  (`--as`); fresh epoch; replay; pending drains and delivers; no receipt
  regression.
- **T10 activation exclusivity and epoch fencing** — second live activation
  refused; stale activation (dead pid) superseded by a fresh epoch; ops carrying
  a superseded epoch rejected under the lock; concurrent bus ops serialized by
  flock with no journal interleaving.
- **T11 journal ordering** — ascending-id sort before append under hostile page
  ordering; crash after partial page fsync loses nothing (the inherited V9 pins,
  re-run through the Pi op path).
- **T12 reservation + de-latch** — write-ahead record fsynced before the identity
  invocation; the de-latch is exactly the proven `hcom list --name <name> --json`
  (re-pinned: no second identity, no unread-state advance, single roster row,
  send-accepted immediately after); a reservation left as a bare one-shot start
  provably gets finalized `launch_failed` by hcom (the hazard pinned, not just
  the fix); crash injected between `hcom start` and the de-latch recovers by the
  reserved name without minting a second row **in the preferred fresh-mint
  shape**; the reclaim-only branch is tested per the P2 outcome (proven-inert
  orphan, or identification-plus-stop of placeholder rows, else the branch is
  blocked for delta and the test asserts the block).
- **T13 identity env allowlist, both layers** — (a) bus ops invoked from an
  environment carrying hostile `CLAUDE*`/`CLAUDECODE`/`HCOM_PROCESS_ID`/
  `CODEX_THREAD_ID` still bind adhoc, never trigger hcom hook installation, and
  never route through a foreign identity; (b) the **bus-op process itself**, as
  spawned by the extension, carries the explicit minimal env — no provider
  credential, no tool signals — asserted on the child's actual environ, not just
  on the hcom grandchild.
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

- **T17 child environment** — constructed, not inherited: exactly one provider
  credential by name; `PI_OFFLINE=1`/`PI_TELEMETRY=0` present; **no Pi state
  re-point variables** (`PI_CODING_AGENT_DIR`/`PI_CODING_AGENT_SESSION_DIR`/XDG
  overrides absent) so the default home resolves, verified in the live process
  env (`/proc`, one-time post-spawn assertion — conditional clause active); no
  credential value in argv or in **anything herder writes** — registry, seat
  state, journal, launch records, logs; owner files in the live home are not
  policed and a conforming default-home seat (owner store/config populated)
  cannot fail this test; and the extension's
  bus-op children provably exclude the provider credential (the T13(b)
  assertion, exercised on the launch path).
- **T18 default-home hygiene + activation predicate** (re-scoped from the
  dissolved scratch-home ceremony) — herder writes no owner Pi config on any
  code path (no `settings.json`/`models.json` writes exist to exercise);
  extension install/update touches only `agent/extensions/` and family state.
  **Predicate branches, pinned exactly (DR-3):** (a) complete coordinate
  tuple + this process is the open attempt's recorded gated child →
  activates; (b) no coordinates (owner-interactive shape) → inert; (c)
  **partial** tuple → inert; (d) complete tuple but stale/ambient — resolving
  to seat state whose open attempt does not record this process's pid +
  start-time (an owner shell's leftover `HERDER_*`
  exports, raced against a genuinely in-flight launch) → inert **and** the
  in-flight launch's bootstrap file is provably untouched (no read, no
  unlink) and that launch still activates; (e) complete tuple + live
  **current activation** + no held token (memory-lost
  reload, DR-2 item 4) → inert exactly like (b)–(d) — no activation
  attempt, no journal write; the seat's `control-degraded` then derives
  write-free from lease decay past the escalation threshold (asserted on
  the status op's output and on the journal's unchanged byte length).
  Inert means: no seat claim, no bus
  ops, no seat-state or journal writes, no bootstrap access, zero bytes to
  model context or pane.
- **T19 vendor resolution + recorded version** (replaces the dissolved install
  gate) — provisioning resolves the vendor entry and records path + observed
  version; launch re-records the version in the registry row and journal; **no
  hash gate and no supported-version refusal exist** (asserted absent — the
  ruling's shape is pinned, not just permitted); an extension-API mismatch
  refuses to claim with a legible seat state and a launch hard-fail naming
  cause+remedy.
- **T20 passthrough refusals** — every colliding passthrough from the DR-3 list is
  refused with a targeted error (finalized against the CLI surface of the vendor
  version current at the launch unit).
- **T21 provider filtering, env channel** — unknown `--provider` refused
  naming the set; cross-provider credential never present in env; provider
  relaunch rebuilds the env; in-process cross-provider `model_select` flags
  provider-drift. Owner-config branch, re-scoped to the DR-5 contract: **no
  launch refusal on the contents of any credential-bearing owner file (auth
  store or custom-provider/models config) and no drift-termination path
  exists** (asserted absent — seats must survive ordinary owner `/login` and
  custom-provider state); if the P7 tightening surface was adopted, its
  per-source disablement is asserted on the seat process for exactly the
  sources it covers; the A10 per-source sizing probe's recorded results are
  referenced, not re-run, here.
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

Inbound driver, fencing, and bounds (the fix-round additions):

- **T28 long-idle pickup (A9 probe, part 1)** — TUI seat idle across ≥2 full
  `--wait` timeout cycles including a 10+ minute soak; a message then arrives and
  delivers end-to-end without any restart.
- **T29 driver lifecycle and liveness (A9 probe, part 2)** — wait child cancelled
  and reaped on `agent_start` and `session_shutdown`; exactly one wait child at
  any time; extension reload and in-TUI session switch rebuild the driver at a
  fresh epoch; forced repeated loop failure flips `driver-degraded` while Pi
  stays healthy; recovery re-arms the driver. Liveness branch: **abrupt top-level driver
  exit** (unexpected promise resolve/reject) is journaled, supervisor-restarted,
  and degrades on exhausted backoff — never a silent green; **hung driver after
  bind**: the driver blocks on a never-resolving await injected inside the loop
  body **while the extension's event loop and every other timer remain fully
  healthy and firing** — renewals stop because only state-machine checkpoints
  renew, the lease expires, and status decays to `driver-degraded` with no
  failure-path writer; the test asserts both the decay and that no renewal
  source outside the loop's checkpoints exists to defeat it.
- **T30 injection crash window** — crash injected between the observed input event
  and the injection record's fsync: replay re-injects with the same visible
  envelope (id + payload hash); with the record fsynced, replay provably does not
  re-inject and uses the nudge path.
- **T31 retirement fencing (two-phase)** — from the `retiring` record onward, no
  non-retirement mutation lands (a woken stale wait child's drain is rejected)
  and no competing activation succeeds; retirement's own records (final
  accounting — admitted drains and the deferred summary — plus undeliverable
  marks) land **between** `retiring` and `retired`; nothing
  whatsoever lands after `retired`; a crash injected between any two retirement
  steps is recovered by idempotent re-entry that completes the sequence — no id
  left neither delivered nor undeliverable, no retry rejected by its own fence.
- **T32 rebind with in-flight wait** — a prior epoch's wait/drain child straddles
  an in-TUI session switch: stale-epoch rejection, output discarded, no duplicate
  queue records; the new driver drains the same messages exactly once.
- **T33 cull final accounting** — messages routed to the seat but not yet
  drained at cull time are reported in the exact undeliverable total (per-id for
  what the admission cap admits; count + id-range summary for the rest); a cull
  skipping the accounting provably undercounts (mutation). **Quota-paused
  branch:** cull while `spool-quota` under a sustained backlog far exceeding the
  cap, **with every reserve class pre-filled to its worst case** (live ids at
  the cap, each carrying its maximum record set with nudge budgets exhausted,
  snapshot count at maximum) — retirement still completes `retiring` →
  summary/marks → `retired`, the journal's on-disk size stays bounded through
  retirement (asserted in bytes against the hard cap), the reported total still
  exactly matches the routed backlog, and the summary record's id range
  resolves per-id against the events store.
- **T34 control-plane capability, every lifecycle branch** — (a) tokenless or
  wrong-token control ops from inside a live seat are rejected against the stored
  hash (token via stdin only; absent from argv, env, and any persistent file —
  the bootstrap file is proven consumed/unlinked after first activation); (b)
  first activation succeeds only via the launch-minted bootstrap, and a missing/
  pre-consumed bootstrap hard-fails the launch with cleanup; (c) activation
  rotates the token and stores the new hash atomically; (d) **three-part
  plaintext invariant + live-model-during-rearm**: `rearm` invoked while the
  recorded Pi process is alive — with an active session and a model mid-turn —
  is **refused**; and the invariant is asserted in its implementable form:
  (i) no plaintext written while a seat process lives, (ii) the bootstrap file
  exists only across the pre-model exec→`session_start` window and is proven
  consumed/unlinked before the first model turn, (iii) after first successful
  activation no plaintext exists on disk for the life of that process — the
  test does **not** assert an existence property the green launch path
  violates; (e) memory-lost reload → `control-degraded` **derives
  write-free from authenticated silence** (DR-2 item 4): with the recorded
  process alive, the lease expired, and no authenticated token-lane record,
  status flips the derived state only after the escalation threshold — and
  an authenticated `driver-degraded` record written before the threshold
  provably caps the state at driver-degraded (token still held). **Spoof
  branch:** an exec'd model-tool child from inside the seat — full seat
  coordinates, matching Pi ancestry/provenance, any declared identity —
  invoking `activate` (or any control op) tokenless is **refused with no
  seat-status effect and no journal append** (asserted on the journal's
  byte length and the status output: no tokenless caller of any provenance
  can degrade, mutate, or signal on a healthy seat); and recovery is
  the controlled relaunch under the launch-attempt protocol:
  open attempt → quiesce/terminate the exact pid+start-time → attempt-keyed
  pre-exec rekey → relaunch → token-authenticated activation restores
  token-lane operation; (f) **launch-attempt race**: two external
  rekeys/restarts raced against one seat — exactly one attempt (the highest
  generation) is consumable, the superseded attempt's activation is refused,
  and the loser's cleanup, being generation-scoped, provably cannot remove or
  alter the winner's bootstrap, hash, epoch, or process;
  **exec-not-yet-activated branch**: the losing attempt's Pi is execed (its
  child pid + start-time recorded in that attempt) but hangs before activation
  and is superseded — after the winner activates, the test asserts **zero live
  seat Pi processes except the winner's**, verified by pid + start-time
  against the attempt records: no same-UID orphan holding the seat's
  provider credential survives; plus the exact
  crash-restart sequence: process provably gone → attempt + rekey by an
  operator-capability-bearing caller → relaunch → seat-token-authenticated
  activate consuming that attempt; a rekey attempted without the capability,
  from inside the target's cgroup, or against a live process, is refused;
  there is no tokenless activate to exercise, and a test proving its absence
  (activate without a valid seat token fails in every state) pins that;
  (g) external, capability-authorized cull proceeds with a live but
  unresponsive extension (no seat token, no extension cooperation) via the
  `retiring` fence; (h) **operator-capability external lane — testing the
  real claims**: **no-auto-resolution pins** — no herder code path locates or
  reads the operator credential store (pinned statically/by mutation: adding
  an auto-resolver fails the test), and every external op takes the
  capability from caller-supplied stdin only. With **valid stdin
  capability** — an operator shell piping its credential is allowed; a
  live-home orchestrator seat piping from its own config against a
  *different* seat is allowed (the fleet's operating norm, pinned as a
  positive test); a fresh reserve with no seat process yet is allowed; and a
  caller whose own cgroup **is** the target seat's cgroup is refused
  regardless (the defense-in-depth belt — no
  operate-your-own-seat-from-inside case exists). With **empty stdin** — in a
  **real user-manager environment**: brokered bare `cull`, `rearm`, and
  `rotate` via `systemd-run --user` (inherited or `--setenv` real `HOME`,
  fresh transient-unit cgroup, clean ancestry) are each **refused on empty
  stdin**, with the cause+remedy error naming explicit presentation — no
  branch asserts brokered launches are refused as a class, and no
  "arrives capability-less" claim exists to test. The pre-activation window
  stays covered: the gated child's record and cgroup membership exist from
  its first instruction, so there is no live-before-record instant to
  exploit. Capability lifecycle branches: mint and rotate require interactive
  confirmation (brokered non-interactive invocation fails on the missing
  interaction) with the cgroup refusal pinned as belt (the former managed-home
  refusal is asserted **absent** — under the ruling it would misfire on every
  legitimate caller sharing the live home); rotate
  additionally requires the old capability on stdin; rotation invalidates old
  material immediately (an op presenting pre-rotation material is refused);
  the credential provably never appears in argv, env, logs, or seat state. (i) **renew is token-lane**: a tokenless or wrong-token
  `renew` is refused and advances nothing — a same-UID process cannot keep a
  dead driver's lease green — and a valid-token renew advances the lease only
  when its reported state passes journal validation under the lock. `herder pi
  send` and read-only `status` remain reachable without any of it.
- **T35 bounds under sustained flood** — batch injection respects count/byte caps
  with the remainder queued and delivered at subsequent boundaries. Flood branch:
  under a sustained flood well past the caps, admission stops **before** any
  record would cross the cap (prospective: no append at 63 MiB pushes past 64),
  state-transition records still land inside the reserved headroom **even with
  every reserve class driven to its worst case** (incl. nudge budgets run to
  exhaustion — the `stalled` terminalization is exercised, not assumed), oversize
  payloads journal as envelope+hash and inject via events-store fetch, journal
  **on-disk size stays bounded** (asserted in bytes, snapshot/segment rotation
  included), `spool-quota` reports an honest hcom backlog count, and no message
  is silently lost: deferred rows deliver after drain-down resumes below the
  low-watermark, or are exactly accounted at cull (T33). Under quota, behavior
  is byte-identical to before.

**Live smokes (isolated, gated, owner spend per §12):** the launch unit's smoke
proves one provider end-to-end under the activation flag: spawn → status-op bind →
doctrine + prompt delivered (T1 shape, real inference) → outbound send lands on an
isolated bus → cull with row-absence confirm. The activation unit's smoke is the
real `herder spawn --agent pi` path (not a direct-launch stand-in — the grok
activation lesson: the spawn path hid a full unit's worth of integration gaps),
repeated per activated provider.

## 12. Owner decisions required

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
5. **Re-characterization appetite under vendor updates** (the former
   version-pin refresh policy is superseded by the ruling — no pin or gate
   exists). Vendor updates land on the vendor's cadence; 0.80.6 is the
   characterized baseline. The owner sets when a recorded-version change
   triggers a re-characterization pass (extension API + offline/state
   behavior) versus riding on the extension's refuse-to-claim guard alone
   (item 9b carries the honesty statement of what drift can invalidate).
6. **Project `.pi` resources in herder seats**: they ship disabled (DR-3) pending
   the P6 trust-surface characterization; whether and where to relax (per-workspace
   allowlist, global off, trust-prompt passthrough) is an owner ruling on that
   evidence.
7. **Superseded by the default-homes ruling.** This item conditioned an
   acceptance on the launch-empty-store contract's residual
   plant-to-checkpoint window; that contract dissolved with the managed home
   (DR-5). The credential-store surface is now item 9(a) — a standing delta,
   not a conditional window. (Number retained so cross-references stay
   stable.)
8. **The same-UID concession, made load-bearing for lifecycle authority.** The
   operator capability's boundary (DR-2, Threat model) refuses naive/direct
   self-operation only; deliberate credential acquisition — broker plus
   credential-path redirect, absolute-path reads, exfiltration — is inside the
   fleet-wide cooperative same-UID concession that every family (grok's
   control plane included) already ships under. This design makes that
   concession carry **lifecycle authority** for Pi seats; owner sign-off on
   this design explicitly covers that sentence.
9. **Default-homes ruling deltas (amendment round 10) — the honesty register
   this sign-off covers.** The 2026-07-14 ruling (standing-orders 20.8) trades
   previously stated properties for fleet-norm operation on single-purpose
   machines. Each place the ruling **weakens** a property this design used to
   claim is listed here explicitly, per the amendment's honesty duty — signed
   as deltas, never silently relabeled:
   - **(a) Credential-bearing owner-config channels are open — every file
     source, not only the auth store.** The former guarantees — seat
     `auth.json` launch-empty, drift detected at bounded checkpoints, seat
     terminated on drift, and a herder-controlled `models.json` — are gone.
     Seats read the owner's live auth store **and** the owner's
     custom-provider/models config through Pi's normal credential resolution;
     on a machine where either holds other providers' credentials,
     single-provider-per-seat holds on the **env channel only** (the only
     single-provider claim this design retains anywhere), and cross-provider
     access via those files is **in-band** (no deliberate acquisition
     required — Pi's own lookup does it). A10 sizes this per source; P7's
     per-invocation disablement, where it exists, closes exactly the sources
     it covers (DR-5 — per-source, never rounded up).
   - **(b) Version drift is unfenced.** No pin, no hash gate, no
     supported-version refusal: a vendor update between launches can silently
     invalidate every probe result and pinned behavior in this document.
     Remaining guards: recorded version at provision and every launch,
     extension refuse-to-claim on API mismatch, and `PI_OFFLINE=1` making the
     version stable for each process's lifetime only. Item 5 owns the
     re-characterization appetite.
   - **(c) The operator credential file sits inside every seat's home.** Seat
     `HOME` is the operator's live home, so the conventional
     `~/.config/herder/pi-operator` path is inside it, and the managed-home
     belt refusal on the external lane dissolved (only the cgroup belt,
     interactive mint/rotate, and no-auto-acquisition remain). The
     authorization boundary — explicit possession, stdin presentation — is
     unchanged, but the deliberate acquisition item 8 concedes got
     mechanically easier: a well-known in-home path instead of a path outside
     every seat's view.
   - **(d) One shared state surface.** All seats and the owner's interactive
     Pi share one home: seat session files intermix with each other's and the
     owner's — and because the shared default session root is a well-known
     path, the **full session JSONL of every seat (doctrine text, bus
     traffic, injected message content, model output) is path-discoverable
     and readable by any same-UID tool in any seat or owner shell**, not
     merely browsable through Pi's own surfaces (session pickers); under the
     managed home that content sat under a per-seat root outside other
     seats' homes. The managed extension loads into the owner's interactive
     runs (inert by design — T18); the owner's user-level Pi resources from
     the default agent home (extensions, tools, settings)
     load into credentialed seat processes (owner-trusted per fleet norm —
     distinct from workspace project `.pi/` resources, which stay disabled
     pending P6, DR-3).
     The sid-glob session-identity fallback dissolved with the shared root —
     session identity now stands on P1 or A5 alone, with a design-delta block
     behind them (DR-4).
   - **(e) State hygiene is fleet-norm, not fenced.** Any Pi invocation
     writes ordinary default-home state (the demo's `--help`-creates-state
     observation is no longer guarded by scratch homes); installer/scratch
     ceremony and the immutable install prefix are gone. This covers the
     whole homed state surface the managed root used to contain (the demo's
     state-model enumeration): debug/crash logs, caches, package resources,
     settings, and any other incidental homedir consumer now have
     **cross-seat and owner-tool read/write visibility both ways** — a seat's
     tools can read and mutate state other seats and the owner's interactive
     Pi will consume, and vice versa, within the same conceded same-UID
     model.
   Retained and expressly not weakened, for contrast: the entire DR-2
   delivery/authority machinery (keep-custom ruling), credential scoping in
   launch env construction, the identity-env allowlist and pinned hcom binary
   for bus ops, and the same-UID concession framework (item 8).

## 13. Staging (mergeable units, territory fences, gates)

Same discipline as the grok program: transport first, activation last and separate,
the shim never routes into a nonfunctional family, each unit independently
reviewable behind its fence. Cross-family adversarial review and the full repository
gate battery apply to every behavior diff (house rules).

| # | Unit | Territory (fence) | Gate |
|---|---|---|---|
| U1 | **Transport core + extension**: spool/state machine, `herder pi bus` ops (reserve/de-latch, activate, **rearm** [pre-exec rekey], **renew** [lease checkpoint carrier], drain, wait, pending, send, status, retire; epoch fencing, seat token + **operator capability** lanes incl. `herder pi operator <init|rotate>`, **launch-attempt protocol**), the TypeScript extension (lifecycle handlers, the DR-2 inbound driver, idle-gated bounded batch injection, replay, nudge with per-id budget), `herder pi send` wrapper. The `grokbridge` extraction follows the **DR-1 reuse boundary exactly** — transport-neutral primitives only; grok's state types, receipt machine, and generation fencing are not touched or reused; the entire grok battery stays green unchanged (any grok behavior diff is a stop-and-flag). Nothing user-reachable changes. | New internal package(s) (e.g. `tools/herder/internal/pibridge/` + the shared primitives package) + `herder pi` command registration + extension artifact in-repo. | **FIRST GATE: the A9 driver probe (T28, T29) — run before any other U1 work is built on the driver.** Then T1–T16, T25, T26, T30–T35 hermetic (mock Pi event harness + isolated bus); T15 against real hcom 0.7.23; grok battery green post-extraction; assumptions A1–A5, A7–A9, A11 and probe P2 verified and recorded (the §10 probe posture: hermetic fixtures and isolated buses where no Pi runs; Pi executions under the real default-home seat launch-env shape; scratch stand-ins only for owner-meaningful mutable files; inference-bearing probes under the §12.2 ruling). |
| U2 | **Vendor resolution + launch contract, behind an activation gate**: vendor entry resolution + recorded version (provision and per-launch), extension install into the default `agent/extensions/` + seat-keyed activation/inertness, launch env construction with credential scoping (no state re-points — DR-3), provider table + filtering, flag mapping + refusals, spool-borne doctrine/prompt, status-op bind capture with hard-fail cleanup, conditional `/proc` assertion. `--agent pi` refuses with a family-not-activated cause+remedy error unless the explicit activation config/env is set. | `launchcmd`/`spawncmd` pi branches + `herder pi provision`; `pibridge` consumed, not modified. | T17–T21 (re-scoped forms) + probes P1/P4/P6/P7/A6 answered and recorded + the isolated **live smoke** (one provider, §12.2 spend) under the activation flag. |
| U3 | **Lifecycle & identity**: resume/fork/cull/relaunch-on-provider-change, session-drift handling, registry capability flags (`bus`, `pending`, `inject`, `driver`, `spool`, `control`, `provider` — the full DR-6 set), retirement reporting. | `lifecyclecmd`/`cullcmd` pi branches, registry schema additions. | T9, T22–T24 + T31/T33 re-run through the cull command path + resume/fork live re-check riding the U2 smoke pattern. |
| U4 | **Observer, transcript & sesh**: session-JSONL adapter (header index, branch-aware rendering), sesh identifier/lineage wiring, labeled `status(pi-ext)` enrichment, honest-unknown reconciliation. | `observercmd` + transcript/sesh plumbing. | T27 against recorded fixtures; `unknown` preserved under mutation. |
| U5 | **Shim/setup/doctor/docs**: `pi` PATH/shim semantics per the default-homes fleet norm (decided in-unit and documented — the grok default-homes unit's pattern; the quarantine-era no-vendor-fallback shim shape does not apply), ai-setup/ai-doctor family checks (report-only; a default doctor run reports the Pi binary/version without surprising side effects), family docs (default-home operation per the ruling). | shims + setup/doctor scripts + docs. | Ships only after U2's live smoke is green; shadowing/recursion checks; doctor checks provably report-only (T18 posture, re-scoped). |
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
3. **Status-op-authoritative liveness on the bind-readiness definition**: bind and
   health claims come from seat-state read-backs showing the current runtime epoch
   AND an armed inbound driver (DR-2 "Seat ownership", fact iii) — never the
   roster row or de-latch alone; no-bind hard-fails with confirmed cleanup.
4. **Placeholder latch covered**: the reserve op's pinned de-latch
   (`hcom list --name <name> --json`) verified against the live hcom version in
   use at activation (re-verified if hcom was upgraded since U1's pins).
5. **`/proc` conditional resolved with evidence**: the activation run characterizes
   the actual pane-spawn path for Pi. If it is env-preserving direct-exec, the
   ceremony's removal is authorized as a follow-up reviewed change; if not, the
   assertion stays. Either way the clause stops being conditional — by evidence,
   not by paper.
6. Per-provider offline residual-network integration check (P5) for each activated
   provider.

Until the activation change, the family is explicitly experimental and opt-in.

## 14. Earned-clause disposition (carried forward from the demo)

The demo's clause verdicts (demo "Earned launch-contract clauses"), with where each
lands in this design. Where the 2026-07-14 default-homes ruling supersedes a demo
"Required" verdict, that is recorded here as the ruling's disposition — the demo
report itself is not rewritten:

| Clause | Demo verdict | Design disposition |
|---|---|---|
| Dedicated managed `PI_HOME` concept | Required | **Superseded by owner ruling 2026-07-14**: live default home (§1, DR-3 seat construction); the demo mapping stays as evidence that the translation variables work |
| Managed environment on every invocation | Required | Re-scoped by the ruling: herder-constructed env on every **seat launch** (DR-3); the every-invocation scratch ceremony dissolved (§12 item 9e) |
| `PI_OFFLINE=1` | Required | DR-3 launch-env delta (per-process version stability) + activation AC 6 per-provider check |
| `PI_TELEMETRY=0` | Required | DR-3 launch-env delta (no settings seeding exists) |
| Provider-specific environment filtering | Required | DR-5 + T17/T21 (env channel; retained by the ruling) |
| Provider pin per seat | Required | DR-5 (relaunch on cross-provider change) |
| Pinned package version and integrity | Required at install/provision | **Superseded by owner ruling 2026-07-14**: vendor-updated default install, observed version recorded, no gate (DR-3 provisioning + T19 re-scoped; drift delta §12 item 9b) |
| Per-launch binary hash gate | Not earned | Not designed; no provision-time gate remains either (ruling) |
| Per-launch config rewrite | Not earned | Not designed; no owner-config writes at all under the ruling (env-only — §1) |
| Per-launch `/proc` environment ceremony | **Conditional** | Carried conditionally: one-time post-spawn assertion every launch (DR-3) until activation AC 5 characterizes the pane-spawn path; resolution only by that evidence |
| Native managed extension | Required | DR-1/DR-2 (the binder-owner) |
| External binder process | Not earned | Not designed; DR-1 fork explicitly keeps all persistent logic inside Pi or in bounded CLI ops |
| Pending-message replay on every start | Required | DR-2 recovery matrix (session_start replay + nudge) |
| Exact resume/fork integration | Required | DR-4 + DR-6 (sesh lineage) + T23 |

## 15. Design-time verification note

Per the docs-only constraint of this unit, **no new probes of the Pi binary or of
hcom were run while writing this design**. Amendment round 10 is likewise
docs-only: it records the 2026-07-14 owner ruling and its deltas (§12 item 9)
without new probes; the hcom-native characterization it cites ran as its own
probed unit. Every behavioral claim cites either the
double-reviewed demo report, the grok design's independently reproduced hcom 0.7.23
verification (V1–V9), the hcom-native Pi characterization, or mechanism-level
grok activation evidence. Where the demo's
evidence basis was API/documentation inventory rather than probe, the claim is
registered in §10 with a conservative posture and a named verification owner. The
first implement unit (U1) begins by discharging the §10 register — in particular
the A9 driver probe (U1's first gate) and A7
(TUI-mode extension parity), which everything else builds on.

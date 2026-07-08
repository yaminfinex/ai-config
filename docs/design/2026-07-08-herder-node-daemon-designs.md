---
title: "Herder node daemon + registry write path — design-it-twice pass"
date: 2026-07-08
status: DECISION PENDING — four divergent designs + comparison + a recommendation; author has
  not picked. Brainstorm mode; nothing here amends the herder spec yet.
purpose: settle whether `herder` grows a node daemon, what it may own (write path? reads?
  observation?), and how the spoke/control-plane duties from the boundaries discussion land —
  by comparing radically different shapes rather than grilling vibes
related:
  - docs/design/2026-07-08-sessions-missions-boundaries.md   # the discussion that raised this (Q8/Q9)
  - docs/specs/herder-spec.md (herder-spec branch)           # §5.2 flock discipline; §10 "registry daemon rejected"
---

# Herder node daemon: four designs

Context: the boundaries discussion assigned herder a per-node presence with three new duties —
**spoke telemetry** to the herd server (bus events, registry deltas, mission-dir full-replace
snapshots), an **inbound control plane** (delivery verbs only), and future **cross-node relay**
— and reopened the write-path question (CLI-direct flock per spec §5.2 vs daemon-mediated).
Evidence in play: the per-occupant sidecar architecture's hardening pain (TASK-033/034/035).
Each design was produced under a forced divergent constraint.

## Design A — "Peer-Writer Daemon" (daemon may never write... except as a peer)

Registry stays a flock-disciplined JSONL with **no privileged writer**. Daemon = observer +
network arm; observation facts append through the same shared writer package the CLI links —
a daemon append is byte-indistinguishable from a CLI append; daemon liveness is never a
precondition for any verb. Key move: **the registry file is the coordination fabric** — the
daemon tails it as its work queue (spawn appends a row → daemon starts observing that seat; no
handoff IPC), and upstream spoke sequence = file position (lossless reconnect replay by
construction). Inbound control plane = argv allowlist executing literally `herder send …`
(remote sends can never be more permitted than local). Daemon-down hour: every command works
identically; degradation = enrichment speed, node-dark-at-server, unrecorded turnovers until
the restart **catch-up scan** (level-triggered re-probe → backdated correction rows).
Reads NEVER go through the daemon — `list` is always answered from the file.

Weaknesses (own admissions): concentrated observation blindness (one daemon vs N sidecars);
houses rather than fixes the stale-enrichment race genre (multi-writer flock stays); no
authoritative hot projection ever — O(file) per verb forever; crude subprocess control plane.
Spec: §10 **sharpened, not reversed** — "registry *write* daemon rejected; the node daemon
holds no write authority"; new invariant: "no write routes through the daemon."

## Design B — "The Daemon Owns Everything" (single-writer herderd)

Registry file stays; **flock dies**. One `herderd` per state dir is the only appender, the
only observer, the spoke terminus; CLI = thin socket client; auto-start with a `daemon.lock`
singleton election (flock demoted to election duty), crash-loop cooldown, bound-then-renamed
sockets. Claimed prize: the TASK-033/035 ambiguity class exists only because sidecars are
separate processes correlating through a lossy medium — absorb observation into the process
that performed the launch and the class is **unrepresentable**. Hot projection answers
`list`/`watch` from memory; inbound `deliver` = the local Send RPC on another transport (one
validation path). Version skew: proto handshake hard, build-hash tolerated (parallel worktrees);
upgrade = explicit drain+exec. Tests: hermetic daemon-per-suite.
Daemon-down hour: reads degrade with banners; **writes, spawn, send refuse** (escape:
`resolve --degraded` + manual hcom composition).

Weaknesses (own admissions): hung-not-crashed daemon stalls everything (flock had no such
mode); zero-moving-parts CLI gone (fresh machine/CI = degraded only); hand-appends silently
diverge from the hot projection; every golden suite carries daemon lifecycle; observation gaps
total, not per-occupant. Spec: strike "registry daemon rejected" outright; delete §5.2.

## Design C — "The Herder Has No Pulse" (zero new daemons; hcom relay as spine)

No new long-lived process, ever; §10 stands byte-for-byte. Discovery: **hcom ships a `relay`
daemon** (MQTT, device identities, durable log, remote fetch) — the herd server joins the
relay group as just another device. Registry-writing verbs gain **post-commit emission**
(after flock release, post the row as a bus message; best-effort, never fails the write);
missed emissions repaired by a stateless `herder sync --once` (runs §8.3 reconciliation first)
hung off existing hooks + one cron timer. Inbound control is **structurally** delivery-only:
the node contains no interpreter — the only inbound surface is hcom message delivery; the herd
server does send-resolution itself against its mirrored projection. Binary upgrade: the
question is deleted. Reboot-unnoticed: server shows last-known-at-T, never fake-live;
self-heals in one sync tick.

Weaknesses (own admissions): freshness floors at cron cadence (alive-and-idle ≈ dead for
minutes); bets the farm on upstream hcom relay ("unused and unmodelled" per spec); mission
dirty-file snapshots don't fit MQTT payload ceilings; **resolver drift** — refusal semantics
duplicated at the server against a mirror; no admission control (trust-the-relay-group) —
weak for team multiplayer. Also quietly makes the herd server (the component that least
exists) the fattest.

## Design D — "The Disposable Projection Daemon" (log-owning writers, view-owning daemon)

Log = only durable object; daemon = materialized view + volatile liveness overlay; **no IPC
verb appends**. Writers untouched — their flock appends *nudge* the daemon (fire-and-forget;
fs-watch safety net; correctness never depends on the nudge). Reads: mode shim — daemon up +
build match ⇒ hot projection with `source: daemon` + `adjudicated_at` stamps; else cold from
file (`--cold` to force). **Barrier** protocol (caller passes its post-append log offset;
daemon answers once its cursor passes it) makes read-your-writes exact. `send` refusal
*categories* mode-invariant; the daemon may refuse *earlier* (freshness delta, not contract
break). New verb `watch` (daemon-only). Sidecars demote to **deputies**: dormant while the
daemon heartbeats, waking on lapse — safe because §5.2 appends are idempotent. Upgrade:
build-hash mismatch ⇒ old daemon told to exit, CLI runs cold, launchd restarts new build,
projection rebuilds <1s — disposable means no handoff protocol exists. Daemon-down hour:
everything works cold except `watch`; deputies observe; spoke dark (`deferred` receipts);
boot sweep self-heals. Caller doctrine: **the file is truth; the daemon is a cursor-stamped
view; liveness without an appended row is advice.**

Weaknesses (own admissions): two read paths forever (permanent hot/cold parity-test tax — the
real price); writes gain nothing; deputy-wake seam double-observation window (idempotence now
load-bearing from two components); the server sees a cache of a cache. Spec: same §10
sharpening as A + invariant "the projection daemon is disposable."

## Comparison (prose, where they diverge most)

**Daemon-down is the cleanest split.** B refuses writes/spawn/send for the outage — deleting
flock left nothing else that can write. A, C, D keep every capability alive because the file
remains the coordination point. B renegotiates the spec's founding escape-hatch value for its
prize.

**B's central claim doesn't fully survive A and D.** `spawn` already appends frozen pane id /
tag / cwd / provenance under flock before returning — **the log itself carries authoritative
birth context**. A log-tailing daemon picks up the child-specific correlate directly; no
roster-guessing. What killed the TASK-033/035 bugs was never "one process" — it was "one
always-on observer with authoritative launch context," and the log delivers that context
across processes. B's remaining edge is serializing label races, which flock already handles.
B's price buys less than it appears to.

**C is the most honest about cost and the most wrong for this workload.** Real contributions:
hcom relay exists (future cross-node transport option); "no executor = structurally
delivery-only" (graft this framing onto any winner); post-commit emission + anti-entropy. But
mission snapshots don't fit MQTT, refusal semantics fork into two codebases, freshness floors
at minutes, admission control is trust-the-group — and the fat lands on the herd server, the
component with the least code today. Complexity moved, not lowered.

**A vs D is the real decision** and reduces to one question: may reads go through the daemon?
A: never — purest invariants, cheapest tests, O(file) forever, and the fast-path door welded
shut. D: yes, with barrier + stamps + cold parity. The readers in the world just designed —
orchestration `wait` loops, spawn's bind gate, the spoke, a future live board via `watch` —
are exactly what a hot projection is for. Under A they re-parse JSONL per call or get rebuilt
server-side only; under D they're one subscription. A pays runtime cost forever to keep one
invariant maximally clean; D pays a parity-test tax forever to make the daemon useful to the
duties that justified it.

## Recommendation (pending author decision)

**D as the destination, entered through A, with C's control-plane framing. Reject B.**

- **Write path settled**: shared flock writer package; new spec invariant "no write ever
  routes through the daemon"; §10 sharpened ("a registry *write* daemon remains rejected"),
  not reversed. Direct answer to the originating question: daemon-mediated writes trade the
  works-anywhere CLI and hermetic tests for serialization flock already provides — wrong trade.
- **Phase 1 = A's subset**: daemon as observer + spoke + inbound `deliver` (D's receipt
  semantics, C's delivery-only-by-structure framing); reads untouched; sidecars untouched.
  Smallest additive step — addresses the short-term-complexity concern directly.
- **Phase 2 = flip reads hot** (D's mode shim: barrier, `source` stamps, `--cold`); sidecars
  demote to deputies. Only after phase 1 earns trust in live herds.
- Each phase independently shippable and abandonable; stopping at A loses nothing.
- Keep from D regardless: *the file is truth; the daemon is a cursor-stamped view; liveness
  without an appended row is advice.*

## Open items on decision

- Author pick (A-then-D per recommendation, straight A, or other).
- If picked: spec amendment pass on herder-spec branch (§2 terms, §3.1 invariant 13, §4
  diagram, §5.2 nudge/barrier notes, §6.4 herd link, §7 verbs, §8.4 catch-up sweep, §9 new
  ACs, §10 rewording) — the four designs' amendment lists converge on these sections.
- Boundaries doc §6 open question 2 (ingestion transport) unaffected; session shipper remains
  a separate component per Q8 ruling.

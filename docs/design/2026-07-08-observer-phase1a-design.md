---
title: "Herder node observer — phase 1a design (universal seat observer)"
date: 2026-07-08
status: DRAFT for review — output of the TASK-073 design pass. Review chain: fresh-context
  adversarial design review → fix round → tomo final intent review. Not buildable until that
  chain completes (TASK-073 AC-4).
purpose: turn the ratified "D-via-A, observer-first" decision
  (docs/design/2026-07-08-herder-node-daemon-designs.md, branch sessions-missions-design,
  commit 1fbe376) into a buildable phase-1a design; every settle-item from TASK-073 is
  answered here.
related:
  - docs/specs/herder-spec.md (main; RATIFIED 2026-07-08) — amended only via the errata
    proposals in 2026-07-08-observer-phase1a-spec-errata.md (spec-steward lane)
  - docs/design/2026-07-08-observer-phase1a-impl-task.md — filed-ready implementation task text
---

# Herder node observer, phase 1a: universal seat observer

## 0. What this is and why

Sessions spawned through herder get a per-occupant sidecar. Sessions that **enroll** — in
practice always the orchestrator's own session — get nothing: `herder enroll` records
pane_id + terminal_id + hcom coordinates and returns (tools/herder/internal/enrollcmd/enroll.go:40-139);
it launches no process, records no pid, and forks no sidecar. Nobody watches that seat again
until a human runs `herder reconcile`. That blind spot produced most of the run-herder-dx
live incidents (stale identity after restarts, dead bus rows trusted, label tombs).

The fix, ratified in the four-design comparison (D-via-A decision record), is a **per-node,
disposable, no-write-authority observer daemon** whose first shipped duty is a **universal
seat observer**: it tails the registry as its work queue and observes every seated row the
same way, regardless of how the seat came to be (spawn / enroll / resume / fork). The
explicitly rejected stopgap — enroll forking its own sidecar — stays rejected. Existing
spawned-session sidecars are untouched in phase 1a.

**Live evidence the design was checked against** (2026-07-08, this machine): registry row
`275a4ac2` says `state: unseated` (migration dormant-default, no seat object at all), while
`herdr agent list` shows a live idle claude in pane `w655a9196cb2ef2a:p1`, pid 828943
(`pane.process_info`), pane label `comments-ux-275a4ac2` — equal to the registry row's label.
A dormant row and a live occupant, invisible to every current component. The observer designed
here flags exactly this within one sweep (§6.3).

### Names used below

- **observer** — the phase-1a daemon, `herder observer run`. One per state dir.
- **observation fact** — a registry row the observer appends through the shared locked
  writer. Never a new storage format; always an existing v2 event.
- **sweep** — one level-triggered pass: full substrate snapshot × full registry projection,
  producing zero or more observation facts.
- **observer-owned seat** — a seated row with no live sidecar (enrolled seats today; all
  seats after a later deputy-demotion phase).

## 1. The five ratified invariants, and where this design honors each

This section is the contract. Every mechanism below traces back here.

| # | Invariant (operative form) | Honored by |
|---|---|---|
| 1 | **No registry write authority.** Observation facts append through the same shared locked writer package every CLI verb uses, byte-indistinguishable from CLI appends. | The observer links `internal/registry` and calls `UpdateLocked` (write.go:29-139) like any verb. It has no privileged path, no IPC append surface, and no listening socket at all (§4.4). §10's daemon rejection is sharpened, not reversed (errata E-8). |
| 2 | **Confirmed-write contract.** Every append reports a typed applied / noop / refused outcome; none may be discarded. | `UpdateLocked` already expresses this structurally (applied = returned rows; noop = normalize declines, write.go:102-108,132-134; refused = error). The observer logs every outcome per fact and counts them in its status file (§4.5); a refused outcome is never retried blind — it triggers a fresh sweep (the projection changed under us). Errata E-5 promotes the reconcile `Write: none|pending|applied|error` vocabulary to normative. |
| 3 | **v2 states only.** The observer's projection consumes seated/unseated/retired/lost, never the legacy 2-state view. | The observer imports `internal/registry/v2` only; it never touches `registry.Record`/`Status` (the legacy view most verbs still use — a migration liability the observer must not extend). Enforced by the contract suite (§7, scenario T-9) and a lint check in the impl task. |
| 4 | **Disposable; no handoff between generations.** Death or rebuild is a non-event. | No durable observer state: the registry cursor is in-memory; boot always runs the full catch-up sweep (§6). The only files are a flock-held lockfile and an advisory status file, both overwritten by any new generation without reading the old (§4). kill -9 is a supported stop mechanism. Upgrade = replace, never drain-and-handoff (§4.3). |
| 5 | **File is truth; observer is a cursor-stamped view; liveness without an appended row is advice; repairs stay explicit verbs.** | The observer rebuilds its view from the file on every change (§5.1); its status file is labelled advice; it appends only on positive evidence and **flags** everything ambiguous or repair-shaped (dormant-live rows, epoch-wide doubt, guarded-match candidates) for the explicit verbs `enroll` / `reconcile --apply` / `resume` (§6.3). |

## 2. Observer substrate: the evaluation TASK-073 required

Three candidate substrates for *how the observer learns seat truth*, evaluated against the
live herdr install (protocol 16, socket `~/.config/herdr/herdr.sock`) and the herder source.

### 2.1 Registry-tail-only (poll the substrates via CLI, no socket protocol)

The observer tails registry.jsonl for its work queue and polls `herdr agent list` /
`herdr pane get` / `hcom list --json` per tick — the sidecar's current architecture
(sidecar.go:651-663) scaled to one process. Works everywhere the CLI works; zero protocol
coupling beyond what herder already has (internal/herdrcli). Cost: freshness floors at the
poll cadence; every tick pays process-spawn overhead per query; occupant exits between ticks
are only ever seen level-triggered.

### 2.2 herdr plugin (packaged long-lived subscriber inside the terminal)

`herdr plugin install/link` exists (none installed on the live machine). Rejected as the
substrate, kept as a possible later packaging: a plugin's lifecycle is owned by the herdr
server (dies with it — so herdr-down means the observer is down exactly when process seats
and hcom-only liveness still need watching); appending observation facts from inside the
terminal means exec'ing the herder binary anyway (the shared writer is a Go package, not a
wire protocol); and it couples the observer to the plugin API's release channel on top of the
socket protocol's. Verified 2026-07-08 (task 73 comment #4): **plugin registration is not
required to use the socket API** — the herdr CLI itself is a plain socket client. The plugin
route buys nothing phase 1a needs.

### 2.3 Direct socket client (chosen), riding the registry tail

The observer keeps the ratified registry-tail work queue and speaks the herdr socket protocol
directly as an ordinary client — the same standing the herdr CLI has. Verified against the
live install:

- Wire: newline-delimited JSON `{id, method, params}` over the unix socket; typed error
  responses (`invalid_request`, …). `herdr status server` reports `protocol: 16,
  compatible: yes` — the version-compatibility surface (§8.4).
- **List**: `session.snapshot` returns agents[] + panes[] + tabs[] + workspaces[] + protocol
  + version in one query — the catch-up sweep is a point-in-time query, not an event replay.
  `pane.get` returns terminal_id, label, agent, agent_status, agent_session (sid, when a
  reporter pushed it). `pane.process_info` returns shell_pid + foreground processes with
  pid/argv/cwd — direct occupant-process truth (verified live against the 275a4ac2 pane).
- **Watch**: `events.subscribe` with broadcast subscriptions `pane.created`, `pane.closed`,
  `pane.exited`, `pane.agent_detected` (verified: these require no per-pane filter;
  `pane.agent_status_changed` requires a pane_id per subscription, but agent busy/idle status
  is hcom's domain, not seat liveness — phase 1a does not subscribe to it).

Why this beats 2.1: pane events arrive per-PANE regardless of how the seat came to be —
enrolled seats are covered identically to spawned ones, which is the universality property
this task exists for; occupant death latency drops from poll-cadence to event latency; and
one persistent connection replaces N CLI spawns per tick. Why the tail stays: **events are
advice** (invariant 5) — a push stream can drop, the daemon can be down, herdr can be down
while process seats live on. The level-triggered sweep over `session.snapshot` + `hcom list
--json` + the registry projection remains the correctness path; subscriptions only reduce
latency. This is the classic list-then-watch pattern with the list side doubling as the
catch-up sweep.

**Decision: direct socket client for probes and events; registry tail as work queue;
CLI-equivalent fallback (2.1) automatic whenever the socket is incompatible or absent.**

## 3. What the observer watches, per seat kind

The registry projection defines the watch set: every `state: seated` row (plus one advisory
duty over unseated rows, §6.3). Seat fields are uniform across spawn/enroll/resume/fork —
that uniformity is what makes the observer universal. Per seat kind:

**herdr seats** (seat.kind = "herdr"; spawned, enrolled, resumed, forked — identical
treatment). Key = `terminal_id` (pane_id is display-only and run-scoped, spec §3.3). Probe
chain: locate terminal_id in the snapshot → pane present? → `pane.process_info`: is there a
live foreground process? → `agent_session` sid if a reporter pushed one. Cross-check the hcom
row (matched via seat.hcom_name within seat.namespace; secondary correlate:
hcom `launch_context.pane_id` == seat.pane_id, valid within a herdr epoch): status_age,
session_id, process_bound. The only observable difference of an *enrolled* seat is what it
lacks: no sidecar (so no sid reporter → usually sid-less → §8.3's sid-less doctrine applies:
re-confirm at `continuity: assumed`, never unseat on absence of evidence) and no recorded pid
(no verb records seat.pid today — enroll.go never sets it; the observer gets process truth
from `pane.process_info` instead, which is *better* than a stale birth pid).

**process seats** (seat.kind = "process"; headless). Key = pid + hcom process binding. Probe:
signal-0 the pid (same user by construction) + hcom row status/process_bound/status_age. herdr
is not consulted. herdr-down never degrades these.

**Verdict discipline (all kinds).** Positive evidence of death is required to unseat:

- pane's occupant exited (pane.exited event, confirmed by a follow-up `pane.process_info`
  showing no tool process) → `unseated`, seat vacant (spec AC-13);
- pane closed / terminal_id absent from a fresh snapshot *within an unchanged herdr epoch*
  → `unseated` (AC-12);
- process seat: pid gone AND bus row stale → `unseated`.

Absence of evidence — socket down, hcom db errored (distinguished from row-absent, same rule
the sidecar uses, spec §4), probe timeout — is an observation gap, never a verdict.
Epoch-wide doubt (reconnect after server restart; snapshot full of unknown terminal_ids) is
§8.3 reconciliation territory: the observer runs the probe portion with the sid-less
fallback's conservatism, appends re-confirms/unseats only where per-seat evidence is
positive, and flags everything else for `herder reconcile` — repairs stay explicit verbs.
The observer never produces `state: lost` (transcript-verified-gone is resume/recognition
evidence, not liveness evidence; note `StateLost` is defined but unproduced in the entire
codebase today — the observer does not change that).

**Turnover detection for observer-owned seats.** Spec §8.1's one rule ("sid changed in my
seat ⇒ turnover") is assigned to "the sidecar" — which enrolled seats don't have, so today
nobody detects the orchestrator's own `/clear`. This is the "stale identity after restarts"
incident class by another name, squarely inside the WHY of this task. Scope call
(flagged for review): the observer applies the §8.1 rule to seats with **no live sidecar**:
hcom-row session_id (or reported agent_session) disagreeing with the row's newest sid, minus
continuity evidence, ⇒ unseat old (displaced) + register newcomer (new guid, `cleared_from`,
unlabelled, unbriefed), child-first, in ONE `UpdateLocked` call (the closure returns both
rows — write.go supports multi-row appends). Idempotent by (seat, new sid) per §5.2. Seats
with a live sidecar are the sidecar's to handle — no double authority in 1a.

## 4. Lifecycle

### 4.1 Process shape and start

The observer is a hidden herder subcommand — `herder observer run` — in the same binary as
every other verb, so it links the same writer package (invariant 1) and ships with zero new
artifacts. Verb family (§7 errata E-6):

- `herder observer run` — the daemon loop (foreground; callers detach it the way launch
  detaches the sidecar, launch.go:292-322 pattern: Setsid, log to
  `<state-dir>/logs/observer.log`).
- `herder observer sweep [--json]` — ONE level-triggered sweep, no daemon, exits. The
  testing/cron/debug surface and the degraded-mode escape hatch. Ships first (§9).
- `herder observer status` — reads lockfile + status file; reports pid, build hash,
  heartbeat age, last sweep summary, protocol compatibility. Read-only.
- `herder observer stop` — SIGTERM to the lockfile pid; that's all stopping is.

**What starts it:** explicit `observer run` always works; the default posture after rollout
step 3 (§9) is **nudge-start** — seat-creating verbs (spawn, enroll, resume, fork) check the
lockfile and start a detached observer if absent or heartbeat-stale, exactly the dumb
supervision disposability buys us. systemd user units are optional operator sugar, never a
dependency. No verb ever *waits* on the observer: daemon liveness is not a precondition for
anything (D-via-A ground rule).

### 4.2 Exactly-one-per-node

Node = state dir (one registry per state dir, spec §2). Enforcement: `flock(LOCK_EX|LOCK_NB)`
on `$HERDER_STATE_DIR/observer.lock`, held for the process lifetime — the same primitive the
registry itself trusts (write.go:207-212), kernel-released on any death including SIGKILL.
Loser exits 0 silently. The lockfile *content* (pid, build hash, generation ulid, started_at)
is advice for `status` and the nudge check; the flock is the enforcement. Never the registry
flock — the observer holds that only inside individual `UpdateLocked` appends like any verb.

### 4.3 Death, restart, upgrade

Clean death (SIGTERM): close the socket subscription, finish any in-flight `UpdateLocked`
(they are synchronous and short), exit. No dying-gasp registry write, no generation
bookkeeping in the registry — death is a non-event (invariant 4). Unclean death (SIGKILL,
OOM): identical outcome from the system's perspective; the kernel drops the flock; whatever
was mid-append either committed under the registry lock or didn't (the registry's own
crash-safety story, unchanged).

Restart: the new generation reads nothing from the old — no cursor file, no queue, no
snapshot. Boot = full catch-up sweep (§6). This is the "no handoff protocol" invariant made
structural: there is nothing to hand off.

Upgrade: a nudging CLI compares its own build hash to the lockfile's; on mismatch it SIGTERMs
the old pid and nudge-starts its own build. Replacement, not handoff: the generations share
only the durable registry. (TASK-046 is the recorded evidence for why no drain protocol.)

Wedge (hung-not-crashed — the failure mode flock alone can't see): the daemon touches the
lockfile mtime every loop tick; a nudge or `status` finding heartbeat age > 5× tick treats
the daemon as dead: SIGTERM, brief grace, SIGKILL, restart. Safe because disposable.

### 4.4 No listening surface

Phase 1a's observer has **no socket server, no IPC, no control plane**. Input: registry file,
herdr socket (as client), hcom CLI, signals. Output: registry appends, herdr
`pane.report_agent_session` (§6.4), log, status file. Phase 1b's inbound `deliver` and phase
2's hot reads bolt onto this process later — nothing in 1a presumes them.

### 4.5 Status file

`$HERDER_STATE_DIR/observer.status.json`, atomically rewritten (tmp+rename) each sweep:
last sweep time, per-outcome append counts (applied/noop/refused), per-seat last-confirmed
map, flagged anomalies (§6.3), protocol compatibility. Explicitly labelled advice (invariant
5), deletable at any time, overwritten without being read by a new generation (invariant 4).

## 5. The loop

### 5.1 Work queue: the registry tail

inotify on registry.jsonl (2s poll fallback). On any change: **reload the whole projection**
via the v2 loader (quarantine-tolerant — a torn row never kills the observer, same as the
CLI). No incremental JSONL parsing, no durable cursor; the in-memory cursor is (inode, size)
purely to detect change and rotation (inode swap ⇒ reopen; rotation rewrites the live file
under the registry lock, migration.go:161-208). The live file is reseeded at 8 MiB so full
reloads stay trivially cheap. The view is literally rebuilt from the file — invariant 5 as
an implementation detail, and it kills the stale-projection race genre for the observer's
own decisions because every *append* additionally re-checks against the projection loaded
inside `UpdateLocked`'s lock (write.go:46): check-then-append is atomic.

### 5.2 Event stream: subscriptions as accelerant

One persistent socket connection; subscriptions: `pane.created`, `pane.closed`, `pane.exited`,
`pane.agent_detected` (all broadcast). Each event schedules a **targeted probe** of the
affected seat (debounced 500ms) — events are never themselves evidence; the follow-up query
is. Dropped connection ⇒ reconnect with backoff+jitter and run a full sweep on reconnect
(the list in list-then-watch). Event overload or missed events cost latency only; the
periodic sweep is the correctness backstop.

### 5.3 Periodic sweep and cadence

Full sweep every 30s (config `observer.sweep_interval`): one `session.snapshot` + one
`hcom list --json` + one projection reload, then per-seat verdicts. O(1) substrate queries
per tick regardless of seat count — contrast N sidecars × 2s ticks today. Probes triggered
by events run between sweeps.

**Append cadence — transition-driven, not heartbeat-driven.** Rows are appended for:
state transitions (unseat, turnover pair), enrichment (sid newly observed, hcom name learned
— `recognised`, mirroring sidecar.go:438's discipline: pane-correlated matches only, never
tag+cwd-fallback matches, the TASK-033 lesson), and **slow re-confirmation**: when a probe
succeeds and seat.confirmed_at is older than `observer.reconfirm_interval` (default 60m),
append a `reconciled` row re-stamping confirmed_at. Rationale: steady-state liveness is
advice (invariant 5) and belongs in the status file, but a *durable* trail of "last positively
confirmed" is exactly the forensic record this run's incidents lacked; at 10 seats ≈ 240
rows/day ≈ 100 KB/day, absorbed by rotation. Reviewers: this is a tunable tradeoff, challenge
the default, not the mechanism.

### 5.4 What an observation row contains

Always an existing v2 `SessionRecord` event — no new event vocabulary, no new row kind:

- **unseat**: `event: unseated`, `state: unseated`, seat dropped; `close_result:
  "observed_dead"`, `close_reason`: evidence detail (e.g. `pane_exited event + process_info
  empty; bus row stale 412s`). Same fields cull already uses (cull.go:417-424).
- **turnover pair**: `registered` (newcomer: new guid, `lineage.cleared_from`, provenance
  mechanism `enroll`-style `observed`? No — provenance.mechanism stays within existing
  vocabulary: `clear`) + `unseated` (displaced), child-first, one lock.
- **enrichment**: `event: recognised`, `state: seated`, sid appended to sids[] with
  `source: "harvest"`, hcom_name filled.
- **re-confirmation / re-bind**: `event: reconciled`, `state: seated`, refreshed
  pane_id/terminal_id/confirmed_at — the shape `reconcile --apply` writes today
  (reconcile.go:94-114).

One new **optional** field, proposed via errata E-4: `observed_via` (string) on session rows
— the probe trail (`"snapshot sweep"`, `"pane_exited event"`, …) for auditability, ignored
by every reader. Everything else reuses the ratified schema; an observer append is
byte-indistinguishable in kind from a CLI append (invariant 1).

## 6. Catch-up sweep (boot = downtime recovery = the same code path)

### 6.1 Semantics

Every observer boot runs the full sweep before entering the loop; "recovery after daemon
downtime" is not a distinct mode — there is no downtime ledger to replay (invariant 4), just
the present: snapshot + hcom list + projection, level-triggered. Missed transitions during
downtime collapse into their end state (an occupant that died and whose pane closed appears
simply as a seated row with no live terminal → one `unseated` row now). The sweep is a
point-in-time query, not an event replay — `session.snapshot` makes this cheap and atomic
enough (single response, one server's coherent view).

### 6.2 No backdating

Correction rows carry `recorded_at` = append time (file order stays the only ordering
authority, spec §5.1); the *evidence* of staleness (bus row age, last confirmed_at) goes in
`close_reason` / `observed_via` prose. The registry never pretends the observer saw something
earlier than it did.

### 6.3 The advisory duty: dormant-live detection

For every **unseated** row, the sweep checks for live counter-evidence: a live hcom row whose
session_id matches the row's sids[] (recognition-grade evidence), or a live pane whose label
matches a (tool, label, cwd)-unique guarded match (assumed-grade). The observer **appends
nothing** for these — re-seating is a repair, and repairs stay explicit verbs (invariant 5;
also §8.2 confines sid-lookup recognition to unregistered seats and seat-continuity to seats,
neither of which a seatless dormant row has). It flags them: status file + log +
`observer status` output, each with the suggested verb (`herder enroll` from the pane /
`herder reconcile --apply` / `herder resume`). The 275a4ac2 test subject is caught here on
the very first sweep: unseated row, live pane, label match ⇒ flagged with evidence. Whether
`herder list` surfaces these flags (it already surfaces anomalies) is left to the impl task
as a SHOULD.

Live unregistered occupants (a pane with an agent no seated row claims) are flagged the same
way, never auto-registered — registration stays with enroll/shim/recognition.

### 6.4 Fallback sid reporting (SHOULD, flagged for review)

Spec §8.3's sid-probe precondition wants an active sid reporter per seat; the sidecar is the
preferred one — and observer-owned seats have none, which is why the live 275a4ac2 pane shows
no `agent_session`. When the observer learns a sid for an observer-owned seat from the bus
(hcom session_id), it pushes `pane.report_agent_session` — a substrate report, not a registry
write (the invariants constrain the registry; the sidecar already performs this exact report,
sidecar.go:686-689). This makes future §8.3 probes of enrolled seats actually work. Scope
call, cheap to drop if review dislikes it.

## 7. Testability: the enroll/observer contract suite

New `tools/herder/tests/check-observer-contract.sh` in the house style (mock-herdr +
mock-hcom + real registry package + temp HERDER_STATE_DIR), exercising `observer sweep`
(single-shot — hermetic, no daemon lifecycle in tests except T-8):

- **T-1 enrolled-seat crash**: enroll a seat (mock pane), kill the mock occupant, sweep ⇒
  exactly one `unseated` row, `close_result: observed_dead`, evidence in close_reason.
- **T-2 enrolled turnover**: mock hcom session_id change in the enrolled seat, sweep ⇒
  child-first turnover pair under one lock; re-run sweep ⇒ noop (idempotence, §5.2 dedupe on
  (seat, new sid)).
- **T-3 downtime catch-up**: seat dies while no observer exists; later sweep converges to the
  same end state as T-1. (Downtime is not a mode — same assertion, different timing.)
- **T-4 absence-of-evidence**: mock herdr socket down ⇒ sweep appends nothing for herdr
  seats, still adjudicates process seats; mock hcom "bus errored" ≠ "row absent".
- **T-5 double observation**: sidecar-style `recognised` append raced with an observer sweep
  ⇒ projection converges, no revert of a rename, no resurrect of a cull (spec AC-32 discipline
  under two writers).
- **T-6 dormant-live flag**: unseated row + live matching pane ⇒ NO append; flag present in
  sweep --json output with suggested verb (the 275a4ac2 scenario, pinned as a test).
- **T-7 ambiguity refusal**: two candidate fallback matches ⇒ no append, flagged.
- **T-8 disposability**: start `observer run`, kill -9 mid-loop, restart ⇒ lock reacquired,
  sweep produces identical net state; no observer-state file is ever read across generations
  (assert by deleting them pre-restart).
- **T-9 v2-only**: suite greps the observer package for legacy `registry.Record`/`Status`
  imports (invariant 3, enforced mechanically).
- **T-10 confirmed-write accounting**: sweep --json reports applied/noop/refused counts that
  reconcile exactly with rows actually appended.

This suite IS the "enroll contract check" the settle-list asks for: T-1/T-2/T-6 are
executable statements of what observation an enrolled seat is owed.

## 8. Failure modes

- **Registry lock contention**: appends block briefly on LOCK_EX like every verb; the
  observer holds the registry flock only inside `UpdateLocked`. Rotation happens under that
  same lock; the tail detects it by inode and reloads (§5.1). Un-acquirable lock (network
  fs) refuses per write.go:39 — outcome `refused`, counted, sweep retries next tick.
- **Partial/torn reads**: only via the quarantining loader; never raw tail-parsing (§5.1).
- **herdr socket down or incompatible**: `status server`-style handshake on connect; protocol
  incompatibility ⇒ log + status-file flag + degrade to CLI fallback if the CLI is itself
  compatible, else herdr-seat probing pauses (observation gap, no verdicts); process seats
  and hcom checks continue. Upstream protocol version (`protocol: 16, compatible: yes`) is
  the recorded stability surface — herdr is an external dependency on its own release
  channel; the observer must fail *soft* on its drift.
- **hcom db locked/errored**: bus-errored ≠ row-absent (sidecar's rule, kept). No occupant
  miss is counted on an errored read.
- **Clock issues**: cadence on the monotonic clock; wall time only in stamps; staleness
  thresholds generous (minutes, not seconds); `recorded_at` never an ordering key (spec).
  Cross-source time comparison (hcom status_age vs local clock) tolerates skew by design of
  the thresholds.
- **Wedged daemon**: heartbeat-mtime detection + nudge kill/restart (§4.3).
- **Event storms / subscription loss**: advice only; sweep is the backstop (§5.2).
- **Two observers racing at boot**: kernel flock arbitration; loser exits 0 (§4.2).

## 9. Migration / rollout order

1. **`observer sweep` + contract suite** (no daemon). Pure, hermetic, reviewable; validates
   probe logic and write discipline before any long-lived process exists. Can be cron'd as a
   stopgap universal observer from day one.
2. **`observer run`** (loop + subscriptions + singleton lock + status/stop verbs), started
   explicitly by operators.
3. **Nudge-start** from seat-creating verbs, behind config `observer.autostart` (ships
   default-on after one bake period on the reference machine).
4. **Parity bake**: observer + sidecars coexist (idempotent double observation, T-5); watch
   refused/noop counts and flagged anomalies.
5. **Deputy demotion decision point** — NOT phase 1a: sidecars keep the status bridge and
   sid reporting; whether their registry-enrichment duty moves to the observer is decided on
   bake evidence. Nothing in 1a blocks either answer (§5.2 idempotence is what makes the
   demotion safe whenever taken — ratified decision record).

Rollback at any step: kill the observer, delete its two files; sidecars never stopped doing
their job; `observer sweep` remains available manually. No verb ever depended on the daemon.

## 10. Explicitly out of phase 1a

Spoke telemetry and inbound deliver verbs (1b, gated on herd-server design); hot reads /
projection cache / `watch` verb (phase 2, gated on legacy-view retirement); any sidecar
behavior change; auto-re-seating of dormant-live rows; recognition/registration of
unregistered occupants; `state: lost` production; plugin packaging.

## 11. Scope calls flagged for adversarial review

1. **Turnover detection for observer-owned seats** (§3) — in, because the incident class is
   the task's WHY; the alternative (observe-only, no turnover) leaves the orchestrator's
   `/clear` invisible for another phase.
2. **Fallback sid reporting** (§6.4) — in as SHOULD; drop is cheap.
3. **60m re-confirmation cadence** (§5.3) — mechanism firm, default tunable.
4. **Dormant-live rows flagged, never repaired** (§6.3) — the conservative reading of
   invariant 5; the aggressive alternative (sid-keyed auto-re-seat) was considered and
   parked: recognition evidence is strong, but re-seating from a daemon crosses the
   advice/repair line the run just ratified.

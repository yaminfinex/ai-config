<!-- Provenance: design-review audit record, 2026-07-14. Docs-only; no production code changed. -->
# Gold-plating audit — grok (shipped) and Pi (designed) delivery/authority machinery

Date: 2026-07-14
Subjects: the shipped grok family (herder `grokbridge`, `launchcmd`, `observercmd`
against hcom 0.7.23) and the Pi first-class design's DR-2/DR-3 machinery (designed,
**nothing built**)
Ground truth inputs:
[flagship crash/parity characterization](2026-07-14-flagship-hcom-crash-parity.md)
(the empirical flagship bar), [hcom-native Pi characterization](2026-07-14-hcom-native-pi-characterization.md),
[grok first-class design](grok-first-class-design.md), [Pi first-class design](pi-first-class-design.md).
Status: audit complete. Verdicts below are **recommendations with evidence for an
owner ruling, never decisions** — the ruling itself is the owner's.

Settled ground honored throughout (not relitigated): the default-homes ruling;
**credential scoping in launch env construction is retained**; herder remains
spawner and registry owner.

## Method

Owner premise under test, verbatim: a "full review of the complexity in both grok
and pi to ensure we aren't trying to build something gold plated when that's not
necessary given the somewhat self healing nature of agent driven systems."

Every mechanism is categorized by **failure class**, because self-healing covers
only some classes:

- **(a) liveness** — stalls, crash-without-replay, hung components. The
  orchestration layer notices silence and re-prompts; machinery guarding only
  this class is the prime gold-plating suspect.
- **(b) integrity** — silent message loss, duplicate/cross-seat delivery, a stale
  process writing as the live one. NOT self-healing: nobody notices, or the
  corruption is permanent by the time they do.
- **(c) authority/security** — capability separation, credential scoping
  (scoping itself is settled-retained and out of scope).
- **(d) observability honesty** — refuse-to-claim versus report-wrong.

For each mechanism: class, whether the flagship bar (claude/codex on native hcom)
has it, whether self-healing actually covers the failure (who heals it, and the
blast radius until they do), measured/estimated cost, and a KEEP / SIMPLIFY /
DELETE verdict with migration cost. Grok verdicts are shipped-code changes; Pi
verdicts are design amendments **before any build — nothing is built, so deletion
is free**.

## The flagship bar (what "parity" means here)

Per the parity characterization: claude and codex acknowledge delivery at hook
**injection** through one shared boundary (`commit_delivery_ack`,
`src/hooks/claude.rs:135` / `src/hooks/codex.rs:427` at hcom 0.7.23), before the
turn settles; a mid-turn crash strands the request with zero unread, no replay,
and recovery is a human/orchestrator re-prompt. Neither flagship has a durable
delivery journal, settlement receipts, epochs, a driver lease, or capability
lanes; deduplication is in-process only. The fleet has run on this bar in
production. Native Pi sits in the same cell on every delivery/recovery row.

## Native-vs-custom: the §0 question, answered for grok

**hcom 0.7.23 does not integrate grok.** The installed binary's launch line is
`hcom [N] claude|gemini|codex|opencode|kilo|pi|omp|antigravity|cursor|kimi|copilot`
(`hcom --help`, v0.7.23), and the installed README's supported-tools table has no
grok row — grok falls under "Anything else: manual via `hcom listen`", and
`hcom listen` is disqualified for a durable consumer by direct probe (destructive
internal cursor, `{from,text}`-only JSON — grok design V5). So unlike Pi, **the
custom grok bridge has no native alternative**: it is not a stronger duplicate of
an existing integration, it is the only transport that exists for the family.
That inverts the scrutiny the Pi custom machinery gets below. Watch item
unchanged: an upstream `hcom grok` launcher has been proposed (grok design
addendum item 4) with PTY-injection delivery, which is owner-rejected for this
family; any hcom upgrade re-opens this section's answer.

For Pi the answer is already on record: hcom 0.7.23 ships a working native Pi
integration (extension + launcher, bound against Pi 0.80.6), with exactly the
flagship receipt placement and crash window.

## Grok — shipped machinery, per-mechanism verdicts

Measured cost of the family (this repository, non-test Go named `*grok*`):
`grokbridge` 2,026 lines (binder 967, journal 436, command 225, mcp 136,
client 130, protocol 83, offline 49); `launchcmd` grok 888; `observercmd` grok
438 — **≈3,350 production lines**, plus wiring in spawn/lifecycle/cull/list
(dozens of touchpoints), **4,324 test lines**, and 311 lines of shim + check
scripts. Launch latency: the bridge-readiness wait is capped at 8 s
(`launchcmd/grok.go:510-523`); typical bind time was not measured in this audit
(unverified).

| Mechanism | Class | Flagship bar has it? | Self-healing covers? | Cost | Verdict | Migration |
|---|---|---|---|---|---|---|
| Anonymous drain contract: oldest-first paged membership query, mandatory ascending-id sort, `msg_delivered_to` predicate, journal-derived cursor (`grokbridge/binder.go:585-624`, `binder.go:812-846`; cursor from replay `journal.go:188-191`) | (b) | n/a — flagships get delivery from hcom natively; grok has no native path (above) | **No** — the guarded failures are reproduced silent permanent loss (page-loss and self-delivery, grok design V3/V9) | Core of binder | **KEEP** | — |
| Spool journal: append-only JSONL, fsync on claim-gating records, truncate-partial-tail + replay (`journal.go:93-132`, `journal.go:243-259`) | (b) | No — and flagships don't need one; grok's cursor must live somewhere durable | **No** — without it a bridge restart either re-delivers or strands backlog silently | 436 lines | **KEEP** | — |
| Wake line + tap + per-seat socket (`binder.go:525-569`, `client.go:52-75`, `command.go:61-75`) | (a) but load-bearing | Equivalent role = hook injection | **No substitute** — an idle grok runs no turns; the monitor line is the only thing that wakes it, and an orchestrator re-prompt is itself a bus message that needs this path to land | Tap is a dumb pipe; modest | **KEEP** | — |
| `HCOM_RECOVER` single recovery line on tap reconnect (`binder.go:492-499`) | (a) | No | Partially — but it is what makes restart cheap instead of a wake-flood | Trivial | **KEEP** | — |
| Fetch stage of the receipt machine (payload served over MCP; foreign-id rejection) (`journal.go:345-363`, `mcp.go:82-95`) | (b) | n/a — flagships inject the payload; grok's wake line carries routing only, so fetch **is** the payload transport | No | Small | **KEEP** | — |
| Ack stage: fetch-before-ack enforcement, `delivered ⇔ acked` (`journal.go:370-388`; enforcement `journal.go:381-383`) | (d), plus closes the crash-after-fetch window | **No — deliberately above the bar**: flagships ack at injection and accept the crash window | Yes for the stall (re-prompt); the ack only changes what herder *claims* | ~40 lines + one MCP call per message + one doctrine line | **KEEP** (evaluated for deletion; see note 1) | Deleting = shipped-code change + doctrine + tests, saves almost nothing |
| Idle-aware nudge loop: `nudgeLoop`, `sessionIdle` phase parsing, `NudgeCandidates`, `--nudge-after`/`--max-nudges` (`binder.go:741-771`, `binder.go:773-809`, `journal.go:418-432`, `command.go:129-131`) | (a) only | No | **Yes** — orchestrator re-prompt is a fresh message = fresh wake; `HCOM_RECOVER` + doctrine `list_pending` also remain | ~130 lines + a vendor-coupled `events.jsonl` phase vocabulary | **DELETE** — and it is already **inert in production**: the launch path never passes `--session-events` (`launchcmd/grok.go:489-494`), so `SessionEvents` is empty and the loop never starts (`binder.go:178-180`). Shipped dead code on the launched path | Small deletion; tests removed; no behavior change on launched seats |
| Generation fencing + exclusive seat flock + stale-generation rejection + client auto-reconnect (`binder.go:108-129`, `journal.go:261-269`, `journal.go:434-436`, `client.go:78-97`) | (b) | No — flagships are a single in-process loop with nothing to fence (parity memo: "in-process dedupe only") | **No** — a tap/MCP connection straddling a binder restart could double-surface or land an ack on a dead generation, silently | Moderate, woven through | **KEEP** — the four-process topology (binder/tap/MCP/spool) creates the race; the topology is forced by grok hosting no managed code | — |
| Session-evidence fencing + `/proc` ancestor capability walk (`binder.go:509-523`, `client.go:100-131`) | (b)/(c) | No | **No** — guards cross-seat MCP calls and the subagent false-`delivered` hazard (DR-4 soundness: ack authorship) | ~80 lines | **KEEP** | — |
| Identity de-latch (`hcom list --name … --json` after `hcom start`) + 15-minute identity refresh loop (`binder.go:221-250`, `binder.go:256-310`, interval `binder.go:30`) | (b) | No (hcom maintains flagship rows itself) | **No** — a reaped/`launch_failed` row drops the seat from hcom's routing fanout; messages sent in that window never enter `msg_delivered_to` and are unrecoverable by any later drain | ~90 lines | **KEEP** — both hazards are live-fleet-proven (placeholder latch; row reaper) | — |
| Manual-guest launch path: ambient-GUID corroboration, foreground wrapper, retire-on-stop (`launchcmd/grok.go:186-293`, `command.go:133`, `command.go:216-225`) | (b) for registry hygiene of a hand-run flow | No | Partly (list-reconcile exists) | ~200 lines | **SIMPLIFY** — candidate 3 below: refuse manual launches with cause+remedy, or keep as a documented dev-only flow | Small shipped-code change |
| Launch-failure marker (`launchcmd/grok.go:432-482`) | (d) | Spawn-side equivalent exists per family | n/a | ~50 lines | **KEEP** — turns an 8 s spawn timeout into an immediate cause+remedy | — |
| Project-config MCP registration + cwd identity checks + symlink refusals + `--cwd` refusal (`launchcmd/grok.go:554-658`, `launchcmd/grok.go:406-416`, refusal `launchcmd/grok.go:810-811`) | (b)/(c) | n/a | No | ~130 lines | **KEEP** — this is the 2026-07-14 default-homes ruling's own shape (seat-worktree `[mcp_servers.hcom]` layer; owner `~/.grok/config.toml` untouched) | — |
| Passthrough refusal list (`launchcmd/grok.go:777-831`) | (c) contract | Flagships have analogous per-family mappings | n/a | ~55 lines | **KEEP** | — |
| Preassigned UUIDv7 session identity, collision checks, resume/fork session evidence (`launchcmd/grok.go:53-69`, `launchcmd/grok.go:93-131`, `launchcmd/grok.go:360-376`, fork argv `launchcmd/grok.go:295-306`) | (b) | Flagships: hcom owns session binding | **No** — the guarded failure is probe-reproduced silent cross-claim (a later same-cwd session claiming an existing identity) | Modest | **KEEP** | — |
| Bridge reuse on resume (`launchcmd/grok.go:418-430`) | (a)/efficiency | n/a | n/a | Trivial | **KEEP** | — |
| Observer incremental cursors + 64-byte SHA-256 fences + fail-closed 16 MiB line cap (`observercmd/grok.go:46-62`, `observercmd/grok.go:277-347`, cap `observercmd/grok.go:22`) | (d) + efficiency | Flagship observers were not compared in this audit (unverified) | Worst case without it: a stale or double-counted status label | ~180 of 438 lines | **KEEP** — it is the refuse-to-claim half of DR-5, and the incremental read is also the sweep-cost bound | — |

**Note 1 — the ack stage, evaluated honestly.** Collapsing `delivered` to
`fetched` would exactly match the flagship injection-time bar (a crash after
fetch, before processing, would strand the message unlisted — the same window
the flagships accept). It was evaluated and is **not recommended**: the stage is
already shipped and working, its whole cost is one MCP call per message and ~40
lines, and it is the only thing that makes "delivered" mean anything for a
harness with no injection surface. This is the one place grok deliberately
exceeds the bar, and it is nearly free. Deleting machinery is worthwhile when it
buys surface reduction; this deletion buys none.

**Grok headline.** The bridge is not gold-plating at its core: hcom 0.7.23 has
no grok integration, so the drain contract, spool, wake path, and fencing are
grok's *substitute* for what hcom does natively for the flagships — parity
plumbing, not extra strength, and most of it guards reproduced integrity
failures (class b) that self-healing does not cover. The genuine gold-plating is
at the margins: the nudge loop (liveness-only, redundant with orchestration
re-prompting, **and already dead on the launched path**) and the manual-guest
launch path (~200 lines for a rare hand-run flow).

## Pi — designed machinery, per-mechanism verdicts

Nothing here is built; every DELETE is a design amendment with **zero code
migration cost**. Estimated build cost of the machinery as designed (from the
grok yardstick plus the design's own test plan): the design's U1 alone carries
the spool/state machine, ten bus ops, two capability lanes, the launch-attempt
protocol, a TypeScript extension with a supervised driver, and gates T1–T16 +
T25/T26 + T28–T35 (T34 alone has nine lettered branches); given grok's smaller
machine cost ≈3,350 production + 4,324 test lines, the Pi build plausibly
**meets or exceeds that scale, in two languages**, plus the probe register
(A1–A11, P1–P7) and five staged units with adversarial review each (estimate —
unverified). The flagship-parity alternative is measured in the parity memo:
add `pi` to `IsHcomCapable` (`tools/herder/internal/launchcmd/launch.go:19-26`,
verified in this repository), one `setEnvDefault` env-pin line, and the existing
exec-into-`hcom <tool>` path — **a few launch-contract lines**.

| Mechanism (as designed) | Class | Flagship bar has it? | Self-healing covers? | Build cost | Verdict | Migration |
|---|---|---|---|---|---|---|
| Durable per-message spool journal, `queued → injected → delivered` state machine, two id namespaces (Pi design DR-2 States/Persistence) | (b) | **No** (parity table: no flagship has any durable journal) | The stall it recovers is class (a) — covered by re-prompt; the duplicate windows it manages are windows the flagships also have and the fleet accepts | Large (journal + state adapter + bus ops + replay) | **DELETE** — adopt hcom-native Pi delivery | Design amendment only |
| Settlement-correlated receipts (`delivered` = settle after durable injection) | (d)+(a) | **No** — both flagships ack at injection (`claude.rs:135`, `codex.rs:427`); one reproduced crash run per harness confirms the identical stranded-request window | **Yes** for the stall: orchestrator/human re-prompts; in the codex probe hcom's request-watch even notified the sender. Blast radius: one stalled task until noticed. The receipt over-claim is real but is precisely the bar the fleet runs on | Medium | **DELETE** — accept the injection-time receipt as the Pi bar. If the one crash window is the sole worry, the parity memo's option 3 (a small settlement-ack fork of the native extension) is the low-regret hedge, orderable separately | Design amendment |
| Crash replay + duplicate reconciliation + per-id nudge budget + `stalled` terminalization | (a)/(b) | No | Mostly yes (re-prompt); the duplicate-recognition envelope exists to manage a window the flagships simply accept | Medium | **DELETE** with the journal | Design amendment |
| Ownership epochs + activation fencing + the launch-attempt protocol (gated child, attempt generations, quiesce sweeps) | (b) | **No** — absent from all three native integrations (parity table) | Not self-healing as a class — but the mutations they fence are journal/bus-op writes that **won't exist** once the journal is deleted; second-process-per-seat is prevented operationally by herder being the sole spawner, exactly as for claude/codex today | Large (the protocol alone is ~90 design lines and test branch T34f) | **DELETE** with the delivery machine | Design amendment |
| Progress-attested driver lease + `renew` op + supervised driver loop | (d)/(a) | No | Yes — a hung driver is a silent seat; the orchestration layer notices silence. Native adoption removes the driver entirely, so there is nothing to attest | Medium | **DELETE** with the driver | Design amendment |
| Capability lanes: seat token (stdin, hash, rotation, bootstrap invariant) + operator capability (interactive mint/rotate, no-auto-acquisition, cgroup belts) | (c) | **No** — flagship hooks hold the ordinary hcom CLI (parity table: "no epoch, lease, or capability lanes"); the whole fleet runs on the cooperative same-UID trust model | n/a (authority, not liveness) — but the control plane they gate (ten bus ops mutating seat state) is deleted with the machine; what remains reachable is what flagship seats already reach | Large (design rounds 4–9 are mostly this; test T34 a–i) | **DELETE** with the control plane. Explicitly out of scope: **credential scoping stays** (settled) — the lanes are not the scoping | Design amendment |
| Spool bounds: prospective admission, oversize records, reserved-headroom arithmetic, quota states | (b) resource | No | n/a | Medium | **DELETE** with the spool | Design amendment |
| DR-3 extension activation predicate + inertness branches + gated-child record check | (c)/(b) | No | n/a | Medium (T18 branches a–e) | **DELETE** with the herder-owned extension — hcom's native extension carries its own launch-state binding, demonstrated against Pi 0.80.6 | Design amendment |
| Retained regardless of the delivery choice | — | — | — | Small | **KEEP**: credential env scoping (settled); launch-contract env pinning + recorded vendor version; herder as spawner/registry owner (settled); the DR-6 observer/sesh session-JSONL adapter (orthogonal to delivery); doctrine content | — |

**The standing keep-custom decision, addressed squarely.** The hcom-native Pi
characterization ruled "keep the custom DR-2 inbound state machine" on two legs:
(1) a small settlement-ack fork does not by itself deliver the full multi-batch
crash-correlation contract, and (2) "more decisively," native Pi has no epochs,
lease, lanes, or herder lifecycle authority. Leg 1 is true but assumes the full
contract is required — the flagship parity memo establishes empirically that the
fleet's production bar does not include it. Leg 2 lists properties that **no
flagship has either** (parity table, bottom four rows), so they cannot be
justified by parity; they must be independently required, and no evidence in any
of the three records establishes that requirement. The parity memo's own framed
recommendation — option 1, flagship parity, unless the non-delivery properties
are required for reasons outside the probes — is the position this audit's
per-mechanism analysis independently reaches. Reconciling the two standing
records is the owner ruling this memo feeds.

**Pi headline.** Measured against the bar the flagships actually run on, the
DR-2/DR-3 delivery and authority machinery is **almost entirely above-bar**:
every distinctive property (durable journal, settlement receipts, epochs, lease,
lanes, launch-attempt protocol, extension gating) is absent from all three
native integrations, and the failures the delivery half guards are dominated by
class (a) stalls that orchestration-layer re-prompting already heals — at the
cost of a build plausibly exceeding the entire shipped grok family, in two
languages. The honest exceptions: the settlement receipt does close a real,
reproduced report-wrong window (class d) — the cheap hedge is the native-fork
settlement ack, not the full machine — and the duplicate windows are real but
identical to the ones claude/codex ship with today.

## Filed-ready simplification candidates (for owner ruling)

1. **Pi design amendment: adopt flagship-parity delivery and delete the
   DR-2/DR-3 delivery+authority machinery.** Before any build unit dispatches:
   amend the Pi first-class design to wrap `hcom pi` exactly as claude/codex are
   wrapped (add `pi` to the launch capability gate, pin the Pi home env var
   beside the existing config-dir pins, exec into the native launcher), deleting
   the spool journal, settlement receipts, replay/nudge policy, ownership
   epochs, launch-attempt protocol, driver lease, capability lanes, and the
   herder-owned extension with its activation predicate. Retain unchanged:
   credential env scoping (settled), launch-contract env pinning with recorded
   vendor version, herder as spawner/registry owner (settled), the observer/sesh
   session-JSONL adapter, and doctrine. Register as an owner-signed delta that
   Pi seats accept the flagship crash window (injection-time receipt, no replay,
   re-prompt recovery). Migration cost: design amendment only — nothing is
   built.
2. **Pi follow-up hedge (separable, optional): settlement-ack fork of the
   native extension.** If the one reproduced silent-strand crash window is worth
   closing, fork hcom's Pi extension to move the ack from post-`sendUserMessage`
   to a settle handler for the serialized single-batch case — the parity memo's
   option 3 — without journal, epochs, lease, or lanes. File only if candidate 1
   is ruled in and the window still worries the owner.
3. **Grok: delete the idle-aware nudge machinery.** Remove `nudgeLoop`,
   `sessionIdle`, `NudgeCandidates`, and the `--session-events`/`--nudge-after`/
   `--max-nudges` surface plus their tests. It is liveness-only, redundant with
   `HCOM_RECOVER` + doctrine `list_pending` + orchestration re-prompting, and it
   is **already inert on the production launch path** (launch never passes
   `--session-events`, so the loop never starts on launched seats) — deleting it
   changes no launched-seat behavior and removes a vendor-coupled `events.jsonl`
   phase vocabulary. Alternative if the owner wants nudges: wire the flag at
   launch instead — but that is adding machinery, and nothing in fleet operation
   has been shown to need it (no telemetry examined — unverified).
4. **Grok: retire or fence the manual-guest launch path.** Replace the
   ambient-identity corroboration, foreground wrapper, and retire-on-stop
   branch (~200 lines) with a cause+remedy refusal pointing at `herder spawn
   --agent grok`, or explicitly document it as a dev-only flow and exclude it
   from the supported contract. Migration: small shipped-code change; check
   first whether any check script or smoke depends on the manual path.

## Honesty register

- Every grok claim above cites this repository at the audited branch head;
  flagship-bar claims cite the parity memo, which itself carries the
  one-reproduced-run caveat per harness.
- **Unverified**, marked as such above: typical (uncapped) grok bridge bind
  latency; the Pi build-cost estimate (scale argument from the grok yardstick
  and the design's own test plan, not a measured figure); whether flagship
  observers carry cursor-fence equivalents; whether any fleet grok seat has
  ever needed a nudge (no run telemetry was examined, and the launched-path
  nudge loop being inert means production could not have exercised it).
- Load-bearing walls, stated plainly so the delete story stays honest: the grok
  drain contract, spool, fencing, identity de-latch/refresh, and session
  evidence all guard **reproduced class-(b) failures** and have no native
  substitute; deleting them would not be simplification, it would be breaking
  the only transport the family has. For Pi, the settlement receipt does close
  a real report-wrong window — the recommendation deletes it because the fleet
  demonstrably operates on the weaker bar and a cheap partial hedge exists, not
  because the window is imaginary.
- Any hcom version change re-opens the native-vs-custom section for both
  families.

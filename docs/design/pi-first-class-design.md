<!-- Provenance: design record, 2026-07-13. Design only; implementation is staged separately (§13). -->
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
and the pinned isolated install dissolve to the live default Pi home and the
vendor-updated default install with a recorded vendor version; credential
scoping in launch env construction is retained, the DR-2 delivery/authority
machinery stood unchanged at that round per the then-standing keep-custom
decision (`docs/design/2026-07-14-hcom-native-pi-characterization.md`), and
every property that ruling weakens is recorded for sign-off in §12 item 9;
round 11,
2026-07-14: the owner **flagship-parity ruling** — Pi adopts flagship-parity
delivery: herder wraps `hcom pi` exactly as it wraps claude/codex, and the
DR-2/DR-3 delivery+authority machinery (durable spool journal +
queued→injected→delivered state machine, settlement-correlated receipts,
crash replay + duplicate reconciliation + nudge budgets, ownership epochs +
activation fencing + the launch-attempt protocol, the progress-attested
driver lease, the seat-token/operator capability lanes, spool bounds, and the
herder-owned TypeScript extension with its activation predicate) is **deleted
from the design before any build**. The flagship crash window
(injection-time receipt, no replay, re-prompt recovery) is an owner-accepted,
registered delta (§12 item 10). The hcom-native Pi characterization's
keep-custom decision is **superseded** by this ruling — its probe evidence
stands. Retained unchanged: credential env scoping, launch-contract env
pinning + recorded vendor version, herder as spawner/registry owner, the
DR-6 observer/sesh session-JSONL adapter, and doctrine content), pending
re-certification on the round-11 amendment diff
Subject: `@earendil-works/pi-coding-agent` against herder + hcom 0.7.23 —
characterized at 0.80.6; deployed vendor-updated at the default install per
the 2026-07-14 default-homes ruling; delivery via hcom's native Pi
integration per the 2026-07-14 flagship-parity ruling

Evidence base (cited throughout by path + section):

- `docs/design/pi-demo-report-2026-07-13.md` — the settled characterization record:
  installation provenance, home/state mapping, offline/telemetry behavior, the
  extension-lifecycle probes, session model, provider routing, earned-clause table.
  Double-reviewed; this design does not re-derive or contradict it.
- `docs/design/2026-07-14-hcom-native-pi-characterization.md` — the hcom-native
  Pi integration probe record: the native extension loads and binds against
  Pi 0.80.6, idle wake, busy follow-up delivery, ordering, resume fidelity, the
  reproduced injection-time-ack crash window, the extension placement coupling
  (`HCOM_DIR`/`HOME`), and the PATH constraint. Its **decision** ("keep the
  custom DR-2 inbound state machine; the Pi design stands unchanged") is
  **SUPERSEDED by the 2026-07-14 flagship-parity ruling** (§12 item 10, and the
  superseded-decision note in DR-2 below); its probe **evidence** remains valid
  and load-bearing throughout this amendment.
- `docs/design/2026-07-14-flagship-hcom-crash-parity.md` — the flagship
  (claude/codex) crash/parity characterization: both flagships acknowledge
  delivery at hook injection through the shared `commit_delivery_ack` boundary,
  carry the identical mid-turn crash window, and run in production on that bar;
  plus the costing of wrapping `hcom pi` exactly as claude/codex are wrapped.
  This is the empirical bar the round-11 ruling adopts.
- `docs/design/2026-07-14-delivery-machinery-gold-plating-audit.md` — the
  per-mechanism audit of the DR-2/DR-3 machinery against that bar; its
  candidate 1 is the ruling this amendment executes.
- `docs/design/grok-first-class-design.md` — the house pattern for a family
  design, the observability-honesty rules (its DR-5), and the
  staging/activation discipline (its §11). (Its DR-1 drain contract was the
  former Pi pickup contract; that inheritance is superseded by the
  flagship-parity ruling — inbound delivery for Pi is hcom-native.)
- Grok family activation and hardening evidence (hcom 0.7.23; recorded in the
  grok program's backlog notes and review threads), retained where the
  surviving surfaces still cite it: credential presence checked by name in a
  fresh non-interactive login-shell environment; status-op-authoritative bind
  capture as the honesty pattern for spawn.

## 1. Settled ground (binding; not relitigated here)

Rows superseded by an owner ruling are rewritten in place; the round-10 and
round-11 header entries and §12 items 9–10 record what changed and what it
costs. The demo report's and the characterizations' findings remain valid
evidence throughout — what changed is the machinery ruled on top of them.

| Constraint | Source |
|---|---|
| Pi seats run against the **live default Pi home and the vendor-updated default install**: `HOME` is the operator's real home; Pi's agent dir, session root, and XDG roots resolve to their defaults. Ruling context, binding: single-purpose machines; ringfencing expressly not required; the claude/codex live-home fleet norm extends to Pi; seat-scoped behavior deltas ride **launch env only**, never owner config writes. | owner ruling 2026-07-14 (default homes; standing-orders 20.8); demo "Managed home and state model" retained as characterization evidence |
| Delivery and binding are **hcom's native Pi integration**: hcom writes its own Pi TypeScript extension, runs Pi under `hcom pty pi`, binds the extension to the reserved identity with `hcom pi-start`, wakes over loopback TCP, drains with `hcom pi-read`, and injects via `pi.sendUserMessage` (`deliverAs: "followUp"` when busy). Herder wraps the launch exactly as it wraps claude/codex (§2) and owns the registry seat. The former herder-owned extension, spool, and bus-op control plane are **deleted** (round 11). | owner ruling 2026-07-14 (flagship parity); hcom-native characterization "Launch mechanism and compatibility" (loaded and bound against Pi 0.80.6) |
| The delivery receipt is the **flagship injection-time receipt**: the unread cursor advances when the message is injected, before the turn settles — the same placement claude and codex commit through the shared `commit_delivery_ack` boundary. The mid-turn crash window this carries is an **owner-accepted, registered delta** (§12 item 10). | owner ruling 2026-07-14 (flagship parity); parity memo "Source: where the flagship cursor advances"; characterization crash probe |
| Offline/update suppression: `PI_OFFLINE=1` (couples the version-check skip) plus `PI_TELEMETRY=0`, carried as seat-scoped launch-env deltas. `PI_SKIP_VERSION_CHECK=1` alone is too narrow. Inference is not gated by offline mode. | demo "Startup network and update behavior" |
| Credentials, **env-channel scoped**: herder routes **one provider per seat**, by environment, referenced **by name only** — never in argv or in anything herder writes (registry, logs, doctrine, reports). A cross-provider model change is a controlled relaunch with a re-filtered environment. This claim scopes the **herder-routed channel only**: Pi's own resolution can also reach credential-bearing owner config in the live home (DR-5; §12 item 9a). **Retained unchanged by the flagship-parity ruling.** | owner rulings (credential scoping settled; retained through both 2026-07-14 rulings); demo "Provider routing and least privilege" |
| Install integrity: the install is the **vendor-updated default**; herder records the **observed vendor version** at provision and at every launch — no hash gate, no supported-version refusal. Version-drift consequences are an owner-signed delta (§12 item 9b). **Retained unchanged by the flagship-parity ruling.** | owner ruling 2026-07-14 (default homes); demo "Installation provenance" retained as evidence |
| Every **seat launch** receives the herder-constructed environment (env deltas + exactly one named provider credential — DR-5), built by the launch wrapper before it execs into `hcom pi` (§2). | owner rulings; demo evidence retained |
| Herder writes **no owner Pi config, ever**. Under the native path herder writes **no artifact in the Pi home at all**: the extension in `agent/extensions/hcom.ts` is hcom's own, installed and refreshed by hcom's launcher (replaced when its contents differ from the bundled source). The former herder-managed extension is deleted (round 11). | owner rulings 2026-07-14; characterization "Launch mechanism" |
| The former per-launch `/proc` environment ceremony (CONDITIONAL under the demo) is **dissolved with the direct-exec launch path** (round 11): Pi launches ride the identical exec-into-`hcom <tool>` chain the flagships use, which carries no such ceremony anywhere in the fleet. Env delivery assurance for Pi seats is exactly the flagship bar. | owner ruling 2026-07-14 (flagship parity); parity memo "Costing" |
| Pi sessions are versioned JSONL trees: header carries format version, session UUID, timestamp, cwd, optional parent-session reference; `--fork` creates a parent-linked file. The observer/sesh adapter (DR-6) consumes this surface — **retained unchanged**. | demo "Session compatibility" |
| The former hcom pickup contract inheritance (grok DR-1: anonymous paged drain, journal-derived cursor, `msg_delivered_to`) is **superseded for this family** (round 11): inbound delivery is hcom's native Pi path; herder runs no drain for Pi seats. The grok contract remains grok's — nothing here touches it. | owner ruling 2026-07-14 (flagship parity) |

## 2. Architecture overview — flagship-parity delivery topology and launch contract

The topology is the flagship topology. Herder is the spawner and registry
owner; hcom owns process binding, identity, and delivery, exactly as it does
for claude and codex today:

```text
herder spawn --agent pi --provider <family> [--model <id>]
   │  launchcmd: IsHcomCapable gate (launch.go:19-26)
   │  env construction: exactly one named provider credential (DR-5);
   │    PI_OFFLINE=1, PI_TELEMETRY=0; Pi-home pin beside the existing
   │    config-dir pins (launch.go:28-48) when HCOM_DIR is isolated;
   │    HCOM_LAUNCH_INFLIGHT=1; sidecar started
   ▼
syscall.Exec → `hcom pi --run-here`          (launch.go:203-216; resume/fork
   │                                          via `hcom r|f <target>`)
   ▼
hcom: reserves the identity, writes its Pi extension
   (agent/extensions/hcom.ts), runs Pi under `hcom pty pi`, binds via
   `hcom pi-start` (session UUID + process id), loopback TCP wake,
   `hcom pi-read` drain, pi.sendUserMessage(...) — followUp when busy
   ▼
Pi turn → provider inference
   receipt: unread cursor advances at injection (the flagship bar;
   owner-accepted crash window — §12 item 10)

herder, alongside: registry seat (spawn/list/cull), bind capture from hcom's
roster (name, tool `pi`, session UUID, transcript path), recorded vendor
version, observer/sesh session-JSONL adapter (DR-6)
```

Outbound is the flagship shape too: doctrine directs the model to `hcom send`
(the ordinary CLI every flagship seat holds). The former journaled
`herder pi send` wrapper dissolved with the spool.

### The launch contract (the retained set, specified)

The additive implementation surface is a few launch-contract lines beside the
existing families (parity memo "Costing", verified against this repository):

1. **Capability gate.** Add `pi` to `IsHcomCapable`
   (`tools/herder/internal/launchcmd/launch.go:19-26`) — the single source of
   truth routing `herder spawn --agent pi` through the exec-into-hcom path.
2. **Pi-home env pin.** A `setEnvDefault` pin for Pi's home variable
   (`PI_CODING_AGENT_DIR`, pinned to Pi's **default** agent dir) beside the
   existing `CLAUDE_CONFIG_DIR`/`CODEX_HOME`/`GEMINI_CLI_HOME` pins in
   `PinConfigDir` (`launch.go:28-48`). Two pinned constraints from the Pi
   record govern this line:
   - **Placement coupling.** hcom honors `PI_CODING_AGENT_DIR` only when its
     derived tool-config root equals `HOME`; otherwise the extension is
     written below the tool-config root, where Pi will not load it
     (characterization, `src/hooks/pi.rs:350-362` at hcom 0.7.23). On the
     default bus (`HCOM_DIR=$HOME/.hcom`) the lineup holds by construction —
     and `PinConfigDir` does not fire there at all. On an **isolated bus**
     (`HCOM_DIR` re-pointed — the only case where the pin fires) the pin
     alone may not restore the lineup; probe P8 (§10) characterizes the
     isolated-bus launch and fixes the working env shape before the build
     unit closes.
   - **Session-root non-pin.** hcom deliberately clears
     `PI_CODING_AGENT_SESSION_DIR` for the child (characterization,
     `src/shared/tool_detection.rs:174-178`); no session-root pin exists or
     is wanted — sessions land under the default agent dir, consistent with
     the default-homes ruling.
3. **Exec wiring.** Reuse the existing exec path unchanged
   (`launch.go:203-216`): `hcom pi --run-here` for spawn, `hcom r|f <target>`
   for resume/fork, `HCOM_LAUNCH_INFLIGHT=1`, sidecar start, tag passthrough.
4. **Credential scoping (DR-5, retained).** The pi branch constructs the
   child environment before the exec: exactly one provider credential, by
   name, per the seat's `--provider`; `PI_OFFLINE=1`/`PI_TELEMETRY=0`. This
   env-construction step is the one place the pi launch differs from
   claude/codex's ambient-env exec, and it is a launch-env delta, not
   delivery machinery. hcom's own forwarding of that environment is
   mechanically private (0600 sidecar, sourced and removed — characterization
   "Launch mechanism") but not policy-scoped; the policy lives upstream in
   herder's construction, which is why scoping survives the ruling intact.
5. **PATH floor.** The directory containing the hcom executable must be on
   the child `PATH`: the native extension invokes `spawn("hcom", ...)`
   (characterization, `src/pi_plugin/hcom.ts:39`); without it the extension
   emits `spawn hcom ENOENT` and never binds. Probe P9 (§10) pins this on the
   real spawn path.
6. **Recorded vendor version.** The launch/bind path reads the installed
   `package.json` (reading it creates no state) and records the observed
   version in the seat's registry row — install-latest, recorded not pinned
   (§12 item 9b). No provisioning ceremony exists or is needed.
7. **Bind capture.** Spawn confirms the seat from hcom's roster exactly as
   for the flagships: `tool: pi`, `hooks_bound: true`, the session UUID and
   transcript path (all demonstrated in the characterization). No-bind within
   the window fails the spawn legibly — the standard family behavior, no
   Pi-specific machinery.
8. **Doctrine.** Content retained (addressing rules, send discipline, the
   credential never-print rule, and the crash-window framing: a re-prompted
   request may repeat content already in context). Carriage rides the same
   bootstrap seam the flagships use — hcom's native Pi extension injects
   bootstrap doctrine at bind (characterization "Launch mechanism", layer 3),
   and the initial task prompt rides herder's standard verified spawn-prompt
   delivery. The exact wiring against the installed hcom's bootstrap surface
   is a build-unit detail (§13), not new machinery.

---

## DR-1 — Binding ownership: hcom's native integration is the binder; herder is spawner and registry owner

**DECISION (rewritten in place, round 11).** The Pi family adopts the
flagship ownership split: **hcom owns the process binding and delivery**
(reserve, launch under `hcom pty`, extension install, bind, wake, drain,
inject, resume), and **herder owns the seat** (spawn, registry row, list,
cull, observation) by wrapping `hcom pi` exactly as it wraps `hcom claude`
and `hcom codex` — the launch contract in §2. Nothing herder-owned runs
inside the Pi process; no herder-owned extension, bus-op control plane, or
drain loop exists for this family.

The former decision here — the herder-owned extension as binder-owner, with
all bus mechanics in `herder pi bus` ops over a transport-neutral extraction
of the grok contract primitives — is **superseded by the flagship-parity
ruling**. Its motivating fork (where do bus mechanics run when herder owns
delivery?) is dissolved rather than re-answered: herder does not own delivery
for this family. The native `hcom pi` launcher/extension, which the previous
round evaluated head-on and ruled out as the production delivery boundary
(the keep-custom decision, now superseded — §12 item 10), **is** the
production delivery boundary.

What survives from the former DR-1, because it was never delivery machinery:

- **Herder as sole spawner and registry owner** (owner-settled, retained).
  Second-process-per-seat is prevented operationally by herder being the only
  spawner — exactly the claude/codex posture (audit, epochs row).
- **hcom-side honesty.** The registry's `tool: pi` row is authoritative for
  seat identity; roster facts (hooks_bound, session UUID, status) are hcom's
  and are surfaced as hcom's (DR-6).

## DR-2 — Inbound delivery state machine and recovery — SUPERSEDED (stub; number retained for external references)

**SUPERSEDED by the owner flagship-parity ruling, 2026-07-14.** The machinery
this DR designed — the durable per-seat spool journal and
queued→injected→delivered state machine with two id namespaces;
settlement-correlated receipts (`delivered` = settle observed after a durable
injection record); crash replay, duplicate reconciliation, and the per-id
nudge budget with `stalled` terminalization; ownership epochs, activation
fencing, and the launch-attempt protocol (gated child, attempt generations,
quiesce sweeps); the progress-attested driver lease and the specified inbound
driver; the seat-token and operator-capability control lanes with their
lifecycles; and the spool bounds (prospective admission, oversize records,
reserved headroom, quota states) — is **deleted from the design before any
build**. Nothing was built; the deletion is a design amendment with zero code
migration cost (audit, candidate 1).

Delivery for Pi seats is hcom's native path (§2), on the flagship bar:
injection-time receipt, in-process dedupe only, no durable journal, no
replay, no epochs/lease/lanes. What that weakens and what heals it is
registered as an owner-signed delta in **§12 item 10** — the honesty register
this stub points at rather than repeats.

Provenance, preserved rather than erased: the keep-custom decision of
`docs/design/2026-07-14-hcom-native-pi-characterization.md` ("keep the custom
DR-2 inbound state machine; the Pi design stands unchanged") was the standing
ruling for this DR through round 10. It is superseded by the flagship-parity
ruling on the evidence of
`docs/design/2026-07-14-flagship-hcom-crash-parity.md` (the flagships run in
production on the identical receipt placement and crash window, through the
shared `commit_delivery_ack` boundary) and the per-mechanism audit
(`docs/design/2026-07-14-delivery-machinery-gold-plating-audit.md`): every
distinctive DR-2 property is absent from all three native integrations, and
the failures its delivery half guarded are dominated by liveness stalls that
orchestration-layer re-prompting already heals. The characterization's probe
evidence (native extension compatibility, busy follow-up delivery, the
reproduced crash window, placement coupling, PATH constraint) remains valid
and is load-bearing in §2. The full superseded text is preserved in this
document's git history at the round-10 revision.

The separable settlement-ack hedge (a small fork of the native extension
moving the ack to a settle handler — parity memo option 3, audit candidate 2)
is **not** part of this design: it is orderable later as its own unit if the
crash window comes to worry the owner, and nothing in this design blocks it.

## DR-3 — Launch contract — SUPERSEDED as designed; replaced by the flagship launch contract in §2 (stub; number retained)

**SUPERSEDED by the owner flagship-parity ruling, 2026-07-14.** The launch
path this DR designed — a herder-owned `launchcmd` branch execing the
provisioning-recorded vendor entry point directly with a fully constructed
environment; `herder pi provision`; the managed-extension install with its
activation predicate and inertness branches; the spool-borne doctrine/prompt;
the gated-child launch sequence under the launch-attempt protocol; the
operator-capability mint; the conditional per-launch `/proc` environment
assertion; and the Pi-specific passthrough refusal list built for that direct
exec — is **deleted from the design**. Its premise ("nothing routes through
an `hcom <tool>` launcher") was the keep-custom decision's, and is superseded
with it.

The launch contract is now the flagship shape, specified in full in §2:
capability-gate line, Pi-home env pin (with the placement-coupling and
session-root constraints), exec into `hcom pi`, credential scoping in env
construction (DR-5, retained), PATH floor, recorded vendor version, bind
capture, doctrine carriage. Retained from this DR **unchanged in substance**
and re-homed there: credential env scoping; the offline/telemetry launch-env
deltas; recorded-vendor-version discipline (install-latest, recorded not
pinned); launch refusal with cause+remedy when no Pi is resolvable or the
named credential is absent from the environment the pane actually receives
(fresh-pane truth — the grok activation lesson). Passthrough refusals are
finalized against the `hcom pi` launch surface at the build unit (§11), per
the family norm.

The former `/proc` ceremony conditional is **dissolved**, not resolved: it
attached to the deleted direct-exec path. Pi launches now ride the identical
exec chain the flagships use, which carries no such ceremony fleet-wide (§1).

## DR-4 — Identity, sessions, lifecycle

**DECISION (rewritten in place, round 11).** Identity, session binding, and
lifecycle ride hcom's native integration, exactly as for claude and codex:
hcom reserves the bus identity, binds it to the Pi session UUID at
`pi-start`, reclaims both on resume, and creates parent-linked sessions on
fork. The characterization demonstrated the full loop against Pi 0.80.6:
generated name, session UUID, cwd, and transcript reported correctly; resume
reclaimed the same name and UUID; `--fork` produces a parent-linked session
file (demo "Session compatibility").

- **Registry binding.** The seat row records the hcom name, `tool: pi`, the
  session UUID and transcript path captured from hcom's roster at bind, the
  provider, and the recorded vendor version. No cwd-keyed claim path exists
  anywhere (unchanged principle: Pi session files are cwd-labeled and the
  shared default session root makes location-based claims meaningless).
- **Resume** re-enters the same seat through `hcom r <target>` via the
  standard herder resume wrapper (§2 exec wiring): same hcom name, same Pi
  session, same registry seat. Unread bus messages deliver on rebind through
  the native drain; there is no replay past the injection-time ack (§12
  item 10).
- **Fork** creates a new seat through `hcom f <target>`: new name, new
  parent-linked session, registry lineage per the standard herder fork path.
- **Cull** is the standard herder row-stop path for hcom-capable families —
  no Pi-specific retirement machinery. Registry lifecycle transitions require
  process-level evidence, never session events (unchanged).
- **Post-resume status blemish, known:** the characterization observed a
  transient `blocked: launch_blocked` roster state immediately after resume
  despite `hooks_bound: true`. An observability blemish, hcom-side; DR-6's
  honest-labeling rule covers it (herder surfaces roster facts as hcom's,
  never re-synthesized).

The former content here — preassigned UUIDv7 session identity with the
P1/A5 resolution order, extension-published capture, the session-drift state,
and the two-phase fenced retirement — attached to the deleted herder-owned
delivery machinery and is superseded with it (round 11). hcom owns session
binding on the native path; herder does not re-derive it.

**Subagents.** Delivery receipts on the native path are extension-observed
(hcom's extension), not model-acked, so a subagent cannot forge a delivery
receipt. The residual risk is credential-shaped (a child inherits the
provider key — inherent, demo-documented) and is inside the accepted
model-tool boundary (DR-5, §9).

## DR-5 — Multi-provider surface and least privilege

**DECISION (retained; mechanism references re-pointed to §2).** A seat
declares its provider explicitly at spawn; herder filters the environment to
exactly that provider's credential; provider changes are supervised
relaunches. Nothing guesses.

**Spawn syntax.** `herder spawn --agent pi --provider <family> [--model <id>]`.

- `--provider` is **required** (no default pending the owner ruling, §12
  item 1). The provider table is family config, initially exactly the
  demo-proven rows:

  | Provider family | Credential name routed | Demo evidence |
  |---|---|---|
  | `anthropic` | `ANTHROPIC_API_KEY` | success (demo provider table) |
  | `openai` | `OPENAI_API_KEY` | success |
  | `xai` | `XAI_API_KEY` | success |

  Unknown provider → refusal naming the supported set. New rows enter via
  characterization, not assumption.
- `--model` passes through to Pi's argv via the `hcom pi` launch line. Herder
  does not maintain a model catalog and does not validate model↔provider
  pairing beyond what Pi itself enforces. There is no model-prefix guessing
  map.
- The registry row records `provider: <family>` and the requested model.

**Least-privilege filtering at exec.** The §2 env construction includes
exactly the one credential name from the provider table — by name, value
never inspected beyond nonempty, never logged. Pi's tools and extension
children inherit the Pi process environment (demo: "a seat must receive only
the credential required for its selected provider") — the accepted model-tool
credential boundary (§9). Outbound `hcom send` invocations the model runs are
ordinary tool children and inherit the seat env, exactly as on claude/codex
seats today; no scrub wrapper exists on the native path, and none is claimed.
hcom's forwarding of the launch environment to Pi is mechanically private
(0600 sidecar) but not policy-scoped — the scoping is herder's env
construction, upstream (§2 item 4).

**Credential-bearing owner config — the env channel is the scoped channel;
every other source is owner state.** Pi resolves credentials from an explicit
CLI key, `agent/auth.json`, environment variables, or custom-provider
(models) config (demo "Provider routing") — four sources, each dispositioned:
the explicit CLI key is herder-controlled (herder never passes one, and
credential/auth-file passthrough arguments are refused — §2/§11); the
environment is herder-constructed; the auth store **and**
custom-provider/models config are **owner state in the live home**, open
channels under the default-homes ruling. The owner may legitimately populate
them at any time, so no launch gate or drift-termination polices them
(dissolved at round 10). Stated exactly, delta included:

- **Env-channel scoping — retained.** The launch env carries exactly one
  provider credential, by name. Through the environment, a cross-provider
  switch cannot obtain a credential. This is the **only** single-provider
  claim this design makes anywhere.
- **Owner-config channel honesty — the delta, owner-signed (§12 item 9a).**
  Whatever credentials the owner's live auth store or custom-provider/models
  config hold are reachable by every seat process through Pi's own resolution
  order — in-band, no deliberate acquisition required. On a machine where
  those files carry other providers' credentials, single-provider-per-seat is
  a policy honored on the env channel only. A10 (§10) sizes this per source.
- **Tightening where the surface allows (P7).** If the installed CLI offers a
  per-invocation surface disabling credential-bearing file sources, launch
  pins it as a seat-scoped env delta — per-source, never rounded up to
  "closed". If none exists, the delta stands as ruled.

**Cross-provider change = controlled relaunch** (settled). Retire the running
seat and respawn/resume with a rebuilt environment for the new provider,
through the standard herder paths. Herder's contract is only: never two
provider credentials in one process environment, ever — on the env channel.
On the native path no herder surface observes in-process model changes (the
former extension-observed provider-drift flag dissolved with the extension);
an in-process cross-provider switch that succeeds via owner-store credentials
is inside the item 9a delta, stated there, not claimed away here.

## DR-6 — Observability, sesh, and honesty

**DECISION (retained — the observer/sesh adapter is expressly unchanged by
the flagship-parity ruling; status sourcing re-pointed to hcom's surfaces).**
Every observation surface reports only what its evidence supports, with the
source labeled (grok DR-5, applied to Pi's surfaces).

- **Transcript** = the seat's session JSONL under Pi's default session root,
  located by the session UUID captured at bind (from hcom's roster) — the
  UUID, never a root scan, is the locator. The observer gets a Pi adapter for
  the JSONL tree format (header + parent-linked entries — demo "Session
  compatibility"). Entries are id/parent-id linked (branching), so the
  adapter renders the active branch and labels branch points rather than
  flattening silently.
- **sesh integration.** Pi is the friendly case sesh was shaped for: the
  adapter indexes the session header (format version, session UUID,
  timestamp, cwd, parent-session reference), uses the session UUID as the
  stable session identifier, and records fork lineage from the parent-session
  link — no SQLite, no scraping. (hcom's own `hcom transcript` also parses Pi
  JSONL faithfully — characterization — a useful cross-check, not the
  adapter's substrate.)
- **Live status:** roster facts (tool `pi`, `hooks_bound`, active-tool and
  listening state) are hcom's, surfaced as hcom's — labeled by source, never
  re-synthesized into herdr's native vocabulary. herdr-reconciled
  `live_status` stays `unknown` where herdr has no Pi integration target —
  never synthesized. The known post-resume `launch_blocked` blemish (DR-4) is
  surfaced as the roster reports it.
- **Registry rows** say `tool: pi` with the standard flagship-family fields:
  hcom name, session UUID, provider, recorded vendor version. The former
  journal-derived capability flags (`bus: reserved|bound`, `pending: <n>`,
  `inject`, `driver`, `spool`) dissolved with the machinery that gave them
  meaning (round 11); no row claims a capability the seat has not proven.

## 9. Threat model (house-inherited; stated, not invented here)

Herder families — this one, grok, and every other — run under the house's
**cooperative same-UID trust model**: every process in a seat (Pi, its tools,
hcom's hooks and extension, herder itself) shares one OS user, and a same-UID
actor that writes state out-of-band is out of scope for this design, exactly
as for the rest of the fleet.

Under the flagship-parity ruling, the in-band authority shape for Pi seats is
**exactly the flagship shape**: hcom's extension and hooks hold the ordinary
hcom CLI; there are no ownership epochs, no driver lease, and no capability
lanes — properties **no flagship has either** (parity memo table, bottom
rows). The former Pi-specific control plane (seat-token lane, operator
capability, launch-attempt fencing) is deleted with the machinery it gated:
the ten mutating bus ops it protected no longer exist, and what remains
reachable from a seat is what flagship seats already reach (audit, capability
lanes row). Second-process-per-seat is prevented operationally by herder
being the sole spawner (DR-1). The owner accepts this authority shape as part
of the flagship-parity delta (§12 item 10).

Credential exposure is unchanged from round 10's honest statement: the seat's
provider credential is in the Pi process environment and inherited by model
tool children (the accepted model-tool boundary — DR-5); owner-config
credential channels are open per §12 item 9a.

## 10. Assumption register (evidence gaps → verify in the build units)

Round-11 disposition: the former register attached almost entirely to the
deleted DR-2/DR-3 machinery. Numbers are retained (the P3 convention);
retired rows keep one line of provenance. Every probe result remains evidence
about the vendor version observed at probe time; a vendor update re-opens the
probes whose surfaces it touches (§12 item 9b).

| # | Status | Assumption / gap → posture |
|---|---|---|
| A1 | retired (round 11) | Reply-content capture — attached to the deleted settlement receipts. |
| A2 | retired (round 11) | Steering/mid-stream injection — the native path's busy delivery is a follow-up turn (characterization), accepted as-is. |
| A3 | retired (round 11) | Injected-content durability for the nudge policy — no nudge policy exists. |
| A4 | retired (round 11) | Session-replacement rebinding — hcom owns session binding on the native path. |
| A5 | retired (round 11) | Extension-published session UUID — hcom's roster reports it (demonstrated). |
| A6 | **open** | Pi's interactive approval/autonomy surface is uncharacterized. Autonomy mapping stays unmapped; seats run Pi defaults until characterized; any bypass-like mapping is an owner decision (§12 item 3). Build-unit probe. |
| A7 | retired (round 11) | TUI-mode extension parity for the herder extension — no herder extension; the native integration ran Pi under `hcom pty` in the characterization. |
| A8 | retired (round 11) | Extension child-process env control — no herder extension spawns children. |
| A9 | retired (round 11) | Inbound driver runtime viability — no driver exists. |
| P1 | retired (round 11) | New-session UUID preassignment — hcom owns session identity natively. |
| P2 | retired (round 11) | `hcom start --as` fresh-mint / placeholder routability — herder runs no identity acquisition for Pi seats. |
| P3 | retired (pre-round-6) | Number retained; no open question lives here. |
| P4 | retired (round 11) | Subagent surface inventory — delivery receipts are not model-acked on the native path; the credential residual is DR-5's accepted boundary. |
| P5 | **open** | Per-provider residual network under `PI_OFFLINE=1` (strace-proven for one Anthropic call only). Offline flags ship regardless; per-activated-provider integration check at the build unit. |
| P6 | **open** | Project `.pi` trust surface: what mechanism the installed CLI offers to withhold project-resource loading, and what an autonomous launch does by default. A workspace `.pi/` can carry executable resources loading into a credentialed seat. Build-unit probe; disposition is an owner ruling on that evidence (§12 item 6). If no enforceable suppression surface exists, that is an owner-ruled acceptance or an upstream ask — never unit improvisation. |
| P7 | **open** | Per-invocation surface disabling credential-bearing file sources (DR-5 tightening). Build-unit probe, per source. |
| A10 | **open** | File-source-vs-env credential resolution on a live seat — sizes the §12 item 9a delta per source. Build-unit probe riding P7, scratch stand-ins for owner-meaningful files. |
| A11 | retired (round 11) | Per-seat cgroup scopes — the quiesce sweep and belt refusal they served are deleted. |
| P8 | **new (round 11), open** | **Isolated-bus placement coupling**: with `HCOM_DIR` re-pointed (the only case where the §2 Pi-home pin fires), does the pinned env shape keep hcom's extension write location and Pi's load location lined up (`src/hooks/pi.rs:350-362` coupling)? Probe on the real spawn path; fix the working env shape before the build unit closes. The default-bus production path lines up by construction. |
| P9 | **new (round 11), open** | **PATH floor**: the launched seat's `PATH` carries the hcom executable's directory (the native extension `spawn("hcom", ...)` — `src/pi_plugin/hcom.ts:39`), asserted on the environment the pane process actually receives. |

## 11. Test and gate plan (contracts the build units must ship)

Round-11 disposition: the former battery T1–T16, T18, T25, T26, T28–T35
attached to the deleted delivery/authority machinery and is retired with it
(numbers retained in git history; no re-use). What remains mirrors the
flagship families' launch-contract coverage plus the retained adapter:

- **L1 — capability gate + exec wiring.** `--agent pi` routes through
  `IsHcomCapable` into the exec-into-hcom path; spawn/resume/fork produce the
  `hcom pi` / `hcom r|f` argv shapes; the live contract suite tier pins the
  installed hcom's `pi` launch line (any hcom upgrade re-opens the pin).
- **L2 — env pin + placement coupling.** The Pi-home pin lands beside the
  existing pins and fires only on isolated `HCOM_DIR`; P8's characterized env
  shape is asserted; `PI_CODING_AGENT_SESSION_DIR` is not set on any path.
- **L3 — credential scoping on the launch path** (re-scoped T17/T21 core,
  retained): exactly one provider credential by name in the environment the
  pane process actually receives (fresh-pane truth); `PI_OFFLINE=1`/
  `PI_TELEMETRY=0` present; unknown `--provider` refused naming the set;
  cross-provider credential never present; no credential value in argv or in
  anything herder writes; no launch refusal on owner-file contents and no
  drift-termination path exists (asserted absent — seats must survive
  ordinary owner `/login` and custom-provider state).
- **L4 — recorded vendor version** (re-scoped T19, retained): launch/bind
  records the observed version in the registry row; **no hash gate and no
  supported-version refusal exist** (asserted absent — the ruling's shape is
  pinned, not just permitted); unresolvable Pi refuses with cause+remedy.
- **L5 — passthrough refusals** (re-scoped T20): colliding passthroughs
  (session re-points, home/state re-points, offline/telemetry overrides,
  credential/auth-file arguments) are refused with targeted errors, finalized
  against the `hcom pi` surface of the vendor version current at the build
  unit; new vendor flags are not auto-refused (§12 item 9b drift honesty).
- **L6 — bind capture + lifecycle.** Spawn bind capture from hcom's roster
  (tool `pi`, hooks_bound, session UUID); no-bind fails the spawn legibly;
  resume reclaims name + session; fork records lineage; cull row-stops via
  the standard path (re-scoped T22–T24 essences through the standard family
  machinery, not Pi-specific code).
- **T27 — observer/sesh adapter (retained unchanged).** Header index (UUID,
  cwd, parent link) against recorded session fixtures, including a branched
  session; herdr `live_status` stays `unknown` under mutation; roster-derived
  status is labeled by source.
- **PATH floor** (P9) asserted on the launched pane's environment.

**Live smoke (isolated, gated, owner spend per §12 item 2):** one provider
end-to-end through the real spawn path — spawn → roster bind → doctrine +
prompt delivered (real inference) → outbound `hcom send` lands on an isolated
bus → resume reclaims name + session → cull. Repeated per activated provider
at activation (§13).

## 12. Owner decisions required

1. **Default provider and default models.** `--provider` ships required with no
   default; no per-provider default model is pinned. Owner may pin either after
   trials (grok precedent: model pinned by ruling post-design).
2. **Inference spend** for build-unit probes and smokes (per-provider). The
   grok blanket approval was scoped to that design's staging; Pi needs its own.
3. **Autonomy mapping** once probe A6 inventories Pi's approval surface — in
   particular whether any herder mode may map to a bypass-like Pi mode (grok
   precedent: no bypass mapping ruled in).
4. **Provider activation set**: which of anthropic/openai/xai activate at the
   activation step (each adds a credential precondition and a smoke).
5. **Re-characterization appetite under vendor updates** (no pin or gate
   exists). Vendor updates land on the vendor's cadence; 0.80.6 is the
   characterized baseline. The owner sets when a recorded-version change
   triggers a re-characterization pass versus riding on recorded-version
   visibility alone (item 9b carries the honesty statement of what drift can
   invalidate). Any **hcom** version change likewise re-opens the native
   integration pins (L1) and the native-vs-custom question itself (audit,
   closing register).
6. **Project `.pi` resources in herder seats**: disposition follows the P6
   trust-surface characterization; whether and where to relax (per-workspace
   allowlist, global off, trust-prompt passthrough) is an owner ruling on that
   evidence. (The former ships-disabled mechanism was DR-3 machinery; under
   the native path the enforceable surface — if any — is what P6 finds.)
7. **Superseded by the default-homes ruling** (round 10). This item
   conditioned an acceptance on the launch-empty-store contract's residual
   window; that contract dissolved with the managed home (DR-5). The
   credential-store surface is item 9(a). (Number retained.)
8. **Superseded by the flagship-parity ruling** (round 11). This item made
   the same-UID concession load-bearing for the operator capability's
   lifecycle-authority boundary. The operator capability and the control
   plane it gated are deleted; no Pi-specific lifecycle-authority weight
   rides the concession beyond what claude/codex already ride fleet-wide.
   The concession framework itself (house-wide, cooperative same-UID) is
   unchanged (§9). (Number retained.)
9. **Default-homes ruling deltas (amendment round 10) — the honesty register
   this sign-off covers.** The 2026-07-14 default-homes ruling
   (standing-orders 20.8) trades previously stated properties for fleet-norm
   operation on single-purpose machines. Each place that ruling **weakens** a
   property this design used to claim:
   - **(a) Credential-bearing owner-config channels are open — every file
     source, not only the auth store.** Seats read the owner's live auth
     store **and** custom-provider/models config through Pi's normal
     credential resolution; on a machine where either holds other providers'
     credentials, single-provider-per-seat holds on the **env channel only**
     (the only single-provider claim this design retains anywhere), and
     cross-provider access via those files is in-band. A10 sizes this per
     source; P7's per-invocation disablement, where it exists, closes exactly
     the sources it covers (DR-5 — per-source, never rounded up).
   - **(b) Version drift is unfenced.** No pin, no hash gate, no
     supported-version refusal: a vendor update between launches can silently
     invalidate probe results and pinned behavior in this document. Remaining
     guards: recorded version at provision and every launch, and
     `PI_OFFLINE=1` making the version stable for each process's lifetime
     only. Item 5 owns the re-characterization appetite. *(Round-11 note: the
     former extension refuse-to-claim guard listed here dissolved with the
     herder-owned extension; recorded-version visibility and item 5 remain.)*
   - **(c)** *(Superseded at round 11 — number retained.)* The operator
     credential file concern dissolved with the operator capability itself.
   - **(d) One shared state surface.** All seats and the owner's interactive
     Pi share one home: seat session files intermix with each other's and the
     owner's, and the full session JSONL of every seat (doctrine text, bus
     traffic, injected message content, model output) is path-discoverable
     and readable by any same-UID tool in any seat or owner shell. hcom's
     extension loads into every Pi run in the home, including the owner's
     interactive runs (hcom's own inertness/binding behavior governs there —
     it is hcom's shipped surface, the same one every flagship home carries);
     the owner's user-level Pi resources load into credentialed seat
     processes (owner-trusted per fleet norm — distinct from workspace
     project `.pi/` resources, item 6/P6).
   - **(e) State hygiene is fleet-norm, not fenced.** Any Pi invocation
     writes ordinary default-home state; installer/scratch ceremony and the
     immutable install prefix are gone. Cross-seat and owner-tool read/write
     visibility both ways, within the same conceded same-UID model.
   Retained and expressly not weakened by the default-homes ruling, for
   contrast: credential scoping in launch env construction, and the same-UID
   concession framework. *(Round-11 note: this item's original contrast list
   also named "the entire DR-2 delivery/authority machinery (keep-custom
   ruling)" and its bus-op hygiene as retained — that clause is superseded by
   the flagship-parity ruling, item 10 below; it is left visible here as
   provenance, never silently rewritten.)*
10. **Flagship-parity ruling delta (amendment round 11) — the crash-window
    register this sign-off covers.** The 2026-07-14 flagship-parity ruling
    adopts hcom-native delivery for Pi at the bar claude and codex actually
    run on, and deletes the DR-2/DR-3 delivery+authority machinery before any
    build. Each property the ruling **weakens** relative to the superseded
    design, stated explicitly:
    - **(a) No durable delivery journal.** The only delivery record is
      hcom's unread cursor plus the Pi session transcript — exactly the
      flagship cell (parity memo table: no flagship has any durable journal,
      settlement receipts, or per-message state).
    - **(b) Injection-time receipt.** The cursor advances when the message is
      injected into the model's context, before the turn settles — the same
      placement both flagships commit through the shared
      `commit_delivery_ack` boundary (`src/hooks/claude.rs:135`,
      `src/hooks/codex.rs:427` at hcom 0.7.23; Pi's native extension acks
      after `sendUserMessage`). "Delivered" means injected-into-context, not
      processed. The receipt over-claim window is real and reproduced — one
      crash run per harness (claude, codex, Pi) stranded a mid-turn request
      with zero unread and no automatic continuation.
    - **(c) No crash replay, no duplicate-reconciliation envelope, no nudge
      budget.** After a mid-turn crash there is no hcom item to replay;
      deduplication is in-process only. Resume restores context but does not
      restart the interrupted turn.
    - **(d) No epochs, no activation fencing, no launch-attempt protocol, no
      driver lease, no capability lanes.** The authority shape is the
      flagship shape: hcom owns the process binding; the extension holds the
      ordinary hcom CLI; no flagship has any of these properties either
      (parity memo table, bottom rows). Second-process-per-seat is prevented
      operationally by herder being the sole spawner.
    What heals the weakened window, and the blast radius: the stranded
    request is a liveness stall — the orchestration layer notices silence and
    re-prompts (the fleet's production recovery for claude/codex, which have
    run on exactly this bar in production); in the codex crash probe hcom's
    request-watch additionally notified the **sender** that the seat stopped
    without responding, so a sender-side signal exists. Blast radius: one
    stalled task until noticed. Doctrine frames re-prompts so a model that
    already saw the first copy recognizes repeated content (§2 item 8). The
    separable settlement-ack hedge (parity memo option 3; audit candidate 2)
    is orderable later as its own unit if this window comes to worry the
    owner; nothing in this design blocks it.
    Evidence base for this sign-off:
    `docs/design/2026-07-14-flagship-hcom-crash-parity.md` (the empirical
    flagship bar and the launch-contract costing),
    `docs/design/2026-07-14-delivery-machinery-gold-plating-audit.md`
    (candidate 1, the per-mechanism verdicts), and the crash probe in
    `docs/design/2026-07-14-hcom-native-pi-characterization.md` — whose
    keep-custom decision this ruling supersedes, with its record preserved.

## 13. Implementation units (flagship parity — filed directly from this section)

The staged five-unit program (U1–U5 + activation) attached to the deleted
machinery and is retired with it (round 11; the unit table is preserved in
git history at the round-10 revision). What remains is deliberately small —
the audit's costing: a few launch-contract lines plus the observer adapter.

**B1 — launch contract.** The complete implementation surface:

1. the capability-gate line: `pi` added to `IsHcomCapable`
   (`tools/herder/internal/launchcmd/launch.go:19-26`);
2. the Pi-home env pin: `PI_CODING_AGENT_DIR` → Pi's default agent dir via
   `setEnvDefault` in `PinConfigDir` (`launch.go:28-48`), under the P8
   placement-coupling characterization;
3. launch wiring: the existing exec-into-`hcom pi` path reused
   (`launch.go:203-216`) with the pi env-construction branch (exactly one
   named provider credential — DR-5; `PI_OFFLINE=1`/`PI_TELEMETRY=0`; PATH
   floor carrying hcom's directory — P9), recorded vendor version at
   launch/bind, passthrough refusals (L5), and doctrine/prompt carriage over
   the native bootstrap seam (§2 item 8);
4. bind capture: roster-derived (tool `pi`, hooks_bound, session UUID,
   transcript path) into the registry row via the standard sidecar path;
   no-bind fails legibly;
5. tests: L1–L6 (§11) + probes P8/P9 discharged and recorded + A6/P5/P6/P7/
   A10 answered where the unit's surfaces expose them + the isolated live
   smoke (§11) under owner spend (§12 item 2).

**B2 — observer/sesh adapter** (retained scope, unchanged by the ruling):
the session-JSONL adapter (header index, branch-aware rendering), sesh
identifier/lineage wiring, source-labeled status surfacing, honest-unknown
reconciliation. Gate: T27 against recorded fixtures, including a branched
session; `unknown` preserved under mutation.

B1 and B2 are independent and may land in either order. Activation follows
the house pattern: the family is opt-in until a real end-to-end
`herder spawn --agent pi` passes through the spawn path per provider in the
owner-ruled activation set (§12 item 4), with the credential precondition
verified as fresh-pane truth and cull confirmed — the grok activation
lessons, inherited.

## 14. Earned-clause disposition (carried forward from the demo)

The demo's clause verdicts (demo "Earned launch-contract clauses"), with where
each lands. Where a 2026-07-14 ruling supersedes a demo "Required" verdict,
that is recorded as the ruling's disposition — the demo report itself is not
rewritten:

| Clause | Demo verdict | Design disposition |
|---|---|---|
| Dedicated managed `PI_HOME` concept | Required | **Superseded by the default-homes ruling** (round 10): live default home (§1); the demo mapping stays as evidence that the translation variables work |
| Managed environment on every invocation | Required | Re-scoped by the default-homes ruling: herder-constructed env on every **seat launch** (§2); the every-invocation scratch ceremony dissolved (§12 item 9e) |
| `PI_OFFLINE=1` | Required | §2 launch-env delta (per-process version stability) + per-provider residual check (P5) |
| `PI_TELEMETRY=0` | Required | §2 launch-env delta |
| Provider-specific environment filtering | Required | DR-5 + L3 (env channel; retained through both rulings) |
| Provider pin per seat | Required | DR-5 (relaunch on cross-provider change) |
| Pinned package version and integrity | Required at install/provision | **Superseded by the default-homes ruling** (round 10): vendor-updated default install, observed version recorded, no gate (§2 item 6; drift delta §12 item 9b) |
| Per-launch binary hash gate | Not earned | Not designed; no provision-time gate remains either |
| Per-launch config rewrite | Not earned | Not designed; no owner-config writes at all (§1) |
| Per-launch `/proc` environment ceremony | Conditional | **Dissolved with the direct-exec path** (round 11): Pi rides the flagship exec-into-hcom chain, which carries no such ceremony fleet-wide (§1) |
| Native managed extension | Required | **Superseded by the flagship-parity ruling** (round 11): the binding extension is **hcom's own** native Pi extension (§2, DR-1); the demo's evidence that a native extension binds Pi stands, and the characterization demonstrated hcom's against 0.80.6 |
| External binder process | Not earned | Not designed; the native path keeps delivery inside Pi's process and hcom's bounded hooks — no persistent herder process (unchanged in spirit) |
| Pending-message replay on every start | Required | Native path: unread bus messages deliver on start/rebind through hcom's drain; **no replay past the injection-time ack** — owner-accepted delta (§12 item 10) |
| Exact resume/fork integration | Required | DR-4 (native `hcom r|f`; name + session UUID reclaim demonstrated) + DR-6 (sesh lineage) + L6 |

## 15. Design-time verification note

Per the docs-only constraint of this unit, **no new probes of the Pi binary or
of hcom were run while writing this design**. Amendment round 10 recorded the
default-homes ruling and its deltas (§12 item 9) without new probes.
Amendment round 11 is likewise docs-only: it executes the owner
flagship-parity ruling — deleting the DR-2/DR-3 machinery, adopting the
launch contract costed in the flagship crash/parity memo, and registering the
crash-window delta (§12 item 10) — citing the parity memo's probe runs, the
gold-plating audit's per-mechanism analysis, and the hcom-native Pi
characterization's probe evidence (whose keep-custom decision round 11
supersedes with provenance). Every behavioral claim cites the double-reviewed
demo report or one of those three records; the open items are registered in
§10 with conservative postures and named verification owners in §13. The full
superseded machinery text is preserved in this document's git history at the
round-10 revision.

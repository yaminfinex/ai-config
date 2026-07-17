# Target-state identity architecture

Provenance: owner-ordered design deliverable, 2026-07-17. Ground truth is the
independent root-cause memo for the registration/assignment brittleness class,
promoted alongside this document as
`docs/design/2026-07-17-registration-brittleness-memo.md` (original working copy:
`napkins/run-herder-dx/registration-brittleness-memo.md`). Every load-bearing code
citation in that memo was independently verified at the checkout this design was
written against; the citations in this document were re-verified read-only during
drafting. Companion document: `docs/design/2026-07-17-identity-migration-plan.md`
(the staged path from here to there).

Relationship to the ratified herder spec (`docs/specs/herder-spec.md`): this
document does not reopen the spec's ontology. Sessions, seats, labels, the
append-only registry, nodes/namespaces/epochs, and the observer are taken as
settled. What the incident season exposed — and what this document designs — is the
**proof plane**: how identity claims are authenticated, how repair terminates, how
liveness is evidenced, and how stored coordinates age. The spec says what identity
*is*; this document says how the system *knows*.

---

## 1. The meta-problem this architecture removes

The memo's central finding, compressed: identity currently lives as **frozen,
inheritable copies** — process env snapshotted at birth, launch coordinates
fossilized in the hcom database, cross-store coordinate duplicates — and those
copies double as both *description* and *proof*. Every incident family is one of
two failure modes of a copy: a copy **aging** (stale env after rename/restart/server
handoff; janitors inferring death from their own silent traffic history; two stores
disagreeing with no fact of the matter to consult) or a copy **being inherited** (a
child process becoming its parent because it holds the parent's env). Even the
"live" identity proof compares fossils: the shared proof core matches a caller's
launch-frozen env against a row's launch-frozen database snapshot
(`tools/herder/internal/hcomidentity/identity.go`, `Resolve`) — two fossils
agreeing is today's definition of liveness.

A second structural property compounds the first: **repair verbs draw their proofs
from the evidence pool the damage depletes**. Enroll repair requires live bus
verification, which requires exactly the correlates whose loss is the damage;
the reconcile dominance exception requires equality on coordinates that
reclaimed/empty-context rows structurally lack; the remedy ladder
(enroll → reconcile → adopt) loops back into shapes whose remedy is enroll. Refusals
are honest, but honest refusal loops still end in database surgery and operator
escalation.

The target state removes both properties. It does not aim to make the four stores
(herder registry, hcom instances db, herdr tracker, process env) agree by
synchronization — cross-store transactionality is unattainable with two of the four
stores owned upstream. It makes disagreement **adjudicable**: there is a
time-ordered record of verified bindings with evidence classes, one canonical way a
seat becomes complete, one repair path whose proof is disjoint from any damage, one
liveness authority fed by evidence rather than traffic, and coordinates that carry
their validity domain.

---

## 2. Invariants the target establishes

Numbered for reference from the migration plan; each stage there establishes or
strengthens one of these.

- **T1 — Description is not proof.** A coordinate or name found in the environment
  is a hint for diagnostics and bootstrap provenance, never an authenticated claim.
  Herder verbs authenticate callers by a minted per-seat credential delivered
  out-of-band of the inheritable environment, rotated at every rebirth. Inheriting
  a parent's env proves nothing to herder. (The vendor CLIs' own env handling is
  outside this boundary — §5.)

- **T2 — One row shape.** Every path by which a session comes to occupy a seat —
  spawn, enroll, enroll-repair, adopt, reclaim, resume, and any future recovery
  verb — terminates in the **same seat-completion step**: resolve the live
  pane/terminal, verify the bus row, backfill missing launch coordinates through
  the sanctioned merge-missing-only write, stamp epochs, mint/rotate the seat
  credential, and append the binding facts. A path that cannot complete refuses
  loudly with the list of missing facts; no verb ever mints a partial row, so no
  later verb needs a "born incomplete" branch.

- **T3 — Every damaged shape has a terminating repair sequence.** For any
  single damaged identity field (bus name, recorded session id, launch context,
  seat coordinates) there exists a repair whose proof requirements are **disjoint
  from the damaged evidence**: explicit operator attestation plus physical-seat
  corroboration (live pane read-back, terminal match). The attested repair is
  logged into the row's history as an evidence-classed binding, preserves stored
  label/role/lineage, and ends in the T2 completion step. All *automated* paths
  remain fail-closed exactly as today; attestation is never inferred.

- **T4 — Liveness is evidence, adjudicated in one place.** The node observer is
  the sole liveness authority herder consults: it fuses process evidence
  (pid/process tree), pane read-back, and bus traffic into per-seat verdicts.
  No component we own reaps on heartbeat silence alone; no `gone`-class verdict is
  presented while the pane reads back; own-launch history never gates whether a
  session is observable. Absence of evidence is an observation gap, never a
  verdict (this restates the spec's §8.4 discipline and makes it the *only*
  liveness path).

- **T5 — Coordinates are compared only within their validity domain.** Every
  stored pane/terminal coordinate carries the epoch (substrate lifetime) it was
  observed in. A cross-epoch comparison is not a mismatch; it is a trigger for
  reconciliation. Within an epoch, mismatch keeps its current meaning. No
  fleet-wide identity loss can result from a substrate restart or handoff.

- **T6 — Binding facts, ordered in time, with evidence class.** The registry's
  rows record *how* each identity binding was established (`hook`, `harvest`,
  `recognition` already exist for sids; seat and bus bindings gain the same
  treatment: live-verified, physical-seat-attested, carried-forward). Stored
  values in any store are cache, reconstructible from binding history; when two
  stores disagree, the latest sufficient-evidence binding is the fact of the
  matter, consulted before any refusal is issued. This is the long-horizon
  direction (a program, not a task); T1–T5 are each checked for compatibility
  with it in §4.

Alongside these, the memo's §4 keep-list is carried forward **unchanged as hard
constraints**: fail-closed multi-match refusal; refuse-to-unseat on conflict;
merge-missing-only, schema-pinned vendor-db writes; guid never re-keyed;
misdelivery-worse-than-drop; launch-time names excluded from proof; ownership
proofs read the caller's claim on pinned paths; evidence-dominance exceptions stay
narrow (exact terminal+pane, verified, unique, empty-context only); no unexplained
passes on identity fences; sender ≠ recipient on continuation delivery. Nothing in
this architecture weakens any of them; several (T2, T3) exist precisely so the
fail-closed rules can stay fail-closed without stranding operators.

---

## 3. The architecture, by plane

### 3.1 Claim plane — minted per-seat credentials

At every seat completion (T2), herder mints a random per-seat token bound to
(guid, epoch) and places it **out of the inheritable environment**: a seat-scoped
credential file whose path is derivable from the seat, not exported into the child
process's env wholesale. Herder verbs that today authenticate by ambient
`HCOM_*`/`HERDER_*` values authenticate by presenting the token; the env vars
remain as diagnostics and birth provenance (the spec's §3.1-8 already restricts
`HERDER_GUID` to birth provenance — this extends the same rule to every
identity-bearing var on herder's own verb surface).

Consequences: a bare vendor-CLI probe or a directly-launched child inherits env
but not the credential, so it can no longer act as the caller on herder verbs; a
credential does not outlive its seat epoch, so a stale credential fails closed at
rebirth. The cutover is a real cut — a transition period with env fallback would
re-open the inheritance hole (memo R3's migration honesty) — which is why the
break-glass verb (T3) must land first as the recovery path.

Boundary honesty: this closes the **herder-side** half of the impersonation class.
The hcom vendor extension honors its own inherited env with no continuity check
(the row-takeover hazard, `docs/hazards/agent-cli-identity-hijack.md`); that is
upstream's surface, the upstream ask stands, and the hazard doctrine (scrubbed env
for any direct vendor-CLI invocation) remains load-bearing regardless of this
plane. The ambient session-id harvest in creator flows is treated as fixed (the
fix is a separate in-flight unit); this architecture assumes rows never receive a
creator's ambient sid.

### 3.2 Completion plane — canonical rebirth

One shared seat-completion step, used by every creation and recovery verb. The
pieces exist today, scattered: the adopt flow resolves a live pane pre-write and
backfills launch context through `RepairLaunchContext` (the sanctioned
merge-missing-only vendor-db write in
`tools/herder/internal/hcomidentity/launch_context.go`); spawn records full
coordinates; reconcile can backfill launch context under its narrow exception.
The target consolidates them: completion = live pane/terminal resolution → bus row
verification → launch-context backfill (merge-missing-only; refusal on conflict is
carried into the row, not swallowed) → epoch stamps (T5) → credential mint (T1) →
evidence-classed binding append (T6). Refusal at completion enumerates the missing
facts and names the verb that can supply each — and because of T3, that list
always terminates in a verb that can actually run.

This deletes the "recovery-born rows are second-class citizens" property: verbs
downstream of creation (spawn's sender fence, compact arming, reconcile's
dominance exception) can require the complete shape because every path mints it.

### 3.3 Repair plane — attested break-glass

One new verb, deliberately boring: rebind a single named identity field on a
single row, on the strength of (a) an explicit operator attestation (interactive
confirmation naming the row and field; never derivable from env, flags alone, or
piped input) and (b) physical-seat corroboration — the caller demonstrates
control of the live pane the row claims (pane read-back of an operator-visible
nonce, terminal id match). The proof deliberately uses **no** bus, sid, or env
evidence, because those are exactly what the damaged states lack.

Constraints (settled, not design freedom): logged into the row's history as an
attested binding with the operator's confirmation recorded; preserves stored
label, role, and lineage; rate-limited and loud; ends in the T2 completion step so
the repaired row is complete, not merely patched. Automated paths never call it.
It fixes no root cause — it caps the *cost* of every residual: the terminal states
that today end in database surgery, fork-swap seat replacement, or abandoned rows
instead end in one attested command.

This verb is a takeover surface by construction; its narrowness (single field,
single row, physical-seat proof, attestation logged) is the security design. The
memo's keep-list is the checklist its review must be run against.

### 3.4 Liveness plane — evidence in the observer

The node observer already exists (per-node daemon, herdr socket client, per-tool
transcript/event cursors, seat confirmation). The target makes it the **sole**
liveness authority for everything herder presents or acts on: `list` verdicts,
repair-verb liveness prechecks, and any janitor herder owns consume observer
verdicts; nothing else infers liveness ad hoc. Verdict discipline is T4: positive
death evidence (process exited, pane gone within an unchanged epoch, dead pid
behind a stale bus row) unseats; silence is a gap; a starving keepalive with a
live process is a readable "holder alive, keepalive failing" signal — surfaced
*before* the upstream janitor's staleness window converts a config problem into
identity loss.

Boundary honesty: hcom's own janitors key on traffic and can still reap a mute
seat or spare a fossil; herder can feed keepalives and surface the warning but
cannot veto. Those janitor behaviors are recorded upstream asks. The tracker's
own-children-only detection is likewise upstream's; the observer's evidence
fusion narrows the *impact* (an undetected-by-tracker session is still observed
via bus and process evidence) without pretending to fix detection.

### 3.5 Epoch plane — validity domains for coordinates

The registry spec already carries the machinery dormant: epoch records
(`kind: epoch`, with substrate and fingerprint) and per-seat `hcom_epoch` /
`herdr_epoch` fields exist in the projection and write path
(`tools/herder/internal/registry/v2/registry.go`,
`tools/herder/internal/registry/write.go`); nothing mints or consumes them yet.
The target activates them: seat completion stamps both epochs; comparisons in
reconcile/list/repair treat cross-epoch mismatch as "reconcile me", never as
`gone`/conflict.

Verified during this design (read-only, protocol 16): **herdr exposes no
server-generation id** — not in `herdr status --json`, not in the api snapshot's
top-level keys, not anywhere in the api schema. The stage is nevertheless firm,
because the ratified spec (§6.3) already designs herdr epochs as probe-inferred,
and a stronger local fingerprint is available without upstream movement: the herdr
API socket is a unix socket, so the server's process incarnation (peer pid +
process start time + kernel boot id) is readable at connect time and can serve as
the epoch fingerprint, rotating exactly when the server process is replaced.
A false rotation (epoch rotates though coordinates survived, e.g. live handoff)
costs one cheap reconciliation pass; a false stability would be a hazard, and the
process-incarnation fingerprint cannot produce one. The upstream ask (a
first-class generation id in status/snapshot) is recorded as a refinement that
would retire the derivation, not a dependency.

### 3.6 Fact plane — evidence-classed bindings (long horizon)

The registry is append-only with typed events and per-sid sources already; T6
generalizes: every binding-establishing append (seat confirmation, bus-name bind,
launch-context backfill, attested repair, epoch stamp) carries its evidence class
and timestamp. Consumers then resolve cross-store disagreement by consulting the
latest sufficient binding instead of refusing on pairwise mismatch. This subsumes
most of the split-brain mechanism and is the direction the memo names as a
program, not a task. The commitment this document makes is weaker and checkable:
every T1–T5 mechanism **writes through** binding-shaped appends (no new
out-of-band state), so the program can be built by adding consumers, not by
migrating producers.

---

## 4. Root-cause disposition (H1–H7)

How each root cause named in the memo is neutralized or explicitly accepted as
residual. "Neutralized" means the generating property is removed for surfaces we
own; residuals are named with their owner.

**H1 — identity spread across stores with no transactional coupling.**
*Neutralized in effect, not by coupling.* The stores remain four (registry, hcom
db, tracker, env) and remain uncoupled — two are upstream's. The property that
made this generative was "identity is defined as agreement among copies; on
disagreement there is no fact to consult." T6 installs the fact of the matter
(latest sufficient binding); T2 makes the copies complete at birth; T5 bounds
their validity. Residual: a disagreement whose latest binding evidence is itself
insufficient still refuses — but T3 guarantees the refusal terminates in an
attested repair rather than a loop.

**H2 — launch-time env snapshots as identity carriers past validity, doubling as
authentication.** *Neutralized on herder's surface by T1* (credentials replace env
as proof; env demoted to hints), with the creator-flow ambient-sid harvest
treated as already fixed (in-flight unit). *Residual, upstream-blocked:* the
vendor extension honors its own inherited `HCOM_*` env cross-tool with no
continuity check, and its exit deletes the row; until the upstream continuity
check exists, the hazard doctrine (scrubbed env for direct vendor-CLI invocation)
remains load-bearing. The frozen `launch_context` in the hcom db also remains
(no upstream setter); T2 guarantees it is backfilled wherever the sanctioned
merge-missing-only write can act, and T5 bounds how long a fossil coordinate can
be trusted.

**H3 — liveness inferred from proxy traffic.** *Neutralized for herder-owned
surfaces by T4* (observer is sole authority; evidence fusion; no reap without
positive death evidence). *Residual, upstream-blocked:* hcom's janitors
(staleness reaping, inactive cleanup, supervised-binder launch-failure
finalization) still key on traffic; herder feeds keepalives, surfaces starvation
early, and endorses the ledgered upstream asks, but cannot veto an upstream reap.

**H4 — repair verbs gated on the state they repair.** *Neutralized by T3 + T2.*
The break-glass verb's proof pool (attestation + physical seat) is disjoint from
every damaged evidence pool, so a terminating repair exists for every shape; T2
removes the born-incomplete shapes that made the remedy ladder circular
(a recovery verb no longer mints the shape whose remedy is another recovery
verb). The narrow evidence-dominance exceptions stay exactly as narrow as today —
they are relief valves, no longer the only exit.

**H5 — creation paths minting rows missing coordinates later verbs require.**
*Neutralized by T2* (one completion step, one row shape, loud refusal with a
terminating remedy instead of a partial mint). *Residual, upstream-blocked:*
strand-at-birth — a launch that never boots (shell-init stall, wrapper prompt)
leaves a pane with no bindable occupant; that needs the upstream launch timeout.
The row shape is still protected (completion never ran, nothing partial was
minted); the pane husk is a liveness/cleanup matter for T4's evidence, not an
identity matter.

**H6 — verification fences testing values derived from the record being
claimed.** *Contained before this design; the residue is closed by T6 discipline.*
The proven tautology class was fixed and adjudicated; what remains is stored
*assertions* (e.g. a verified-flag written by past binaries with heterogeneous
semantics) admitted as conjuncts without provenance. Under T6, flags become
evidence-classed bindings — a conjunct's provenance is part of the proof — and
the no-unexplained-passes discipline (every admitting path pinned by the fence's
matrix) is carried forward as a hard constraint.

**H7 — coordinates carry no validity domain.** *Neutralized by T5.* Epoch-stamped
coordinates make a substrate restart/handoff a reconciliation trigger instead of
fleet-wide identity loss. Firm without upstream (probe-inferred boundaries per
the ratified spec, strengthened by the locally-derived process-incarnation
fingerprint); the first-class upstream generation id is a recorded refinement.

---

## 5. What the target explicitly does not do

- It does not make hcom or herdr correct. The janitor asymmetry, the extension's
  env-honoring takeover, the launch-context setter gap, the codex pane-id
  omission, the launcher strand — all remain upstream asks, endorsed and
  ledgered. Every mechanism above works if upstream never moves; several get
  simpler (and some of our machinery retires) if upstream does.
- It does not protect direct vendor-CLI invocation from an identity-bearing
  shell. That is the vendor extension's env boundary; doctrine (scrub the env)
  remains load-bearing there.
- It does not introduce cross-store transactions, multi-node identity, or any
  new store. The env stays a store — demoted, not eliminated.
- It does not weaken a single keep-list fence. Where a fence's refusal was the
  season's pain, the answer is a terminating repair path beside the fence, never
  a wider admitting predicate.

# Target-state identity architecture

Provenance: owner-ordered design deliverable, 2026-07-17; revised same day after
adversarial design review round 1 (security-boundary honesty, repair-vocabulary
narrowing, evidence lattice, write-spine and spec-conformance corrections).
Ground truth is the independent root-cause memo for the registration/assignment
brittleness class, promoted alongside this document as
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
*is*; this document says how the system *knows*. Where a mechanism below would
change ratified semantics, it is flagged as a spec amendment explicitly; nothing
here amends silently.

**Trust boundary, stated once.** Herder runs on a single-purpose machine where
agents and the operator share one OS account. A same-uid process can read any
same-uid file, open ptys, and drive the herdr pane API (socket mode excludes other
uids only). Therefore **no mechanism in this document claims to distinguish the
operator from deliberate same-uid automation on this platform**; the owner has
previously ruled this posture acceptable for vendor homes. What the mechanisms do
claim — and are tested for — is the elimination of *ambient* and *inherited*
authority: identity that leaks by default into children, survives its validity,
or gets honored without a deliberate act. Where a stronger boundary is possible
at real cost, it is written out as an owner decision, not silently chosen.

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
seat becomes complete, a repair corridor that terminates for every enumerated
damage shape, one shared liveness predicate fed by evidence rather than traffic,
and coordinates that carry their validity domain.

---

## 2. Invariants the target establishes

Numbered for reference from the migration plan; each stage there establishes or
strengthens one of these.

- **T1 — Description is not proof.** A coordinate or name found in the environment
  is a hint for diagnostics and bootstrap provenance, never an authenticated claim.
  Herder verbs authenticate callers by a minted per-seat credential delivered
  outside the inheritable environment and rotated at every rebirth. The boundary
  this buys, precisely: identity no longer propagates **by default** — a child
  inherits env but not authority, and a snapshot of authority dies at the next
  rebirth. It is *not* a boundary against a same-uid process that deliberately
  reads another seat's credential file (trust boundary above); the claim is
  unforgeable-by-inheritance, not unforgeable-by-intent. (The vendor CLIs' own
  env handling is outside this boundary — §5.)

- **T2 — One row shape per seat kind.** Every path by which a session comes to
  occupy a seat — spawn, enroll, enroll-repair, adopt, reclaim, resume, and any
  future recovery verb — terminates in the **same seat-completion step**, which is
  seat-kind-aware per the spec's seat model: a herdr seat resolves live
  pane/terminal and, for bus-capable tools, verifies the bus row and backfills
  missing launch coordinates through the sanctioned merge-missing-only write; a
  process seat resolves pid and bus binding; a busless tool (e.g. bash) completes
  without the bus leg. In every case completion stamps epochs, mints/rotates the
  seat credential, and appends the binding facts with their evidence class. A path
  that cannot complete *for its kind* refuses loudly with the list of missing
  facts; no verb ever mints a partial row, so no later verb needs a "born
  incomplete" branch.

- **T3 — Every enumerated damage shape has a terminating repair sequence.** The
  break-glass vocabulary is exactly the memo's: **stored bus name, recorded
  session id, launch context** — the identity fields whose damage today produces
  refusal loops. The repair proof is operator attestation plus seat-control
  corroboration (§3.3), deliberately using **no** bus, sid, or env evidence —
  disjoint from the damage for every field in the vocabulary. Registry seat
  coordinates are *not* break-glass vocabulary: re-binding a session to a seat is
  what the existing re-seat verbs (enroll, adopt) are for, and break-glass exists
  to repair the identity fields those verbs' proofs require, after which the
  ordinary corridor runs. §3.3 enumerates the damage shapes and their terminating
  sequences honestly, including the one shape whose termination is
  upstream-gated. Attested repairs are logged into the row's history as
  evidence-classed bindings, preserve stored label/role/lineage, and end in the
  T2 completion step. All *automated* paths remain fail-closed exactly as today.

- **T4 — Liveness is evidence, adjudicated by one shared predicate.** One liveness
  predicate — what counts as positive death evidence, what counts as an
  observation gap — is defined once and applied by every component that observes
  seats: sidecar, node observer, and CLI verbs alike. Whichever component first
  observes positive death appends the unseat, exactly as the spec assigns
  (§3.3/§8.1); the node observer remains the *continuous* adjudicator and the
  advice surface, but is neither a required daemon nor a sole author — observer
  liveness is never a precondition for any verb (spec invariant, preserved). No
  component we own reaps on heartbeat silence alone; no `gone`-class verdict is
  presented while the pane reads back; own-launch history never gates
  observability. Absence of evidence is an observation gap, never a verdict.

- **T5 — Coordinates are compared only within their validity domain.** Every
  stored pane/terminal coordinate carries the epoch (substrate lifetime) it was
  observed in. A cross-epoch comparison is not a mismatch; it is a trigger for
  reconciliation. Within an epoch, mismatch keeps its current meaning. An
  incarnation that cannot be verified yields **epoch unknown**, which routes to
  reconciliation — never to a same-epoch comparison (§3.5). No fleet-wide
  identity loss can result from a substrate restart or handoff.

- **T6 — Binding facts, ordered in time, with evidence class, under a defined
  lattice.** Registry rows record *how* each identity binding was established.
  Evidence classes, ordered: `live-verified` (independent live correlates at
  observation time) > `attested` (operator break-glass) > `harvest`/`carried`
  (copied from provenance or carried forward) > `assumed`. Adjudication rules:

  1. **Conflicting independent live evidence is a hard refusal, always.** No
     binding-history entry, of any class or recency, ever overrides a live
     conflict. This is the keep-list, restated as the lattice's top rule.
  2. Binding history adjudicates **only in the live-evidence-absent quadrant**:
     where today two stores' stale copies disagree and no live correlate can
     arbitrate, the latest sufficient binding is consulted *instead of refusing
     on the pairwise copy mismatch*. "Sufficient" = class ≥ `attested`, within
     epoch validity for coordinate-valued fields.
  3. **Correction semantics.** An attested correction (a break-glass rebind,
     §3.3) appends the new binding **and tombstones the specific binding it
     supersedes** — named by field and **binding id**, in the same locked
     batch. A binding id is a **durable, persisted identifier minted at append
     time and stored in the row JSON** — never a load-time line number or any
     other load/rotation-derived value, which the registry reassigns on load
     and resets at rotation/reseed. Binding histories ride *inside* the
     session row as append-only lists (the pattern the sid history already
     uses): each entry carries its binding id, evidence class, timestamp, and
     — when tombstoned — the id of the correction that invalidated it. Because
     every appended row is a full self-contained snapshot and rotation reseeds
     the latest row per guid, the complete adjudication-relevant binding set,
     tombstones included, **survives rotation inside the reseeded row by
     construction**; rotation archives retain the full pre-rotation event
     history for forensics, but adjudication correctness never depends on
     reading an archive. Tombstoned bindings stay in history (nothing is
     deleted) but are not candidates for adjudication. Only an
     `attested`-or-better event may tombstone, a tombstone names exactly one
     binding id, and blanket invalidation does not exist. This is what lets a
     newer attested repair beat an older *stale* `live-verified` binding
     without weakening rule 4: the old binding loses by being tombstoned by an
     explicit logged correction, never by being outranked. (List growth is
     bounded by binding-*changing* events — rebinds, corrections, epoch
     re-stamps — not by traffic, the same growth class as the existing sid
     history.)
  4. Among surviving (non-tombstoned, epoch-valid) candidates in the absent-live
     quadrant, class dominates recency; a later `live-verified` binding
     supersedes any earlier `attested` one; coordinate bindings expire at their
     epoch boundary (T5).
  5. Every admitting path is pinned by the matrix below — no unexplained passes.

  **Field-by-field admitting matrix** (the correction cell shown, not promised):

  | Field | Live evidence now: conflicts with stored | Live evidence now: absent | Correction path |
  |---|---|---|---|
  | Stored bus name | Refuse (rule 1) | Latest surviving candidate by class-then-recency (rules 3–4) | Attested rebind appends new binding + tombstones the named stale one — including a stale `live-verified` — so the attested value wins by survivorship |
  | Recorded sid | Refuse (rule 1) | Same as bus name | Same as bus name (adopt's resumed-sid authorization unchanged for automated paths) |
  | Launch context | Governed by the vendor-db fence, not the lattice: merge-missing-only backfill when empty; wrong-nonempty → recreate protocol (§3.3), never adjudicated or rewritten | — | The attested record authorizes the recreate; no tombstone (the vendor store, not history, holds the value) |
  | Registry seat coordinates | Not lattice-adjudicated: re-seat corridor (§3.3 table) re-stamps from live evidence at completion | — | None (out of break-glass vocabulary) |
  | Seat credential | Not a binding: reissue operation (§3.3), no adjudication | — | — |

  Worked instance of the correction cell: bus name A was once recorded
  `live-verified`; after a rename/reclaim the live correlates are unavailable
  and A is stale; the operator attests B. The attested correction tombstones
  the A-binding and appends B; adjudication sees only B among survivors, so
  completion binds B. Without rule 3, class-dominates-recency would have
  restored A and re-created the refusal loop — that contradiction is the rule's
  reason for existing.

  Authority semantics are unchanged: the registry remains snapshot-per-event and
  the sole seat→session authority per the ratified spec; evidence class is an
  additive row field, and "reconstructible cache" describes the **foreign
  stores'** copies (hcom db, tracker, env), never the registry itself. Any
  future widening of rule 2 beyond the absent-live quadrant would be a spec
  amendment requiring ratification, and is not proposed here.

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
(guid, credential generation) — the generation is a per-seat value rotated at
every completion, deliberately independent of the T5 substrate epochs so this
plane carries no ordering dependency on the epoch plane. The token lives in a
seat-scoped credential file outside the inheritable environment; herder verbs
that today authenticate callers by ambient `HCOM_*`/`HERDER_*` values
authenticate by presenting the token, and the env vars demote to diagnostics and
birth provenance (extending the spec's §3.1-8 rule for `HERDER_GUID` to every
identity-bearing var on herder's own verb surface).

**Rotation commit protocol (two stores, one commit point).** The token file and
the registry generation live in different stores, and a locked registry append
cannot atomically publish a file — so every mint/rotation (completion and
reissue alike) follows one logical transaction:

1. Mint the new generation id and token; **write + fsync an immutable,
   generation-keyed token file** (`…/<guid>/<generation>.token`, never
   overwritten in place; directory fsynced).
2. **The locked registry append that flips the row's credential generation is
   the commit point.** The registry is the sole source of generation truth;
   the file is possession evidence only — verification always checks a
   presented token against the *registry-current* generation.
3. The old generation's file is retained but dead (its generation is no longer
   registry-current); files for generations that were staged but never
   committed are orphans, garbage-collected lazily by later completions —
   never inside the transaction.

Crash analysis, exhaustive by construction: a crash before or during staging
leaves the registry at the old generation with its token file intact (the
partial staged file is an orphan); a crash after staging but before the
registry flip is the same state; a crash after the flip is a committed
rotation whose token file already durably exists — staging precedes the commit
point, so there is **no crash point at which either generation is stranded**.
After the registry flip — and only after it — replay of the pre-rotation token
fails closed at step 2's verification rule; before the flip the old generation
*is* registry-current and keeps authenticating, which is exactly what makes the
pre-commit crash states safe.

**Identity selection order is part of the design, not an implementation detail:**
on a credential-authenticated verb, the credential selects the acting identity
(credential → guid → registry row); ambient correlates
(`hcomidentity.CurrentEvidence`) are then used only to *verify* the selected
row's bus binding, and a verification mismatch is a refusal — never a
re-selection by ambient evidence. Ambient correlates must not be able to choose
who the caller is on any cut-over verb.

What this buys, stated to the trust boundary: accidental inheritance stops
conferring authority (env propagates to every child by default; a credential
file does not follow a child unless something deliberately reads it); stale
authority dies at rebirth (rotation kills the frozen-at-birth carrier class);
cross-seat confusion stops (a seat's verbs act as that seat). What it does not
buy: protection from deliberate same-uid reads — see the trust boundary. The
season's herder-side impersonation incidents were all of the accidental/ambient
kind; that is the class this plane deletes.

The cutover is a real cut per verb — a transition period with env fallback would
re-open the inheritance hole — which is why legacy issuance must precede any
verb's cut (an issuance sweep mints tokens for existing live seats before the
first verb switches) and why the break-glass surface (T3) must land first: the
recovery path for a lost token is the dedicated **credential reissue** operation
(§3.3), authenticated from the break-glass proof pool — never a credential-gated
verb, which after cutover could not authenticate without the very token it is
supposed to restore.

Boundary honesty, upstream: the hcom vendor extension honors its own inherited
env with no continuity check (the row-takeover hazard,
`docs/hazards/agent-cli-identity-hijack.md`); that is upstream's surface, the
upstream ask stands, and the hazard doctrine (scrubbed env for any direct
vendor-CLI invocation) remains load-bearing regardless of this plane. The
ambient session-id harvest in creator flows is a gated prerequisite of the
migration's first stage (in-flight as a separate unit); this architecture
assumes rows never receive a creator's ambient sid.

### 3.2 Completion plane — canonical rebirth

One shared seat-completion step, used by every creation and recovery verb, with
seat-kind-aware legs (T2). The pieces exist today, scattered: the adopt flow
resolves a live pane pre-write and backfills launch context through
`RepairLaunchContext` (the sanctioned merge-missing-only vendor-db write in
`tools/herder/internal/hcomidentity/launch_context.go`); spawn records full
coordinates; reconcile can backfill launch context under its narrow exception.
The target consolidates them: completion = seat-kind resolution (live
pane/terminal for herdr seats; pid + bus binding for process seats) → bus row
verification for bus-capable tools → launch-context backfill
(merge-missing-only; a `pane_conflict` refusal from the vendor-db write is
carried into the completion output, never swallowed — the never-rewrite-existing
fence holds) → epoch stamps (T5) → credential mint/rotation (T1) →
evidence-classed binding append (T6). Refusal at completion enumerates the
missing facts for the seat's kind and names the verb that can supply each — and
because of T3, that list always terminates in a verb that can actually run (or
names the upstream-gated shape honestly, §3.3).

Completion also has an **attestation-consuming mode** for exactly one caller —
the break-glass verb: the attested binding substitutes for the live-verification
leg *of the attested field only*, recorded at evidence class `attested` rather
than `live-verified`; every other leg (live seat resolution, uniqueness checks,
merge-missing-only discipline) runs unchanged. This is what lets an attested
repair end in a complete row without pretending the bus proof existed.

This deletes the "recovery-born rows are second-class citizens" property: verbs
downstream of creation can require the complete shape for the seat's kind
because every path mints it.

### 3.3 Repair plane — attested break-glass

One new verb, deliberately boring: rebind a single named identity field — stored
bus name, recorded session id, or launch context (exactly the memo's break-glass
vocabulary; the earlier draft's addition of registry seat coordinates is
withdrawn as an uncalled-out scope broadening that also made the proof circular)
— on a single row, on the strength of:

- **(a) explicit operator attestation** naming row, field, and new value,
  unforgeable from flags alone or piped input; and
- **(b) seat-control corroboration**: the caller demonstrates control of the live
  pane the row claims (nonce round-trip through the pane, terminal-id match
  where the stored terminal is intact).

**Named honestly:** corroboration (b) proves *control of the pane by the calling
OS account*, nothing more. The herdr pane API (pane read/send over the
uid-scoped socket) lets any same-uid process both observe and inject the nonce,
and a non-human process can allocate a pty, so (a)+(b) as specified do **not**
distinguish an operator from deliberate same-uid automation. Two designs follow,
and the choice is flagged for ratification:

> **OWNER-DECISION — break-glass trust anchor.** Both branches are fully
> designed and written to the trust boundary; neither is silently assumed
> elsewhere in this document.
>
> **Branch A — operator-held factor, verifier anchored outside uid write
> authority.** A passphrase hash alone is not enough: a same-uid process could
> replace the stored hash with one for an attacker-known passphrase, so
> preimage secrecy without **verifier integrity** proves nothing. Branch A
> therefore requires *both*: (i) an operator-held factor entered on a channel
> same-uid processes cannot observe — not a herdr-managed pane (pane
> keystrokes are API-readable); a direct tty outside herdr is the supported
> channel — and (ii) a verifier the calling uid cannot rewrite. Concrete
> anchor options, in rough order of fit for a single-user box: a root-owned
> hash file plus root-owned verifier helper (installed once with sudo; honest
> **precondition: root escalation must itself be password-gated** — on a box
> with passwordless sudo this variant collapses into Branch B and must not be
> claimed); a hardware user-presence factor (FIDO2 touch — same-uid automation
> cannot produce a physical touch); or remote approval from a second device.
> Costs: setup ceremony, an outside-herdr entry channel, factor-loss recovery
> (re-setup at the anchor's level), and the stated precondition. If no anchor
> option is acceptable on the target machine, Branch A is *unavailable
> honestly* — there is no same-uid-resistant middle ground.
>
> **Branch B — posture reduction.** The claim is reduced to what (a)+(b)
> actually prove: *a deliberate, named, logged action by the OS account that
> controls the pane*. Same-uid takeover through this verb is explicitly
> accepted at the machine boundary — consistent with the owner's prior
> single-purpose-machine ruling — and the verb's security value is honestly
> restated as: narrowness (single field, single row), rate limit, loudness at
> time of use (stderr + bus/observer event streams give contemporaneous
> visibility), and a **normal-path audit record**: it reliably records
> ordinary deliberate use, but the registry is same-uid-editable and no
> integrity mechanism (hash chain, MAC, remote sink) is designed, so it is
> *not* tamper-evident against the deliberate adversary and is not claimed to
> be. A tripwire for the normal path, not a wall and not a forensic seal. The
> forgery-path test (pty + pane-API nonce loopback) *documents the accepted
> bypass* instead of asserting its absence.
>
> Branch B matches the ruled posture and costs nothing operationally; Branch A
> is the only option that makes "operator" literally true, at the price of its
> anchor's preconditions. Default in the migration plan is Branch B pending
> ratification; switching to Branch A later is additive (the anchored factor
> becomes one more conjunct).

**Damage shapes and their terminating sequences** (the honest enumeration T3
promises):

| Damaged field / shape | Terminating sequence |
|---|---|
| Stored bus name uncorroboratable (live bus proof unavailable) | Attested rebind of `hcom_name` → completion in attestation-consuming mode. Terminates. |
| Recorded sid wrong/foreign (poisoned or resumed-elsewhere) | Attested rebind of the recorded sid → completion. Terminates. (Adopt's authorization rule for resumed-sid claims is unchanged for automated paths.) |
| Launch context empty | No attestation needed in the ordinary case (merge-missing-only backfill at completion); attestation supplies the pane fact when live bus proof is unavailable. Terminates. |
| Launch context wrong-nonempty (`pane_conflict`) | Never rewritten (keep-list fence). Terminating protocol: recreate the vendor row through hcom itself from the verified live pane (leave/stop the wrong row, rejoin under the same name), which yields an empty launch context that completion then backfills. The attested record covers the operator's authorization of the recreate. **Upstream-gated residual:** if hcom's reclaim guard refuses the rejoin (its refusal exits rc=0 — recorded upstream defect), the shape is *not* terminable inside herder; the documented owner-approved database recovery recipe in the hazard doc is the honest fallback, and this row of the table says so rather than claiming termination. |
| Registry seat coordinates wrong/stale | Out of break-glass vocabulary. Cure: the existing re-seat corridor (enroll/adopt from the live seat, or reconcile re-confirmation), which ends in completion; break-glass repairs the bus/sid/launch-context fields those verbs' proofs need, then the corridor runs. Terminates via composition. |
| Seat credential lost (T1) | **Dedicated reissue operation** (below): attested + seat-control corroborated under the ratified branch, no identity field rebound, ends in re-completion which mints the new token under the §3.1 rotation commit protocol (registry generation flip = commit point; no crash point strands either generation). Never prescribed as "re-run a credential-gated verb" — that would be the circularity class re-entering through the new machinery. Terminates. |

**Credential reissue (the one non-rebind operation on this verb surface).**
Credential loss is a damage shape *created by* the claim plane (§3.1), so its
recovery lives here by design, not as scope creep on the memo's rebind
vocabulary: a `reissue-credential` operation authenticated exactly like a
rebind (attestation + seat-control corroboration under the ratified branch),
which rebinds **no identity field** — row identity facts are untouched — and
ends in the T2 completion step, which rotates the generation and mints the new
token under the §3.1 rotation commit protocol (staged generation-keyed token
first; the locked registry generation flip is the commit point; no crash point
strands either generation). It exists precisely so that no credential-gated
verb is ever its own credential recovery: the authentication for reissue is
drawn from the break-glass proof pool, which is disjoint from the missing
token.

Constraints (settled): logged into the row's history as an attested
evidence-classed binding recording the attestation; preserves stored label,
role, and lineage; rate-limited and loud; single field (or the reissue
operation) per invocation; ends in the T2 completion step. Automated paths
never call it — no attestation means exactly today's fail-closed refusals. It
fixes no root cause — it caps the *cost* of every residual. The **registry
side** of an attested rebind or reissue plus its completion is one locked
batch (see §3.5 write-spine note): a sparse or half-applied attested row must
be impossible by construction. The credential file is outside that batch by
nature and is governed by the §3.1 rotation commit protocol instead — the
locked batch *is* that protocol's commit point.

### 3.4 Liveness plane — one predicate, evidence-based

The liveness predicate is defined once — what constitutes positive death
evidence (occupant exited, pane gone within an unchanged epoch, dead pid behind
a stale bus row), what constitutes an observation gap (everything else) — and
every observing component applies it: sidecars for their occupants, the node
observer for every seated row, CLI verbs when they happen to observe first. The
spec's assignment stands: whichever component first observes death appends the
unseat through the ordinary locked-writer discipline; the observer holds no
write authority and its liveness is never a precondition (spec §3.1-13/14,
preserved — this plane is a predicate consolidation, not a daemon promotion).

What changes from today: ad-hoc liveness *inference* scattered through herder
(anything keying on traffic history or own-launch records) is deleted in favor
of the shared predicate; the observer becomes the continuous adjudicator whose
evidence-carrying advice (`observed_via`-style provenance, display-tier, never
mistakable for registry facts) is what `list` and repair prechecks surface; and
keepalive starvation with a live holder becomes a loud "holder alive, keepalive
failing" advisory *before* the upstream janitor's staleness window converts a
config problem into identity loss.

Boundary honesty: hcom's own janitors key on traffic and can still reap a mute
seat or spare a fossil; herder feeds keepalives and surfaces the warning but
cannot veto. The tracker's own-children-only detection is likewise upstream's;
the shared predicate narrows the *impact* (an undetected-by-tracker session is
still observed via bus and process evidence) without pretending to fix
detection.

### 3.5 Epoch plane — validity domains for coordinates

The registry **projection** already models epochs — epoch records
(`kind: epoch`, substrate, fingerprint) and per-seat `hcom_epoch`/`herdr_epoch`
fields exist in `tools/herder/internal/registry/v2/registry.go`, and seat epoch
stamps survive the carry/merge/equality rules in
`tools/herder/internal/registry/write.go`. **The write spine for them does not
exist yet**: the locked-append API accepts session records only, there is no
typed locked append for epoch records, and the normalizer knows only the
existing session events — new event kinds (attested rebinds, epoch stamps)
need explicit normalizer ownership and carry rules so a sparse patch can never
project as a destructive latest row. That work is planned scope in the
migration plan (write-spine units), not claimed as existing machinery.

Semantics (T5): seat completion stamps both epoch ids; comparisons anywhere
treat cross-epoch as "reconcile me", never as `gone`/conflict; within-epoch
mismatch keeps today's meaning.

**Epoch identity discipline.** Verified during this design and independently
re-confirmed by review against an isolated server (protocol 16): **herdr exposes
no server-generation id** — not in `status --json`, not in the inner snapshot
keys, not in the api schema. The stage proceeds without it, on two legs with an
explicit fallback rule:

- **Probe-inferred boundaries** (ratified spec §6.3): a recorded terminal id
  unknown to the live daemon implies a boundary. This detects **disappearance**
  regardless of what any fingerprint claims — but not **reuse**: the spec
  itself warns terminal ids may be reissued wholesale, and a restarted daemon
  reusing the old id set for different occupants leaves every recorded id
  "known". Hence the next rule.
- **Discontinuity rule (normative, the reuse backstop): unexplained multi-seat
  discontinuity ⇒ epoch unknown ⇒ reconcile.** When one observation pass finds
  two or more seats simultaneously showing turnover-shaped disagreement
  (SID/occupant/coordinate mismatch) with no recorded lifecycle events
  explaining them, the pass treats the substrate epoch as unknown and routes
  *all* affected seats to reconciliation instead of emitting per-seat
  turnover/conflict/`gone` verdicts. Rationale: genuinely simultaneous
  independent multi-seat turnover without observed causes is far less likely
  than an epoch boundary, and the costs are asymmetric — a wrong epoch-unknown
  is one cheap reconcile pass, a wrong per-seat verdict is identity loss.
  Single-seat discontinuity keeps today's semantics (real turnovers are
  overwhelmingly single-seat, and the session layer's sid-changed-in-my-seat
  rule already owns that case).
- **Process-incarnation fingerprint** (accelerator, not authority): the herdr
  API socket is a unix socket, so a peer's process incarnation (pid + start
  time + kernel boot id) is readable at connect time. A fingerprint is
  admissible **only when its transport invariants hold**: direct dial of the
  configured socket path, peer verified as the serving process (not a proxy or
  forwarder holding a passed listener fd), same pid-namespace vantage, and
  stable start-time source. Containers sharing a kernel boot id, socket
  proxies, fd handoff, and cross-namespace pid readings all violate the
  invariants.
- **Fallback rule (normative): any unverifiable incarnation ⇒ epoch unknown ⇒
  reconcile.** Epoch-unknown is never compared as same-epoch.

**Failure-mode honesty.** False rotation (over-reconciling) is the intended
failure mode and costs one cheap pass. False stability is **bounded, not
absolutely excluded**: disappearance is caught by probe-inference, wholesale
reuse/permutation by the discontinuity rule, unverifiable transport by the
fallback rule. The residual is the single-seat coincidence — a falsely-stable
fingerprint (transport invariants passing while wrong) *and* a reused terminal
id landing a different occupant on exactly one seat *and* no other discrepancy
in the pass. Its blast radius is one seat, and its observable shape (a
sid-changed-in-seat event) is exactly what today's turnover semantics already
handle — so the residual is no worse than current behavior, on a strictly
rarer path. A candidate refinement (an authenticated incarnation marker herder
plants and reads back through the substrate) could close it but rides on
unverified substrate surface and is recorded as a refinement to verify, never
load-bearing.

The upstream ask (a first-class generation id in status/snapshot) is recorded as
a refinement that would retire the fingerprint derivation and shrink the
probe-inference reliance; it is not a dependency.

### 3.6 Fact plane — evidence-classed bindings (long horizon)

The registry is append-only with typed events and per-sid sources already; T6
generalizes: every binding-establishing append (seat confirmation, bus-name
bind, launch-context backfill, attested repair, epoch stamp) carries its
evidence class and timestamp, and consumers adjudicate under the T6 lattice —
live conflict refuses, history arbitrates only where live evidence is absent.
This narrows the split-brain mechanism to the quadrant where it is safe and is
the direction the memo names as a program, not a task. The commitment this
document makes is weaker and checkable: every T1–T5 mechanism **writes through**
binding-shaped appends on the ordinary locked spine (no new out-of-band state),
so the program can be built by adding consumers, not by migrating producers.
Registry authority and snapshot-per-event storage are unchanged (T6, rule
footer); any widening of history's adjudication role is a future spec amendment,
not implied here.

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
under a lattice whose top rule preserves fail-closed refusal on live conflict;
T2 makes the copies complete at birth; T5 bounds their validity. Residual: a
disagreement whose latest binding evidence is itself insufficient still
refuses — but T3's enumerated corridors mean the refusal terminates in a
documented sequence rather than a loop, except the one upstream-gated shape
named in §3.3.

**H2 — launch-time env snapshots as identity carriers past validity, doubling as
authentication.** *Neutralized on herder's surface by T1 for the ambient and
inherited class* (credentials replace env as proof; env demoted to hints;
rotation kills stale authority), with the creator-flow ambient-sid harvest
closed by the gated prerequisite unit. *Accepted at the machine boundary:*
deliberate same-uid credential reads are not excluded on this platform (trust
boundary; owner-ruled posture). *Residual, upstream-blocked:* the vendor
extension honors its own inherited `HCOM_*` env cross-tool with no continuity
check, and its exit deletes the row; the hazard doctrine remains load-bearing
for direct vendor-CLI invocation. The frozen `launch_context` in the hcom db
also remains (no upstream setter); T2 backfills it wherever the sanctioned
merge-missing-only write can act, and T5 bounds how long a fossil coordinate can
be trusted.

**H3 — liveness inferred from proxy traffic.** *Neutralized for herder-owned
surfaces by T4* (one shared evidence predicate; no reap without positive death
evidence; first-observer-appends preserved per spec). *Residual,
upstream-blocked:* hcom's janitors (staleness reaping, inactive cleanup,
supervised-binder launch-failure finalization) still key on traffic; herder
feeds keepalives, surfaces starvation early, and endorses the ledgered upstream
asks, but cannot veto an upstream reap.

**H4 — repair verbs gated on the state they repair.** *Neutralized by T3 + T2
for every enumerated damage shape except one.* The break-glass proof pool
(attestation + seat-control corroboration) is disjoint from the bus/sid/env
evidence whose damage it repairs; T2 removes the born-incomplete shapes that
made the remedy ladder circular; registry-seat damage terminates via the
existing re-seat corridor composed with break-glass. The honest exception: the
wrong-nonempty launch-context shape terminates only through the vendor-row
recreate protocol, whose rejoin step upstream's reclaim guard can refuse — that
single shape remains upstream-gated (§3.3 table). The narrow evidence-dominance
exceptions stay exactly as narrow as today — relief valves, no longer the only
exit.

**H5 — creation paths minting rows missing coordinates later verbs require.**
*Neutralized by T2* (one completion step, one row shape per seat kind, loud
refusal with a terminating remedy instead of a partial mint). *Residual,
upstream-blocked:* strand-at-birth — a launch that never boots leaves a pane
with no bindable occupant; that needs the upstream launch timeout. The row
shape is still protected (completion never ran, nothing partial was minted);
the pane husk is a liveness/cleanup matter for T4's evidence, not an identity
matter.

**H6 — verification fences testing values derived from the record being
claimed.** *Contained before this design; the residue is closed by T6
discipline.* The proven tautology class was fixed and adjudicated; what remains
is stored *assertions* (e.g. a verified-flag written by past binaries with
heterogeneous semantics) admitted as conjuncts without provenance. Under T6,
flags become evidence-classed bindings — a conjunct's provenance is part of the
proof — and the no-unexplained-passes discipline (every admitting path pinned
by the fence's matrix) is carried forward as a hard constraint and extended to
the new lattice's admitting paths.

**H7 — coordinates carry no validity domain.** *Neutralized by T5.*
Epoch-stamped coordinates make a substrate restart/handoff a reconciliation
trigger instead of fleet-wide identity loss. Firm without upstream: every
identified false-stability path routes to epoch-unknown ⇒ reconcile
(disappearance via probe-inference, wholesale reuse/permutation via the
discontinuity rule, unverifiable transport via the fallback rule), and the
fingerprint only ever accelerates. The honestly-stated residual is the
single-seat reuse coincidence (§3.5), whose blast radius is one seat and whose
observable shape today's turnover semantics already own. The first-class
upstream generation id is a recorded refinement.

---

## 5. What the target explicitly does not do

- It does not make hcom or herdr correct. The janitor asymmetry, the extension's
  env-honoring takeover, the launch-context setter gap, the reclaim-guard
  stranding (which gates one repair shape, §3.3), the codex pane-id omission,
  the launcher strand — all remain upstream asks, endorsed and ledgered. Every
  mechanism above works if upstream never moves; several get simpler (and some
  of our machinery retires) if upstream does.
- It does not distinguish the operator from deliberate same-uid automation
  anywhere except break-glass Branch A, if the owner selects it. The trust
  boundary in the preamble governs; claims are written to it.
- It does not protect direct vendor-CLI invocation from an identity-bearing
  shell. That is the vendor extension's env boundary; doctrine (scrub the env)
  remains load-bearing there.
- It does not introduce cross-store transactions, multi-node identity, or any
  new store. The env stays a store — demoted, not eliminated.
- It does not weaken a single keep-list fence, and it does not silently amend
  the ratified spec: T6's lattice keeps live-conflict refusal supreme and
  registry authority unchanged; T2 keeps seat kinds; T4 keeps
  first-observer-appends and observer disposability. The one place semantics
  could widen (history adjudicating beyond the absent-live quadrant) is named
  as a would-be spec amendment and not proposed.

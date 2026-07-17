<!-- Provenance: promoted 2026-07-17 from napkins/run-herder-dx/registration-brittleness-memo.md
(gitignored working copy — single-copy risk) as part of the owner-ordered identity
target-state design unit. This is the owner-accepted independent root-cause memo the
design documents build on (docs/design/2026-07-17-identity-architecture-target.md,
docs/design/2026-07-17-identity-migration-plan.md). Content below is unchanged from
the accepted memo except for this header. -->

# Registration/assignment brittleness — independent root-cause memo

Investigator: independent Fable lane (board task: backlog/tasks/task-266). Read-only
investigation: no code edits, no live-state writes, no fleet interaction. Every claim
below is tagged by how it was verified:

- **[code]** — traced in the checkout at HEAD (d50acfa), cited file:line.
- **[record]** — recorded field evidence in board task files / the run journal /
  docs/hazards (cited by task id as an evidence pointer, per brief rules).
- **[upstream-forensics]** — behavior of hcom/herdr internals taken from recorded
  db-verified forensics; not independently re-reproduced here (archived probes
  exist under napkins/run-herder-dx/archive/ for the two fixed classes).

The orchestrator's summaries were treated as an index, not as conclusions. Two places
where my reading **disagrees with or sharpens the record** are flagged inline (§2-D11,
§3-H6) and collected in §7.

---

## 1. Taxonomy — the incident season clustered by mechanism

Six mechanisms. Most incidents are one mechanism wearing several symptoms; several
famous incidents are two mechanisms compounding.

### M1. Frozen-at-birth identity carriers

Identity facts are captured once at process launch and then trusted indefinitely, in
two layers:

- **Process env**: `HCOM_INSTANCE_NAME`, `HCOM_SESSION_ID`, `HCOM_PROCESS_ID`,
  `HERDER_GUID`/`LABEL`, `HERDR_PANE_ID` — snapshotted into the process at birth,
  never refreshed. Goes stale on rename/reclaim (043, 065, 175), pane
  moves/renumbering (041), restart and resume-in-place (041 stale-env variant, 042),
  and wholesale on herdr server handoff (046: the pane-id *scheme* changed and every
  pre-handoff coordinate died at once). [record]
- **The hcom db itself**: `instances.launch_context` (pane_id, process_id) is captured
  from launch-time env and never refreshed — hcom exposes no setter (029 ledger,
  launch-context-ownership candidate). [record][upstream-forensics]

The deep form, which the record circles but never states flatly: **the "live" identity
proof is itself a frozen-coordinate proof**. `hcomidentity.Resolve` — the shared proof
core used by enroll repair, spawn's sender fence, compact --then arming, reconcile —
matches callers against `row.LaunchContext.PaneID/ProcessID`
(hcomidentity/identity.go:156-161) [code]. So "verify against the live roster" means
"compare the caller's launch-frozen env against the row's launch-frozen db snapshot."
Two fossils agreeing is the system's definition of liveness. Everything downstream of
that (M3, M4) inherits the fragility.

Incidents: 041 (all variants), 043, 046, 065 carry-forward, 175, 262's corroborating
stale `--from-pane`, the codex pane_id-omission bind failures (036/045, 029 cand-12).

### M2. Ambient env doubles as authenticated claim — inheritance is impersonation

The same env vars that *describe* a session also *prove* it. Any process that inherits
them becomes the caller. Sub-flavors, all recorded:

- (a) **Vendor-CLI row takeover**: a bare `pi -p` probe from an identity-bearing shell
  inherited `HCOM_*`, the hcom extension honored it with no tool/session continuity
  check, took over the caller's live bus row in place, and its exit **deleted the
  row** (230; docs/hazards/agent-cli-identity-hijack.md). [record][upstream-forensics]
- (b) **Creator-flow SID harvest — still open at HEAD**: `registry.BuildProvenance`
  unconditionally stamps `ToolSessionID: os.Getenv("HCOM_SESSION_ID")`
  (registry/registry.go:776) into rows minted by spawn/fork, and the projection then
  records it as `SIDs[{source: "harvest"}]` **and upgrades the row to
  Continuity: "confirmed"** (registry.go:879-882) [code]. This is 244's wire-proven
  second vector: the caller's sid lands on the child's row, after which the caller's
  own identity-correlated verbs resolve to the child. The function's own comment block
  documents exactly this hazard for `spawnedBy` (the grandparent bug, 016) — the
  reasoning was never extended to the sid one field below. [code]
- (c) **Direct `herder launch` passes `HERDER_*`/`HERDR_*` through** — child acts as
  the caller for guid-keyed verbs (244 first vector, open). [record]
- (d) **Suites inherit seat env** and refuse/void batteries (258 class 1). [record]
- (e) **Tool-signal keying**: hcom `start` side-effects (hook install + exit 1) keyed
  off inherited `CLAUDE*` env (191, 029 cand-16). [record]

### M3. Rows born incomplete on non-primary creation paths

The spawn path records full coordinates; every *other* way a row comes to exist mints
a shape some later verb refuses:

- adopt / `hcom start --as` reclaim → bus row with `launch_context={}` → spawn-dead
  seat, no healing verb existed (262, 222, 029 ledger). [record]
- launch stranding (mise trust prompt, wrapper update check) → half-born rows: pane +
  registry row, no bus bind, launcher sleeps forever (258 class 3, 263, 029 new
  candidate). [record]
- old-binary repairs → rows with `hcom_verified` *absent* (vs true/false) that
  fail-closed arming conjunctions silently skip (226). [record]
- pi seats: 0/6 lifetime boots — the bind predicate is never satisfiable while the
  vendor wrapper stalls pre-child (263). [record]

The system's verbs were designed against the spawn-born row shape; recovery-born rows
are second-class citizens. 262 fixed the flagship instance (adopt now resolves a fresh
live pane pre-write and backfills; spawn gained the narrow empty-context fallback,
spawn.go:2165-2191 [code]) but the property — many creation paths, one assumed shape —
remains.

### M4. Circular repair: verbs gated on the state they exist to repair

Code-traceable, not incidental:

- **enroll repair** hard-requires `live.Verified` (enrollcmd/enroll.go:404) [code] —
  and live verification is M1's fossil-agreement proof, which requires exactly the
  correlates whose loss is the damage. Verified live in the field: "stored bus name
  cannot be corroborated because live bus identity proof is unavailable" (262
  refusal 2). A session seated on its own row cannot even repair its own bus binding,
  because its own row occupies the seat the proof needs (262 field-case note). [record]
- **reconcile D11 dominance exception** requires
  `ResolveExactSessionPane(recordedSID, pane)` — equality on the recorded SID *and* on
  the bus row's launch-frozen `launch_context.pane_id`
  (reconcilecmd/reconcile.go:444-446 → hcomidentity/identity.go:210-229) [code]. A row
  reclaimed via `start --as` fails the SID leg; an empty-launch-context row fails the
  pane leg. The rows this exception exists to heal are structurally outside its
  predicate. Worse, the launch-context *backfill* only arms on a `re-confirm` outcome
  (reconcile.go:477-481), which the conflict blocks — repair of the missing coordinate
  requires the coordinate. [code]
- **The remedy ladder loops**: refusal texts prescribe enroll → reconcile → adopt;
  adopt creates the M3 shape; whose remedy is enroll. Recorded terminal states:
  remedies that mint new-guid rows poisoning sid resolution (242), a duplicate seated
  row on a live pane with no cleanup verb (243), a prescribed verb that was
  `Empty()`-gated and structurally could not cure the state it was prescribed for
  (262 review). [record]

242/243/255/262 each fixed one cell; the *property* (repair proofs drawn from the same
evidence pool the damage depletes) is intact everywhere else.

### M5. Liveness inferred from proxy traffic, firing on both wrong sides

- hcom's `stale_cleanup` reaped a **live** seat whose keepalive was silently starved by
  a config-layer failure (mise trust), converting a config problem into identity loss
  after one staleness window; the same day a **dead** seat's row fossilized at
  "listening" for 4+ hours (258 mechanism 3 + janitor asymmetry corollary).
  [record][upstream-forensics]
- `inactive_cleanup` reaps idle-but-live bridge-bound seats at 1h (197);
  `instance_lifecycle` finalizes externally-supervised binders `launch_failed` at 30s
  while the process is alive and serving (029 cand-15). [record]
- herdr's tracker detects only agents it launched: shell-relaunched or
  resumed-in-place sessions are `undetected` forever, and resume-in-place never
  re-fires launch-time detection (070, 044, 041 resume-path refinement). [record]
- herder's own earlier version of the disease: pane-based liveness left dead rows
  "working" forever on reused panes (035, fixed). [record]

Common property: each store infers liveness from *its own interaction history* with
the session, not from evidence about the process. So janitors reap the living (mute
but alive) and spare the dead (no traffic to age out).

### M6. Cross-store identity split-brain

Identity is spread across **four** stores, not three — the herder registry (guid,
label, seat), the hcom instances db (bus name, sid, launch_context), the herdr tracker
(pane/terminal/display name), and the process env as an unversioned, ungoverned cache
of all of them. Each store holds *copies* of the others' coordinates, not references:
the registry stores hcom_name + pane_id; hcom stores pane_id; the tracker stores a
name. Writes are transactional per store at best (registry `UpdateLocked` flock;
`RepairLaunchContext` BEGIN IMMEDIATE with guarded re-read — both good [code]) and
never across stores.

Recorded split-brain: `rename --take-from` moved the registry label while the tracker
kept the fork-era name → D11 conflict (264); `start --as` moved the bus identity while
the registry kept the old name (262 field case); a mis-bound hcom_name survived row
succession because carry-forward never re-consulted the bus (065, fixed); remedial
mints left a retired row owning a live session's sid (242) and a duplicate seated row
on one pane (243). Repair verbs then arbitrate pairwise reconciliations with
fail-closed rules — so any two-store disagreement without a third corroborator becomes
a *permanent refusal*. That is the season's signature "spawn-dead but fully alive"
state. [record]

---

## 2. Root causes — the architectural properties that keep generating the class

The brief's six candidate hypotheses, tested against the record and code:

**H1 — identity spread across stores with no transactional coupling, no single source
of truth: ACCEPT, sharpened.** Count the env as a fourth store — it is the least
governed and the most load-bearing (it is the *claim* interface, M2). The deeper
property: stores hold aging **copies** of each other's coordinates with no binding
provenance and no arbiter ordering. Identity is *defined as agreement among copies*;
when copies disagree there is no fact of the matter to consult, only refusal.

**H2 — launch-time env snapshots as identity carriers past validity: ACCEPT, sharpened
twice.** (a) The channel doubles as authentication, so staleness is only half the
hazard — inheritability is the other half (M2: takeover, harvest, passthrough).
(b) The freeze extends into the hcom db: `launch_context` fossilizes launch env, and
the shared proof core matches against it, so even the post-A1 "live roster evidence"
discipline is frozen-coordinate matching at one remove (identity.go:156-161) [code].

**H3 — liveness inferred from heartbeat traffic rather than evidence: ACCEPT**, with
the extension that it is not only hcom's janitor: herdr's own-children-only detection
and herder's former pane-based liveness are the same property expressed per store
(M5). Both wrong-side failures are proven in one day's record.

**H4 — creation paths can mint rows missing coordinates later verbs require: ACCEPT,
narrowed** to *non-primary* paths (adopt, reclaim, manual enroll, strand-at-birth).
The primary spawn path is coordinate-complete; the system just never promoted the
recovery paths to the same contract, and upstream offers no launch-context setter so
completeness was unachievable for reclaimed rows until the 262 direct-db adapter.

**H5 — repair verbs gated on the very state they repair: ACCEPT**, and it is the most
mechanically demonstrable root cause in the codebase (enroll.go:404;
reconcile.go:444-446 + 477-481) [code]. Corollary the record supports: the remedy
*ladder* is also circular (M4), so refusal honesty alone cannot fix it — some verb
must accept a proof drawn from outside the damaged evidence pool.

**H6 — verification fences test values derived from the record being claimed: NARROW
to "contained, with a residue."** The proven tautology (pinned-repair ownership proof
fed by the stored label) was found and fixed, and the core-key variant was explicitly
adjudicated sound because seat selection independently proves terminal+pane+bus (255)
[record]. I did not find another live tautology in the current proof chains [code].
The systemic residue is subtler: (i) stored *assertions* gate fallback admission —
`hcom_verified` (a flag written by past binaries with heterogeneous semantics, per
226) is a conjunct in both the spawn empty-context fallback (spawn.go:2176) and the
reconcile empty-context heal (reconcile.go:462) [code]; corroborated by live
uniqueness checks, so not tautological, but the flag's provenance is not part of the
proof. (ii) The 264 unexplained-pass: an identity fence admitted the field row for
reasons the matrix does not cover — the general form of "we don't know what our proofs
prove," which is how tautologies get in.

**H7 (unlisted, mine) — coordinates carry no validity domain (no epoch).** Pane ids
re-key on moves and wholesale on server handoff (046: `w-N` → `w-pN`); terminal ids
are move-stable only within a server run. Stores treat these as durable identifiers
with no generation stamp, so an epoch rollover is indistinguishable from identity loss
and produces fleet-wide `gone`/conflict verdicts (046, 041, 070 post-handoff hits).
[record]

---

## 3. Improvement directions — ranked by prevented-incident mass per unit blast radius

Design directions, not implementations. Each names the invariant, what it would have
prevented, migration honesty, and what it does NOT fix.

**R1. One canonical rebirth: every creation/recovery verb ends in the same
seat-completion step.** Adopt, enroll(-repair), reclaim, and future recovery verbs
finish by running the identical completion the spawn path runs: resolve live
pane/terminal, verify the bus row, record/backfill launch coordinates, and refuse
loudly with the missing-fact list rather than minting a partial row.
*Invariant: exactly one row shape exists; no verb ever needs a "born incomplete"
branch.* Prevents: the 262 class, 222, 226, the backfill half of 264, and it deletes
the M3 second-class-citizen property. Migration: moderate — 262 already built the
pieces (live-pane pre-write, schema-gated adapter, empty-context fallback); this
consolidates them into a shared step and extends to enroll/reclaim. Does NOT fix:
strand-at-birth (the pane never boots — needs the upstream launch timeout), or any
M2 contamination.

**R2. A non-circular break-glass repair verb: proof requirements disjoint from the
state being repaired.** An operator-attested repair (explicit human confirmation +
physical-seat corroboration only: live pane read-back + terminal match) that can
rebind any single identity field — bus name, sid, launch context — without requiring
the bus/sid/env proof that is broken, logging the attestation into the row's history.
*Invariant: for every damaged shape there exists a terminating repair sequence — no
refusal loops.* Prevents: the entire operational tail of the season — db surgery
(230 recovery), fork-swap seat replacement (262), env-prefix workarounds (242/250),
the 242/243 strands. Migration: small code, large policy surface — this is a takeover
surface by construction; it must be attested, logged, rate-limited, and must preserve
stored label/role/lineage (the 255 lesson). Fail-closed automated paths stay exactly
as they are. Does NOT fix: any root cause — it caps the *cost* of the class while
R1/R3/R4 shrink it. I rank it #2 anyway because the record shows the dominant damage
was operator-time and owner-escalations, and every root-cause fix below still leaves
some residue this catches.

**R3. Separate identity description from identity proof: minted per-seat credentials.**
Herder mints a random per-seat token at spawn/enroll/adopt, delivered out-of-band of
the inheritable env (seat-scoped file keyed to guid+epoch, or equivalent); herder
verbs authenticate by token, and env vars demote to hints/diagnostics. Inherited env
in a child then proves nothing.
*Invariant: identity claims are unforgeable-by-inheritance and rotate at rebirth.*
Prevents: 244 both vectors, 258 class 1, the herder-side half of the hijack class
(230), 043-class stale-env writes. Immediate cheap slice while designing it: stop
harvesting ambient `HCOM_SESSION_ID` in `BuildProvenance` for creator flows
(registry.go:776) — pass explicit values the way `spawnedBy` already works; that
closes the one *open, code-verified* contamination hole today. Migration honesty:
the vendor extension path is out of reach — hcom will keep honoring its own env
(upstream ask stands), so doctrine (hazard doc rules) remains load-bearing for
direct vendor-CLI invocation. A transition period with env fallback re-opens the
hole; the cutover must be a real cut.

**R4. Evidence-based liveness in one place.** The observer becomes the sole liveness
authority: it fuses process evidence (pid/process tree), pane read-back, and bus
traffic; `list`, janitors-we-own, and repair verbs consume its verdicts; heartbeat
silence alone never reaps and own-launch history never gates detection.
*Invariant: no reap without positive death evidence; no `gone` verdict while the pane
reads back.* Prevents: 070/044 presentation, 035-class, the herder-side detectability
of 254/258-mechanism-3 (starvation becomes a readable "keepalive failing while holder
alive" signal before the upstream janitor fires). Migration: the observer
(073/080/081) already exists and confirms seats; this is authority consolidation, not
new machinery. Does NOT fix: hcom's own janitors (upstream asks already ledgered:
liveness-weighted staleness, keepalive affordance) — herder can only feed keepalives
(197 pattern) and detect, not veto.

**R5. Epoch-stamp stored coordinates.** Every stored pane/terminal coordinate carries
the herdr server generation; cross-epoch mismatch triggers re-resolution instead of
conflict/`gone`.
*Invariant: a coordinate is only ever compared within its validity domain.*
Prevents: the 046 class wholesale, the renumbering flavor of 041, a slice of D11
conflicts. Migration: small (one field + comparison discipline) IF herdr exposes a
server-run/epoch id — verify; if not, it is an upstream ask (the handoff-survivor
re-adoption candidate on the 029 ledger is adjacent).

**R6 (long horizon). Binding events instead of coordinate copies.** The registry's
event log records *verified bindings* (guid ↔ bus row ↔ terminal@epoch ↔ sid, with
evidence class and timestamp) rather than copies of foreign coordinates; other stores'
values are cache, reconstructible from binding history; disagreement between stores
resolves by consulting the latest binding, never by refusal.
*Invariant: there is a fact of the matter about identity, ordered in time.*
This subsumes R1/R5 and most of M6. Blast radius is the whole verb surface —
staged consumers over the existing event-sourced registry make it feasible, but it is
a program, not a task. Named so the smaller steps above can be checked for
compatibility with it rather than against it.

---

## 4. Keep-list — fences that earned their keep this season

Improvements above must not weaken these; each is evidence-backed:

- **Fail-closed conflicting-correlate resolution** (identity.go:176-191 refuses
  multi-match rather than choosing) and **D11 refuse-to-unseat** — D11's false
  positives are M6's fault, but its refusals are why no live seat was destroyed by a
  repair verb all season; the janitor incident shows the cost of the opposite policy.
- **Occupied-seat no-mint fence + atomic repair-first ordering** in enroll (243 fix;
  enroll.go:106-131, 215-238) — duplicate minting is proven worse than refusal.
- **Merge-missing-only, schema-pinned vendor-db write** (`RepairLaunchContext`:
  pane_conflict refusal, guarded update, re-read-before-commit) — the only sanctioned
  cross-store write, and its never-rewrite-existing rule is what makes it safe.
- **Guid never re-keyed** (spec D1: new transcript = new guid; adopt composes
  enroll + transfer + retire) — prevented identity aliasing through every recovery.
- **Misdelivery-worse-than-drop**: no bus name from tag+cwd guesses (033), ambiguity
  refusals on positional resolution (035), empty-better-than-wrong hcom_name (043/A1).
- **Name-from-env excised from proof**: `CurrentEvidence` deliberately excludes
  `HCOM_INSTANCE_NAME` (identity.go:79) — do not let a convenience path reintroduce it.
- **Ownership proofs read the caller's claim, not stored values, on pinned paths;
  stored values admissible only where selection independently proves the seat**
  (255 settled contract) — plus its mutation-armed fixture discipline.
- **The narrowness template of the 262 fallback** (exact terminal+pane + verified +
  unique-joined + empty-context only) — evidence-dominance exceptions should stay this
  shaped.
- **No unexplained passes on identity fences** (264 discipline): a desired outcome
  with an unpinned admitting path is a matrix gap, not a win.
- **sender ≠ recipient invariant + observation-only verdict prose** on continuation
  delivery (257), and adopt's authorization requirement for resumed-SID claims (222).

---

## 5. Ours vs upstream split

**Ours (herder) — fixable locally:**
- Creator-flow ambient-SID harvest (registry.go:776) — open, code-verified (M2b).
- `HERDER_*`/`HERDR_*` passthrough on direct launch (244a).
- Circular repair gating (M4) and remedy-ladder loops — R1/R2 territory.
- Coordinate copies without binding provenance in the registry (M6/R6).
- Recovery-path row completeness (M3/R1) — the adapter half is ours; the clean fix
  is upstream's setter.
- Liveness presentation and authority consolidation (070 residue, R4).

**Upstream hcom (asks mostly already on the 029 ledger — this memo endorses them):**
- `launch_context` is launch-frozen with no setter; rows created by `start`/`start
  --as` keep `{}` forever (the direct enabler of the spawn-dead class).
- Janitors key on heartbeat/traffic: stale_cleanup reap-the-living, the fossil
  non-reap asymmetry, inactive_cleanup 1h, 30s launch_failed on supervised binders.
- Extension honors inherited identity env cross-tool with no continuity check; exit
  deletes the row; reclaim guard strands the rightful owner and the refusal exits
  rc=0 (the hijack triad).
- `--run-here` launcher strands forever on shell-init failure (half-born rows).
- codex rows omit `launch_context.pane_id` (kills fast child correlation).
- `start` side-effecting hook install keyed on ambient env detection.

**Upstream herdr:**
- Tracker never adopts foreground/foreign-launched agents (070) — the whole
  `undetected` class and the resume-in-place hole.
- Handoff re-keys coordinates with no epoch signal and no server-side re-adoption of
  surviving processes (046; R5's dependency).

**Shared/doctrinal:** identity env IS state (hazard doc) — remains load-bearing for
direct vendor-CLI invocation until the upstream continuity check exists, regardless
of R3.

---

## 6. What this memo did not do

No live probes were run: every mechanism claim was verifiable from code at HEAD or
from recorded, db-verified forensics; the archived probes cover two already-fixed
cells (255, 262). Upstream-internal behaviors (janitor thresholds, extension takeover
semantics) rest on the recorded forensics and are flagged as such.

## 7. Disagreements / sharpenings vs the orchestrator record

1. **264's ordering hypothesis is wrong in detail**: at HEAD the D11 evidence-dominance
   exception IS consulted on tracker-name conflicts (reconcile.go:427-455); the field
   row fails because the exception's predicate (recorded-SID equality + bus-row
   launch-frozen pane equality) is unsatisfiable for reclaimed/empty-context shapes —
   and the backfill that would fix the pane leg only arms on `re-confirm` (477-481),
   which the conflict blocks. The fix scope is the predicate's evidence classes, not
   consultation order. (The task's own "suspected second divergence" — the SID leg —
   is correct.)
2. **The hazard doc's assurance is env-channel only**: "managed launches discard every
   ambient HCOM_*" is true of the child *environment*, but the child *row* still
   receives the caller's ambient sid via BuildProvenance harvest, stamped
   Continuity:"confirmed" (registry.go:776, 879-882). 244 is open; until it lands the
   doc's "mechanical fixes" section overstates the boundary.
3. **"Three stores" undercounts**: the process env is a fourth store — unversioned,
   inheritable, and the only one that is also an authentication interface. Framing it
   as a store makes R3 the obvious shape.
4. **H6 (derived-value fences) is largely contained** post-255; the live residue is
   stored-flag (`hcom_verified`) provenance feeding fallback conjunctions, and the
   unexplained-pass matrix gap — not tautologies.

## 8. Top-three findings (as reported on the bus)

1. One meta-root: identity lives as frozen, inheritable **copies** (env + db-fossilized
   launch_context + cross-store coordinate duplicates) that double as description and
   proof; every incident family is a copy aging (M1/M5/M6) or a copy being inherited
   (M2). Even the "live" proof core compares fossils (identity.go:156-161).
2. Repair circularity is structural and code-traceable (enroll.go:404,
   reconcile.go:444-481): repair proofs draw on exactly the evidence pool the damage
   depletes, and the remedy ladder loops through adopt back into the damaged shape.
   Highest-leverage fixes: canonical rebirth (R1) + an attested break-glass verb with
   disjoint proof requirements (R2).
3. One contamination hole is open and code-verified at HEAD: creator flows harvest the
   caller's ambient HCOM_SESSION_ID onto the child row and mark it
   Continuity:"confirmed" (registry.go:776/879-882) — 244's second vector. Cheap,
   contained fix; recommend it not wait for the full R3 credential design.

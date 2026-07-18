# Credential DX: verb-level self-resolution from live correlates

- **Task:** TASK-282 (design; adversarial design review before any implementation task is cut)
- **Date:** 2026-07-18 (rev 6, after adversarial review rounds 1–5 — reviewer-rofe; disposition maps in §12)
- **Status:** Revised draft for re-review
- **Amends:** the double-reviewed "ambient evidence may verify but never select" boundary, per the owner-ratified direction of 2026-07-18: *"low ceremony for sane defaults, explicit at the API layer, and escape hatches."*

## 1. Problem

Since the TASK-272 cutover, every credential-authenticated herder verb demands
`--credential-file PATH`, and field experience shows every call site performing
the identical incantation:

```sh
herder send --credential-file "$(herder credential path --guid "$HERDER_GUID")" @target 'msg'
```

Two things are wrong with this:

1. **The incantation launders ambient env back into authority selection.** The
   path — and therefore the credential presented — is chosen by
   `HERDER_GUID`, an inherited env value the credential system explicitly
   demoted to a hint. Worse, `seatcred.VerifySelectedBus` returns `nil` when
   *no* ambient correlate is present at all
   (`tools/herder/internal/seatcred/credential.go:346`), so in an environment
   with a poisoned `HERDER_GUID` and no `HCOM_*`/pane correlates, the
   incantation authenticates *as the poisoned seat* with no live check.
   (Reviewer-confirmed against `credential.go:269-311` and `343-348`.)
2. **Per-verb friction for humans and agents alike.** When every call site
   performs the same incantation, the incantation belongs in the callee.

The ratified direction: a credential-authenticated verb invoked without
`--credential-file` resolves the caller's own seat from **live correlates**,
opens that seat's registry-current canonical credential, and then runs the
*unchanged* explicit authentication pipeline. `--credential-file` becomes the
explicit override; the raw seatcred API stays explicit.

## 2. Design overview

The default is sugar for computing the path — never for skipping the fence.

```
                       no --credential-file            --credential-file PATH
                       ─────────────────────           ───────────────────────
 [NEW] SelfResolve:    occupancy anchor (kernel
                       ancestry ∩ live pane process
                       inventory) → one pane → one
                       seated row → one joined bus
                       row → guid → CurrentPath()      (skipped entirely)
                                │                              │
                                ▼                              ▼
 [UNCHANGED] seatcred.Authenticate(registryPath, path)  ← same call, same checks
                                │
                                ▼
 [UNCHANGED] seatcred.VerifySelectedBus + per-verb post-selection fences
```

Normative rules:

- **R1 — verb-level only.** Self-resolution is one new helper (working name
  `seatcred.SelfResolve`, dependency-injected so seatcred stays exec-free: it
  takes the caller's process ancestry, live pane inventories, live roster
  rows per namespace, the registry projection, and the env *hints*, and
  returns a canonical credential path or a typed refusal). It is called only
  from the six verb fences that today call `seatcred.ExtractFlag`, plus
  `herder credential path --self`. `Stage`, `Authenticate`,
  `VerifySelectedBus` keep their exact signatures and semantics;
  `Authenticate(registryPath, "")` still returns `ErrCredentialRequired`.
- **R2 — the anchor is caller-bound, not env-claimed.** Selection is rooted
  in two non-env facts: the calling process's own ancestry (kernel-reported
  ppid chain) and herdr's live per-pane process inventory
  (`herdr pane process_info`: `shell_pid` + foreground process pids,
  `herdrcli.go:122-128`, already consumed by spawn/observer/lifecycle). Env
  values — `HERDR_PANE_ID` **included** — never select; each, if set, may
  only veto (§2.1 step 6). This is stronger than the legacy fences, where
  `HERDR_PANE_ID` is the entry point; the asymmetry is deliberate and is
  what closes the coherent-poison steering hole (review P1-1, §5.2).
- **R3 — fail-closed, no ambient fallback.** Any conflict, ambiguity, or
  absence of live proof refuses with the escape hatch named. Resolution
  failure never falls back to pre-cutover ambient attribution. Exactly two
  verbs have ratified, explicitly-pinned **miss-only** fall-throughs (§4):
  flag-less `enroll` falls through to the credential-free *fresh mint*, and
  flag-less `spawn` falls through to the credential-free
  `spawned_by: "user"` leg **whenever neither an hcom prompt sender nor an
  implicit notify recipient is required** (the §4.1 structural rule — this
  includes explicit-`--notify-to` and bash boot-paste-prompt misses, not
  only promptless/notify-less ones). A *miss* is the anchor finding
  no occupied pane or no seated candidate; a *conflict or ambiguity* (a
  candidate found but cardinality or a hint veto fails) is never a miss and
  always refuses on every verb — poison can therefore strip nothing and
  select nothing. Neither fall-through attributes an existing identity:
  fresh mint creates a new one, and `user` is the no-identity attribution
  spawn already uses for humans.
- **R4 — the resolved path is the canonical registry-derived path.**
  `SelfResolve` ends in `seatcred.CurrentPath(registryPath, guid)`
  (`credential.go:137`), reading only non-secret registry metadata. The
  subsequent `Authenticate` performs every existing check unchanged:
  owned-regular-file, 0600, size bound, version, generation-currency,
  constant-time token match, audit append. Because the presented path is the
  canonical path, the audit records `presentation: "canonical"` — see the
  withdrawn audit delta in §9 for why this note no longer claims a distinct
  `self-resolved` audit value.
- **R5 — explicit flag always bypasses resolution.** When
  `--credential-file` is present, `SelfResolve` is not consulted at all and
  authentication behaves byte-for-byte as today. Whether the *act completes*
  is governed by each verb's post-selection fences — unchanged except the
  one named delta D6 (§9; the former D5 fence change was deleted in round
  5, §6.1); the honest per-verb truth table is §6. This note no longer
  claims the override is universally sufficient from arbitrary
  environments.
- **R6 — pre-cutover behavior unchanged for every authority-changing
  behavior.** Before the cutover marker exists, verbs keep the current
  legacy ambient-verified path, and every **authority-changing** behavior
  this design adds — the D1 self-resolution default, the D6 verification,
  and (for wording coherence, though it is a lookup) the D4 `--self`
  helper — is marker-gated and inert marker-off (rounds 3–4; D5 no longer
  changes any fence and its refusal DX rides the marker-on refusal path).
  Deleting the marker therefore rolls back every behavior through which
  this design selects, waives, verifies, or resolves authority, explicit-
  flag paths included; this lever has been exercised live and must stay
  whole. The one deliberately *non*-gated piece is the D3 herdr surface
  extension: additive `process_info` response fields are
  deployment-persistent server API — present regardless of marker state,
  carrying observation data and no authority (§9). Self-resolution
  replaces exactly one thing: the post-cutover no-flag
  `ErrCredentialRequired` refusal. The rollback story (§11) is untouched.
  Making any gated delta marker-independent would be an explicit R6
  doctrine revision requiring its own owner sign-off — it is not proposed
  here.
- **R7 — SelfResolve owns its cardinality checks.** At every stage it counts
  *row instances* it matched itself. It deliberately does **not** inherit
  `hcomidentity.Resolve`'s semantics: `Resolve` keys matches by row *name*
  and applies its raw `rowMatches > 1` guard only to the `name` signal
  (`identity.go:173-186`), so two joined rows sharing one name collapse to
  one map entry and verify (review P1-2). SelfResolve's roster step counts
  matching row instances directly; two joined rows with the same name is a
  refusal, not a match.

### 2.1 Resolution algorithm (normative)

Given the registry projection and a live herdr client:

1. **Ancestry.** Walk the calling process's ppid chain via `/proc`
   (bounded depth, stop at pid 1). Linux-only, like the rest of the fence
   tooling (`syscall.Kill`-based probes in `liveness/observe.go:11`).
2. **Occupancy anchor with proven process identity.** Enumerate live
   panes (`herdr agent list` / session snapshot) and fetch
   `pane process_info` for each. A pane is *occupied by the caller* iff
   (a) a pid in the pane's inventory (`shell_pid` or a foreground process
   pid) appears in the ancestry chain, **and** (b) *process identity is
   positively established* for at least one matched pid via the D3 herdr
   surface extension (§9): `process_info` reports, per process, its PID
   **namespace identity** (ns inode) and **start time**, and the caller
   compares both against its own `/proc` view of that ancestor
   (`/proc/<pid>/ns/pid` inode, `stat` starttime). Equality of the
   (ns-inode, pid, starttime) triple proves both views name the same
   process; that is what makes the anchor caller-bound. Numeric pid
   intersection alone is never caller-bound across PID namespaces, and —
   per review round 3 — **argv equality cannot discharge the
   cross-namespace collision either** (two namespaces can hold different
   processes with equal pid and identical cmdline; common agent/shell
   invocations make that plausible), so argv is not proof of anything in
   this design. When the surface is absent (older herdr) or the fields
   are missing for every matched pid, agreement is unestablishable:
   **hard-refuse** — on such a deployment the default is unavailable and
   the explicit flag is the supported path. Exactly one occupied pane is
   required; zero (herdr down, `process_info` unavailable, caller
   reparented to init by setsid/daemonization, caller outside any pane,
   identity not established) or more than one (nested seats, §5.2
   residuals) refuses. A dead or unresolvable pane can never be replaced
   by bus-only proof: no anchor, no default (review harness shape 1).
   The start-time comparison also closes the earlier pid-reuse residual:
   a reused pid has a new start time and fails the triple.
3. **Seat mapping.** Exactly one *seated* registry row whose recorded
   `seat.pane_id`/`seat.terminal_id` matches the occupied pane's live
   coordinates. Zero or >1 (reused coordinates) refuses with the candidate
   list (guid, label, bus name — the `formatCandidates` shape,
   `send/send.go:373`).
4. **Bus corroboration.** The row must be bus-bound (recorded `hcom_name`),
   and that name must match **exactly one** joined row instance on the
   roster of the row's *recorded* `seat.namespace` — never ambient
   `HCOM_DIR` (review P2-6; §5.4). Count per R7: duplicate same-name joined
   rows refuse. Bus-less seated rows (bash operators) are not resolvable by
   design — no downstream fence can consume a bus-less selection anyway
   (review P2-4; §7 tells the honest operator story).
5. **Cross-namespace cardinality.** Candidate namespaces are the deduped
   recorded `seat.namespace` values of seated rows (in practice one global
   bus). If steps 2–4 could complete against more than one candidate seat
   across namespaces, refuse. Ambient `HCOM_DIR` is never read by
   SelfResolve.
6. **Hint vetoes.** Each of `HERDER_GUID`, `HERDR_PANE_ID`,
   `HCOM_SESSION_ID`, `HCOM_PROCESS_ID`, if set, must be consistent with
   the resolved seat (guid equality; pane naming the occupied pane; session/
   process matching the resolved roster row's recorded
   `session_id`/`launch_context.process_id`). Any mismatch — including a
   hint naming nothing at all — refuses. Hints can veto, never steer.
   Unset hints are fine.
7. **Path.** `CurrentPath(registryPath, guid)` → canonical path. A legacy
   row (no generation) or a missing token file surfaces that function's
   existing refusals (§8 rows 6–7).
8. **Unchanged pipeline.** The verb calls `Authenticate(path)` and its
   existing post-selection fences (`VerifySelectedBus`, compact's terminal
   equality, adopt's target-guid check, …) with no changes. This is
   partially redundant with resolution — deliberately: one choke point for
   both entry modes, and it closes part of the resolution-to-use race. A
   generation flip between step 7 and `Authenticate` refuses via the
   existing `ErrStaleCredential` check; the default path performs no
   automatic retry — the refusal says to rerun the verb (§8 row 8).

## 3. What the security claim now is — stated exactly

The prior draft claimed "a poisoned env cannot steer the default." Review
P1-1 showed that claim was too strong for an env-probe design: a *coherent*
poison (a live victim seat B's `HERDR_PANE_ID` + `HCOM_SESSION_ID` +
`HCOM_PROCESS_ID`, guid matching or unset) satisfies every env-probe check,
because `hcomidentity.CurrentEvidence` reads env (`identity.go:81-92`) and
proves only that the claimed coordinates belong to *some* joined row — not
that the invoking process occupies them.

The revised design anchors selection on facts a poisoned environment cannot
supply: the kernel's ppid chain for the calling process, intersected with
herdr's live statement of which processes are in which pane. The claim,
narrowed to what the evidence proves:

> **Given positively established process identity (§2.1 step 2 — the
> herdr-reported ns-inode + start-time triple), a poisoned environment
> cannot steer the default to any seat whose pane's live process tree does
> not contain the calling process.** Environment values can only cause
> refusals (veto), never selection. In the coherent all-live victim-tuple
> attack, the caller's ancestors are in pane A; pane B's inventory does
> not contain them; B is never a candidate no matter what the env claims
> (harness N2). When identity cannot be positively established — surface
> absent, fields missing, namespace-split deployment — there is no anchor
> and the default hard-refuses; it never degrades to numeric-pid or
> argv-similarity trust.

Deployment note: on a namespace-split deployment (e.g. herder inside a
PID-namespaced container reaching a host herdr socket) the ns-inode
comparison fails by construction, so self-resolution is honestly
unavailable there and the flag is the supported path. The fleet norm
(herder and herdr sharing one namespace) is unaffected.

Explicit residuals — what this does **not** claim:

- **Same-uid explicit access is out of scope, as at cutover.** Any same-uid
  process can read any 0600 token and present it via the flag. The default,
  like the flag, provides selection discipline, not intra-uid access
  control.
- **Observation-window race.** The identity triple (ns-inode, pid,
  starttime) is compared against herdr's observation, which is a snapshot:
  a process that exits and is replaced *between herdr's read and the
  caller's comparison* fails the start-time check and refuses (safe
  direction). The residual is the classic TOCTOU sliver between the
  caller's own `/proc` reads and its use of the result — one CLI
  invocation wide, and steps 3–6 (seat + roster + hint consistency) must
  *also* line up for a wrong selection. The former pid-reuse and
  argv-collision residuals are closed by the triple. Accepted, named.
- **Same-tree nesting.** A process manually launched *inside* another
  seat's pane (e.g. a hand-run `claude` under a seated bash shell, both
  enrolled) makes two occupied-pane/seat candidates share one ancestry;
  step 2/3 cardinality refuses rather than picks (harness N4).
- **Compromised kernel or herdr is out of scope**, as it is for every
  existing fence.
- **The trust root is the state dir.** `registry.DefaultPath()` and the
  credential tree are anchored where they are today (including
  `HERDER_STATE_DIR` for isolated registries). That is pre-existing cutover
  ground, not widened here; harness N11 pins that an isolated registry
  cannot be escaped via resolution.

Evidence-class note: the anchor combines the existing pane class (live herdr
pane state, already fence evidence) and the existing process class (live
pids, already used by `liveness.ProbePID` and `SeatProcess` completion,
`seatcompletion/completion.go:349-353`). It is a new *proof form* pairing
two existing classes with a kernel-truth source, not a new evidence class;
it is nonetheless named as a delta (§9 D3) and rides the owner sign-off.
D3 carries one **herdr surface extension** as an explicit implementation
dependency (round 3 P1-3): `pane process_info` — which exists and is
already parsed (`herdrcli.go:253`) — additionally reports each process's
PID-namespace inode and start time, so the caller can positively prove
process identity rather than trust numeric pids or argv similarity. The
default hard-refuses against a herdr without the extension. Notably,
normal pane seats record no `seat.pid` (only headless `SeatProcess` seats
do), which is why the anchor uses live pane inventory rather than a
recorded pid.

## 4. Per-verb default semantics

"Self" is the seat whose pane's live process tree contains the caller.

| Verb | What the credential selects today | Default (no flag, post-cutover) |
|---|---|---|
| `send` | caller/sender attribution (`send.go:64-72`) | SelfResolve the caller seat; sender name from the selected row exactly as `credentialCallerSender` does today |
| `spawn` | `spawned_by` attribution, initial-prompt sender, `--notify` recipient (`spawn.go:937-994`) | SelfResolve the spawner; outcomes are normative per the full matrix in **§4.1** (child capability × notify-to presence × marker state). Summary: **resolve** → the seat is caller for attribution, prompt sender, and notify; **miss** → refuse only where an identity is structurally required (hcom-child prompt sender; `--notify` without `--notify-to`), otherwise proceed as `spawned_by: "user"` — the second miss-only fall-through (R3); **conflict/ambiguity** → refuse outright, never downgrade to `user` (pinned by N16). The bash boot-paste prompt flow is preserved (§4.1) |
| `compact` | proof of the caller's own pane (`compact.go:122-142`) | SelfResolve; compact's extra credential-branch fences (terminal equality, bus verify) run unchanged |
| `cull` | caller identity for authority + release-notice sender (`cull.go:71-89`) | SelfResolve the caller seat |
| `adopt` (seated source) | **the source seat's** credential as custody proof (`adopt.go:80-100`) | Resolve the *source* seat by live occupancy: the caller's occupied pane (step 2) must be the seated source's recorded pane — the same occupancy `adoptionUnseatReason` demands, now proven by ancestry instead of env. Adopt is the one verb where "self" means "the seat whose pane I demonstrably occupy." A caller not occupying the source's pane gets no default; cross-pane recovery of a dead source is the D5 composition (§6.1: applier-settled unseat, then plain unseated adopt on the fresh-enroll leg), never a seated-source override |
| `enroll` | existing-live-seat re-enroll requires the seat's credential (`enroll.go:488-490`); fresh enroll mints credential-free | SelfResolve; on success, re-enroll/repair as the resolved seat (the common "run `herder enroll` from this session" remedy, now bare). On resolution **miss**, fall through to the credential-free fresh mint — the deliberate per-verb exception to fail-closed (R3), because fresh-self must stay possible and blocking it would break bootstrapping (review harness shape 8, pinned by N12). A resolution *conflict* that names an existing seated row the caller appears to be (reached via occupancy) does not fall through: it refuses, so poison can still never convert a fresh mint into a seat takeover |

Surfaces that **never** get the default:

- The raw seatcred API: `Stage`, `Authenticate`, `VerifySelectedBus`.
  Extensions, hooks, and wrappers that do not invoke a herder verb acquire
  nothing (§5.1).
- `herder repair reissue-credential` — interactive, audited token-loss
  recovery stays fully explicit.
- `herder credential sweep` — owner-run issuance gate, explicit.
- `herder credential path --guid GUID` — unchanged non-secret lookup; gains
  a sibling `--self` riding `SelfResolve` (§6) with identical refusals.
- Any env-derived path construction. No code path lets `HERDER_GUID` (or
  any env value) pick which credential file is opened.

### 4.1 Spawn default matrix (normative; review round 3 P2-5)

Flag-less spawn, **marker on**. "Miss" and "conflict" are the R3
definitions. An identity is structurally required only where something
must be *sent or routed as someone*: an hcom child's initial prompt is a
bus message needing a sender; `--notify` without `--notify-to` needs the
spawner as recipient. A **non-hcom (bash) child's initial prompt is
keystroke boot-paste** — the spawn-private ruled exception to bus-only
transport — and carries no sender identity today; it must keep working
identity-free (no-fresh-self-regression), which the generic
"prompt-bearing miss refuses" rule of rev 3 would have broken.

| Child | Prompt | Notify | Resolve → | Miss → | Conflict → |
|---|---|---|---|---|---|
| hcom-capable | yes | any | seat is prompt sender (verified) | **refuse** (sender required; remedy: enroll or flag) | refuse |
| hcom-capable | no | `--notify` without `--notify-to` | seat is recipient | **refuse** (recipient required) | refuse |
| hcom-capable | no | `--notify-to TARGET` explicit | seat attribution; target validated as today | **`user`** + notify-to target (current post-cutover behavior, preserved) | refuse |
| hcom-capable | no | none | seat attribution | **`user`** | refuse |
| non-hcom (bash) | yes (boot-paste) | none / `--notify-to` explicit | seat attribution | **`user`**, prompt pasted (flow preserved) | refuse |
| non-hcom (bash) | any | `--notify` without `--notify-to` | seat is recipient | **refuse** (recipient required) | refuse |

**Marker off:** every cell reverts to today's legacy behavior byte-for-byte
(ambient `HERDER_GUID` attribution, legacy prompt-sender verification) —
SelfResolve is not consulted pre-cutover (R6), and D6's verification
addition is likewise inert (§9). Explicit `--credential-file` continues to
behave exactly as it does today in both marker states, except the
marker-on delta D6.

## 5. The three preserved properties, argued

### 5.1 Property 1 — implicit layers cannot act

- The raw API is untouched. `Authenticate("")` still returns
  `ErrCredentialRequired`; `Stage` still demands an explicit guid;
  `VerifySelectedBus` still only vetoes. A library consumer, hook, or shim
  linking seatcred gains no ambient acting power. Pinned by harness N13 and
  a contract grep confining `SelfResolve` call sites to the six fences plus
  `credential path --self`.
- A wrapper could always shell out to a verb, and today does so via the
  `--guid $HERDER_GUID` incantation. Self-resolution grants shell-out
  callers nothing they lack; it narrows what a poisoned-env shell-out can
  achieve (§3, §5.2).
- Within one uid the flag never provided access control — it provided
  *selection discipline*. The default preserves selection discipline in a
  stronger currency: act as a seat only by demonstrably occupying its pane
  (kernel + live herdr), or by naming its token explicitly.

### 5.2 Property 2 — fail-closed selection

- Selection is anchored in caller-bound live facts (§2.1 steps 1–2); env
  values are veto-only at every stage (step 6). The coherent-poison shapes
  from review P1-1 — victim tuple with guid, and with guid unset — cannot
  select: the anchor never proposes the victim's pane (harness N2).
- Cardinality is owned by SelfResolve over row instances at every stage
  (R7): occupied panes, seated candidates, joined roster rows,
  cross-namespace candidates. The duplicate same-name roster shape that
  slips through `Resolve`'s name-keyed map refuses (harness N3). This
  matters because not every verb re-counts downstream: send/spawn call
  `JoinedNamedCount` after selection, but cull/compact rely on
  `VerifySelectedBus` alone — so the guarantee must hold *before*
  authentication, uniformly.
- On any failure the verb refuses and names the road out (§8). There is no
  code path from "resolution failed" to "use ambient attribution"; the
  legacy path exists only behind an absent cutover marker, whose
  fail-closed handling (`CutoverEnabled`, `credential.go:36`) is unchanged.
  The single ratified fall-through (enroll → fresh mint) creates a new
  identity and can never re-seat an existing one (§4).
- One refusal class disappears on the default path by construction: a
  self-resolved presentation can never be generation-stale at resolution
  time (`CurrentPath` is registry-current); a flip before `Authenticate`
  still refuses via the unchanged fence (§8 row 8).

### 5.3 Property 3 — escape hatches (honest scope)

`--credential-file` always bypasses SelfResolve (R5). What the *explicit*
path can accomplish is bounded by each verb's unchanged post-selection
fences — see §6 for the per-verb truth table, which replaces the prior
draft's false "proceed exactly as today from anywhere" claim (review P1-3).
The three ratified escape-hatch classes, restated against that table:

- **(a) Broken-correlate recovery (fork-mismatch class).** Live resolution
  refuses (rows 2–5 of §8); the refusal names the flag; the operator
  fetches the path with `herder credential path --guid GUID`. Completing
  the act may additionally require scrubbing stale `HCOM_*`/`HERDR_*`
  correlates so the unchanged `VerifySelectedBus` does not veto — §6 states
  exactly when. This is today's behavior, now documented instead of
  implied.
- **(b) Deliberate act-as.** Explicit credential plus, on send/cull and
  non-hcom spawn attribution, a scrubbed-correlate environment; impossible
  by design on compact/enroll/prompt-spawn, whose own fences are self-only.
  Cross-pane recovery of a **dead** seated source via adopt is a real,
  ratified escape-hatch flow that review rounds 2–5 proved cannot soundly
  execute as a *seated*-source override (no one-shot CLI can construct
  epoch-proven death evidence). It is served instead by the **D5
  composition** (§6.1): the continuously-running appliers settle
  `positive_death` and unseat the row, after which plain unseated adopt —
  already the supported fresh-self recovery leg — completes from the
  operator's pane. No fence is scoped; recovery is availability-bounded
  by evidence settling, with the refusal DX naming the evidence state and
  the path to run once the seat clears.
- **(c) Harness / isolated-registry use.** Harnesses pass explicit paths
  and never engage resolution; flag-less runs inside an isolated
  `HERDER_STATE_DIR` can resolve only seats of the isolated registry
  (candidate set is registry-derived, §2.1 step 5) and refuse when there
  are none — hostile live global state cannot leak in (harness N11).
- **Scripting helper:** `herder credential path --self` prints the path the
  default would use, riding `SelfResolve` (same refusals, same vetoes).
  Read-only print; no authentication; no audit entry.

## 6. Explicit-override truth table (per verb, post-cutover)

"Scrubbed" means no `HCOM_SESSION_ID`, `HCOM_PROCESS_ID`, and no
`HERDR_PANE_ID` beyond what the verb's own preconditions force; the
empty-evidence pass in `VerifySelectedBus` (`credential.go:346`) then
applies. That pass is pre-existing ratified behavior; this design keeps it
**unchanged** and records here that it is load-bearing for recovery — a
future decision to close it must revisit this table. Conflicting *live*
correlates always veto (that is the ratified "verify or refuse" half of the
boundary working as intended, not a defect).

| Verb | Outer preconditions | Explicit act-as / recovery from outside the seat's pane | Cite |
|---|---|---|---|
| `send` | `HERDR_ENV=1` only | **Works when scrubbed** (evidence empty → verify pass); refused when live correlates resolve a different row | `send.go:46`, `send.go:222-228` |
| `cull` | `HERDR_ENV=1` only | **Works when scrubbed**; refused under conflicting live correlates | `cull.go:44`, `cull.go:84` |
| `adopt` (seated source) | none beyond registry | **Not executable from outside the source pane — in any environment, by design** (rounds 2–5): with the caller's real pane, `VerifySelectedBus` vetoes the source selection (`adopt.go:89-98`) or `preflightRecordedSessionClaim` rejects the caller pane (`adopt.go:462-485`); scrubbed, the mandatory replacement-enroll leg refuses on missing `HERDR_PANE_ID` (`adopt.go:122-131`, `enroll.go:54-56`). Cross-pane dead-source recovery is the **D5 composition** (§6.1): applier-settled unseat, then plain *unseated* adopt (fresh-enroll leg, no source credential). A *live* source remains adoptable only from its own pane |
| `spawn`, hcom-capable child | `HERDR_ENV=1` (`spawn.go:263-265`) + live pane demanded by the sender fence | **Self-only**: `verifyPromptSender` runs for *every* hcom-capable child whenever a credential is presented — even promptless, notify-less (`spawn.go:955-958`); a foreign pane's evidence fails `Resolve` and vetoes | `spawn.go:2284-2321` |
| `spawn`, non-hcom child (`--agent bash`, …) | `HERDR_ENV=1` | **Attribution-level act-as, today unverified**: an explicit credential sets `spawned_by` (and feeds `--notify` spawner resolution) with *no* `VerifySelectedBus` call at all (`spawn.go:937-950`; `launchcmd` hcom-capability gate). That is an inconsistency with the verify-or-refuse doctrine every other explicit selection obeys — **D6** (§9) aligns it: the same verification runs, so scrubbed works, conflicting live correlates refuse | `spawn.go:937-950` |
| `compact` | live herdr pane; credential row's terminal must equal the caller's live terminal | **Self-only by design** (self-pane-only doctrine) | `compact.go:128-131` |
| `enroll` (existing seat) | live herdr pane; credential seat's terminal must equal the caller's live terminal | **Self-only by design** | `enroll.go:100` |

Post-selection fences change in exactly one named place — D6 (non-hcom
spawn verification, §9, riding the owner sign-off). D5 changes no fence
(§6.1); everything else in the table is documentation of unchanged cutover
behavior.

### 6.1 D5 — adopt dead-source cross-pane recovery (composition, no fence change)

Rounds 2–4 tried to make a *seated*-source cross-pane adopt execute by
scoping the caller-evidence veto behind a deadness gate. Round 5 killed the
last version on an implementation truth: an epoch-proven `positive_death`
Input cannot be constructed by a one-shot adopt CLI — the epoch/continuity
evidence lives only in the continuously-running observers. Rather than
grow a new epoch surface for adopt, **the seated-source waiver is deleted
entirely.** No `VerifySelectedBus` scoping, no adopt-side deadness logic,
no new evidence machinery.

Cross-pane dead-source recovery is instead the **composition of already-
shipped pieces**:

1. The continuously-running shared appliers — the autostarted observer and
   the sidecar holder-exit path — evaluate the shared liveness predicate
   with the continuity only they possess, and on a settled
   `positive_death` verdict **unseat the row** (the existing
   `ApplyPositiveDeath` path, `liveness/apply.go:44`).
2. Adopting an **unseated** target from a fresh pane is *already* the
   supported recovery leg of the merged credential design: it rides the
   fresh-enroll path, needs no source credential, and transfers the label
   (`docs/credentials.md`: "Adopting an already-unseated label uses that
   same fresh-enroll leg").

So the operator story for a dead source seen from another pane is: wait
for evidence to settle (or check `herder observer status`), and when the
applier has cleared the seat, run the plain adopt — which then succeeds
end-to-end through real, existing paths. Recovery is availability-bounded
by evidence settling, which is the honest bound rounds 4–5 established:
nothing the one-shot CLI could prove would be sound sooner.

What D5 now *is* — documentation plus refusal DX (marker-on refusal path;
marker-off keeps today's legacy messages byte-for-byte):

- **Seated-source adopt from another pane refuses with the evidence
  state.** The refusal distinguishes `alive` (names the live evidence:
  the source is running — adopt from its pane or cull it deliberately)
  from `observation_gap` (names the unsettled cause — e.g.
  `epoch_uncertain` after a herdr restart — points at
  `herder observer status` / evidence settling, and says recovery
  becomes available once the seat clears). Both name the unseated-adopt
  path as *what to run next* once the applier unseats.
- **`--confirm-dead` gains no new semantics.** It keeps exactly the
  meaning the existing seated/dead paths give it today; it is not a risk
  authority and cannot substitute for the applier's verdict. (The
  gap+confirm-dead operator-risk mode remains not proposed, as ruled in
  round 4.)
- **The same-pane seated-adopt flow is untouched**, including its source
  credential requirement and credential forwarding.

**Named residual for the owner (not solved here):** positive death settles
only via `holder_exited`, `pane_gone_same_epoch`, or (process seats)
`dead_pid_stale_bus_row` (`liveness.go:179-190`). A pane-kind seat
orphaned by a **permanent epoch change** — herdr restarted, no sidecar
holder observation surviving — can sit in `observation_gap`
(`epoch_uncertain`) indefinitely, leaving cross-pane recovery through
this composition permanently unavailable for that row; the way out today
is the existing manual repair/reconcile/retire surface. This is a
liveness/observer-domain gap, named rather than papered over with adopt
machinery; if the owner wants it closed, that is observer work
(post-restart death evidence), not adopt work.

## 7. Operator-shell story (corrected)

The prior draft said every verb works bare from any enrolled pane. Review
P2-4 showed the bus-less branch composed with nothing: send requires a
recorded bus name (`send.go:214-217`), cull's verify needs a roster match
for nonempty pane evidence, prompt-spawn needs a joined sender, compact
refuses bash outright (`compact.go:143-147`). The honest story:

- **Bus-bound enrolled operator seat** (the normal owner session — a
  herdr pane, hcom-joined, enrolled): every credential-authenticated verb
  works bare. `herder send @nova 'msg'`, `herder cull --guid G`,
  `herder spawn --role r --agent claude --prompt '…'`. This is the
  low-ceremony surface, and it is the *already-supported* configuration.
- **Bus-less bash seat:** identity-bearing verbs refuse for it today
  post-cutover (the fences above), and this design does not change that —
  SelfResolve deliberately does not resolve bus-less rows (§2.1 step 4), so
  the default neither helps nor further restricts them; nothing regresses.
  Its supported actions remain: every `spawn` form that requires neither
  an hcom prompt sender nor an implicit notify recipient (the §4.1
  structural rule — promptless/notify-less, explicit `--notify-to`, and
  bash boot-paste-prompt spawns, all credential-free as
  `spawned_by: "user"`), read-only verbs, and joining the bus + fresh
  `herder enroll` to become a first-class seat — which is the remedy the
  refusal texts name.
- **Unenrolled shell inside a herdr pane:** first identity-bearing verb
  refuses (§8 row 2) naming `herder enroll` — one-time, credential-free.
  After that (bus-joined), zero ceremony.
- **Outside herdr entirely:** unchanged — and stricter than the previous
  draft claimed: `send`/`compact`/`cull` gate on `HERDR_ENV=1`, and
  `spawn` **also refuses unconditionally** when `HERDR_ENV != 1`
  (`spawn.go:263-265`); there is no outside-herdr spawn of any kind. The
  credential-free promptless `user` spawn is an *inside-herdr,
  non-seat-pane* affordance (§4 spawn row), not an outside-herdr one.
- **Doc surface delta:** `tools/herder/docs/credentials.md`, verb help
  texts, and the hcom session boilerplate (maintained outside this repo)
  stop teaching the `--guid $HERDER_GUID` incantation and teach bare verbs,
  with the flag documented as override/recovery. Retiring the env-keyed
  incantation from guidance is itself a security improvement (§1).

## 8. Refusal matrix

Post-cutover, no `--credential-file`:

| # | Condition | Outcome | Refusal must name |
|---|---|---|---|
| 1 | Occupancy anchor + seat mapping + bus corroboration yield exactly one seat; hints absent or agreeing | **Act** as that seat (audit `presentation: "canonical"` — §9, withdrawn audit delta) | — |
| 2 | No occupied pane: caller outside herdr, herdr/`process_info` unavailable, pane dead, or caller detached from the pane's process tree. **Bus-only proof never substitutes** (live session/process env matching a joined row does not create an anchor) | Refuse | why no anchor; `herder enroll` for an unenrolled session; `--credential-file PATH` as override |
| 3 | Anchor ok, but zero seated rows match the occupied pane, or the matched row is bus-less | Refuse | enroll/bus-join remedy; `--credential-file` override |
| 4 | Ambiguity at any stage: >1 occupied panes (nested seats), >1 seated rows on the coordinates (reused pane/terminal), >1 joined roster row instances for the name (duplicate same-name rows), >1 cross-namespace candidates | Refuse | candidate list (`formatCandidates` shape); `--credential-file` override |
| 5 | A set env hint disagrees with the resolved seat (guid mismatch, guid naming no row, pane/session/process naming other coordinates) | Refuse | the disagreeing hint; scrub stale `HCOM_*`/`HERDER_*`/`HERDR_*` values; `--credential-file` override. Poison can force a refusal (fail closed), never a selection |
| 6 | Resolved seat is legacy (no `credential_generation`) | Refuse | **`herder credential sweep` first** (or a completion-bearing recovery verb); flag second — pre-issuance there is no file to pass (refusal-text pass below) |
| 7 | Resolved seat's generation names a missing/unreadable token file | Refuse | `herder repair reissue-credential --guid GUID` (`credential.go:295`) |
| 8 | Generation flip between `CurrentPath` and `Authenticate` (rotation race) | Refuse via existing `ErrStaleCredential`; no retry, no side effect | rerun the verb (the next resolution picks up the new generation) |
| 9 | Registry missing, unreadable, or corrupt | Refuse | **restore/repair the registry** — the explicit flag is *not* the named remedy here, because `Authenticate` must load the same registry (`credential.go:277-279`) and would fail identically (review harness shape 5) |
| 10 | Resolution succeeded but a post-selection fence fails (`VerifySelectedBus` race, compact terminal check, …) | Refuse | existing per-fence texts, unchanged |

With `--credential-file PATH`: rows 1–8 are bypassed (no resolution runs)
and today's matrix applies verbatim; §6 governs completion. Row 9 applies
to both modes.

**Never in any row:** silent fallback to ambient attribution, a synthetic
sender, or acting as an env-named identity.

### Refusal-text pass over the cutover refusals (in scope per task)

- `seatcred.ErrCredentialRequired` (`credential.go:101`) leads with the
  flag and names the sweep last; pre-sweep there is no token file anywhere,
  so flag-first is a dead-end remedy. Reordered: when the cause is an
  unissued (legacy) seat, name `herder credential sweep` first, then the
  flag. `CurrentPath`'s legacy-seat message (`credential.go:147`) is the
  template. Row 6 inherits this.
- Every resolution refusal ends with the same escape-hatch line: *"or pass
  `--credential-file PATH` explicitly (`herder credential path --guid GUID`
  prints the non-secret path)"* — except row 9, whose only true remedy is
  the registry.
- Verb help texts change `--credential-file PATH` from "(required)" to
  "explicit caller override; default is self-resolution from live
  occupancy, which refuses rather than guesses".

## 9. Deltas from the ratified cutover design

All deltas in one list; they ride the owner sign-off (AC#3). Nothing else
in this note changes ratified behavior.

- **D1 (the amendment).** Post-cutover, flag-less invocation of a
  credential-authenticated verb changes from `ErrCredentialRequired` to
  live self-resolution per §2.1, with the per-verb semantics of §4 —
  including both miss-only fall-throughs (enroll → fresh mint; spawn →
  `spawned_by: "user"` whenever neither an hcom prompt sender nor an
  implicit notify recipient is required, per the §4.1 matrix), with
  conflict/ambiguity refusing everywhere (R3).
- **D2 (refusal texts).** The `ErrCredentialRequired` family is replaced on
  the default path by the §8 refusals; legacy-seat wording reordered
  sweep-first; help texts updated.
- **D3 (occupancy-anchor proof form + herdr surface dependency).**
  Selection is anchored on kernel process ancestry intersected with live
  herdr pane process inventory — a new caller-bound proof form composed
  from the existing pane and process evidence classes (no new class;
  linux-only like existing pid probes). Process identity is positively
  established via a named **herdr surface extension**: `pane process_info`
  additionally reports per-process PID-namespace inode and start time,
  compared against the caller's own `/proc` view (§2.1 step 2). Argv is
  not proof (round 3 P1-3). The default hard-refuses when the surface or
  fields are absent; the implementation task carries the herdr change as
  an explicit dependency. The added response fields are
  **deployment-persistent** (round 4 P2-3): they are additive server API
  carrying observation data, not authority, and remain present regardless
  of the credential marker — marker rollback disables every consumer of
  them, not the fields. This inverts the role of `HERDR_PANE_ID` for the
  default path from entry point to veto-only hint, and the start-time
  comparison closes the pid-reuse residual.
- **D4 (`credential path --self`).** New read-only helper riding
  `SelfResolve`. Marker-gated like every authority-adjacent behavior
  (round 4 P2-3): marker-off it refuses with the legacy guidance rather
  than resolving — it is a lookup, but gating it keeps the R6 rollback
  wording exact; pinned by harness N17.
- **D5 (adopt dead-source recovery = applier composition + refusal DX;
  no fence change).** Round 5 deleted the seated-source waiver entirely:
  no one-shot adopt CLI can construct an epoch-proven `positive_death`
  input — that continuity lives only in the running observers. Cross-pane
  dead-source recovery is the composition of shipped pieces (§6.1): the
  autostarted observer / sidecar appliers settle `positive_death` and
  unseat the row; plain **unseated** adopt (the already-supported
  fresh-enroll recovery leg, no source credential) then completes from
  the operator's pane. What D5 adds is only documentation plus the
  marker-on refusal DX: seated-source cross-pane adopt refuses naming
  the evidence state (`alive` vs `observation_gap` + cause), pointing at
  `herder observer status` / evidence settling, and naming unseated
  adopt as the next step once the seat clears. `--confirm-dead` gains no
  new semantics. One named residual rides to the owner: a pane seat
  orphaned by a permanent epoch change may never settle
  (`liveness.go:179-190`) — an observer-domain gap, not adopt work
  (§6.1).
- **D6 (non-hcom spawn attribution verification).** Explicit-credential
  spawns of non-hcom children (`--agent bash`, …) currently perform **no**
  ambient verification of the selected caller (`spawn.go:937-950`) — the
  only explicit selection in the verb set that skips the verify-or-refuse
  doctrine. D6 runs the same `VerifySelectedBus` call there, **marker-on
  only** (round 3 P1-4): scrubbed environments still work (empty-evidence
  pass), conflicting live correlates now refuse; marker-off behavior is
  byte-identical to today. This tightens an existing hole; nothing that
  works legitimately today stops working except marker-on act-as *with*
  contradicting live evidence, which every other verb already refuses.
- **Explicitly NOT changed (previous draft's D3 withdrawn):** the credential
  audit. `Authenticate` derives `presentation` solely by comparing the
  presented path to the canonical path (`credential.go:300-302`), so a
  self-resolved canonical presentation records `canonical`. Distinguishing
  default-vs-explicit usage in the audit would require a new seatcred entry
  point or audit parameter — an API delta this note deliberately does not
  smuggle (review P2-5). If the owner wants that observability, it is a
  separate, explicitly-reviewed change. Consequence: v1 ships without an
  audit-level adoption metric.
- **Explicitly NOT changed:** the seatcred API surface; post-selection
  fences other than the one named delta D6 (the rest of §6, D5 included,
  is documentation, not change), including the empty-evidence
  `VerifySelectedBus` pass now recorded as load-bearing for recovery; token
  file discipline; rotation and its commit point; the cutover marker and
  its fail-closed handling; the sweep's literal-100% gate; fresh-self
  flows; the per-verb code-level rollback order and the marker-deletion
  emergency lever (`docs/credentials.md` §Transaction and rollback).

## 10. Poisoned-env & fail-closed harness deltas

Existing poisoned-env cases (adopt_test.go, compact_test.go,
sender_identity_test.go, check-spawn-contract.sh §poison,
check-enroll-contract.sh §poison, graceful_test.go) stay green unchanged.
The new suite mocks `herdr` (`agent list`, `pane get`, `pane process_info`)
and `hcom list --json`, and fabricates `/proc`-ancestry via an injected
ancestry provider (R1's dependency injection exists for exactly this). All
cases are post-cutover and flag-less unless noted. Reviewer shapes (1)-(9)
from round 1 are mapped inline.

- **N1 — hint cannot steer to a real other seat.** Anchor proves seat A;
  env carries seat B's guid (and separately: B's pane id; B's session id).
  Exit 2, refusal names the disagreeing hint, no side effect, and no
  `credential_authenticated` audit entry for B — audit-entry absence is the
  steering proof for every N-case below.
- **N2 — coherent all-live victim tuple cannot steer** *(shape 2)*. Seats A
  and B both live and joined; caller's ancestry sits in A's pane; env is
  B's complete tuple (`HERDR_PANE_ID`+`HCOM_SESSION_ID`+`HCOM_PROCESS_ID`),
  run twice: with `HERDER_GUID=B` and with guid unset. Both refuse (hints
  veto against the A anchor); no audit entry for B; nothing acted. Third
  leg, pinning the chosen identity boundary (rounds 2–3): pane B's
  inventory carries a pid numerically equal to a caller ancestor but the
  identity triple fails (ns-inode or start-time mismatch, or the D3
  surface fields absent) — identity is not established, hard-refuse; B
  never enters the candidate set on numeric intersection or argv
  similarity.
- **N3 — duplicate same-name joined rows refuse** *(shape 3)*. Two joined
  roster row instances named `@x`; one seated row maps to `@x`; anchor
  proves the pane. SelfResolve's instance count refuses (the shape
  `Resolve`'s name-keyed map would have collapsed, `identity.go:181-186`).
- **N4 — nested/reused coordinates refuse per verb** *(shape 4)*. (a) Two
  seated rows recording the same pane/terminal; (b) two occupied panes in
  one ancestry (hand-run agent inside a seated shell's pane). Concrete
  per-verb invocations (all six), each asserting the candidate-list refusal
  and no side effect — not a generic resolver-only test.
- **N5 — poison alone cannot act (anti-incantation case).** Env fully
  poisoned with a real seat's tuple but no live roster/pane state backs it
  (roster empty / rows not joined / pane dead). Refusal at resolution, not
  authentication: assert no audit entry exists at all. Distinguishes the
  default from the old incantation, which authenticates under
  empty-evidence verify (`credential.go:346`).
- **N6 — dead pane / failed `process_info` hard-refuses** *(shape 1)*.
  Env session+process match a joined row, but `pane get`/`process_info`
  fails or the pane is gone: refuse row 2; bus-only proof must not select.
- **N7 — poisoned `HCOM_DIR` is inert.** Hostile roster dir named by env
  `HCOM_DIR` (fake joined rows matching the caller); recorded
  `seat.namespace` differs. Resolution consults only the recorded
  namespace; the hostile roster is never listed (assert via mock-hcom call
  log); selection unaffected.
- **N8 — multi-namespace candidates refuse** *(P2-6)*. Two seated rows in
  different recorded namespaces both plausibly matching the anchor: refuse
  cross-namespace cardinality; same-name-different-bus included.
- **N9 — registry missing/unreadable/corrupt** *(shape 5)*. Refusal names
  registry restore/repair, not the flag; assert the flag is genuinely
  absent from the remedy text.
- **N10 — rotation flip race** *(shape 6)*. Mock flips the generation
  between resolution and `Authenticate`: `ErrStaleCredential` refusal, no
  action, no retry loop, audit contains no entry for the stale
  presentation.
- **N11 — isolated `HERDER_STATE_DIR` with hostile globals** *(shape 7)*.
  Real/global live state hosts an attractive seat; isolated registry has
  (a) no seats — flag-less refuses without touching global state;
  (b) one seat whose recorded coordinates don't match the caller — refuses.
  Explicit-flag invocation asserts SelfResolve was never entered (call-log
  assertion). Resolution cannot escape the isolated registry.
- **N12 — fresh enroll unblocked, unsteerable** *(shape 8)*. Flag-less
  `enroll` with no seated match and poisoned parent env: falls through to
  fresh mint, new guid, poison values absent from the row (extends the
  existing check-enroll-contract poison case to the SelfResolve path).
- **N13 — raw API regression pin.** `seatcred.Authenticate(path, "")`
  returns `ErrCredentialRequired` (unit test); contract grep pins
  `SelfResolve` references to the six fences + `credential path --self`.
- **N14 — per-verb happy path + side-effect absence on refusal**
  *(shape 9)*. For each of the six verbs: (a) clean state, bare invocation
  acts as the resolved seat, audit entry `presentation: "canonical"` with
  the correct guid; (b) under N1/N2 poison, the verb-specific side effect
  is asserted absent — nothing sent, nothing launched, nothing typed,
  nothing unseated, no label transferred, no row enrolled.
- **N15 — real `/proc` ancestry + identity adapter** *(rounds 2–3)*. The
  injected provider pins resolver logic; this pins the production adapter
  that makes the proof caller-bound, against real `/proc`: includes self
  and parent; terminates bounded (depth cap, stop at pid 1, cycle-safe);
  fail-closed on unreadable/garbled `stat`/`ns/pid` (refusal, not partial
  chain). Identity policy legs: (a) **equal numeric pid + identical argv,
  different process** — mocked `process_info` entry whose pid matches a
  real ancestor and whose argv equals the real cmdline but whose ns-inode
  or start time differs: **refuses** (the round 3 counterexample; the easy
  perturbed-argv negative is not sufficient and argv is not consulted as
  proof); (b) surface fields absent (old herdr): hard-refuse; (c)
  integration: mocked entry carrying the real ancestor's true
  (ns-inode, pid, starttime) triple resolves.
- **N16 — spawn matrix pins (§4.1), never env-attributed.** Flag-less
  spawn from a non-seat pane with fully poisoned parent env, one case per
  §4.1 row, marker on: hcom child + prompt → refuse; hcom child +
  `--notify` without `--notify-to` → refuse; hcom child + explicit
  `--notify-to` → spawns as `user` with the notify target honored;
  hcom child promptless/notify-less → `user`; **non-hcom bash child +
  prompt → spawns as `user` with the prompt boot-pasted** (the preserved
  flow — this leg fails if the generic prompt-miss refusal leaks into the
  bash path); bash + `--notify` without `--notify-to` → refuse. In every
  `user` leg the poison guid is asserted absent from the row (extends the
  existing check-spawn-contract poison case to the SelfResolve path). The
  same spawns under a resolution *conflict* (N2-style) refuse entirely
  rather than downgrading to `user`. Marker-off legs (round 3 P1-4): each
  case behaves byte-for-byte as legacy — including legacy `HERDER_GUID`
  parent attribution, which remains *intentionally* legacy pre-cutover.
- **N17 — `credential path --self` marker gating** *(round 4 P2-3)*.
  Marker on, clean state: prints the resolved seat's canonical path.
  Marker off: refuses with the legacy guidance (`--guid` lookup) without
  invoking resolution — pinning that every authority-adjacent behavior in
  this design, lookup included, is inert pre-cutover.

End-to-end explicit-override cases (round 2 P2: resolver-only assertions
cannot expose composition defects — the adopt P1 and both spawn findings
would have been caught here):

- **E1 `send` / E2 `cull`:** explicit flag, scrubbed correlates → acts;
  explicit flag with conflicting live correlates → refuses. 
- **E3 `adopt` (D5 composition; real paths, no fabricated `Evaluate`
  inputs — round 5).** (a) **Live source**, cross-pane seated adopt →
  refuses; the refusal names the live evidence and the source's own pane
  as the supported adopt site. (b) **Observation gap** (herdr restarted:
  epoch not provably unchanged; or roster transiently unavailable) →
  refuses; the refusal names the gap cause, points at
  `herder observer status` / evidence settling, and names unseated adopt
  as the next step — no label transfer, no waiver, in any gap shape.
  (c) **End-to-end recovery through the real composition:** source dies
  where the appliers can observe it (holder exit / pane gone within the
  running epoch); the autostarted applier settles `positive_death` and
  **unseats the row** via the real `ApplyPositiveDeath` path; plain adopt
  of the now-**unseated** target from operator pane O then completes on
  the fresh-enroll leg — label transferred, fresh guid and first
  credential minted for O, **no source credential involved anywhere**.
  (d) Marker-off leg: seated-source cross-pane adopt refuses with today's
  legacy message byte-for-byte (the D5 refusal DX is marker-on only).
- **E4 `spawn` hcom-capable:** explicit credential from a foreign live
  pane → refuse (self-only, even promptless — `spawn.go:955-958`); from
  the credential seat's own pane → acts.
- **E5 `spawn` non-hcom (D6):** marker on: explicit credential, scrubbed →
  spawns with credential attribution; explicit credential with conflicting
  live correlates → refuses (this leg *is* the D6 behavior change and pins
  it). Marker-off leg: the conflicting-correlates invocation proceeds
  unverified exactly as today — D6 is inert pre-cutover (round 3 P1-4).
- **E6 `compact` / `enroll`:** explicit credential from a foreign
  terminal → refuse (self-only fences, `compact.go:128-131`,
  `enroll.go:100`).

## 11. Rollback

Unchanged, restated: reverting self-resolution is a per-verb local change
(the fence returns to `ErrCredentialRequired` on empty path) strictly
smaller than the cutover's own per-verb rollback; deleting the cutover
marker remains the larger lever and behaves exactly as documented today —
and because D1, D4, and D6 are marker-gated (R6, §9; D5 changes no fence),
marker deletion rolls back **every authority-changing behavior** this
design adds,
explicit-flag paths included. The sole survivor is the D3 additive herdr
response fields — deployment-persistent observation data with no
authority, disabled-by-having-no-consumer rather than removed. No token
file, registry row, or generation is written differently under this
design, so rolling either direction requires no state migration.

## 12. Review finding disposition

### Round 1

| Finding | Disposition |
|---|---|
| P1-1 coherent all-live victim tuple | Fixed by design: occupancy anchor (§2.1 steps 1–2, §3); env demoted to veto-only including `HERDR_PANE_ID`; claim restated exactly with residuals (§3); harness N2/N6 |
| P1-2 Resolve cardinality not exact-one | Fixed: R7 — SelfResolve owns instance-counted cardinality at every stage; `Resolve` semantics not inherited (cite `identity.go:173-186`); harness N3/N4 |
| P1-3 override overstated | Fixed by narrowing: R5 + §6 per-verb truth table; scrubbed-env requirement documented; empty-evidence pass named as retained load-bearing behavior; no post-fence change (so no new delta) |
| P2-4 bus-less branch non-composable | Fixed: branch dropped (§2.1 step 4); operator story rewritten honestly (§7); no regression for bus-less seats |
| P2-5 D3/H7 audit contradiction | Fixed: audit delta withdrawn; self-resolved presentations audit `canonical`; observability gap named as a consequence (§9) |
| P2-6 namespace enumeration undefined | Fixed: registry-recorded namespaces only, cross-namespace cardinality (§2.1 steps 4–5); ambient `HCOM_DIR` never consulted; harness N7/N8 |
| P2-7 nine missing harness shapes | All folded: shapes 1→N6, 2→N2, 3→N3, 4→N4, 5→N9, 6→N10, 7→N11, 8→N12, 9→N14; none argued away |

### Round 2

| Finding | Disposition |
|---|---|
| P1 adopt escape hatch cannot execute end-to-end | Fixed by named composition delta **D5** (§6.1, §9): explicit source credential + `--confirm-dead` + proven deadness scopes out the caller-evidence veto on the source selection; enroll leg runs on the caller's real pane, no scrubbing; live/inconclusive source keeps the veto. Chosen over "unsupported" because that would silently shrink the ratified escape-hatch intent. End-to-end harness E3 |
| P2 spawn second fall-through contradicts R3 | Fixed: R3 now names exactly two miss-only fall-throughs; §4 spawn row specifies resolve/miss/conflict branches normatively (miss+prompt refuses, miss+promptless → `user`, conflict never downgrades); pinned by N16 |
| P2 spawn truth-table row incomplete / bash act-as unclassified | Fixed: table split into hcom-capable (self-only, even promptless — `spawn.go:955-958`) and non-hcom rows; the unverified bash attribution act-as is classified honestly and aligned by named delta **D6** (verification runs; scrubbed works, conflict refuses); §7 outside-herdr claim corrected (`spawn.go:263-265` refuses unconditionally) |
| P2 PID-namespace precondition | Fixed: argv-corroboration gate in §2.1 step 2 — numeric intersection alone never establishes agreement, bare `shell_pid` match hard-refuses; deployment precondition + narrowed claim + revised pid-reuse residual in §3; start-time corroboration named deferred hardening; N2 third leg pins the boundary |
| P2 harness gaps (truth table, real adapter) | Fixed: E1–E6 end-to-end explicit-override cases per verb (scrubbed / conflicting / self-only, incl. D5 and D6 legs); N15 real-`/proc` adapter test (self/parent inclusion, bounded termination, fail-closed parse, argv policy, integration leg) |

### Round 3

| Finding | Disposition |
|---|---|
| P1-1 D5 enroll leg cannot execute (forwarded credential hits enroll's terminal check) | Fixed inside D5 (§6.1, §9): the source credential is recovery authority for the waiver only, never the enrollment identity — it is *not forwarded* to `RunFreshForAdoption`; pane O enrolls as a plain fresh self. E3 runs the real enroll path and asserts the source credential is authenticated exactly once and never presented by the enroll leg. Same-pane adopt keeps forwarding unchanged |
| P1-2 deadness gate can displace a live moved source; missing correlates treated as negative | Fixed (§6.1): deadness requires positive absence across **every** recorded live coordinate, led by the move-stable recorded terminal (subsuming re-keyed panes, `enroll.go:662-676`); any missing/unreadable/ambiguous correlate is **inconclusive → veto stays**. E3 counter-legs: re-key-on-preserved-terminal, empty bus name, empty SID, ambiguity, probe unavailability |
| P1-3 argv equality cannot discharge cross-namespace collision | Fixed by taking guidance option (i) (§2.1 step 2, §3, D3): named herdr surface extension — `process_info` reports per-process PID-namespace inode + start time; caller compares the (ns-inode, pid, starttime) triple against its own `/proc` view; hard-refuse when the surface/fields are absent. Argv dropped as proof entirely. Bonus: start-time comparison closes the prior pid-reuse residual. N15 gains the equal-pid identical-argv foreign-process leg; N2 third leg restated on the triple |
| P1-4 D5/D6 change marker-off behavior, breaking the rollback lever | Fixed: both deltas gated on cutover-enabled (§6.1, §9); R6 restated — every delta inert marker-off, marker deletion is a complete rollback including explicit-flag paths (§11). Marker-off legs added to E3, E5, N16. Marker-independence explicitly not proposed; it would be its own R6 revision for owner sign-off |
| P2-5 spawn miss semantics ambiguous; bash boot-paste prompt flow would regress | Fixed: §4.1 normative matrix (child capability × notify-to presence × marker state): explicit `--notify-to` miss → `user` with target honored (current behavior preserved); identity required only for hcom prompt sender and target-less notify; **bash boot-paste prompt spawns as `user` and is named preserved**; marker-off rows byte-identical legacy. N16 expanded to one pin per matrix row plus conflict and both marker states |

### Round 4

| Finding | Disposition |
|---|---|
| P1-1 coordinate-absence deadness treats stale-as-a-set records as death (restart/handoff counterexample) | Fixed per ruling (§6.1, D5): the private coordinate gate is deleted; D5 defines no deadness logic and **consumes the shared liveness predicate** — waiver fires only on `liveness.Evaluate` → `positive_death` (epoch-checked: `pane_gone_same_epoch` et al.); `observation_gap` (epoch unknown/changed, transient bus absence, restart windows, unavailable observations) keeps the veto — recovery honestly unavailable until evidence settles. `--confirm-dead` stays **required** as operator intent attestation on top of the verdict (verdict-without-flag refuses) and never overrides gaps; gap+confirm-dead risk recovery explicitly not proposed (separate owner question if ever needed). E3 re-pinned against the predicate composition with restart/handoff and replacement-terminal counter-legs |
| P2-2 three stale summaries contradict the §4.1 matrix | Fixed: R3, D1, and the §7 bus-less operator bullet now state the structural rule — miss proceeds as `user` whenever neither an hcom prompt sender nor an implicit notify recipient is required (explicit `--notify-to` and bash boot-paste-prompt misses included) |
| P2-3 rollback wording broader than the gating contract (D4 ungated, D3 fields marker-independent) | Fixed: R6/§11 narrowed to every **authority-changing** behavior; D4 `--self` marker-gated anyway for wording coherence (refuses with legacy guidance marker-off, pinned by new N17); D3's additive `process_info` fields named deployment-persistent — observation data, no authority, disabled by having no consumer rather than removed |

### Round 5

| Finding | Disposition |
|---|---|
| P1 no production observation path lets a one-shot adopt CLI construct an epoch-proven `positive_death` input (that continuity lives only in the running observers) | Fixed per ruling by **deleting the seated-source waiver entirely** (§6.1, D5 rewritten): no `VerifySelectedBus` scoping, no adopt-side deadness logic, no new epoch surface. Cross-pane dead-source recovery = the composition of shipped pieces: autostarted observer / sidecar appliers settle `positive_death` and unseat the row (`ApplyPositiveDeath`, `liveness/apply.go:44`); plain **unseated** adopt — already the supported fresh-enroll recovery leg, no source credential — then completes from the operator's pane. D5 shrinks to documenting that composition plus marker-on refusal DX (seated-source cross-pane adopt refuses naming `alive` vs `observation_gap` + cause, pointing at `herder observer status`, naming unseated adopt as the next step); `--confirm-dead` gains no new semantics. E3 rewritten to real paths with no fabricated `Evaluate` inputs: live→refuse, gap→refuse-with-settle-guidance, applier-unseat→unseated-adopt succeeds end-to-end, marker-off legacy refusal. **Named residual for the owner:** a pane seat orphaned by a permanent epoch change can never settle (`liveness.go:179-190` — only `holder_exited` / `pane_gone_same_epoch` / process-seat `dead_pid_stale_bus_row` produce positive death), leaving such rows recoverable only via the manual repair/reconcile/retire surface; closing it is observer-domain work, not adopt machinery. Cascading spec updates: R5/R6/§4/§5.3(b)/§6 table/§9/§11 now state D6 as the sole fence delta and D1/D4/D6 as the marker-gated authority set |

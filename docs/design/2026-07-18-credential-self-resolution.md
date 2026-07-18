# Credential DX: verb-level self-resolution from live correlates

- **Task:** TASK-282 (design; adversarial design review before any implementation task is cut)
- **Date:** 2026-07-18 (rev 2, after adversarial review round 1 — reviewer-rofe, seven findings, all addressed below; finding-to-section map in §12)
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
  failure never falls back to pre-cutover ambient attribution. One verb has
  a ratified, explicitly-pinned exception: flag-less `enroll` falls through
  to the credential-free *fresh mint* on resolution miss (§4, enroll row) —
  falling through to minting a brand-new identity is not ambient
  attribution of an existing one.
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
  is governed by each verb's unchanged post-selection fences — the honest
  per-verb truth table is §6; this note no longer claims the override is
  universally sufficient from arbitrary environments.
- **R6 — pre-cutover behavior unchanged.** Before the cutover marker exists,
  verbs keep the current legacy ambient-verified path. Self-resolution
  replaces exactly one thing: the post-cutover no-flag
  `ErrCredentialRequired` refusal. The rollback story (§11) is untouched.
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
2. **Occupancy anchor.** Enumerate live panes (`herdr agent list` /
   session snapshot) and fetch `pane process_info` for each. A pane is
   *occupied by the caller* iff its `shell_pid` or a foreground process pid
   appears in the ancestry chain. Exactly one occupied pane is required;
   zero (herdr down, `process_info` unavailable, caller reparented to init
   by setsid/daemonization, caller outside any pane) or more than one
   (nested seats, §5.2 residuals) refuses. A dead or unresolvable pane can
   never be replaced by bus-only proof: no anchor, no default (review
   harness shape 1).
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

> **A poisoned environment cannot steer the default to any seat whose pane's
> live process tree does not contain the calling process.** Environment
> values can only cause refusals (veto), never selection. In the coherent
> all-live victim-tuple attack, the caller's ancestors are in pane A;
> pane B's inventory does not contain them; B is never a candidate no matter
> what the env claims (harness N2).

Explicit residuals — what this does **not** claim:

- **Same-uid explicit access is out of scope, as at cutover.** Any same-uid
  process can read any 0600 token and present it via the flag. The default,
  like the flag, provides selection discipline, not intra-uid access
  control.
- **PID-reuse race.** An ancestor pid could in principle be reused as a
  pane's `shell_pid` between herdr's observation and the check. The window
  is one CLI invocation; steps 3–6 (seat + roster + hint consistency) must
  *also* line up for a wrong selection. Accepted, named.
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
it is nonetheless named as a delta (§9 D3) and rides the owner sign-off. It
requires no new herdr surface — `pane process_info` exists and is already
parsed (`herdrcli.go:253`). Notably, normal pane seats record no `seat.pid`
(only headless `SeatProcess` seats do), which is why the anchor uses live
pane inventory rather than a recorded pid.

## 4. Per-verb default semantics

"Self" is the seat whose pane's live process tree contains the caller.

| Verb | What the credential selects today | Default (no flag, post-cutover) |
|---|---|---|
| `send` | caller/sender attribution (`send.go:64-72`) | SelfResolve the caller seat; sender name from the selected row exactly as `credentialCallerSender` does today |
| `spawn` | `spawned_by` attribution, initial-prompt sender, `--notify` recipient (`spawn.go:937-994`) | SelfResolve the spawner. Promptless, notify-less spawn from a non-seat stays credential-free with `spawned_by: "user"` (fresh-self preserved, unchanged) |
| `compact` | proof of the caller's own pane (`compact.go:122-142`) | SelfResolve; compact's extra credential-branch fences (terminal equality, bus verify) run unchanged |
| `cull` | caller identity for authority + release-notice sender (`cull.go:71-89`) | SelfResolve the caller seat |
| `adopt` (seated source) | **the source seat's** credential as custody proof (`adopt.go:80-100`) | Resolve the *source* seat by live occupancy: the caller's occupied pane (step 2) must be the seated source's recorded pane — the same occupancy `adoptionUnseatReason` demands, now proven by ancestry instead of env. Adopt is the one verb where "self" means "the seat whose pane I demonstrably occupy." A caller not occupying the source's pane — including `--confirm-dead` recovery from elsewhere — gets no default and must present the source credential explicitly (§6 for what that requires) |
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
- **(b) Deliberate act-as.** Same: explicit credential plus, on
  send/cull/adopt, a scrubbed-correlate environment; impossible by design
  on compact/enroll/prompt-spawn, whose own fences are self-only. Nothing
  here changes at cutover semantics; the table makes the limits explicit.
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
| `adopt` (seated source) | none beyond registry | **Works when scrubbed**, composed with `--confirm-dead` when not occupying the source pane; refused when live correlates resolve the operator's own row | `adopt.go:95`, `adopt.go:277-293` |
| `spawn` with `--prompt`/`--notify` | live herdr pane required by the sender fence | **Self-only**: the caller's live pane must verify the credential row; a foreign pane's evidence fails `Resolve` and vetoes | `spawn.go:2284-2321` |
| `compact` | live herdr pane; credential row's terminal must equal the caller's live terminal | **Self-only by design** (self-pane-only doctrine) | `compact.go:128-131` |
| `enroll` (existing seat) | live herdr pane; credential seat's terminal must equal the caller's live terminal | **Self-only by design** | `enroll.go:100` |

No post-selection fence changes anywhere in this design; the table is
documentation of unchanged cutover behavior, not a delta.

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
  Its supported actions remain: promptless/notify-less spawn (credential-
  free, `spawned_by: "user"`), read-only verbs, and joining the bus + fresh
  `herder enroll` to become a first-class seat — which is the remedy the
  refusal texts name.
- **Unenrolled shell inside a herdr pane:** first identity-bearing verb
  refuses (§8 row 2) naming `herder enroll` — one-time, credential-free.
  After that (bus-joined), zero ceremony.
- **Outside herdr entirely:** unchanged. `send`/`compact`/`cull` gate on
  `HERDR_ENV=1`; promptless spawn works attributed `user`; prompt-bearing
  spawn refuses (its sender fence requires a live pane, `spawn.go:2284`).
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
  live self-resolution per §2.1, with the per-verb semantics of §4
  (including enroll's resolution-miss fall-through to fresh mint).
- **D2 (refusal texts).** The `ErrCredentialRequired` family is replaced on
  the default path by the §8 refusals; legacy-seat wording reordered
  sweep-first; help texts updated.
- **D3 (occupancy-anchor proof form).** Selection is anchored on kernel
  process ancestry intersected with live herdr pane process inventory — a
  new caller-bound proof form composed from the existing pane and process
  evidence classes (no new class, no new herdr surface; linux-only like
  existing pid probes). This inverts the role of `HERDR_PANE_ID` for the
  default path from entry point to veto-only hint.
- **D4 (`credential path --self`).** New read-only helper riding
  `SelfResolve`.
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
  fences (§6 is documentation, not change), including the empty-evidence
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
  veto against the A anchor); no audit entry for B; nothing acted.
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

## 11. Rollback

Unchanged, restated: reverting self-resolution is a per-verb local change
(the fence returns to `ErrCredentialRequired` on empty path) strictly
smaller than the cutover's own per-verb rollback; deleting the cutover
marker remains the larger lever and behaves exactly as documented today. No
token file, registry row, or generation is written differently under this
design, so rolling either direction requires no state migration.

## 12. Review round 1 — finding disposition

| Finding | Disposition |
|---|---|
| P1-1 coherent all-live victim tuple | Fixed by design: occupancy anchor (§2.1 steps 1–2, §3); env demoted to veto-only including `HERDR_PANE_ID`; claim restated exactly with residuals (§3); harness N2/N6 |
| P1-2 Resolve cardinality not exact-one | Fixed: R7 — SelfResolve owns instance-counted cardinality at every stage; `Resolve` semantics not inherited (cite `identity.go:173-186`); harness N3/N4 |
| P1-3 override overstated | Fixed by narrowing: R5 + §6 per-verb truth table; scrubbed-env requirement documented; empty-evidence pass named as retained load-bearing behavior; no post-fence change (so no new delta) |
| P2-4 bus-less branch non-composable | Fixed: branch dropped (§2.1 step 4); operator story rewritten honestly (§7); no regression for bus-less seats |
| P2-5 D3/H7 audit contradiction | Fixed: audit delta withdrawn; self-resolved presentations audit `canonical`; observability gap named as a consequence (§9) |
| P2-6 namespace enumeration undefined | Fixed: registry-recorded namespaces only, cross-namespace cardinality (§2.1 steps 4–5); ambient `HCOM_DIR` never consulted; harness N7/N8 |
| P2-7 nine missing harness shapes | All folded: shapes 1→N6, 2→N2, 3→N3, 4→N4, 5→N9, 6→N10, 7→N11, 8→N12, 9→N14; none argued away |

# Credential DX: verb-level self-resolution from live correlates

- **Task:** TASK-282 (design; adversarial design review required before any implementation task is cut)
- **Date:** 2026-07-18
- **Status:** Draft for adversarial review
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
   path — and therefore the credential that gets presented — is chosen by
   `HERDER_GUID`, an inherited env value the credential system explicitly
   demoted to a hint. Worse, `seatcred.VerifySelectedBus` returns `nil` when
   *no* ambient correlate is present at all
   (`tools/herder/internal/seatcred/credential.go:346`), so in an environment
   with a poisoned `HERDER_GUID` and no `HCOM_*`/pane correlates, the
   incantation authenticates *as the poisoned seat* with no live check. The
   ceremony the flag was supposed to buy has become a mechanical env-derived
   lookup that is weaker than what this note proposes.
2. **Per-verb friction for humans and agents alike.** When every call site
   performs the same incantation, the incantation belongs in the callee.

The ratified direction is to move the default into the verb: a
credential-authenticated verb invoked without `--credential-file` resolves the
caller's own seat from **live correlates**, opens that seat's registry-current
canonical credential, and then runs the *unchanged* explicit authentication
pipeline. `--credential-file` becomes the explicit override; the raw seatcred
API stays explicit.

## 2. Design overview

The default is sugar for computing the path — never for skipping the fence.

```
                       no --credential-file            --credential-file PATH
                       ─────────────────────           ───────────────────────
 [NEW] SelfResolve:    live correlates → one seated
                       row → guid → CurrentPath()      (skipped entirely)
                                │                              │
                                ▼                              ▼
 [UNCHANGED] seatcred.Authenticate(registryPath, path)  ← same call, same checks
                                │
                                ▼
 [UNCHANGED] seatcred.VerifySelectedBus + per-verb post-selection fences
```

Normative rules:

- **R1 — verb-level only.** Self-resolution is implemented as one new helper
  (working name `seatcred.SelfResolve`, dependency-injected so seatcred stays
  exec-free: it takes the live hcom roster rows, the live `herdr pane get`
  facts, the registry projection, and the env *hints*, and returns a canonical
  credential path or a typed refusal). It is called only from the verb fences
  that today call `seatcred.ExtractFlag`. `Stage`, `Authenticate`,
  `VerifySelectedBus` keep their exact signatures and semantics;
  `Authenticate(registryPath, "")` still returns `ErrCredentialRequired`.
- **R2 — live evidence selects; env at most corroborates.** Resolution uses
  exactly the correlate classes the fences already use (no evidence-class
  widening): the live hcom roster (`hcomidentity.Resolve` semantics over
  joined rows, matched by session/process/pane correlates), the live
  `herdr pane get` result for the caller's pane, and seated registry rows.
  `HERDER_GUID` is never a lookup key and never derives a file path. If set,
  it must agree with the resolved seat or the verb refuses; it can veto,
  never steer.
- **R3 — fail-closed, no ambient fallback.** Any conflict, ambiguity, or
  absence of live proof refuses with the explicit-flag escape hatch named.
  Resolution failure never falls back to pre-cutover ambient attribution;
  post-cutover the legacy path stays dead.
- **R4 — the resolved path is the canonical registry-derived path.**
  `SelfResolve` ends in `seatcred.CurrentPath(registryPath, guid)`
  (`credential.go:137`), which reads only non-secret registry metadata. The
  subsequent `Authenticate` performs every existing check: owned-regular-file,
  0600, size bound, version, generation-currency, constant-time token match,
  audit append. The audit record gains `presentation: "self-resolved"`
  (today: `canonical` / `same-uid-copy`) so operators can measure default
  usage.
- **R5 — explicit flag always wins.** When `--credential-file` is present,
  `SelfResolve` is not consulted at all. Behavior with the flag is
  byte-for-byte today's behavior.
- **R6 — pre-cutover behavior unchanged.** Before the cutover marker exists,
  verbs keep the current legacy ambient-verified path. Self-resolution
  replaces exactly one thing: the post-cutover no-flag
  `ErrCredentialRequired` refusal. The rollback story (§10) is untouched.

### Resolution algorithm (normative)

Given registry projection, live roster rows for the relevant bus dir, and live
pane facts for the caller's `HERDR_PANE_ID`:

1. Build evidence with `hcomidentity.CurrentEvidence(envPane, livePaneID)` —
   session id, process id, pane ids. These are env-sourced *probes*, but they
   select nothing by themselves: each must match a **live joined roster row**
   to count, and `hcomidentity.Resolve` (`identity.go:151`) already fails
   closed when probes match different rows or one probe matches several.
2. `Resolve` must yield exactly one verified live row; otherwise refuse
   (reason forwarded into the refusal text).
3. Map that live row to exactly one **seated** registry row whose recorded
   `hcom_name` equals the live name *and* whose recorded pane/terminal is
   consistent with the caller's live pane facts — the same agreement
   `verifiedCallerSender` demands today (`send/send.go:261-283`). Zero or >1
   matches refuse with the candidate list.
4. For a bus-less caller (a seated `bash` operator row with no roster
   presence), step 2-3 degrade to: the caller's live terminal (from
   `herdr pane get`, not the raw env value) must equal exactly one seated
   row's recorded terminal. Same classes, narrower proof; ambiguity refuses.
5. If `HERDER_GUID` is set and does not equal the resolved row's guid — or
   names no registry row at all — refuse (hint conflict; mirrors compact's
   guid/session disagreement refusal, `spawncmd/compact.go:423-425`).
6. `CurrentPath(registryPath, guid)` → canonical path. A legacy row (no
   generation) or a missing token file surfaces that function's existing
   refusals (sweep / reissue remedies, §6 rows 5-6).
7. Return the path. The verb then calls `Authenticate` and its existing
   post-selection fences (`VerifySelectedBus`, compact's terminal check,
   adopt's target-guid check, …) with no changes. The verification step is
   partially redundant with resolution — deliberately so: it is the same
   choke point for both entry modes and closes the race between resolution
   and use.

## 3. Per-verb default semantics

"Self" is the seat the caller's live correlates prove it occupies.

| Verb | What the credential selects today | Default (no flag, post-cutover) |
|---|---|---|
| `send` | caller/sender attribution (`send.go:64-72`) | self-resolve the caller seat; sender name comes from the selected row exactly as `credentialCallerSender` does today |
| `spawn` | `spawned_by` attribution, initial-prompt sender, `--notify` recipient (`spawn.go:937-994`) | self-resolve the spawner. Promptless, notify-less spawn from a non-seat stays credential-free with `spawned_by: "user"` (fresh-self is preserved, unchanged) |
| `compact` | proof of the caller's own pane (`compact.go:122-142`) | self-resolve; compact's extra credential-branch fences (terminal equality, bus verify) run unchanged |
| `cull` | caller identity for authority + release-notice sender (`cull.go:71-89`) | self-resolve the caller seat |
| `adopt` (seated source) | **the source seat's** credential as proof of custody (`adopt.go:80-100`) | resolve the *source* seat by live occupancy: the caller's live pane must be the seated source's recorded pane (the same proof `adoptionUnseatReason` already demands). Adopt is the one verb where "self" means "the seat whose coordinates I now occupy." A caller not on the source's pane — including the `--confirm-dead` recovery leg run from elsewhere — gets no default and must present the source credential explicitly |
| `enroll` (existing live seat) | the seat's credential for re-enroll/repair (`enroll.go:488-490`) | self-resolve when live correlates prove the caller *is* that seat — this is precisely the common "run `herder enroll` from this session to repair its binding" remedy. Fresh enroll stays credential-free (fresh-self mints a new guid and its first credential, unchanged) |

Surfaces that **never** get the default:

- The raw seatcred API: `Stage`, `Authenticate`, `VerifySelectedBus`.
  Extensions, hooks, and wrappers that do not invoke a herder verb acquire
  nothing (§5, property 1).
- `herder repair reissue-credential` — interactive, audited token-loss
  recovery stays fully explicit.
- `herder credential sweep` — owner-run issuance gate, explicit.
- `herder credential path --guid GUID` — unchanged non-secret lookup. It
  gains a sibling, `--self`, which rides `SelfResolve` (§7) and refuses
  identically; `--guid` remains available for operators inspecting *other*
  seats (printing a path is not acting).
- Any env-derived path construction. There is no code path in which
  `HERDER_GUID` (or any env value) picks which credential file is opened.

## 4. Refusal matrix

Post-cutover, no `--credential-file`:

| # | Condition | Outcome | Refusal must name |
|---|---|---|---|
| 1 | Live correlates resolve exactly one seated row with a current credential; env hints absent or agreeing | **Act** as that seat; audit `presentation: "self-resolved"` | — |
| 2 | No live correlates at all (outside a herdr pane where the verb allows it, roster unavailable/empty, no seated match) | Refuse | why resolution found nothing; `herder enroll` for an unenrolled session; `--credential-file PATH` as the override |
| 3 | Correlates conflict (session vs pane vs process prove different rows) | Refuse | the conflicting correlates; scrub stale `HCOM_*`/`HERDER_*`/`HERDR_*` values; `--credential-file` escape hatch |
| 4 | Ambiguity (>1 joined rows or >1 seated candidates) | Refuse | candidate list (guid, label, bus name — the `formatCandidates` shape); `--credential-file` escape hatch |
| 5 | Resolved seat is legacy (no `credential_generation`) | Refuse | **`herder credential sweep` first** (a completion-bearing recovery verb as alternate), because before issuance there is no file to pass — see refusal-text pass below |
| 6 | Resolved seat's generation names a missing/unreadable token file | Refuse | `herder repair reissue-credential --guid GUID` (token loss, existing remedy, `credential.go:295`) |
| 7 | `HERDER_GUID` set but disagrees with the resolved seat, or names no row | Refuse | the disagreement; scrubbing the env; `--credential-file` escape hatch. Poison can force a refusal (fail closed), never a mis-selection |
| 8 | Resolution succeeded but a post-selection fence fails (`VerifySelectedBus`, compact terminal check, …) | Refuse | existing per-fence texts, unchanged |

With `--credential-file PATH`: rows 1–7 are bypassed (no resolution runs) and
today's matrix applies verbatim — stale generation names
`herder credential path --guid GUID`, mismatch refuses, cutover-marker damage
fails closed with the repair/rollback remedy.

**Never in any row:** silent fallback to ambient attribution, a synthetic
sender, or acting as an env-named identity.

### Refusal-text pass over the cutover refusals (in scope per task)

- `seatcred.ErrCredentialRequired` (`credential.go:101`) currently leads with
  "`--credential-file` is required" and names the sweep last. Pre-sweep there
  is no token file anywhere, so flag-first is a dead-end remedy. The message
  family is reordered: when the underlying cause is an unissued (legacy)
  seat, name `herder credential sweep` as the remedy, then the flag. Row 5
  above inherits this ordering. `CurrentPath`'s legacy-seat message
  (`credential.go:147`) already gets this right and is the template.
- Every resolution refusal introduced by this design ends with the same
  one-line escape hatch: *"or pass `--credential-file PATH` explicitly
  (`herder credential path --guid GUID` prints the non-secret path)"*. The
  fail-closed default must never strand a caller without naming the explicit
  road out.
- Verb help texts change `--credential-file PATH` from "(required)" to
  "explicit caller override; default is self-resolution from live correlates,
  which refuses rather than guesses".

## 5. The three preserved properties, argued

### Property 1 — implicit layers cannot act

Ratified wording: extensions/hooks/wrappers that do not call herder verbs
acquire nothing.

- The raw API is untouched. `Authenticate("")` still returns
  `ErrCredentialRequired`; `Stage` still demands an explicit guid;
  `VerifySelectedBus` still only vetoes. A library consumer, hook, or shim
  linking seatcred gains no ambient acting power. This is pinned by a harness
  regression (§8, H6) and a contract grep confining `SelfResolve` call sites
  to the six verb fences.
- What changes is verb ergonomics, and a wrapper could always shell out to a
  verb: today it can (and does — that is the cargo-cult finding) run the
  `--guid $HERDER_GUID` incantation. Self-resolution does not grant shell-out
  callers anything they lack; it *narrows* what a shell-out with a poisoned
  env can achieve, because the verb now derives selection from live proof
  instead of accepting a path the caller derived from env (§ Problem, item 1).
- Within one uid the explicit flag never provided access control — any
  same-uid process can read any 0600 token it owns. What the flag bought at
  the verb layer was *selection discipline*: you act as a seat only by
  naming its token. Self-resolution preserves selection discipline in a
  stronger currency: you act as a seat only by *demonstrably occupying it
  live*, or by naming its token explicitly.

### Property 2 — fail-closed selection

- Resolution reuses `hcomidentity.Resolve`'s conflict semantics unchanged:
  each env probe must match a live joined row; a probe matching multiple
  rows, or probes matching different rows, refuses (`identity.go:181-199`).
  No new resolution logic is invented; the registry-agreement step is the
  same one `verifiedCallerSender` runs today for legacy sends.
- The env guid is demoted to exactly what the task requires: a corroborating
  hint that can only veto (matrix row 7). It is consulted *after* live
  resolution has produced a candidate, and only for equality.
- On any failure the verb refuses and names the escape hatch (matrix rows
  2–7). There is no code path from "resolution failed" to "use ambient
  attribution": the pre-cutover legacy path is only reachable when the
  cutover marker is absent, and the marker's fail-closed handling
  (`CutoverEnabled`, `credential.go:36`) is unchanged.
- One refusal class *disappears* on the default path by construction: stale
  generation. `CurrentPath` always yields the registry-current generation, so
  the default can never present yesterday's token. (A registry flip between
  resolution and `Authenticate` still refuses via the existing generation
  check — the fence, not the sugar, is authoritative.)

### Property 3 — escape hatches

`--credential-file` remains a first-class, always-available override for:

- **(a) Broken-correlate recovery (fork-mismatch class).** Forked/resumed
  sessions whose roster `launch_context` or recorded coordinates are stale
  will fail live resolution (matrix rows 2–4). The refusal names the flag;
  the operator fetches the path with `herder credential path --guid GUID` and
  proceeds, exactly as today. Nothing about recovery gets harder.
- **(b) Deliberate act-as.** Presenting another seat's credential explicitly
  (e.g., adopt's `--confirm-dead` leg from a different pane, or an operator
  acting for a wedged seat) keeps working with today's exact semantics,
  including `VerifySelectedBus`'s right to veto on conflicting live evidence.
- **(c) Harness / isolated-registry use.** Test harnesses and
  `HERDER_STATE_DIR`-pointed registries pass explicit paths and never depend
  on live herdr/hcom state; the mock-based contract suites keep doing exactly
  this. The default requires live state and therefore cannot silently engage
  in a harness that doesn't provide it — it refuses, and the harness's
  explicit flags are unaffected.
- **Scripting helper:** `herder credential path --self` prints the path the
  default would use, riding the same `SelfResolve` (same refusals, same
  hint-veto). It exists so scripts can be explicit without re-deriving
  selection from env; it is a read-only print, performs no authentication,
  and appends no audit entry.

## 6. Operator-shell story

The human at a terminal gets the same low-ceremony default, because the
default keys off live occupancy, not agent-ness:

- **Enrolled operator pane** (e.g., the owner session, or any bash/claude
  pane that has run `herder enroll` once): every credential-authenticated
  verb works bare — `herder send @nova 'msg'`, `herder cull --guid G`,
  `herder spawn --role r --agent claude --prompt '…'`. Resolution proves the
  operator's seat from the live pane/roster and self-authenticates.
- **Unenrolled shell inside a herdr pane:** first identity-bearing verb
  refuses with matrix row 2, naming `herder enroll` — a one-time,
  credential-free ceremony (fresh enroll mints the guid and first token).
  After that, zero ceremony.
- **Outside herdr entirely:** unchanged. `send`/`compact`/`cull` already
  gate on `HERDR_ENV=1`; promptless `spawn` still works attributed
  `spawned_by: "user"`; prompt-bearing spawn still refuses (its sender fence
  requires a live pane today, `spawn.go:2284-2289`).
- **Doc surface delta:** `tools/herder/docs/credentials.md`, the verb help
  texts, and the hcom session boilerplate (maintained outside this repo)
  currently teach the `--credential-file $(herder credential path --guid
  "$HERDER_GUID")` incantation. All three are rewritten to teach bare verbs,
  with the flag documented as override/recovery. Retiring the env-keyed
  incantation from guidance is itself a security improvement (§ Problem).

## 7. Poisoned-env harness deltas

Existing poisoned-env cases (adopt_test.go, compact_test.go,
sender_identity_test.go, check-spawn-contract.sh §poison,
check-enroll-contract.sh §poison, graceful_test.go) stay green unchanged.
New cases, all post-cutover and flag-less unless noted:

- **H1 — poison cannot steer to a real other seat.** Live state proves seat
  A (mock roster + mock `herdr pane get`); env carries seat B's
  `HERDER_GUID`. Expect exit 2, refusal naming the disagreement, no send/
  spawn/unseat side effect, and *no `credential_authenticated` audit entry
  for seat B's guid* — the audit-absence assertion is the steering proof.
- **H2 — poison alone cannot act (the anti-incantation case).** Env fully
  poisoned with a real seat's guid/sid/pane values but no live row matches
  (roster empty or rows not joined). Expect refusal at resolution, not
  authentication: assert no audit entry exists at all. This is the case that
  distinguishes the default from the old incantation, which would have
  authenticated (VerifySelectedBus's empty-evidence pass, `credential.go:346`).
- **H3 — conflicting probes refuse.** Poisoned `HCOM_SESSION_ID` matching
  live row X while the live pane proves row Y: refusal names the conflicting
  correlate; nothing acted.
- **H4 — nonexistent hint refuses.** `HERDER_GUID` naming no registry row,
  with otherwise clean live resolution: refuse (fail closed), matrix row 7.
- **H5 — ambiguity refuses with candidates.** Two seated rows plausibly
  matching the caller's coordinates: refusal carries the candidate list and
  the escape hatch; nothing acted.
- **H6 — raw API regression pin.** `seatcred.Authenticate(path, "")` returns
  `ErrCredentialRequired` (unit test), and a contract grep (in the style of
  `check-compact-contract.sh`) pins that `SelfResolve` is referenced only
  from the send/spawn/adopt/cull/compact/enroll fences and `credential path
  --self`.
- **H7 — per-verb default happy path.** Each of the six verbs, no flag,
  clean live state: acts as the resolved seat; audit records
  `presentation: "self-resolved"`; explicit-flag invocation of the same verb
  still records `canonical`.
- **H8 — legacy/token-loss remedies.** Row 5 asserts the refusal names the
  sweep before the flag; row 6 asserts the reissue remedy.

## 8. Deltas from the ratified cutover design

Named per the task constraint — nothing here is silently absorbed:

- **D1 (the amendment itself):** post-cutover, flag-less invocation of a
  credential-authenticated verb changes from `ErrCredentialRequired` to live
  self-resolution. This is the owner-ratified direction; everything else in
  this note exists to show D1 preserves the three properties.
- **D2:** the `ErrCredentialRequired` refusal family is replaced on the
  default path by resolution refusals with context-specific remedies, and
  the legacy-seat wording is reordered to name the issuance sweep first.
- **D3:** the credential audit gains `presentation: "self-resolved"`.
- **D4:** `herder credential path` gains `--self`.
- **Explicitly unchanged:** the seatcred API surface; token file discipline
  (0600, owned, immutable generations, no argv/env/log carriage); rotation
  and its commit point; the cutover marker and its fail-closed handling; the
  sweep's literal-100% gate; fresh-self flows; the per-verb code-level
  rollback order and the marker-deletion emergency lever
  (`docs/credentials.md` §Transaction and rollback). Rollback still reopens
  ambient authority for the legacy path only; self-resolution is compiled
  into the post-cutover branch and simply stops being reachable when the
  marker is removed.

## 9. Alternatives considered

Briefly — the direction is ratified; these are implementation-shape rejections.

- **Default the path from `HERDER_GUID` inside the verb** (mechanize the
  incantation): rejected — env-derived path auto-open is exactly the
  laundering this task forbids, and it inherits the empty-evidence
  authentication hole.
- **Make `Authenticate("")` self-resolve inside seatcred:** rejected — it
  would hand the default to every API consumer and break property 1.
- **Shell aliases / wrapper scripts owning the incantation:** rejected —
  moves the laundering into more copies instead of retiring it, and does
  nothing for refusal quality.

## 10. Rollback

Unchanged, restated for completeness: reverting self-resolution is a per-verb
local change (the fence goes back to `ErrCredentialRequired` on empty path)
strictly smaller than the cutover's own per-verb rollback; deleting the
cutover marker remains the larger lever and behaves exactly as documented
today. No token file, registry row, or generation is written differently
under this design, so rolling either direction requires no state migration.

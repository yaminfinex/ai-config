> Provenance: authored 2026-07-08 by sysreview-fifi (read-only systemic review over TASK-001..070 + run-log + spec), harvested from napkins/run-herder-dx/systemic-review-memo.md on 2026-07-09. Proposals 3.1-3.8 dispositioned on the board (see run-log); §5 anti-recommendations are standing do-NOT-do rulings preserved here.

# Systemic design review — run-herder-dx board (TASK-001..070)

Reviewer: sysreview-fifi (Fable, read-only). 2026-07-08.
Corpus: all 70 board items incl. comments, run-log.md (full), herder-spec.md (ratified),
and an architecture pass over internal/{registry,sidecarcmd,spawncmd,cullcmd,enrollcmd,
renamecmd,hookcmd} incl. registry/write.go normalization and the spawn awaitBind path.
PROPOSE-ONLY: every proposal below is a candidate board item; @hera owns the board.

Summary verdict up front: the board is NOT 70 unrelated bugs. About 80% of the defect
mass falls into six clusters, and four of those six trace to two design-level facts:
(1) a seat binding is a join across three substrates whose handles each expire on their
own schedule, and the machinery that re-verifies the join is only fully built for
SPAWNED sessions — manual/enrolled sessions (i.e. the orchestrator itself) live in a
structural blind spot; (2) the registry's append-only snapshot model plus its legacy
2-state view put a heavy carry/clear/stamp obligation on every writer, and the write
API lets callers claim success without confirming the write happened. The remaining
two clusters are upstream-shaped (missing substrate signals; upstream version churn)
and are best killed by the TASK-029 filing batch plus live-contract tests, not by more
local hardening. Two clusters on the candidate list are, on the evidence, NOT systemic
(wrapper/toolchain churn; docs hygiene) — reasoning in §1.G/H.

---

## 1. Defect taxonomy

Instances listed by task number; overlaps are real (several tasks sit in two clusters)
and flagged where they matter. Counts are of distinct board items touched, not severity.

### A. Launch-time coordinates trusted after they drift (~14 items — largest cluster)

004, 005, 013, 016, 019, 023, 033, 035, 041, 043, 044, 046, 049, 065.

One-line mechanisms:
- 004/016 — provenance `spawned_by` read from ambient env (`HERDER_SPAWNED_BY`), which in a
  spawned session names that session's own spawner → grandparent recorded.
- 013/019 — inherited `AI_CONFIG_ROOT`/`HERDER_BIN` from spawner beat the worktree's own
  checkout → suites silently exercised the wrong tree.
- 023 — post-transport-kill notify resolution treated `--notify-to` purely as a registry
  hint; the literal-bus-name case (the common one) was dropped.
- 033 — spawn's capture loop enriched the row from a tag+cwd unique guess → stale
  same-tag agent's bus name attached to a fresh guid.
- 035 — reused pane accumulated three "working" manual rows; pane-id resolution silently
  picked the stale one (@zero over live @hera).
- 041 (×3 live hits) — compact self-location dead-ends on a registry row whose
  pane/terminal coordinates predate a renumbering/handoff; no recovery affordance.
- 043 — enroll writes `hcom_name` from `HCOM_INSTANCE_NAME`, frozen at process launch;
  goes stale the moment the session reclaims a different bus identity
  (enrollcmd/enroll.go:132, confirmed still present).
- 044/046 — liveness/coordinates invalidated wholesale by a herdr handoff (new terminal-id
  scheme, new pane-id scheme); every pre-handoff row dead-keyed.
- 065 — a wrong bind, once recorded, is now faithfully carried forward (post-064) — bad
  coordinates became sticky.
- 049 — doctrine TEXT drifted the same way the data does (claims about pane-id recycling
  stale in both directions across 0.7.x).

### B. Claimed-success-without-confirmed-effect — the "dead recovery" class (~8 items)

024, 031, 034 (review P1/P2), 045 (F1 round-1 + LOW-1), 053 (F1), 062, 069, plus the
Unit-R receipt-verification triple inside 032.

One-line mechanisms:
- 053 F1 — `enrichedSessionID` advanced unconditionally while `lastReportedSID` only set
  on success → one transient herdr failure permanently disarmed sid reporting.
- 045 F1/LOW-1 — poll-loop enrichment gate unreachable for empty-sid rows; then a
  reachable gate guarding an UNCONFIRMED write (void append with flag set anyway).
- 069 — cull's pane_not_found path prints "still marked closed", exits 0; the append
  no-ops (row already `unseated`, normalizeSessionAppend returns not-appended) and
  cullcmd discards UpdateLocked's returned rows (`_, err :=` at cull.go:333) — the exit
  code and message are derived from err==nil, not from a confirmed write.
- 032/R — `verify=delivered` had been unreachable since TASK-003 (receipt query keyed to
  the wrong side); all sends silently degraded to "queued" while reporting truthfully-
  looking vocabulary.
- 034 — grace path fired on sampled state + wall clock without proof of turn end;
  watermark fail-open.
- 024/031 — the inverse pole: honest-but-wrong verify verdicts from screen-scrape
  evidence (claimed-failure-despite-success), which invites destructive resends.
- 062 — spawn creates the pane before the registry write; a refused write orphans the
  pane (partial effect, no unwind).

### C. Missing/asymmetric substrate signals — codex and the TUI-as-API (~14 items)

002, 010, 014, 017, 022, 024, 027, 031, 032, 036, 045, 048, 053(codex-inert), 063.

One-line mechanism, generalized: herder needs a signal (readiness, bind, sid, receipt,
composer state, a bootstrap seam, a statusline hook) that a substrate either does not
publish at all, publishes for claude but not codex, or exposes only as pixels. Every
missing signal grew a heuristic (paste sigils, tag+cwd guesses, wall-clock windows,
/proc environ scans), and each heuristic became its own bug class:
- codex: no sessionstart seam (002/014), developer_instructions stripped on resume/fork
  (017/027), roster omits `launch_context.pane_id` (036), hooks never bind under hcom
  0.7.23 → sid empty + name capture structurally dead (045, fixed locally via /proc
  environ correlation), no statusline hook (063).
- TUI-as-API: bootpaste's whole readiness/land/Enter/verify state machine (024, 031, 048)
  — retired for delivery by 032's bus-first rework, surviving only in compact.
- hcom: no receipt-await primitive (all three receipt-reconstruction layers were live
  bugs — 029 candidate 9), `-p` one-shots backgrounded (010).

### D. Two data models over one file — append-only snapshot + legacy view foot-guns (~9 items)

055 (F1), 056 (A2 MEDIUM), 057 (A3 BLOCKER), 058 (A4 BLOCKER), 059 (A5 BLOCKER),
064, 065, 069 (its projection half), 042 (leg 3).

One-line mechanisms:
- 064 — spawn's `registered` snapshot masked the sidecar's earlier `recognised`
  enrichment in latest-row collapse (carry-forward missing) — TASK-045's symptom
  returned through a different seam.
- A2 — rename dropped the seat of legacy-v1 sessions (nil-seat carry); the golden had
  enshrined the bug.
- A3 — writers carried `Node` forward instead of stamping it → clone repair bricked all
  prior sessions (some fields must NEVER be carried — the mirror image of 064).
- A4/A5 — crash-window interactions between migration recovery, rotation recovery, and
  the count-based trigger (each recovery path individually fine, composition wrong).
- 069/042 — the legacy view maps `unseated → status:"active"`
  (registry.go legacyRecordFromV2Object:199), so every legacy predicate
  (`ActiveLabelOwner`, cull's selectTargets, sidecar's closed-check) conflates
  "dormant corpse" with "live holder": enroll refuses a label held by a dead row;
  cull's unseat append no-ops against an already-unseated corpse.

### E. Liveness joins missing their input (~5 items)

044, 046, 070, plus the observability halves of 020/037 (hook-fed status stalls).

One-line mechanism: `list`/`wait` join registry rows against herdr's agent tracker, but
whole categories of live sessions produce no tracker input — pre-handoff processes
(hook reports never re-reach the new server, 046 mechanism 2), shell-relaunched agents
in existing panes (tracker only adopts agents it started, 070), and manual sessions
generally (044). Post-046 the tri-state (`undetected` vs `gone`) is honest, but
"undetected" still cannot distinguish alive-unobserved from dead for the one session
class that matters most (see §2.E).

### F. Identity-lifecycle verb gaps — invariants shipped before their escape verbs (~6 items)

042, 051 (message-polish half), 054, 061, 068, 069 (its consequence half).

One-line mechanism: the spec defines label leases with explicit release (`retire`,
`rename --take-from`, `reopen`) and one resolver over guid|label. Wave A shipped the
ENFORCEMENT (label uniqueness over all non-retired sessions, per §3.1-6/AC-18) while
the release verbs are wave-C territory: `retire` does not exist, `rename` has no
`--take-from`, `cull` rejects label targets (054) and — per cluster B — its --force
escape hatch silently no-ops. Net: after any restart, the dead session's label is
unreclaimable by ANY current verb (the standing orchestrator runs as
`hera-restart-050b` today). Every gap pushes operators into off-registry dances
(raw `hcom start --as`, env-override enroll, variant labels — 042/043 evidence;
061: lale nearly used raw hcom spawn), and those dances MANUFACTURE the stale rows
that clusters A and E then trip over. This is a self-feeding loop.

### G. Wrapper/toolchain environment churn (7 items) — NOT systemic beyond what's done

007, 008, 012, 015, 018, 020, 037 (+ the AA cache-contention flake).

Mechanism: compile-on-demand wrappers + shared caches + inherited env across sibling
checkouts. Verdict: normal infra churn for a repo that carries its own build shim; the
class was killed methodically in waves 1–2 + 037 (toolchain pinning, per-hash caches,
age pruning, last-good serving, LC_ALL, suite env hygiene) and is now guarded by
dedicated suites (check-wrapper-lastgood, the 019 env stanzas). No design change
recommended; the residual risk (shared-tmp contention under concurrent batteries) is
already handled procedurally (sequential batteries). Accept + existing guards.

### H. Upstream version churn / doctrine drift (~12 items) — process fix mostly in place

009, 021, 026, 028, 029, 030, 038, 039, 040, 047, 049, 050.

Mechanism worth keeping: herder mirrors upstream BEHAVIOR PREDICATES (the codex strip
predicate, the reTag quote regex, `list --json` single-object shape, roster field
shapes, pane-id stability semantics) grounded in one specific upstream version; canned
fixtures keep the battery green while the live pairing is broken (040's exact words:
"battery blind — canned fixtures"). The audit-before-upgrade pattern (028, 047–050) and
docs/hcom-upgrade.md now institutionalize the response. Residual systemic gap → §3.6.

Cross-cutting hypothesis from the brief — "bus-name vs registry-label vs guid: how much
of the board is this one problem?" Answer: it shows up in ~12 items (005, 023, 033,
035, 039, 042, 043, 044, 045, 052, 064, 065, 068) but it is NOT one root cause; the
spec's session/seat/label factoring resolves it conceptually and correctly (hcom_name
is a seat coordinate, not identity). What remains is three implementation gaps riding
that seam: the observer gap (E — who keeps hcom_name fresh for sessions without a
sidecar), the verb gap (F — no lease-transfer/retire), and a DX gap (068 — operators
think in bus names; send won't resolve them; labels and bus names look alike and
routinely diverge, e.g. label task063-6cf471f0 vs bus task063-taro).

---

## 2. Root-cause analysis per cluster

### A — stale coordinates. Seam: "commands learn things only when invoked."
Spec-DIAGNOSED, implementation lagging. Spec §4 names this class verbatim as the
original flakiness and prescribes the architecture that kills it (commands write
intent, per-occupant observer writes observations, registry arbitrates, everything
else is a cache; §8.3 reconciliation for epoch mismatch; §3.1-8 env is birth
provenance only). Most instances predate that machinery or occupy corners where it is
not yet built: reconcile is a one-shot 046 command, not the §8.3 procedure; enroll
still reads env for a seat coordinate (043) in direct tension with the SPIRIT of
invariant 8 (the letter covers only HERDER_GUID resolution — spec-silent corner worth
an erratum: "no seat coordinate may be sourced from launch-frozen env when a live
observation is available"). Upstream-shaped subparts: coordinate reissue at handoff
(046), pane-id re-key semantics (047/049). Ours to fix otherwise.

### B — claimed success. Seam: write/report APIs don't force acknowledgment.
Implementation-caused; spec-SILENT on the caller obligation. The spec mandates
idempotent no-op appends (§5.2) — a good property — but says nothing about callers
distinguishing wrote from no-op'd, and `UpdateLocked` returns the appended rows as a
value callers may (and do) discard. The failure is always the same shape: state (a
flag, a message, an exit code) advances on INTENT while the effect silently didn't
happen or wasn't checked. The run-log checklist rule ("recovery reachable AND its
write confirmed") is correct but is enforced by reviewer vigilance — it has now been
independently rediscovered four times (053, 045 ×2, 069). A checklist rule hit four
times is an API contract waiting to be written. Ours.

### C — missing substrate signals. Seam: integration by heuristic where upstream
publishes nothing. UPSTREAM-SHAPED, and the run's own record proves the leverage
direction: when a real primitive existed (hcom queue-until-deliverable), switching to
it (032 bus-first) DELETED the paste failure class overnight — 024, 031 closed by
design change, the whole Enter/verify state machine retired to compact-only. The
remaining heuristics sit exactly where no primitive exists: codex pane_id (029 cand
12), codex hook binding (F3, filed-held), receipt-await (cand 9), codex bootstrap seam
(cand 1/2). The local scar tissue (positive-child-specific-signal-or-refuse, TASK-033
discipline) is the correct defensive posture and is now consistently applied
(sidecar findRowCorrelated, awaitBind childBoundBusOnce). Upstream first; ours only
for the discipline.

### D — projection/carry foot-guns. Seam: snapshot-per-event puts a per-writer carry
obligation on every append; the legacy 2-state view multiplies the consumers that get
state semantics wrong. Split verdict:
- Snapshot-per-event itself: spec-caused and DEFENSIBLE (D9 rationale is real: rotation
  reseed legality, file-shaped ops). The 064 ruling centralized the carry in ONE locked
  helper (normalizeSessionAppend + carry*/mergeSeatFields) — the right architectural
  answer, and the A3 ruling added its inverse (envelope fields stamped, never carried).
  After those two rulings the residual foot-gun surface is small and tested.
- The legacy view: implementation transition scaffolding, and now the main remaining
  generator. `unseated → "active"` (registry.go:199) means every predicate written
  against `Status == "active"` conflates four states into two. 069's label deadlock and
  042-leg-3's enroll refusal are not exotic bugs — they are the mapping doing exactly
  what it says. Ours; finite; enumerable by grep.
- The A2–A5 crash-window blockers: implementation-caused composition bugs between
  individually-correct recovery paths (migration vs rotation vs node mint). All caught
  pre-merge by the adversarial pipeline — evidence the process handles this subclass.

### E — liveness join inputs. Seam: the observer architecture has a hole for
non-spawned sessions. Spec-SILENT in a load-bearing way: §4 defines the sidecar as
"one per occupant, forked by `herder launch`" — an ENROLLED occupant has no sidecar,
so nothing bridges its status, refreshes its hcom_name, watches its sid, or detects
its turnover. Every consequence follows: 043 (enroll trusts env because no observer
owns the live value), 044/070 (list has no liveness input), 041 (compact's self-ladder
has no live fallback), and the 034 experiment-2 failure. The victim in virtually every
live incident on this board was the ORCHESTRATOR'S OWN SESSION — always manual, always
in the blind spot, while spawned workers enjoy the full machinery. Upstream half: herdr
tracker adopts only agents it started (070a, ledger candidate). Herder half is ours,
and it is a spec-shaped hole, not just a backlog item: the spec should say what
observes an enrolled seat.

### F — verb gaps. Seam: enforcement shipped a wave ahead of its escape verbs.
Sequencing, not spec (the spec defines the full verb set; wave A implemented the
invariant, wave C owns the verbs). But one genuine spec TENSION hides inside 069's
in-flight fix: "label freed once closed" is only spec-legal if the freed state is
RETIRED — AC-18 explicitly includes unseated holders in the collision rule, and cull
is defined as unseat-not-retire (§2, §7). A cull that frees labels via unseated rows
would violate AC-18; a pane-less `--force` cull that appends `retired` instead is the
clean escape (precedent: A2's launch-failed close was reclassified to retired). Flag
this to the 069 unit before it lands. Ours.

### G — wrapper churn: no systemic cause; closed by waves 1–2 + 037 + suites. Normal
frontier churn around carrying a private build shim.

### H — upstream churn: cause is real (behavioral mirrors + canned fixtures) but the
process countermeasure (upgrade audits + runbook) already exists; the residual gap is
that NOTHING runs the mirrors against the LIVE substrate between upgrades → §3.6.

---

## 3. Kill-the-class proposals (ranked by leverage ÷ cost; PROPOSE-ONLY)

### 3.1 Confirmed-write contract on the registry API — kills cluster B at the type level
Priority: HIGH. Cost: small (one signature + ~8 call sites).
WHERE: tools/herder/internal/registry/write.go — make `UpdateLocked` (or a wrapping
helper) return a typed per-row outcome: `applied | noop_already_target_state |
refused(reason)`, and sweep the writers (cullcmd/cull.go appendClosed — the live 069
site; enrollcmd; sidecarcmd appendEnrichment — already half-converted by 045 LOW-1;
lifecyclecmd; spawncmd) so no success message or state flag is emitted without
witnessing `applied` or an explicit no-op-with-target-state-confirmed. Add one grep
gate to the battery: no `_, err := registry.UpdateLocked` discards. This converts the
run-log's thrice-learned checklist rule into something a reviewer cannot miss and a
writer cannot skip. Fold into or immediately behind the in-flight 069 unit.

### 3.2 Ship the identity-release verbs as ONE unit — kills cluster F's deadlock loop
Priority: HIGH (every restart bleeds until then; the orchestrator's label is stuck on a
corpse today). Cost: medium.
WHERE: new `retirecmd` (spec §7: releases label, refuses resume, reopen returns
unlabelled), `rename --take-from` (renamecmd/rename.go — atomic transfer + notify per
AC-19), cull positional `<target>` + already-closed refusal wording (054 + 051 nit),
and the 042 composite documented as the restart runbook (enroll new guid → rename
--take-from → retire old). Design note: keep AC-18 intact — the escape from a stuck
label is retire/transfer, NEVER weakening uniqueness over unseated holders (see the
069 tension flagged in §2.F).

### 3.3 Close the observer blind spot for manual/enrolled sessions — kills cluster E
and the largest share of cluster A's live incidents
Priority: HIGH-MEDIUM. Cost: medium, and it needs a small spec amendment.
WHERE, in increasing ambition (all three sub-steps are independently shippable):
  (a) enrollcmd: resolve the LIVE bus identity from hcom (by process id, the 045 /proc
      pattern is precedent) instead of `HCOM_INSTANCE_NAME` env, or refuse loudly on
      env-vs-live mismatch — the 043 fix, and the cheapest single win on this list.
  (b) listcmd/waitcmd + compact.go self-ladder: when seat.terminal_id exactly matches a
      live pane, report `seated (unverified live)` instead of bare `undetected` (070b),
      and give compact the same pane-list fallback (041b) — both are read-side.
  (c) Spec §4/§8 amendment + implementation: define what OBSERVES an enrolled seat.
      Cheapest honest option: enroll forks the same sidecar (it is tool-agnostic and
      exits with its occupant); alternative: a `herder reconcile --self` affordance.
      Without (c), turnover detection (§8.1) simply does not exist for the session
      class that runs every orchestration.

### 3.4 Finish the legacy-view retirement — kills cluster D's residual generator
Priority: MEDIUM (wave-B/C natural home, pairs with TASK-066). Cost: medium, mechanical.
WHERE: enumerate `Status == "active"` / `isNonRetired` consumers (registry.go
ActiveLabelOwner, ActiveByPaneOrTerminal, ActiveCandidates...; cullcmd selectTargets;
sidecarcmd latest.Status; spawncmd/send resolvers) and re-express each against the v2
four-state machine, choosing per call site whether it means "seated" (liveness-ish) or
"non-retired" (lease-ish) — the conflation of those two meanings IS the bug class.
Then delete `legacyRecordFromV2Object`'s two-state mapping. Until that lands, every
new consumer written against the legacy view re-rolls the 069/042 dice.

### 3.5 Execute the TASK-029 upstream filing batch — kills the biggest slice of cluster C
Priority: HIGH leverage, near-zero cost (drafts exist; owner-gated at closeout).
The three that each collapse a whole local heuristic layer: codex
`launch_context.pane_id` (cand 12 — retires the /proc environ scan, the 036 resend
affordance, and the structural codex bind asymmetry), codex hook binding regression
(F3 — revives codex sid reporting, completing AC-24/D11 substrate), receipt-await
primitive (cand 9 — deletes the receipt-reconstruction machinery Unit R had to fix
three times, incl. the per-tuple flock). This is the only place on the board where a
few paragraphs of prose delete whole Go files.

### 3.6 Live-contract suite tier for the substrate mirrors — kills cluster H's
"battery green, live pairing broken" mode
Priority: MEDIUM. Cost: small-medium.
WHERE: tools/herder/tests/check-live-contract.sh (new tier, skips cleanly when the real
binaries are absent so CI-less boxes stay green): pin the ~6 mirrored predicates
against the INSTALLED hcom/herdr — bootstrap tag-line extraction (the 040 regex, both
quote styles against real `hcom` output), `hcom list --json` shape (single-object/
base-name, cand 10), roster launch_context fields per family, herdr agent-list
envelope, `herdr api schema --json` snapshot diff (046 comment already proposed this —
mechanical drift detection instead of golden-string parsing). Run it in the upgrade
runbook AND periodically, not only at upgrades.

### 3.7 Bus-name DX: send resolves hcom_name; surface the label/bus pair — shrinks
cluster A's operator-error feeder
Priority: LOW-MEDIUM. Cost: small.
WHERE: send resolver (internal/send) gains seat.hcom_name as a key with the same
ambiguity-refusal discipline as labels (068); `spawn` summary and `list` print label
and bus name adjacently so divergence is visible instead of surprising. Rationale:
every time an operator falls back to raw hcom because the canonical path would not
take the name they were looking at, the registry accrues one more stale/foreign row.

### 3.8 Standing controlled-restart drill — process, not code
Priority: LOW cost, real payoff. The 050 controlled restart was the single most
productive evidence event of the run (confirmed 043, falsified the 042 stale-env
hypothesis, found 069 and 070). Epoch-boundary bugs (clusters A/E) are structurally
invisible to per-unit gates (§4). Make "restart a real session against a repro
checklist" a standing closeout drill in the orchestrate skill, alongside the upgrade
audits. WHERE: skills/orchestrate references + run playbook template.

---

## 4. What the process caught vs what it structurally cannot catch

Caught — and this deserves stating plainly: the per-unit pipeline (independent gate
re-run + adversarial review + live smokes + spec-ravu rulings) is unusually strong.
The run-log's own scorecard: ~13 engine reviews commissioned, ~12 with substantive
findings, at least 6 P1-class caught PRE-merge, several reviewer-REPRODUCED (A4's
crash-window blocker, tuba's TERM-morph, kato's clone-repair brick). Every wave-A
blocker was a crash-window or composition bug that gates could not see and review did.
The doctrine feedback loop (dead-recovery checklist growing across 053→045→069) shows
the process learning inside one run.

Structurally cannot catch — four modes, with whether §3 changes them:

1. Cross-unit interleaving semantics. 064 (sidecar-enriches-before-spawn-registers) and
   065 shipped through clean per-unit gates because no unit's tests seed another unit's
   TIMING; per-unit adversarial review sees one diff, not the composition. §3.1 shrinks
   the blast radius (a masked write becomes a loud no-op at the caller) and §3.4
   removes the ambiguity the compositions trip on, but the mode itself remains; the
   honest mitigation is the targeted multi-writer ordering tests the run already adds
   post-incident, not a general integration tier (see anti-recommendations).

2. Absent-signal blindness: the battery is green against canned fixtures while the live
   pairing is broken (040 reTag; 045 codex bind; 034's hcom list shape). §3.6 is aimed
   exactly here and would have caught 040 and cand-10 class breaks; it cannot catch
   what only load/latency reveals (036).

3. Epoch-boundary behavior (handoff, restart, db reset, upgrade). These bugs only
   surfaced when the real event happened (046 at the herdr upgrade; 069/070 at the
   controlled restart). No golden reproduces a server handing off. §3.8
   institutionalizes the only method that has worked.

4. The operator's own identity class. Units test what units create — spawned agents.
   The manual/enrolled session class had zero coverage until it broke in production,
   repeatedly, always on the orchestrator. §3.3 both fixes the machinery and, by making
   enrolled seats first-class, makes them testable (an enroll contract suite becomes
   possible once enroll has defined observer behavior).

One meta-observation: a striking amount of load-bearing doctrine lives in run-log prose
(confirmed-write checklist, brief-template lessons, model policy, pane hygiene).
§3.1 promotes the most-hit rule into the type system; the compound/orchestrate skills
are the right home for the rest — worth a deliberate harvest at closeout rather than
letting the run-log be the only copy.

---

## 5. Anti-recommendations — attractive from the bug list, advised against

1. Auto-healing reads (list/resolve/send silently adopting, re-binding, or rewriting
   stale rows). The bug list makes this tempting (035/041/044/046/065 all "stale row
   misleads"). The run's own evidence says no: every silent-guess path that existed
   became a P1 factory (033 tag+cwd, 035 last-pick, 046 F1), and the ratified stance —
   list stays read-only, reconciliation is triggered and auditable (§8.3, 046 decision
   d) — is what made the fixes reviewable. Keep repair explicit; fix the blind spot
   (§3.3) instead of guessing harder.

2. Same-guid adoption for restarted sessions ("just let the new process take the old
   guid"). Already respec'd once (042: spec-illegal under D1, owner-ratified). Any
   `herder adopt` convenience must COMPOSE enroll + take-from + retire; re-keying a
   guid re-introduces the branch-in-a-container ambiguity the whole identity model
   exists to prevent.

3. Weakening label uniqueness to unstick corpses (e.g. "unseated+gone rows don't hold
   labels"). This is the shortcut hiding inside 069's consequence chain. AC-18 is
   explicit that unseated holders collide, and it is RIGHT (a dormant resumable session
   that silently loses its address on liveness flaps is the pre-spec world). The
   correct escape is retire/transfer (§3.2); at most, pane-less --force cull may append
   `retired` — never a liveness-coupled lease.

4. Smarter TUI-paste hardening (late-submit loops, composer-state machines, per-family
   readiness oracles) for the remaining paste users. 031's late-submit was superseded
   for exactly the right reason: 032 proved the state that defeats paste (dirty
   composer) also defeats its own recovery, and bus-first made the whole layer
   unnecessary for delivery. compact's residual paste should stay minimal and
   fail-closed (it already does); invest nothing further there.

5. sqlite (or any db) for the registry in response to the A2–A5 crash-window findings.
   Those bugs were in migration/rotation COMPOSITION, all caught pre-merge, all now
   pinned by crash tests; sqlite trades them for a different class (whole-db corruption
   unit, sync hazards under a synced home, driver weight) that §5.1 already litigated
   with recorded rationale. The projection-cache option remains open if read cost ever
   matters.

6. Per-family bind windows / longer timeouts for codex spawns. Tried and reverted on
   measurement (036: direction (a) — the signal is structurally ABSENT, not slow; the
   8-minute HERDER_SPAWN_BIND_MS experiment on 045 proved window length irrelevant).
   The fix is the upstream pane_id/hooks filings (§3.5) plus the /proc correlation
   already shipped.

7. A general pre-merge integration-test tier (spin up multi-unit concurrency harnesses
   for every engine change). The cross-unit escapes (064/065) sting, but the run closed
   them within hours at the cost of two scoped incidents, while a general concurrency
   harness would tax every unit forever and still not enumerate the interleavings that
   matter. Targeted ordering regressions per incident (which the run already writes) +
   §3.1's loud no-ops are the better trade.

---

## Appendix: instance→cluster index (primary cluster only)

A: 004 005 013 016 019 023 033 035 041 043 044 046 049 065
B: 024 031 034 045 053 062 069 (+032's receipt sub-findings)
C: 002 010 014 017 022 027 032 036 048 063
D: 042 055 056 057 058 059 064
E: 070 (044/046 shared with A)
F: 051 054 061 068
G: 007 008 012 015 018 020 037
H: 009 021 026 028 029 030 038 039 040 047 050
Not classified (normal work, no defect class): 001 003 006 011 025 052 060 066 067

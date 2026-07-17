---
id: TASK-270
title: >-
  Implement: canonical rebirth — one shared seat-completion step (identity
  migration U1)
status: Done
assignee: []
created_date: '2026-07-17 04:26'
updated_date: '2026-07-17 07:19'
labels:
  - herder
  - identity-migration
dependencies: []
priority: high
ordinal: 269500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
IMPLEMENT unit, stage 1 of the ratified identity migration. GROUND TRUTH (settled, double-reviewed + owner-ratified — do NOT relitigate; deviations are STOP-AND-REPORT to @hera, read this twice): docs/design/2026-07-17-identity-migration-plan.md §U1 and docs/design/2026-07-17-identity-architecture-target.md (invariant T2; trust boundary as stated there). The memo keep-list (docs/design/2026-07-17-registration-brittleness-memo.md §4) is a set of HARD constraints.

Goal: every creation/recovery verb (spawn, enroll, enroll-repair, adopt, reclaim, resume) terminates in ONE shared, seat-kind-aware completion step (new package, e.g. tools/herder/internal/seatcompletion/): herdr seats resolve live pane/terminal and, for bus-capable tools, verify the bus row and backfill launch coordinates via the sanctioned merge-missing-only write; process seats resolve pid + bus binding; busless tools (bash) complete without the bus leg. Completion appends the seat binding with its evidence class and refuses loudly with a kind-appropriate enumerated missing-fact list otherwise (each fact naming the verb that supplies it). Exactly one row shape per seat kind after any verb.

SETTLED DECISIONS: consolidation not invention (adopt/spawn/reconcile already carry the pieces); merge-missing-only write stays in hcomidentity/launch_context.go and is called ONLY from the completion step; pane_conflict from the vendor-db write is surfaced, never swallowed (never-rewrite-existing is a keep-list fence); multi-match on live correlates fail-closes; the narrow evidence-dominance exceptions (empty-context fallback, reconcile heal) keep their exact predicates and become CALLERS of the shared step; completion exposes an attestation-consuming mode for exactly one future caller (the break-glass verb, next stage) substituting the attested binding for the live-verification leg of the attested field only, at evidence class attested. WRITE-SPINE SCOPE (per the plan's shared scope note): the seat/bus binding evidence-class field + the durable per-binding id (minted at append, persisted in row JSON, NEVER derived from load-time ordinals) on session records, with normalizer and carry-rule ownership in internal/registry/ — write-spine tests prove full projection preservation, typed outcomes, rotation survival.

ENTRY GATE: SATISFIED — the creator-flow ambient-sid-harvest fix is merged to main with its regression tests.

DESIGN CHECKPOINT REQUIRED BEFORE CODE: short design note (shared-package shape + API, completion sequence per seat kind, refusal matrix, evidence-class field + binding-id write-spine design incl. carry rules) posted to @hera for approval; code is cut only after approval. Adversarial review is orchestrator-dispatched after your DONE; do not arrange reviewers yourself; want mid-unit eyes, message @hera. The orchestrator's bus address is @hera; there is no @orchestrator alias. Commit on your unit branch before DONE. Hygiene: no agent names, task numbers, run identifiers, or SHAs in code comments, fixtures, goldens, or refusal text.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Design checkpoint note approved by orchestrator BEFORE any code (package shape, per-seat-kind completion sequence, refusal matrix, write-spine field + binding-id design)
- [x] #2 All six creation/recovery verbs terminate in the shared completion step; cross-verb row-shape parity suite proves byte-equivalent seat shapes for the same live facts across paths, for all three seat kinds (herdr/process/busless)
- [x] #3 Every plan §U1 test scenario implemented and green: reclaimed-row backfill then spawn-capable; bus-row-absent refusal with no partial row; busless bash completes without bus facts; headless process seat completes on pid+bus; pane_conflict carried not rewritten; two-bus-row multi-match fail-closed; write-spine projection preservation + typed outcome + rotation survival; mutation-armed pins on every admitting path of the completion predicate
- [x] #4 Keep-list re-audit of the final diff: no widened admitting predicate, no unexplained pass, no stored-value ownership proof on a pinned path, no weakened refusal
- [x] #5 Full existing enroll/adopt/reconcile/spawn suites green + manual pass: a reclaimed seat is spawn-capable after completion with no fallback branch firing
<!-- AC:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-17 04:38
---
Design checkpoint APPROVED (after one amendment round). Builder's note covered package/API shape, per-seat-kind sequences, refusal matrix, and the binding-fact write spine — conformant to the plan's settled decisions on all axes. Orchestrator raised 3 corrections, all resolved in the amendment: (1) spawn bind expiry reclassified as an observation window — teardown only on positive death/no-occupant evidence; live-but-slow or unknown-liveness children keep their pane with a retryable refusal, and the sidecar's late correlated recognition converts to the shared completion step so slow binders still mint atomically (all three cells test-pinned); (2) adopt wrong-nonempty launch-context refusal ships with a terminating ordinary remedy (stop wrong vendor row -> hcom start --as -> herder enroll) and names the reclaim-guard shape as upstream-gated pointing at the durable hazards-doc recovery recipe; (3) normalizer binding-fact requirement scoped to seated-state establish/change; lifecycle seat-clearing exempt from fact creation but not history carry; seated partial clears refused. Hygiene re-confirmed (no stage letters/identifiers in code). Code unlocked.
---

created: 2026-07-17 06:04
---
Adversarial review round 1 (incumbent opus + grok calibration seat, shared worktree under serialized mutation slots — both held/released clean, byte-clean restores verified): FIX ROUND REQUIRED. Ten findings consolidated, all mapping to checkpoint/contract text: 4 P1 (sidecar hand-builds Verified:true around the resolver — multi-match chooses + empty-name admits; registry infra errors route around the occupant-liveness gate and tear down live children; normalizer bus guard short-circuits on cleared/demoted projections; observer reconfirm wired to a zero ObservedBus so real changes refuse), 2 P2 (WriteNoop latches as sidecar success killing the designated retry recovery + feeds the teardown P1; empty-id/uniqueness guards + several hostile matrix cases unpinned — reviewer-executed deletions left the suite green), 4 P3 (contracted source-inventory tests absent; attested arm unvalidated when bus verifies; seat-pointer alias in carry; dead raw-mint helpers). Incumbent explicitly verified clean: creator-provenance/sid-harvest goldens preserved at equal strength, three-cell teardown gate itself correct (positive-evidence only), adopt corridor honest, hygiene clean, core append-only pins real (deliberately broken, all caught). Both grok P1s orchestrator-verified in code before inclusion (calibration protocol). Fix round dispatched; builds held for a host quiesce window.
---

created: 2026-07-17 07:19
---
MERGED efd80bc (--no-ff, pushed); post-merge battery on main green 61/61 + 4 modules (tail read, counted). Final head 919a655 = design commit + 10-finding hardening + lifecycle micro fix. Full chain: design checkpoint (approved with 3 amendments) -> build red-first -> review round 1 (incumbent opus + grok calibration, 10 consolidated findings incl. 4 P1) -> fix round -> delta (incumbent APPROVE with P1->P2 self-correction; calibration found the lifecycle occupant-gate sibling, orchestrator-verified) -> micro fix round -> final delta APPROVE x2 (17 incumbent mutation probes total; every pin verified red-on-revert incl. the two source-inventory tripwires by real violation). Orchestrator gates: independent battery at DONE, final-head battery at 919a655, identifier sweep clean (zero matches both patterns), post-merge battery on main — all green 61/61. Notable: fix rounds DELETED the raw row-minting helpers rather than bypassing them; both teardown corridors now report registry state honestly; sidecar production+test paths consolidated onto the resolver-backed route. Two non-blocking advisories filed as a follow-up task (observer turnover child born factless via legacy exemption — self-heals; lifecycle pre-completion conversion branch ungated — defensively unreachable). Upstream-candidate sweep: none new (unit fully herder-internal). Battery count unchanged at 61.
---
<!-- COMMENTS:END -->

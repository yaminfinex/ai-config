---
id: TASK-270
title: >-
  Implement: canonical rebirth — one shared seat-completion step (identity
  migration U1)
status: In Progress
assignee: []
created_date: '2026-07-17 04:26'
updated_date: '2026-07-17 04:28'
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
- [ ] #1 Design checkpoint note approved by orchestrator BEFORE any code (package shape, per-seat-kind completion sequence, refusal matrix, write-spine field + binding-id design)
- [ ] #2 All six creation/recovery verbs terminate in the shared completion step; cross-verb row-shape parity suite proves byte-equivalent seat shapes for the same live facts across paths, for all three seat kinds (herdr/process/busless)
- [ ] #3 Every plan §U1 test scenario implemented and green: reclaimed-row backfill then spawn-capable; bus-row-absent refusal with no partial row; busless bash completes without bus facts; headless process seat completes on pid+bus; pane_conflict carried not rewritten; two-bus-row multi-match fail-closed; write-spine projection preservation + typed outcome + rotation survival; mutation-armed pins on every admitting path of the completion predicate
- [ ] #4 Keep-list re-audit of the final diff: no widened admitting predicate, no unexplained pass, no stored-value ownership proof on a pinned path, no weakened refusal
- [ ] #5 Full existing enroll/adopt/reconcile/spawn suites green + manual pass: a reclaimed seat is spawn-capable after completion with no fallback branch firing
<!-- AC:END -->

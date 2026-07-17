---
id: TASK-272
title: >-
  Implement: minted per-seat credentials — env demoted to hints (identity
  migration U3)
status: To Do
assignee: []
created_date: '2026-07-17 04:28'
labels:
  - herder
  - identity-migration
dependencies:
  - TASK-270
  - TASK-271
priority: medium
ordinal: 271500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
IMPLEMENT unit, stage 3 of the ratified identity migration. GROUND TRUTH (settled, double-reviewed + owner-ratified — do NOT relitigate; deviations are STOP-AND-REPORT to @hera, read this twice): docs/design/2026-07-17-identity-migration-plan.md §U3 and docs/design/2026-07-17-identity-architecture-target.md (T1; §3.1 rotation commit protocol; trust boundary). Memo §4 keep-list = HARD constraints. DEPENDS ON stage 1 (issuance point = completion step) and stage 2 (reissue-credential recovery surface pre-landed). Explicitly NO dependency on the epoch stage — credential generations are seat-local.

Goal: herder mints a random per-seat token at every completion, bound to (guid, credential generation), delivered via seat-scoped permission-restricted credential file (NOT env); herder verbs authenticate callers by token; HCOM_*/HERDER_*/HERDR_* demote to diagnostics and birth provenance. Honest boundary (settled): unforgeable-by-inheritance, not unforgeable-by-intent — deliberate same-uid file reads are accepted documented posture; the stage deletes the accidental/ambient impersonation class.

SETTLED DECISIONS: rotation commit protocol per architecture §3.1 — immutable generation-keyed token file written+fsynced FIRST; locked registry append flipping the row's credential generation is the SOLE commit point; verification checks presented token against registry-current generation (registry = generation truth, file = possession only); orphan staged files GC'd lazily by later completions, never in the transaction; no crash point strands a seat with zero working generations. Identity selection order is NORMATIVE: credential selects the acting identity (credential → guid → registry row); ambient correlates only VERIFY the selected row's bus binding, mismatch refuses, ambient never re-selects on a cut-over verb. MIGRATION GATE: issuance sweep mints tokens for all currently seated rows before any verb cuts; per-verb cutover order = sender-fence surfaces first (spawn/send), then lifecycle verbs (adopt, cull, compact, enroll); once a verb cuts, NO env fallback; refusals on token-less legacy seats name the issuance remedy. Token-loss recovery is ONLY reissue-credential on the break-glass surface (proof pool disjoint from the missing token) — prescribing a credential-gated verb as its own recovery is forbidden by construction. Cutover inventory: the plan's eight CurrentEvidence call sites across five packages + two env-construction surfaces is the design-time floor — RE-RUN the inventory at your HEAD. State-dir/HOME/worktree variance and harness cases are an explicit design-checkpoint item, not assumed away.

DESIGN CHECKPOINT REQUIRED BEFORE CODE (token path scheme incl. HOME/worktree variance, verification API, cutover sequencing, sweep mechanics). Adversarial review is orchestrator-dispatched after your DONE; do not arrange reviewers yourself. The orchestrator's bus address is @hera; there is no @orchestrator alias. Commit on your unit branch before DONE. Hygiene: no agent names, task numbers, run identifiers, or SHAs in durable text.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Design checkpoint note approved by orchestrator BEFORE any code
- [ ] #2 Every plan §U3 test scenario implemented and green, including: env-only child selects nothing; deliberate same-uid read documented-accepted with audit surface; previous-generation token refused; token-present-env-scrubbed succeeds; poisoned-correlates-plus-valid-token pins selection order; legacy-seat pre/post-cutover refusal naming remedy; token-loss end-to-end through reissue-credential; all three crash-point drills with exactly-one-working-generation asserted at every point; spawned-child battery simulation acts as itself; permission + no-token-in-env/registry checks
- [ ] #3 Cutover inventory re-run at unit HEAD and reconciled against the design-time floor; issuance sweep verified on a fixture registry with pre-cutover seated rows before the first verb cut
- [ ] #4 Poisoned-env harness run over the full cut-over verb inventory: zero caller-attribution successes from inherited env; scrubbed-env run fully green; launcher-family HCOM_* scrub tests still pin
- [ ] #5 Keep-list re-audit of the final diff; per-verb rollback story (revert verification to ambient) documented
<!-- AC:END -->

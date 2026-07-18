---
id: TASK-272
title: >-
  Implement: minted per-seat credentials — env demoted to hints (identity
  migration U3)
status: Done
assignee: []
created_date: '2026-07-17 04:28'
updated_date: '2026-07-18 04:36'
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
- [x] #1 Design checkpoint note approved by orchestrator BEFORE any code
- [x] #2 Every plan §U3 test scenario implemented and green, including: env-only child selects nothing; deliberate same-uid read documented-accepted with audit surface; previous-generation token refused; token-present-env-scrubbed succeeds; poisoned-correlates-plus-valid-token pins selection order; legacy-seat pre/post-cutover refusal naming remedy; token-loss end-to-end through reissue-credential; all three crash-point drills with exactly-one-working-generation asserted at every point; spawned-child battery simulation acts as itself; permission + no-token-in-env/registry checks
- [x] #3 Cutover inventory re-run at unit HEAD and reconciled against the design-time floor; issuance sweep verified on a fixture registry with pre-cutover seated rows before the first verb cut
- [x] #4 Poisoned-env harness run over the full cut-over verb inventory: zero caller-attribution successes from inherited env; scrubbed-env run fully green; launcher-family HCOM_* scrub tests still pin
- [x] #5 Keep-list re-audit of the final diff; per-verb rollback story (revert verification to ambient) documented
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged to main at 85e8214 (branch task-272-credentials: mint commit + cutover-fence hardening round). Post-merge gate 61/61 scripts + 4 module passes.

AC#1: design checkpoint + addendum approved as design-of-record before code; release-time inventory delta posted and ratified before implementation; builder self-caught a GC-timing deviation pre-commit (architecture over checkpoint — retained-dead prior generation, later-Stage locked GC) and it was triaged and pinned.

AC#2: full scenario suite green — env-only selects nothing; same-uid read accepted with guid+generation audit surface; stale generation refuses (with non-secret recovery command after the fix round); scrubbed-env + token succeeds; poisoned correlates + valid token refuse-not-reselect; legacy refusals name remedies; token loss blocks with reissue remedy (never silent remint); three crash drills leave exactly one working generation; permission/symlink/euid enforcement; token absent from env/registry/child listings.

AC#3: inventory re-run at merged HEAD (9 production CurrentEvidence calls) and again at final HEAD (15; all 6 additions independently verified as credential-selected verification gates by both reviewers); issuance sweep fixture proves literal-100% coverage before the first cutover verb.

AC#4: poisoned-env harness zero caller-attribution successes across send/spawn/adopt/cull/compact/enroll; scrubbed run green; launcher scrub pins hold.

AC#5: keep-list re-audit clean (refuse-not-mint, fail-closed multi-match, no ambient selection, unforgeable-by-inheritance); per-verb rollback documented plus deliberate time-bounded cutover-marker rollback guidance.

Review: dual adversarial (opus incumbent + grok calibration seat), one fix round of five contract-mapped items (locked-enroll ambient-reselect under cutover — grok-found, orchestrator-verified; O_EXCL immutability pin; stale-refusal remedy; cutover marker absence-vs-tamper fail-closed; adopt unseated-target fresh-self alignment), dual delta APPROVE with red-verified mutations on every fix.

ROLLOUT NOTE: merging does NOT flip the cutover — the marker is created only by `herder credential sweep` reaching literal-100% coverage then explicit enable; owner-paced per the rollout docs in the diff.
<!-- SECTION:NOTES:END -->

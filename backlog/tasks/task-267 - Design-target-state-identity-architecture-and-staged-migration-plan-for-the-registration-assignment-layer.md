---
id: TASK-267
title: >-
  Design: target-state identity architecture and staged migration plan for the
  registration/assignment layer
status: In Progress
assignee: []
created_date: '2026-07-17 02:19'
updated_date: '2026-07-17 02:52'
labels:
  - herder
  - design
dependencies: []
priority: high
ordinal: 266500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner-ordered follow-on to the accepted registration-brittleness investigation (memo: napkins/run-herder-dx/registration-brittleness-memo.md, acceptance record on task-266). A Fable-lane design unit produces (a) the target-state identity architecture — compatible with the memo's long-horizon binding-events direction, with every root cause H1-H7 neutralized or explicitly residual — and (b) a staged migration plan under the ce-plan discipline: stages evaluated are canonical rebirth, attested break-glass repair, minted per-seat credentials, evidence-based liveness consolidation, epoch-stamped coordinates; each stage independently shippable, ordered with rationale, zero dependency on upstream shipping. The memo §4 keep-list is inlined as hard invariants; the ambient-SID harvest fix runs as a separate parallel implement unit (task-244) and the design targets the post-fix state. Unit also promotes the memo to docs/design/ with provenance (it is single-copy in gitignored napkins). Chain per design-task pattern: designer → adversarial design review (cross-family) → memo-author intent-holder sanity check → fresh-eyes offer → owner ratification → task breakdown. Brief: napkins/run-herder-dx/designer-identity-brief.md
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Target-state architecture doc in docs/design/ with provenance header; each memo root cause (H1-H7) neutralized or explicitly accepted as residual; keep-list invariants preserved verbatim
- [ ] #2 Staged migration plan (ce-plan discipline) with independently-shippable ordered stages, per-stage invariant + verification story + blast-radius honesty, no upstream dependency; upstream-blocked residuals marked
- [ ] #3 Investigation memo promoted to docs/design/ with provenance header, content otherwise unchanged
- [ ] #4 Adversarial design review and memo-author intent-holder check passed; owner ratification received before any task breakdown
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Designer DONE 2026-07-17 (commit 68abf3f, branch task-267-identity-design): target architecture (T1-T6 invariants, plane-by-plane, H1-H7 disposition), migration plan (U1-U5: rebirth, break-glass, credentials, observer liveness, epochs — each independently shippable, no upstream dependency), memo promoted with provenance (byte-identical verified). Designer verified read-only that herdr exposes no server-generation id at protocol 16; epoch stage ships on spec-sanctioned probe inference + process-incarnation fingerprint. Orchestrator read both docs in full; verified commit + memo diff. Adversarial design review dispatched cross-family (codex 5.6 high) with designer-nominated attack surfaces (break-glass proof forgeability, credential real-cut availability split, epoch false-stability) + write-spine pre-trace + independent re-verification of the negative claim. Chain: review -> fix rounds -> memo-author intent-holder check -> fresh-eyes offer -> owner ratification -> task breakdown.
<!-- SECTION:NOTES:END -->

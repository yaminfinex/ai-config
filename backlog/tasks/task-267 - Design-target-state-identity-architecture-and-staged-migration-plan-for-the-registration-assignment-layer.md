---
id: TASK-267
title: >-
  Design: target-state identity architecture and staged migration plan for the
  registration/assignment layer
status: In Progress
assignee: []
created_date: '2026-07-17 02:19'
updated_date: '2026-07-17 04:16'
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

Round-1 adversarial design review (cross-family incumbent): FIX ROUND REQUIRED — 4 P1 (break-glass proof forgeable by same-uid automation via herdr pane API; T3 termination claim false for seat-coordinate damage + pane_conflict fence collision; credential same-uid possession boundary + epoch ordering + CurrentEvidence smuggling inventory; T6 latest-binding-wins undermines fail-closed fences without an evidence lattice), 4 P2 (epoch write path is projection-only — no locked append API exists, verified by write-spine trace; U1 breaks busless/process seats and U4 sole-authority contradicts spec whoever-observes-first; U5 fingerprint false-stability via proxy/fd-handoff — fallback demanded: unverifiable incarnation = epoch unknown = reconcile; U1 dependency honesty re the in-flight harvest fix), 1 P3 (break-glass field scope broadened beyond memo R2 without flagging — orchestrator-verified). Reviewer independently re-verified the herdr generation-id negative on an isolated server. Findings verbatim at napkins/run-herder-dx/review-267-findings-r1.md; fix round dispatched with triage (owner-decision points to be marked in-doc, both branches written). Reviewer holds for delta.

Delta review round 2: FIX ROUND 2 REQUIRED — four residuals (Branch A verifier writable by defended-against uid + Branch B tamper-evidence overclaim: owner-decision point not ratification-ready until both branches honest at the trust boundary; token-loss recovery circular — credential-gated verb prescribed as its own credential recovery, the H5 class re-entering the new design; lattice class-dominates-recency blocks attested correction of stale live-verified bindings — correction/tombstone semantics + real admitting matrix demanded; terminal-id same-set reuse defeats the probe backstop — reuse/permutation drill demanded). Round-1 items otherwise verified closed; reviewer independently exercised the vendor-row recreate corridor on isolated hcom 0.7.23 (holds; reclaim-guard honestly named as the one upstream-gated non-terminating shape). Fix round 2 dispatched.

Fix rounds 3+4: rotation commit protocol (registry generation flip = sole commit point; immutable generation-keyed staged token, fsync discipline, lazy orphan GC; exhaustive crash analysis — exactly one working generation at every crash point) and durable binding IDs (persisted in row JSON, load/rotation-derived values named as excluded class; in-row append-only histories survive reseed by construction — reviewer verified against migration.go reseed code; correction-through-rotation pins). Round-4 micro: replay assertions rescoped strictly post-commit-point (reviewer-supplied rewrite adopted exactly). FINAL DESIGN VERDICT: APPROVE at 3c0018e (4 rounds total). Dirty-tree note withdrawn by reviewer. Intent-holder check dispatched to the memo author (last gate before owner ratification + fresh-eyes offer). ONE owner decision pending: break-glass trust anchor (Branch A verifier-integrity anchor menu vs Branch B same-uid posture reduction, tripwire-not-wall).

Intent-holder verdict (memo author): CONCUR-WITH-NOTES at 3c0018e. Faithfulness confirmed — no place where the design claims the memo supports something it does not; both letter-departures labeled and endorsed (reissue = the anti-circularity finding applied prospectively; T4 recast to one-predicate-many-appliers endorsed: evidence-basedness was the load-bearing part, centralization was not). Sole substantive ask (round 5, dispatched): T6 lattice absent-vs-unavailable condition — absence = consulted-successfully-no-match, never source-unavailable (else history adjudication arms during recorded outage windows). Recorded observations for owner scoping: U1+U2 alone retire the season's two dominant costs (spawn-dead class + repair-loop operator tail); the record contains zero deliberate same-uid adversaries (every recorded impersonation was ambient/inherited) so Branch B suffices on the evidence, Branch A is an additive posture upgrade; §3.5 transport invariants = simple direct-dial check in implementing units, not detection machinery.

CHAIN COMPLETE at 4dd9d9d: adversarial design review final APPROVE (6 rounds); intent-holder CONCUR-WITH-NOTES, sole condition discharged and confirmed (absent-vs-unavailable lattice rule landed + verified by both). Merge-readiness verified: docs-only diff proven (3 files, 0 non-docs paths), identifier sweep clean (single match = the promoted memo's own provenance citation, reviewer-verified non-load-bearing). AC#1-3 satisfied; AC#4 awaits owner ratification. Presented to owner with the ONE decision (break-glass trust anchor) + scoping observations + fresh-eyes offer. Seats held pending ratification: designer (amendments), incumbent reviewer (delta on amendments), memo author.

OWNER RATIFIED 2026-07-17: Branch B (same-uid posture reduction, tripwire-not-wall) ruled for the break-glass trust anchor. Owner rationale verbatim: takeover is always by accident and repairing should be easy and trusted; low stakes; no complexity for failure scenarios that are not real. Execution (ratification commit, merge, Done flip, task breakdown U1-U5) proceeds post-compact per resume steer.
<!-- SECTION:NOTES:END -->

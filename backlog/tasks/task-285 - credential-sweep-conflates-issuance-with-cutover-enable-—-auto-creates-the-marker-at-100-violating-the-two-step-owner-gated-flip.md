---
id: TASK-285
title: >-
  credential sweep conflates issuance with cutover enable — auto-creates the
  marker at 100%, violating the two-step owner-gated flip
status: Done
assignee: []
created_date: '2026-07-18 13:51'
updated_date: '2026-07-18 14:56'
labels:
  - herder
  - bug
dependencies: []
priority: high
ordinal: 284500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field incident 2026-07-18 (twice in one day): credentialcmd sweep, after reaching 100% coverage, unconditionally calls seatcred.EnableCutover — so ANY successful sweep flips the fleet to credential-authenticated verbs. The ratified design and rollout docs describe two separate steps: sweep = issuance only (behavior-neutral, safe to run operationally), explicit enable = the owner-gated flip. Consequence today: the orchestrator ran the sweep twice as an operational unblock (documented as behavior-neutral) and unknowingly flipped the cutover both times while a registry carry defect was live-stripping generations, forcing two emergency marker rollbacks; the operator-facing confusion (verbs suddenly demanding --credential-file) was attributed to the owner having flipped it, which never happened. Fix: sweep stops at the coverage report (exit 0 at 100%, naming the enable command as the next step); a separate explicit subcommand (e.g. herder credential enable) creates the marker, refusing below 100% coverage. Contract suite pins: sweep at 100% does NOT create the marker; enable refuses below 100%; enable creates it at 100%. Docs updated to match. NOTE: the credential-DX design (approved, pending owner sign-off) already assumes the two-step contract — this fix restores the implementation to the ratified design; coordinate merge order with the carry fix so re-enable happens only after both.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged to main at 6b441c5 (split commit + pin-depth round; production byte-identical across the round). Post-merge gate 63/63 + 4 module passes (battery grew 62→63: check-credential-contract.sh joined via this unit). Sweep at 100% now exits 0, names herder credential enable, never creates the marker; enable is the sole production EnableCutover caller, recomputes coverage independently (per-seat Authenticate — token loss blocks with reissue+sweep remedies and blocker lines), refuses below literal 100%, creates the owner-only marker at 100%. Pins: auto-enable reintroduction reddens unit+contract; Authenticate-skip reddens unit+contract (both sweep and enable paths guarded); blocker lines pinned. Review: dual adversarial (opus + grok calibration), dual APPROVE, one pin-depth mini round driven by the grok seat's executed Authenticate-skip mutation, dual delta APPROVE with independent re-execution. Known pre-existing informational: sub-second read→write window inside enable itself (no lock) — unchanged from prior structure, backlog-noted. RE-ENABLE PATH now safe: fresh sweep (report-only) → soak → owner runs herder credential enable.
<!-- SECTION:NOTES:END -->

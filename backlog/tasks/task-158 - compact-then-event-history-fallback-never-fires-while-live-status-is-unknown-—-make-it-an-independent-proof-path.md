---
id: TASK-158
title: >-
  compact-then: event-history fallback never fires while live status is unknown
  — make it an independent proof path
status: Done
assignee: []
created_date: '2026-07-12 06:52'
updated_date: '2026-07-12 08:31'
labels: []
dependencies: []
priority: medium
ordinal: 157000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
From the compact-then proof investigation (memo docs/design/2026-07-12-compact-then-proof-failure-investigation.md): the delivery predicate gates turnEndedSince behind status==listening, so a session whose live status reads unknown never has its event history consulted — even when a valid post-arm listening event exists under the queried identity. Also: the timeout log line reports event_proof=true when it means only snapshot-established (a zero watermark from an unknown agent reads as trusted empty history). FIX: consult turnEndedSince whenever the arm watermark is trusted, regardless of live status; keep the strict post-arm event-ID comparison; split the diagnostics into snapshot_established and turn_end_event_found. A characterization test pinning the current gated behavior ships with the investigation merge and must be INVERTED by this fix.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Unknown live status plus a matching post-arm listening event proves turn end and delivers exactly once
- [x] #2 Unknown live status without a matching event still fails closed; pre-arm and same-watermark events never deliver
- [x] #3 Logs report snapshot_established and turn_end_event_found separately; zero watermark is never called event_proof=true
- [x] #4 The shipped characterization test of the gated fallback is inverted, not deleted
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
SHIPPED, merged 462787f (branch task-158-event-proof, commit 6ec0d73). The detached sender now computes turn-end event proof whenever the arm watermark is trusted, independent of live status: unknown status + a strict post-arm listening event (event.ID > watermark, same-ID excluded) delivers once; missing/pre-arm/same-watermark events and wrong identity still fail closed. The poisoned-arm signature is closed at the boundary: empty event output is trusted as watermark (0, trusted) ONLY when a scoped hcom list confirms the agent exists — unknown agents and transient list failures arm untrusted (proof path disabled, fail-closed). Diagnostics split snapshot_established from turn_end_event_found; event_proof=true is gone; zero watermark is never labeled as proof. Characterization tests from the investigation handled per spec: wrong-coordinate test STILL asserts fail-closed timeout + zero deliveries (the fix correctly does not rescue wrong identities — A1's arm preflight owns that); suppressed-event-proof test inverted to assert real delivery through send.DeliverBus, plus a new hermetic CLI contract scenario + golden. At-most-once is structural (single deliver call, mutually-exclusive proof branches). 15m timeout unchanged. Adversarial review (opus, cross-family): APPROVE, all lenses; one non-blocking observation recorded (known-agent transiently-empty events read as trusted zero — matches spec, strictly tighter than pre-fix). Gates: independent 4-module + 53-script battery green from the worktree; post-merge battery green on main.
<!-- SECTION:NOTES:END -->

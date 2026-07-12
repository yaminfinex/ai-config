---
id: TASK-158
title: >-
  compact-then: event-history fallback never fires while live status is unknown
  — make it an independent proof path
status: In Progress
assignee: []
created_date: '2026-07-12 06:52'
updated_date: '2026-07-12 08:06'
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
- [ ] #1 Unknown live status plus a matching post-arm listening event proves turn end and delivers exactly once
- [ ] #2 Unknown live status without a matching event still fails closed; pre-arm and same-watermark events never deliver
- [ ] #3 Logs report snapshot_established and turn_end_event_found separately; zero watermark is never called event_proof=true
- [ ] #4 The shipped characterization test of the gated fallback is inverted, not deleted
<!-- AC:END -->

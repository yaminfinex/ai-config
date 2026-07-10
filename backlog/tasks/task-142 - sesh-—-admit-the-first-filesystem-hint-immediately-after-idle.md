---
id: TASK-142
title: sesh — admit the first filesystem hint immediately after idle
status: To Do
assignee: []
created_date: '2026-07-10 01:39'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 142000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Evidence and full context: backlog doc-001 (sesh shipper and ingest efficiency findings).

The fixed two-second minimum interval between hint-admitted shipper passes prevents runaway passes under continuous writes, but makes an isolated save wait behind the same cooldown even when the shipper has been idle — measured isolated append-to-ACK moved from ~25ms to ~1.3s mean when the interval landed.

Settled decisions:
- Every admitted pass remains the complete RunOnce; there is no dirty-file fast path.
- The sustained ceiling is start-to-start and remains two seconds by default.
- No new public configuration flag is added.
- Prefer a testable timer/clock state machine over sleeps embedded across select branches.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The first filesystem hint after at least one admission interval without hint-driven work starts a full authoritative pass immediately, subject only to scheduling
- [ ] #2 Under continuous writes, authoritative pass starts remain no closer than the configured two-second interval
- [ ] #3 A burst during the cooldown creates at most one pending pass; no unbounded queue or catch-up storm
- [ ] #4 Periodic rescans, store backoff, cancellation, watcher overflow handling, and directory registration retain their current guarantees
- [ ] #5 Deterministic clock-based tests cover idle first hint, save burst, continuous appends, periodic tick racing a pending hint, backoff, and shutdown
- [ ] #6 Isolated append-to-ACK latency is below 250ms on the representative tree while sustained CPU stays within 10% of fixed-interval admission
<!-- AC:END -->

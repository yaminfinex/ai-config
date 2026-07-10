---
id: TASK-142
title: sesh — admit the first filesystem hint immediately after idle
status: Done
assignee: []
created_date: '2026-07-10 01:39'
updated_date: '2026-07-10 02:29'
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
- [x] #1 The first filesystem hint after at least one admission interval without hint-driven work starts a full authoritative pass immediately, subject only to scheduling
- [x] #2 Under continuous writes, authoritative pass starts remain no closer than the configured two-second interval
- [x] #3 A burst during the cooldown creates at most one pending pass; no unbounded queue or catch-up storm
- [x] #4 Periodic rescans, store backoff, cancellation, watcher overflow handling, and directory registration retain their current guarantees
- [x] #5 Deterministic clock-based tests cover idle first hint, save burst, continuous appends, periodic tick racing a pending hint, backoff, and shutdown
- [x] #6 Isolated append-to-ACK latency is below 250ms on the representative tree while sustained CPU stays within 10% of fixed-interval admission
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Fixed on branch sesh-shipper-efficiency (1aa0464, shipper lane worker, orchestrator-verified). Sleep/debounce sequencing replaced with a private hintAdmission state machine: first hint after an idle interval admits immediately; cooldown hints coalesce to one start-to-start deadline; periodic ticks, pending hints, and store retries share one pending admission so races produce one full RunOnce. Store backoff is a hard not-before floor for all admission classes (conservatively stronger than old selectable-delay); periodic ticks still run watchDirs immediately even while a retry is delayed, preserving the 60s nested-directory recovery guarantee; cancellation precedence pinned by test after a repeated-latency run exposed a retry spin under zero backoff. Sliding door (disclosed): the 200ms idle debounce was removed rather than layering a fast path — burst AC still holds (at most one pending pass). No config surface; 2s ceiling internal. Measured: isolated append->ACK 7.5-9.1ms on a real 750-file daemon (AC <250ms; was ~1.3s mean); 60s deterministic continuous hints: 30 admissions adaptive vs 30 fixed (0% delta vs <=10% AC); race detector green. Orchestrator re-ran pinned gate uncached on the full three-commit lane tree: all packages + check scripts green. Shipper lane implementation complete; cross-family review before hera handoff.
<!-- SECTION:NOTES:END -->

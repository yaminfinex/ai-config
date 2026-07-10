---
id: TASK-144
title: sesh — shut down the store through its context
status: Done
assignee: []
created_date: '2026-07-10 01:39'
updated_date: '2026-07-10 02:32'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 144000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Evidence and full context: backlog doc-001 (sesh shipper and ingest efficiency findings).

The store command (sesh serve) currently takes the default SIGTERM action, so main does not unwind and deferred cleanup never runs — discovered when a CPU profile's deferred flush produced a zero-byte file on service stop. Any deferred hook (profiling/telemetry flushes, future cleanup) is silently skipped on operator-requested termination.

Settled decisions:
- Use the same signal-context ownership pattern as the shipper.
- Do not add a remote shutdown endpoint.
- Preserve fsync-before-ACK and let already-entered store critical sections finish or fail explicitly before listener teardown completes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 SIGINT and SIGTERM cancel the store context and return through command execution
- [x] #2 Ingest and surface listeners close, the index consumer exits, the database closes, and deferred cleanup hooks run before process exit
- [x] #3 Shutdown is bounded and returns success for an operator-requested signal
- [x] #4 Tests prove both listeners stop accepting, in-flight durable writes either ACK or fail without a false ACK, and no goroutine/process remains
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Fixed on branch sesh-store-efficiency (974abcc, store lane worker, orchestrator-verified). sesh serve owns SIGINT/SIGTERM via signal.NotifyContext (same pattern as ship); operator cancellation returns success, unexpected listener failures stay errors. Local and tsnet modes share one coordinated serveHTTP helper: both listeners stop accepting, http.Server.Shutdown waits for active handlers with a 10s bound and forced Close on timeout. Index consumer runs on context.WithoutCancel so cancellation cannot abort indexing of a durably-ACKed event; after HTTP drain it drains queued events FIFO via StopAndWait (own 10s bound, explicit timeout error), then the DB close defer unwinds. No remote shutdown endpoint; mirror fsync-before-ACK untouched. Child-process SIGINT/SIGTERM tests prove return-through-command-execution with post-return markers and refused ports; in-flight PUT test proves 200 only after durable mirror; consumer test proves queued ACKed event indexed before StopAndWait returns. Orchestrator re-ran pinned gate uncached: all packages + check scripts green; worker also ran -race and -count=5 on cli green. Store lane implementation complete; cross-family review before hera handoff.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-07-10 02:32
---
AMENDED after cross-family review: 974abcc -> 8d068a9. Finding 1 (drain-timeout stranded ACKed events unindexed, dirty=0) fixed: StopAndWait dirty-marks still-buffered generations, mark failures joined onto the timeout error, regression test proves dirty=1; errors.Join on both RunE paths; txIdx constraint comment. Reviewer confirmed closed, no new hazards; lock ordering deadlock-free vs single-conn pool. Accepted residuals: non-ctx mutex can extend a timed-out shutdown by the in-flight event (finite via busy_timeout); post-Close straggler sliver healed by shipper replay. Review also confirmed 143 fixes a pre-existing empty-UUID duplicate-on-retry bug. Orchestrator gate on amended lane: green.
---
<!-- COMMENTS:END -->

---
id: TASK-144
title: sesh — shut down the store through its context
status: To Do
assignee: []
created_date: '2026-07-10 01:39'
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
- [ ] #1 SIGINT and SIGTERM cancel the store context and return through command execution
- [ ] #2 Ingest and surface listeners close, the index consumer exits, the database closes, and deferred cleanup hooks run before process exit
- [ ] #3 Shutdown is bounded and returns success for an operator-requested signal
- [ ] #4 Tests prove both listeners stop accepting, in-flight durable writes either ACK or fail without a false ACK, and no goroutine/process remains
<!-- AC:END -->

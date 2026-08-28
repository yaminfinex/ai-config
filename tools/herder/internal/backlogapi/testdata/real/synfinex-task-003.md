---
id: TASK-003
title: Build the in-process threaded-pipeline throughput and tail harness
status: To Do
assignee: []
created_date: '2026-06-30 03:13'
labels:
  - single-sequencer
  - harness
  - performance
  - latency
  - instrument
dependencies: []
documentation:
  - doc-003
priority: high
ordinal: 3000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The campaign's only throughput instrument drives the engine single-threaded and is blind to everything between the threads of the real pipeline: cross-thread handoff cost, wake/park overhead, which stage is the bottleneck, and end-to-end tail. This builds an in-memory benchmark that drives the production threaded pipeline (run_core) directly, feeding the fanout ring from a pre-generated command stream and draining the ack ring to a discard sink, with no TCP, driver, or kernel sockets in the loop. It is the prerequisite instrument that turns the wake-batching, encode-off-thread, and outbox-backed-replay levers from unmeasurable into gated.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Drives run_core from memory: fanout ring fed from the in-memory skew-scenario stream, ack ring drained to a byte-discard sink, outbox backed by discard or tmpfs; no TCP/driver/kernel sockets in the loop.
- [ ] #2 Reports end-to-end steady-state throughput and the full latency distribution (median through p99.9) from fanout enqueue to ack dequeue, with the tail as a first-class number.
- [ ] #3 Reports per-stage busy-versus-parked occupancy for journaler, matching, publisher, and writer -- the decisive number for whether matching is CPU-bound or stalled, and which stage is the real ceiling.
- [ ] #4 Reports ring-pressure and backstop counters: per-ring occupancy and full/empty events, per-stage park-backstop entry count and time parked, wake counts split by genuinely-parked vs already-running, and a writer per-flush batch-size histogram.
- [ ] #5 All timestamps come from one monotonic clock source; busy is strictly work and parked is strictly waiting, with no conflation of internal busy time and blocked-on-ring time.
<!-- AC:END -->

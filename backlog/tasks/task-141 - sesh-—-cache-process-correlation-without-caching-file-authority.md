---
id: TASK-141
title: sesh — cache process correlation without caching file authority
status: Done
assignee: []
created_date: '2026-07-10 01:39'
updated_date: '2026-07-10 02:14'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 141000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Evidence and full context: backlog doc-001 (sesh shipper and ingest efficiency findings).

Every authoritative shipper pass repeats a full /proc scan even though owner correlation is best-effort enrichment and positive observations are never retracted. Measured: correlation is 14.58ms of a 20.92ms quiescent pass (70%); sustained profile attributes 25% of shipper CPU to CorrelateAll.

Settled decisions:
- Cache correlation results/process observations only; never cache Discover, file size, fingerprint, cursor authority, or shipping decisions.
- Default TTL is 10 seconds and is internal, not a CLI/config surface.
- Identity-set growth invalidates enough cache state to attempt the new identity promptly.
- I8 remains unchanged: observations are remembered and never retracted.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Repeated passes over an unchanged discovered identity set perform at most one full correlation sweep per 10-second TTL
- [x] #2 A newly discovered identity without a persisted owner forces an immediate correlation attempt rather than waiting for TTL expiry
- [x] #3 Cached positive observations may be recorded, but absence never erases a persisted owner and cross-user /proc reads remain forbidden
- [x] #4 Expiry, PID churn, unreadable proc entries, and process death preserve honest absence and never stop byte shipping
- [x] #5 Linux correlation tests cover cache hit, expiry, identity-set change, owner persistence, PID reuse/churn, and same-cwd ambiguity; Darwin remains facts-only
- [x] #6 A representative 750-file benchmark reduces correlation CPU by at least 70% across five two-second passes, with every pass still executing full discovery and cursor work
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Fixed on branch sesh-shipper-efficiency (1fb4377, shipper lane worker, orchestrator-verified). Linux procCorrelator caches identity->positive-owner observations for an internal 10s TTL (private ttl/clock, no CLI/config surface). Any identity-set growth — including shrink-then-reappear — forces an immediate full sweep, conservatively stronger than the AC's ownerless-new-identity case. Cache hits filter cached positives to the current discovered set; absence never mutates a persisted owner (I8 intact); cache lives strictly inside the correlator so Discover/stat/fingerprint/cursor/ACK are structurally uncacheable. Darwin facts-only untouched (cross-build verified). Coverage: hit, exact expiry, growth-before-expiry, PID death/reuse churn, positive-then-expired absence, persisted-owner-survives-absence, plus all pre-existing cross-user wall and ambiguity tests. Measured: 750-identity five-pass benchmark 79.6-80.8ms -> 15.8-17.7ms per pass (77.7-80.5% wall vs 70% AC); matched 30x profiles 2.92s -> 0.75s CPU (74.3%); integration proof ships bytes on a cache-hit pass with scan count 1. Orchestrator re-ran pinned gate uncached: all packages + check scripts green. Worker deviation (procedural only, disclosed immediately): a sed range printed adjacent backlog prose; no cross-unit work occurred. Merge pending lane review + hera handoff.
<!-- SECTION:NOTES:END -->

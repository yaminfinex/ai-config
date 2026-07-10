---
id: TASK-141
title: sesh — cache process correlation without caching file authority
status: To Do
assignee: []
created_date: '2026-07-10 01:39'
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
- [ ] #1 Repeated passes over an unchanged discovered identity set perform at most one full correlation sweep per 10-second TTL
- [ ] #2 A newly discovered identity without a persisted owner forces an immediate correlation attempt rather than waiting for TTL expiry
- [ ] #3 Cached positive observations may be recorded, but absence never erases a persisted owner and cross-user /proc reads remain forbidden
- [ ] #4 Expiry, PID churn, unreadable proc entries, and process death preserve honest absence and never stop byte shipping
- [ ] #5 Linux correlation tests cover cache hit, expiry, identity-set change, owner persistence, PID reuse/churn, and same-cwd ambiguity; Darwin remains facts-only
- [ ] #6 A representative 750-file benchmark reduces correlation CPU by at least 70% across five two-second passes, with every pass still executing full discovery and cursor work
<!-- AC:END -->

---
id: TASK-189
title: >-
  sesh — shipper wedge on tailnet flap: unbounded HTTP round trips + silent
  non-resumable first recovery pass
status: Done
assignee: []
created_date: '2026-07-13 10:14'
labels:
  - sesh
dependencies: []
priority: high
ordinal: 188000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
FIELD BUG found on the owner Mac during first fleet onboarding, root-caused by an on-device debugging agent: ship.Client with nil HTTPClient fell back to http.DefaultClient (no timeouts) — a tailnet flap mid-recovery-GET parked the single-threaded daemon forever (pid alive, zero log bytes, zero cursors); and the first recovery pass over 3,253 files was silent and non-resumable (not_found recorded nothing, interruption restarted from zero, 17-70 min sequential). Fix on branch mac-ship-wedge-fix: bounded fallback client (10s dial / 30s response-header / 5min cap), INFO with file count before the pass, not_found records the offset-0 cursor so the pass resumes. Reviewer ruled the zero-cursor recording safe under wire/R23/I4 at-least-once semantics; timeout tradeoff safety-preserving. Follow-ups spun off: TASK-187 (bounded-concurrency recovery, launchd fd limits, Darwin status config, bound update/status clients), TASK-188 (Darwin test health).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Bounded fallback client with regression test exercising the production fallback path (mutation-proven 3/3 fail pre-fix)
- [ ] #2 Recovery pass observable (INFO w/ count) and resumable (not_found records zero cursor)
- [ ] #3 Live verification on the affected Mac: first log line at startup, 671 cursors recovered in 209s, tailnet outage produced clean hold/backoff WARNs
<!-- AC:END -->

---
id: TASK-187
title: >-
  sesh — first recovery pass: bounded-concurrency GETs + launchd fd soft limits
  + Darwin status config resolution
status: Done
assignee:
  - mika
created_date: '2026-07-13 07:49'
updated_date: '2026-07-13 10:07'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 186000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Follow-ups from the Mac wedge investigation (branch mac-ship-wedge-fix): (1) first recovery pass is sequential — 3,253 files at ~310ms-1s each is 17-70 min; bound-concurrency the recovery GETs; (2) under launchd the 256-fd soft limit starves kqueue-fsnotify over a large corpus (4,389 fds observed foreground) — non-causal today (rescan fallback covers) but plist should set SoftResourceLimits; (3) sesh status run interactively on Darwin reports 'store: not configured' because the URL lives in the plist that only the service sees — status should resolve config from the installed plist the way sesh update does, so Mac users get honest output.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Recovery pass runs GETs with bounded concurrency; total first-pass time on a 3k-file corpus measured and recorded
- [x] #2 launchd plist template sets SoftResourceLimits appropriate for kqueue watching; rendered by sesh setup
- [x] #3 sesh status on Darwin resolves the store URL from the installed plist; tested
<!-- AC:END -->

## Evidence (Done, 2026-07-14)

Merged to main at 656bafa (--no-ff, linear 15eec15 -> 106d5bf -> 4bf1a17,
16 files), pushed; deployed live as sesh-v0.1.7 (store + release, client
update verified 0.1.6 -> 0.1.7, service restarted on the new shipper).

Delivered: recovery GETs and initial PUT streams run on 8 bounded workers
(fixed; the store's single write connection makes a wider bound queue
server-side — see docs/design/2026-07-13-sesh-store-read-write-split.md).
Measured on the 3k-file fixture with 10ms injected store delay: serial
61.7s -> 7.9s (7.8x); at the real ~177ms link that extrapolates ~18min ->
~2.3min of round trips. Ordering invariant (at most one in-flight op per
file identity; per-file PUT offsets strictly sequential) is enforced by
the Shipper itself (pass mutex) and pinned by regression + an in-tree
negative self-check on the overlap detector. Bulk HTTP clients moved off
wall-clock timeouts onto an idle-progress watchdog (a progressing transfer
at any rate is never killed; every zero-progress mode stays bounded);
interactive status ping keeps its 15s cap. launchd plist sets
SoftResourceLimits 8192 with upgrade-insert for existing renders. Darwin
sesh status resolves the store URL from the installed plist (flag > env >
installed config).

Review: 3 findings (2xP1: RunOnce re-entry could break the invariant —
including a latent race; wall-clock cap could kill slow-but-progressing
transfers, a latent livelock predating this task; 1xP2: detector negative
proof belonged in-tree). All closed and independently re-verified (-race
x10, keep-alive reuse through the watchdog confirmed). Final verdict
APPROVE; merge-gate battery 58/58 green post-merge.

Accepted post-deploy gaps (need the owner Mac / real WAN, non-blocking):
launchd actually honoring the 8192 fd limit (launchctl print after next
setup run); sesh status on real Darwin; real-WAN first-pass timing at the
next onboarding; 8-wide ingest behavior against the loaded live store.


## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Reviewer-naze area-(d) note from the wedge-fix review: internal/update and cli/status still use bare http.DefaultClient (unbounded) — same wedge class as the fixed shipper fallback, milder because interactive. Bound them when doing item (1).

Owner concern (2026-07-13, raised alongside the surface-latency report): initial sync feels heavy and the fleet will have HEAVY consumers onboarding with 3-5k files each. Item (1) is the answer inside the wire — the first pass is RTT-serialized per file (177ms RTT from Sydney → ~10-15 min in round trips alone before transfer), so bounded-parallel recovery GETs AND pipelined/parallel initial PUTs both belong in this task's measurement. The rsync-instead question was assessed and declined (bulk throughput through the store measured fine at 2.6-4MB/s; the wire ships only appended tail bytes, which beats rsync rolling checksums for append-only files; rsync would need per-user ssh accounts on the shared prod VM and would lose grant identity, ACK durability, and index-on-ingest). If item (1) measurements still show unacceptable initial-sync times after parallelism, revisit transport with data.

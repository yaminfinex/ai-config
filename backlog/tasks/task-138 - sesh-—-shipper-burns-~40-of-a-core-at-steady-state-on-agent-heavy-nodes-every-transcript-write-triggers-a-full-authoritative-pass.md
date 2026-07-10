---
id: TASK-138
title: >-
  sesh — shipper burns ~40% of a core at steady state on agent-heavy nodes:
  every transcript write triggers a full authoritative pass
status: Done
assignee: []
created_date: '2026-07-10 01:00'
updated_date: '2026-07-10 01:17'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 138000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Field-observed 2026-07-10 on the M2 look-see node right after the scalability fixes landed: with the backfill complete and all cursors quiescent, sesh ship sits at ~38% of one core indefinitely (measured via /proc jiffies over 10s windows), ~390 read syscalls/sec with read_bytes flat (all directory walks and stats, no data).

Mechanism (internal/ship/watch.go Run + tail.go RunOnce): any fsnotify event under either root wakes a pass after a 200ms debounce, and every pass is the full authoritative RunOnce — Discover walks both roots entirely (~750 session files across 117 project dirs on this node), watchDirs re-walks both roots to re-register directories, the platform correlator does a /proc-wide sweep, and every cursor is checked. The design assumes saves are bursty ("one save burst is one pass"), but an agent-heavy node breaks the assumption: 8 live agent sessions append their transcripts continuously, so the wake channel refills before each pass finishes and the shipper runs back-to-back passes at the debounce ceiling (~5/sec) forever.

The per-pass authoritative model is deliberate (backfill and live tailing are one code path) and should stay. The cost fix is pass admission, not pass content — candidates, combinable: (a) a minimum inter-pass interval (e.g. 2-5s) so continuous writes coalesce into a few passes per interval rather than 5/sec, keeping the periodic full rescan as the guarantee; (b) cache the /proc correlation sweep with a short TTL instead of re-sweeping per pass; (c) skip the watchDirs re-walk on hint-driven passes (fsnotify Create events already add new dirs in the event loop; the rescan-ticker pass can keep the belt-and-braces re-walk). Measure before/after on a node with several live sessions writing; target is low single-digit %CPU at quiescence with tail latency still comfortably under the store surface's freshness needs.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Steady-state shipper CPU on a node with continuous transcript writes drops to low single digits, measured over a 60s window with several live sessions
- [x] #2 Live-tail latency from transcript append to store ACK stays within the surface freshness envelope (document the measured value)
- [x] #3 Backfill and periodic-rescan guarantees unchanged (existing check scripts green; late-onboard backfill behavior untouched)
- [x] #4 Full pinned gate green uncached
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Fixed on branch task-138-shipper-cpu (fc457ae, worker codex, orchestrator-verified). Change is pass ADMISSION only: hint-driven (fsnotify) passes admitted at most once per 2s under continuous writes (200ms burst debounce retained); every admitted pass is still the full authoritative RunOnce. watchDirs re-walk moved off the hint path — runs at startup and on periodic-rescan admission; fsnotify Create keeps adding new dirs immediately, periodic rescan remains the overflow/race guarantee. Retry/backoff, cold-start backfill, explicit RunOnce untouched. /proc correlation caching NOT added: admission bounding alone met the target and avoids observation staleness. Regression test drives continuous appends and asserts bounded pass-start spacing. Measured (isolated workload, 750 files / 117 dirs / 8 appenders at 20Hz, 60s /proc jiffies): CPU 22.73% -> 3.28% of one core. Append->store ACK 24.6ms -> ~1.3s mean (direct cost of the 2s admission interval; worst case ~2.2s — inside the human-browsed surface freshness envelope per AC2, value documented). Pinned gate re-run uncached by orchestrator from the worktree: all packages + all check scripts green. Merge of the branch is owner/hera's call; all sesh services remain stopped per user directive.
<!-- SECTION:NOTES:END -->

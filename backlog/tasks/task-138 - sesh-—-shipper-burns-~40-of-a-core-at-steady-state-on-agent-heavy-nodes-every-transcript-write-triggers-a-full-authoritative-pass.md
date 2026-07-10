---
id: TASK-138
title: >-
  sesh — shipper burns ~40% of a core at steady state on agent-heavy nodes:
  every transcript write triggers a full authoritative pass
status: To Do
assignee: []
created_date: '2026-07-10 01:00'
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
- [ ] #1 Steady-state shipper CPU on a node with continuous transcript writes drops to low single digits, measured over a 60s window with several live sessions
- [ ] #2 Live-tail latency from transcript append to store ACK stays within the surface freshness envelope (document the measured value)
- [ ] #3 Backfill and periodic-rescan guarantees unchanged (existing check scripts green; late-onboard backfill behavior untouched)
- [ ] #4 Full pinned gate green uncached
<!-- AC:END -->

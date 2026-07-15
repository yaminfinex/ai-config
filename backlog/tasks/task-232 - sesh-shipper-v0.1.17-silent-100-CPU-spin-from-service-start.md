---
id: TASK-232
title: 'sesh shipper v0.1.17: silent 100% CPU spin from service start'
status: To Do
assignee: []
created_date: '2026-07-15 06:57'
labels:
  - sesh
dependencies: []
priority: high
ordinal: 231500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field observation (owner-reported CPU usage, 2026-07-15): the per-user sesh-ship.service process spins at 100% of one core continuously from process start, while logging NOTHING after systemd's Started line. Environment: the shipper restarted at 05:49:07 during the v0.1.17 deploy (binary replaced 05:49, sesh version reports sesh-v0.1.17); the store was mid-reindex (down) at restart time and became healthy shortly after (health probe 200 in 0.36s). Evidence at capture: 66+ min at 100% CPU, 38 threads, near-zero context switches on the main sample (tight CPU-bound loop, not syscall churn), zero journal entries post-start (the PRE-restart process logged loud hold-position retry warnings against the down store, then was stopped by the deploy).

Control comparison: the second user's shipper on the same box runs a pre-v0.1.17 binary (installed Jul 13), started Jul 13, averages ~4% CPU with normal behavior — same machine, same store. Strong signal the spin is new in v0.1.17 (candidate areas: the resume/recovery path when the store is unavailable at boot, or the TASK-188-era rewalk/watch changes) rather than environmental.

Investigation notes: ptrace is not permitted on the box (no live strace); consider a debug/pprof endpoint or SIGQUIT goroutine dump (kills the process — capture on a sacrificial restart), and reproduce by starting a v0.1.17 shipper against an unreachable store. Whether shipping is actually functioning despite the spin was not yet determined — check store-side byte progress for this node before assuming it is only a CPU bug.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Root cause identified with a reproduction (v0.1.17 shipper, store down at boot or equivalent)
- [ ] #2 Fix proven: shipper at idle-normal CPU after restart-into-healthy-store AND restart-into-down-store
- [ ] #3 Red-first regression test on the spin path
- [ ] #4 Verify shipping progress was/was not occurring during the spin; data-loss statement in the DONE record
<!-- AC:END -->

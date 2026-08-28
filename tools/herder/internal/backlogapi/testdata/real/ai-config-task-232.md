---
id: TASK-232
title: 'sesh shipper v0.1.17: silent 100% CPU spin from service start'
status: Done
assignee: []
created_date: '2026-07-15 06:57'
updated_date: '2026-07-15 07:41'
labels:
  - sesh
dependencies: []
priority: high
ordinal: 231500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Field observation (owner-reported CPU usage, 2026-07-15): the per-user sesh-ship.service process spins at ~100% of one core continuously from process start while logging NOTHING after systemd's Started line. CRITICAL UPDATE: the owner restarted the service with the store HEALTHY (health 200 in 0.36s) and the fresh process (new pid) was back at 103% CPU within 2 minutes — the spin is UNCONDITIONAL in v0.1.17 on this node, not a boot-into-down-store transient.

Original context: first spin observed from the 05:49:07 restart during the v0.1.17 deploy (binary replaced 05:49; store mid-reindex at the time). 38 threads, near-zero context switches (tight CPU loop, not syscall/retry churn), zero journal output. Control: the second user's shipper on the same box runs a pre-v0.1.17 binary (Jul 13), same store, ~4% CPU normal — regression is in v0.1.17. Candidate areas: shipper resume/recovery loop, or the rewalk/watch changes in the recent test-hardening era releases.

Investigation notes: ptrace blocked on this box; use a debug/pprof surface if the binary has one, else SIGQUIT goroutine dump on a sacrificial restart (capture stderr via journal), else reproduce locally from source (repo tools/sesh) with a store stub. MUST determine whether shipping actually progresses during the spin (store-side byte offsets for this node) — CPU bug vs data-stall changes severity.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Root cause identified with a reproduction (v0.1.17 shipper, store down at boot or equivalent)
- [ ] #2 Fix proven: shipper at idle-normal CPU after restart-into-healthy-store AND restart-into-down-store
- [ ] #3 Red-first regression test on the spin path
- [ ] #4 Verify shipping progress was/was not occurring during the spin; data-loss statement in the DONE record
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged 43cad00 (--no-ff, 2 commits: a6b7e1a fix + dafb84c doc line). Root cause: Codex SESSION_OWNER correlation walked every same-UID /proc/<pid>/fd table once PER transcript — O(transcripts x processes x FDs), silent one-core spin before any log/store call; LATENT (predates v0.1.17; corpus scale exposed it). Fix: per-sweep path->identity index, each FD table read once, attribution semantics preserved (incumbent replayed old impl over 18 shapes: 17 identical; the 1 divergence — dup UUID at two paths w/ conflicting owners — old was order-dependent arbitrary stamp, new is unanimity absence, ruled better + documented). Measured 82x at 200 transcripts; extrapolates ~6.2s -> ~20ms per sweep at field scale. NO DATA LOSS: shipping progressed during the spin (high_water advanced +109KB in sample; cursors advance only on durable ACK) — CPU amplification, not stall. Red-first FD-read-once regression + healthy/unresponsive-store process verification. Review: opus incumbent APPROVE (1 required doc line, discharged, delta confirmed) + grok calibration APPROVE (ledger row 17). Gates: independent 60/60 at fix head, post-merge 60/60 on main. Fix rides v0.1.18 — DEPLOY PENDING: live shippers keep spinning until the next sesh release+deploy; follow-up TASK-235 (codex benchmark arm) filed.
<!-- SECTION:NOTES:END -->

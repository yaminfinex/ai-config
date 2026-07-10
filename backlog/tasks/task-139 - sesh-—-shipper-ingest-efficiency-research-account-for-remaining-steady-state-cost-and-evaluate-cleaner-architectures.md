---
id: TASK-139
title: >-
  sesh — shipper/ingest efficiency research: account for remaining steady-state
  cost and evaluate cleaner architectures
status: Done
assignee: []
created_date: '2026-07-10 01:19'
updated_date: '2026-07-10 01:39'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 139000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: research. Follow-on to the shipper CPU fix (hint-pass admission bounded to one per 2s; steady-state CPU 22.7% -> 3.3% of a core, append->ACK latency 25ms -> ~1.3s mean as the accepted trade-off). The owner suspects the current shape — every pass is a full authoritative RunOnce that re-walks both roots, re-stats every session file, and re-sweeps /proc for owner correlation — is brute-force, and that a smarter/cleaner design exists. This unit answers that with evidence, not opinions.

Questions to answer:
1. Where does the remaining post-fix cost actually go? Profile a pass on an agent-heavy workload (several hundred files, several continuous appenders): time and syscalls split across root walks/stats (Discover), /proc correlation sweep, per-cursor checks, mirror reads, HTTP to store, and store-side ingest (parse + sqlite index writes). CPU pprof plus syscall counts, not vibes.
2. For each candidate, an explicit adopt/reject/defer verdict with measured or estimated payoff and its cost in complexity/correctness: (a) dirty-set passes — fsnotify events mark specific files dirty so hint passes touch only the dirty set, with the periodic rescan keeping the full authoritative sweep as the guarantee; (b) TTL-cached /proc owner correlation; (c) adaptive admission — immediate pass when the shipper has been idle (restores ~25ms ACK for the common single-agent save) while keeping the interval under sustained load; (d) persisted or cached walk/stat state across passes; (e) anything found in (1) that dominates and isn't on this list, including store-side ingest costs.
3. Spec compatibility: the authoritative-pass model (backfill and live tailing one code path, I3) is deliberate in docs/specs/session-service-spec.md. Any recommendation that weakens it must be framed as proposed spec errata with the invariant-level argument — the researcher proposes, never edits the spec.

Deliverable: a findings memo (durable) with a verdict per question, plus filed-ready task text (ACs + settled-decisions list) for any build work recommended. No behavior-changing code lands on this unit; scratch experiment code stays out of the branch.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Profile-backed cost accounting of a post-fix authoritative pass on an agent-heavy workload, split across walk/stat, /proc sweep, cursor checks, mirror reads, HTTP, and store ingest
- [x] #2 Explicit adopt/reject/defer verdict with payoff estimate for each candidate: dirty-set hint passes, TTL-cached /proc correlation, adaptive admission for idle-machine ACK latency, cached walk state, plus anything dominant found in profiling
- [x] #3 Recommendations that touch the authoritative-pass model are framed as proposed spec errata, not designed around silently
- [x] #4 Findings memo delivered plus filed-ready task text for recommended build work; no behavior-changing code committed
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Research complete (worker codex, resumed from the shipper CPU fix; orchestrator-verified: branch clean at fc457ae, no behavior code landed, memo internally consistent with pprof+jiffies+strace evidence). Full findings memo durable as backlog doc-001. Executive verdict: the authoritative full-pass architecture is sound — pure walk/stat is ~6ms/750 files; the avoidable cost is side effects done per-file instead of per-pass. Cost split of a 20.92ms quiescent pass: /proc correlation 70%, cursor checks 22%, discovery 6%. Active-pass dominant term: whole-registry cursor saves (~24ms of ~60ms for 8 dirty files; up to 33% of sustained shipper CPU incl MarshalIndent). Store CPU dominated by SQLite commit/statement churn (no event transactions). Verdicts — ADOPT: pass-batched cursor persistence (TASK-140, highest payoff), TTL-cached /proc correlation (TASK-141), adaptive first-hint-after-idle admission (TASK-142, restores ~25ms isolated ACK), transactional index ingest (TASK-143, ~4x est), graceful serve shutdown (TASK-144, found via zero-byte pprof on SIGTERM). REJECT: dirty-set hint passes (saves only the ~21ms baseline, would need spec errata weakening the authoritative-pass model, high invalidation risk) and cached walk/stat state (saves <0.3 core points). No spec erratum recommended; all adopted items preserve I3/I4/I8. Deviation accepted: sustained store pprof lost to the SIGTERM bug itself; store attribution from short-burst profile instead.
<!-- SECTION:NOTES:END -->

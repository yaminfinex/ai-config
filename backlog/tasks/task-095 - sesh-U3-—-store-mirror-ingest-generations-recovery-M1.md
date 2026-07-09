---
id: TASK-095
title: 'sesh U3 — store: mirror ingest + generations + recovery (M1)'
status: Done
assignee: []
created_date: '2026-07-09 05:27'
updated_date: '2026-07-09 06:22'
labels:
  - sesh
dependencies:
  - TASK-093
  - TASK-094
priority: high
ordinal: 95000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Lane: store (codex worker). Deliverable: sesh serve accepting byte ranges — mirror on disk (file per tool/sid/file-uuid/generation, fsync before ACK), high-water ACK, conflict generations, recovery GET, last-seen tracking, in-process append-event bus (buffered channel; U6 is its first consumer). Requirements R6,R7,R8,R11,R12,R13,R19,R25. Loopback bind ONLY (R19), listener behind an interface U11 swaps for tsnet. SQLite (modernc.org/sqlite, WAL) for file registry, high-waters, last-seen, facts observation log (hostname + OS user recorded from day one).

Ingest path: validate tool enum (closed: claude,codex) -> route on offset vs high-water per the wire doc -> fsync -> ACK -> publish append event. Divergent bytes at an ACKed offset NEVER overwrite: distinct conflict error, new generation; repeat conflict -> poisoned (visible in sesh status). Any mirror storage error -> 5xx, never ACK.

Read first: /home/grace/Coding/ai-config/napkins/sesh-build/playbook.md, plan U3 section (git show 05dfc47:docs/plans/2026-07-09-001-feat-sesh-session-service-plan.md), docs/specs/sesh-wire.md on sesh-build, captures Lane 2 settled decisions (git show 6843649:docs/design/2026-07-09-sesh-task-captures.md). Thread: sesh-u3.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Append at high-water ACKs + advances; identical replay no-ops (S9); divergent replay -> conflict -> generation 1 with generation 0 bytes intact
- [x] #2 Offset gap returns current high-water; unknown tool rejected; injected write failure returns 5xx and never ACKs
- [x] #3 kill -9 mid-PUT, restart, replay: no corruption, correct high-water
- [x] #4 Recovery GET returns high-waters + fingerprint for known and UUID-only identities
- [x] #5 All wire-doc error codes exercised by name in tests; mirrored files byte-identical to shipped fixtures
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged to sesh-build @ dea4ba0. Provenance: 73b0493 impl -> cross-family adversarial review (opus, MERGE-WITH-FIXES) -> 0b42f32 fixes (F1 HIGH: generation routing now highest-match-first + regression for the spurious-poison recovery sequence; F5 conflict_pending cleared; F2 dirty_for_reindex on dropped events; F3 durable default data dir; F4/F6 AC test gaps; F8 UUID canonicalization; F9 error relabel) -> dea4ba0 wire Amendment 1 (drafted per zomi ruling #25692, co-signed suki #25940, FINAL CONFIRM zomi #25965 at hash). Orchestrator re-ran gates fresh at each step. Deferred: F7 global-mutex fsync serialization (flagged for post-M2 perf). Binding sign-off notes both landed and test-verified. Trail: threads sesh-u3 + sesh-u1.
<!-- SECTION:NOTES:END -->

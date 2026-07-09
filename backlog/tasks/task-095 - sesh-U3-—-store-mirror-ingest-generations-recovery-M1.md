---
id: TASK-095
title: 'sesh U3 — store: mirror ingest + generations + recovery (M1)'
status: To Do
assignee: []
created_date: '2026-07-09 05:27'
updated_date: '2026-07-09 05:47'
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
- [ ] #1 Append at high-water ACKs + advances; identical replay no-ops (S9); divergent replay -> conflict -> generation 1 with generation 0 bytes intact
- [ ] #2 Offset gap returns current high-water; unknown tool rejected; injected write failure returns 5xx and never ACKs
- [ ] #3 kill -9 mid-PUT, restart, replay: no corruption, correct high-water
- [ ] #4 Recovery GET returns high-waters + fingerprint for known and UUID-only identities
- [ ] #5 All wire-doc error codes exercised by name in tests; mirrored files byte-identical to shipped fixtures
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
From M0 sign-off review (thread sesh-u1, #25130), binding for this unit: (a) when the store computes a generation fingerprint that differs from the client claim, record the computed value and LOG the mismatch — claim/computed divergence is an early corruption signal; (b) add a test acknowledging the sub-window poison-key edge: a file that never reaches 1 KiB has fingerprint null, so a second legitimate sub-window recreate poisons (file_uuid, null) — rare, accepted, but must be a named test not a surprise; strengthens the later operator un-poison verb case.
<!-- SECTION:NOTES:END -->

---
id: TASK-140
title: sesh — batch cursor-registry durability at the authoritative-pass boundary
status: In Progress
assignee: []
created_date: '2026-07-10 01:39'
updated_date: '2026-07-10 01:54'
labels:
  - sesh
dependencies: []
priority: high
ordinal: 140000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Type: implement. Evidence and full context: backlog doc-001 (sesh shipper and ingest efficiency findings).

Each cursor mutation serializes the full registry, fsyncs the temp file, renames it, and fsyncs the directory. Several active files therefore multiply whole-registry work inside one authoritative pass even though a crash before local persistence is already safe at-least-once replay. Measured: eight one-line appends cost 23.1–26.3ms of registry saves in a ~60ms pass; sustained profile attributes up to 33% of shipper CPU to Registry.save (+MarshalIndent).

Settled decisions:
- Keep the JSON registry and atomic temp-file + fsync + rename + directory-fsync format; this task changes commit frequency, not storage technology.
- The batch boundary is one RunOnce, not a timer and not a number of bytes.
- Store ACK remains the only event that advances an offset. Local batch persistence may lag within the running pass because a crash produces safe idempotent replay.
- Flush successful mutations even when another file in the same pass holds or fails.
- Do not weaken recovery refusal, schema-generation checks, lifetime locking, or surfaced durability errors.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A pass containing multiple successful ACKs performs at most one durable registry replacement, including backfill needing multiple PUT chunks
- [ ] #2 A cursor changes in memory only after the corresponding durable store ACK or required catalog transition; an unreachable/refusing store never advances it
- [ ] #3 Before RunOnce returns, all mutations from that pass are durably persisted or the pass returns a surfaced persistence error
- [ ] #4 Killing the shipper after store ACK but before batch flush replays safely after restart and converges without duplicate mirror bytes or lost source bytes
- [ ] #5 Deletion GC, path moves, owner observations, truncation, fingerprint transitions, poison state, recovery, and partial-pass errors persist correctly in the same batch
- [ ] #6 A 750-cursor/eight-dirty benchmark shows one rename and two fsyncs per pass and at least 70% reduction in registry-persistence wall time versus per-cursor saves
- [ ] #7 Existing unit and scenario gates remain green uncached
<!-- AC:END -->

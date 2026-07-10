---
id: TASK-140
title: sesh — batch cursor-registry durability at the authoritative-pass boundary
status: Done
assignee: []
created_date: '2026-07-10 01:39'
updated_date: '2026-07-10 02:03'
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
- [x] #1 A pass containing multiple successful ACKs performs at most one durable registry replacement, including backfill needing multiple PUT chunks
- [x] #2 A cursor changes in memory only after the corresponding durable store ACK or required catalog transition; an unreachable/refusing store never advances it
- [x] #3 Before RunOnce returns, all mutations from that pass are durably persisted or the pass returns a surfaced persistence error
- [x] #4 Killing the shipper after store ACK but before batch flush replays safely after restart and converges without duplicate mirror bytes or lost source bytes
- [x] #5 Deletion GC, path moves, owner observations, truncation, fingerprint transitions, poison state, recovery, and partial-pass errors persist correctly in the same batch
- [x] #6 A 750-cursor/eight-dirty benchmark shows one rename and two fsyncs per pass and at least 70% reduction in registry-persistence wall time versus per-cursor saves
- [x] #7 Existing unit and scenario gates remain green uncached
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Fixed on branch sesh-shipper-efficiency (d66e18a, shipper lane worker, orchestrator-verified). RunOnce opens a registry batch and defers the closing flush, joining persistence failure into the pass error via errors.Join so early returns cannot bypass it. Put/Delete keep immediate durability outside a pass (batching private to the RunOnce boundary, reentrant depth counter); inside a pass they mutate authoritative memory and mark dirty for one outer flush. Failed flushes stay dirty for later-pass retry; atomic temp+fsync+rename+dir-fsync format unchanged; cursor transition sites untouched (store ACK remains sole advance; recovery refusal, locking, I8 preserved). Tests cover multi-file/multi-PUT batching, partial-pass hold durability, post-ACK flush failure, crash-before-flush replay convergence. Measured: 750-cursor/8-dirty benchmark 19.3-20.9ms -> 2.05-2.35ms per pass (87.9-90.2% reduction vs 70% AC); strace shows exactly 1 renameat + 2 fsyncs per pass. Orchestrator re-ran pinned gate uncached from the lane worktree: all packages + check scripts green. Merge pending lane review + hera handoff.
<!-- SECTION:NOTES:END -->

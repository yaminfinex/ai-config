---
id: TASK-086
title: >-
  sesh-store: central service — idempotent byte-range ingest, byte mirror,
  parse-on-ingest index, WhoIs stamping, grant check
status: To Do
assignee: []
created_date: '2026-07-09 04:11'
updated_date: '2026-07-09 04:19'
labels:
  - sesh
dependencies: []
priority: medium
ordinal: 86000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
sesh is a team-visibility service for AI coding sessions. Every machine (node) runs a small per-OS-user agent (the SHIPPER) that tails the transcript files Claude Code and Codex CLI already write to disk, and ships their raw bytes plus four identity facts to one central service (the STORE), which keeps a byte-faithful mirror, parses it centrally into a per-message index, and serves one read-only web page (the SURFACE) answering 'what has everyone been working on?'

UNIT TYPE: implement. Designer-authored capture (docs/design/2026-07-09-sesh-task-captures.md @ 6843649 on branch sessions-missions-design) — the settled-decisions list below is DO-NOT-REVERSE; if one seems wrong or blocking, STOP and escalate to @hera (who routes to tomo/owner). Never substitute and disclose later.

PINNED REFS (read in this order, all at commit e58f50a on branch sessions-missions-design; a worker starting from main runs: git fetch origin sessions-missions-design, then git show e58f50a:<path>):
1. docs/specs/session-service-spec.md — THE CONTRACT, read fully. Section refs below (§N); I-n = invariants (§3.3); S-n = acceptance scenarios (§6).
2. docs/design/2026-07-09-session-service-build-brief.md — working mode + verify-early items.
3. docs/design/2026-07-09-session-shipping-prior-art.md — why each mechanism, with upstream bug refs.
(If the branch has merged to main by pickup, paths are the same; e58f50a stays the pinned wording.)

SEQUENCING: lanes 1+2 (shipper/store) freeze the spec §8 wire contract together first, in a short shared doc PR, before parallelizing. Lane 3 (surface) depends only on the index schema. Lane 4 (deploy) is unblocked the moment the store boots anywhere.

BUILD: the central service at tools/sesh: idempotent byte-range ingest (spec §8 API), byte-faithful mirror storage, parse-on-ingest index (per-message rows, message-uuid dedup across a session's files, trailing-partial-line holdback, quarantine on parse failure, a re-derive-index-from-mirror command), tailnet identity stamping via WhoIs, grant check on every connection.

SETTLED DECISIONS (do not reverse; escalate if blocked):
- HTTP PUT byte ranges under /v1 is the wire (owner-confirmed 2026-07-09): PUT /v1/files/{tool}/{session_id}/{file_uuid}/bytes?offset=N, facts in headers. Curl-debuggable; idempotency falls out of addressing bytes. This API is the ONLY contract between shipper and store — do not add a second channel.
- Message-uuid dedup is core correctness, not an optimization: Claude verifiably writes one session across multiple files with duplicated history.
- The mirror is the durable record; the index is disposable/re-derivable. Parse failures quarantine — they never block or mutate the mirror.
- Identity is stamped from tailscaled/tsnet WhoIs at the connection, never trusted from the client. Attribution is never authentication (facts never gate access).
- Access is grant-scoped, not whole-tailnet: transcripts contain pasted secrets, and tailnets contain phones and CI boxes.
- The store joins the tailnet under its own node identity so it can change hosts without any shipper change beyond the URL.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 [S1] Mirror contents byte-compare identical to shipped sources for every file
- [ ] #2 [S2] Ingest two files belonging to one Claude session with overlapping message history (resume-into-new-file): the index holds each message uuid once; both files remain intact in the mirror
- [ ] #3 [S5] After a client deletes its source file, the mirror and index retain the full transcript indefinitely
- [ ] #4 [S8] A tailnet device outside the grant can neither PUT nor read — connection-level deny. An in-grant shipper's uploads carry a store-stamped WhoIs identity; any identity claim inside request content is ignored
- [ ] #5 [S9] Re-send an already-ACKed byte range: the store overwrite-compares; no mirror corruption, no duplicate index rows. Restart the store mid-ingest and repeat: same
- [ ] #6 [S10] Feed valid-JSONL-but-unparseable lines: the mirror stores them, the affected index entries quarantine (visible as such), ingest of other files continues. After a parser fix, the re-derive command rebuilds the index from the mirror alone and the transcript renders
- [ ] #7 Trailing partial line (no newline yet): mirrored as-is; the index excludes it until the line completes
- [ ] #8 PUT response returns the durable high-water mark; a cursor-recovery GET tells a shipper with a lost registry what the store already has for a file identity
<!-- AC:END -->

# sesh Wire and Index Schema

Status: **FROZEN 2026-07-09; implementation conformance verified 2026-07-10.** This is the
shipper-to-store wire and index contract. Changes listed under Compatibility Rules require
an amendment here before code lands.

## Authority

This document freezes the v1 HTTP wire, the file identity rules used on that wire, the
store recovery shape, and the message index schema shared by the store and surface.
Anything not named here is not part of the shipper/store contract.

The shipper stays dumb: it discovers files, ships bytes, and attaches facts. It never
parses transcript JSONL and never computes display ownership. The store owns parsing,
deduplication, facts interpretation, conflict generations, quarantine, and auth.

## Constants

- Wire version: `1`.
- API root: `/v1`.
- Allowed tools: `claude`, `codex`. Unknown tools are rejected.
- Rescan interval (shipper-local default, NOT wire contract): 60 seconds — tunable per
  node; fsnotify-coverage calibration may adjust it without a wire amendment.
- Maximum PUT body: 4 MiB.
- Fingerprint algorithm: `sha256-first-1024`.
- Fingerprint window: bytes `[0, 1024)`.
- Fingerprint readiness: the shipper records and sends the fingerprint only once the
  source file size is at least 1024 bytes. Before that point, identity is UUID-only.
- Byte offsets are zero-based and byte counts are over the raw file bytes, not JSONL
  lines or UTF-8 characters.
- A durable ACK means the mirrored bytes are on disk and fsynced. Indexing happens
  after ACK and may mark a file dirty for reindex without invalidating the ACK.

## File Identity

Every shipped file is identified by:

- `tool`: closed enum, `claude` or `codex`.
- `session_id`: the session id claim carried by the filename/path convention.
- `file_uuid`: the UUID portion of the transcript filename.
- `fingerprint`: optional lowercase hex SHA-256 over the first 1024 file bytes.

The path and inode are never identity. The `session_id` in the URL is a wire claim; the
index derives `logical_session_id` from parsed content and falls back to the wire claim
only when parsing cannot provide one.

The store preserves conflicting histories by assigning a zero-based `generation` per
`(tool, session_id, file_uuid)`. Generation `0` is the first history seen. Later
generations are opened only by store conflict handling or by a new fingerprint for the
same file UUID. The shipper does not invent generation numbers. The active generation of
a file identity is the highest generation number. The active generation is the routing
target only for PUTs that carry no fingerprint; a PUT with a fingerprint routes to its
matching generation regardless of which generation is active.

The shipper evaluates size regression (source size below its cursor) before fingerprint
comparison; a file recreated below the fingerprint window is caught by size regression,
never by fingerprint mismatch.

A generation's recorded fingerprint is authoritative from its own mirrored bytes: once a
generation's high-water reaches the fingerprint window, the store computes and records
the fingerprint over mirrored bytes `[0, 1024)`; before that point the recorded value is
the client's claim, or null. Recovery GET returns the recorded value.

## PUT Bytes

`PUT /v1/files/{tool}/{session_id}/{file_uuid}/bytes?offset=N`

The request body is the exact byte range read from the source file beginning at `N`.

Required headers:

| Header | Value |
|---|---|
| `Content-Type` | `application/octet-stream` |
| `X-Sesh-Wire-Version` | `1` |
| `X-Sesh-Hostname` | Hostname observed by the shipper |
| `X-Sesh-OS-User` | OS user running this shipper |

Conditional headers:

| Header | Value |
|---|---|
| `X-Sesh-Fingerprint-Algorithm` | `sha256-first-1024`, required when `X-Sesh-Fingerprint` is present |
| `X-Sesh-Fingerprint` | Lowercase hex SHA-256 of source bytes `[0, 1024)`, present only once the file is at least 1024 bytes |
| `X-Sesh-Session-Owner` | `SESSION_OWNER` observed by the shipper, omitted when absent or ambiguous |

In tailnet-native mode, the store stamps tailnet identity from the connection. Any client-supplied
tailnet identity or display-owner header is ignored and must not affect storage,
auth, or rendering.

Facts are append-only observations. Omitting `X-Sesh-Session-Owner` on a later PUT
never retracts a previously shipped observation; the store must not interpret absence
as a change of owner.

### Successful ACK

Status: `200 OK`.

```json
{
  "wire_version": 1,
  "status": "ack",
  "tool": "claude",
  "session_id": "session-uuid",
  "file_uuid": "file-uuid",
  "generation": 0,
  "high_water": 12345,
  "fingerprint_algorithm": "sha256-first-1024",
  "fingerprint": "lowercase-hex-or-null"
}
```

`high_water` is the next byte offset the store will accept for the selected generation.
On any `200` the shipper sets its cursor to `min(returned high_water, the source size it
most recently observed for this file)` — never above the store's answer, never above the
source. It never uses `offset + body length`. This clamp is what makes same-prefix
truncation quiesce instead of looping (S3): the replay `200` returns the old
`high_water`, the clamp pins the cursor at the truncated size, and no further regression
fires. If the file later grows with bytes that diverge from the mirror, convergence is
the normal `byte_conflict` → `generation_opened` path. The clamp applies uniformly to
every `200`, append ACK and replay ACK alike. No other response advances a cursor.

### PUT Semantics

- Fingerprint routing happens before offset routing and is silent. When the request
  carries a fingerprint, the store routes the PUT to the generation whose recorded
  fingerprint matches it (the highest-numbered one when several match); offset routing
  then proceeds against that generation, and the ACK or error envelope carries that
  generation's number and high_water. If no generation matches, the store opens a new,
  empty generation for that fingerprint and returns 409 fingerprint_conflict carrying it
  with high_water: 0 — the only case that returns fingerprint_conflict. A request
  without a fingerprint routes to the active generation and is evaluated by byte
  comparison alone — absence is never a mismatch.
- If `offset == high_water`, the store appends the body to the active generation's
  mirror file, fsyncs, records last-PUT time and facts, returns `200`, then publishes an
  internal append event for indexing.
- If `offset < high_water`, the store compares the request bytes with the mirrored bytes
  at that offset.
  - Identical bytes are an idempotent replay and return `200` with the current
    `high_water`.
  - Divergent bytes are never overwritten and never poison a file on first sight. The
    first divergent PUT against a generation returns `409 byte_conflict` with the
    current generation and `high_water` unchanged. If the next PUT for that generation
    diverges again (no intervening successful PUT), the store opens a new generation —
    empty, `high_water: 0` — and returns `409 generation_opened` carrying it. If
    conflict handling would open a second conflict-driven generation for the same
    `(file_uuid, fingerprint)`, the store instead marks the file identity poisoned and
    answers `423 poisoned_file` from then on. Existing generations' bytes are never
    modified by any of this.
  - A request body may span the high-water. The store compares the overlapping range
    `[offset, high_water)` against mirrored bytes; any divergence in the overlap follows
    the conflict rules above and nothing from the request is appended. When the overlap
    is identical, the store appends the excess `[high_water, offset + body length)` to
    the mirror under the same fsync-before-ACK rule and returns `200` with
    `high_water = offset + body length`. A `200` therefore always means every byte of
    the request body is durably mirrored.
- If `offset > high_water`, the store returns `422 offset_gap` with the current
  `high_water`; the shipper rewinds to that value.
- If the mirror append or fsync fails, the store returns `5xx` and does not ACK.
- If indexing fails after a successful mirror ACK, the store keeps the ACK valid and
  marks the file generation dirty for reindex.

## Error Catalog

All error responses use `application/json`:

```json
{
  "wire_version": 1,
  "error": "offset_gap",
  "message": "human-readable operator text",
  "tool": "claude",
  "session_id": "session-uuid",
  "file_uuid": "file-uuid",
  "generation": 0,
  "high_water": 8192,
  "shipper_action": "rewind"
}
```

| HTTP | `error` | Meaning | Required shipper reaction |
|---|---|---|---|
| 400 | `malformed_request` | Bad path, missing required header, bad offset, invalid UUID, invalid fingerprint syntax, or wrong wire version | Do not advance; surface in `sesh status`; retry only after local config or code changes |
| 400 | `unknown_tool` | Tool segment is not in the closed enum (shipper/store version skew, or a tool added without a wire amendment) | Hold every file of that tool; surface prominently; no retry loop — resolution is an upgrade or a wire amendment |
| 403 | `out_of_grant` | Tailnet identity is authenticated but not allowed to ship or read | Hold cursor; retry with slow jittered backoff (grants change without redeploys); surface as auth/config failure |
| 404 | `not_found` | Recovery lookup has no mirror state for this file UUID/fingerprint | Start from offset 0 |
| 409 | `byte_conflict` | Request bytes diverge from mirrored bytes at an already-ACKed offset | Re-check local identity: size regression first, then re-fingerprint. Fingerprint changed → resume with the new fingerprint (the `fingerprint_conflict` path selects the right generation). Unchanged → retry the same PUT once; a second divergence yields `generation_opened` or `poisoned_file` |
| 409 | `fingerprint_conflict` | Request fingerprint is new for this file UUID; the store opened an empty generation for it | Set cursor to the returned `high_water` (0 for a fresh generation) and continue with the current fingerprint |
| 409 | `generation_opened` | Repeated divergence made the store open a new, empty generation | Set cursor to the returned `high_water` (0) and re-ship from there, so the new generation receives the complete new history from offset 0 |
| 413 | `body_too_large` | PUT body exceeds 4 MiB | Split the range into smaller PUT bodies and retry without advancing |
| 422 | `offset_gap` | Request offset is beyond the store high-water | Rewind cursor to returned `high_water` and retry |
| 423 | `poisoned_file` | Conflict recurred for the same `(file_uuid, fingerprint)` after a conflict-driven generation was already opened for it | Stop retrying this file; freeze its cursor (deletion GC still applies); keep other files shipping; surface poisoned state in `sesh status` |
| 500 | `mirror_write_failed` | Store could not durably write or fsync mirror bytes | Do not advance; retry with backoff |
| 503 | `store_unavailable` | Store is temporarily unable to accept bytes | Do not advance; retry with backoff |

`shipper_action` in error bodies is informational for logs and operators; the `error`
code is the normative field a shipper switches on. A store that cannot be reached at
all is treated exactly like `store_unavailable`: hold position, jittered backoff,
cursor untouched, no local queue — the source file is the only buffer.

`out_of_grant` is used for both PUT and read denial once tailnet auth is enabled. In
loopback-only mode, non-loopback ingest attempts must be
denied before bytes are accepted.

## Recovery GET

`GET /v1/files/{tool}/{session_id}/{file_uuid}`

Optional query:

- `fingerprint={lowercase_hex}` narrows the response to generations for that
  fingerprint.

This GET exists for a shipper whose cursor registry is missing or unreadable. UUID-only
lookup is allowed before the source file reaches the fingerprint window.

Status: `200 OK`.

```json
{
  "wire_version": 1,
  "tool": "claude",
  "session_id": "session-uuid",
  "file_uuid": "file-uuid",
  "fingerprint_algorithm": "sha256-first-1024",
  "fingerprint_window_bytes": 1024,
  "generations": [
    {
      "generation": 0,
      "fingerprint": "lowercase-hex-or-null",
      "high_water": 12345,
      "poisoned": false,
      "dirty_for_reindex": false,
      "last_put_at": "2026-07-09T00:00:00Z"
    }
  ]
}
```

Status `404 not_found` with `shipper_action: "start_from_zero"` means the store has no
mirror state for this file UUID; the shipper starts from offset 0.

Required shipper reactions to a `200` recovery response:

- Local fingerprint equals a returned generation's fingerprint → resume that generation
  at its `high_water` (the highest-numbered match when several share a fingerprint).
- Local fingerprint matches no returned generation → ship from offset 0 with the local
  fingerprint; the store's fingerprint routing silently selects the right generation or
  opens a new one (fingerprint_conflict).
- No local fingerprint (source below the fingerprint window) → ship from offset 0; the
  body is at most window-sized and the store absorbs replays by overwrite-compare.
- The generation the shipper would resume is marked `poisoned` → do not resume; treat
  the file as after a `423` (frozen cursor, surfaced in `sesh status`).

Recovery GET requires `X-Sesh-Wire-Version` like every other call.

## Internal Append Event

After a successful mirror ACK, the ingest handler publishes an in-process append event
for the indexer:

```json
{
  "tool": "claude",
  "wire_session_id": "session-uuid",
  "file_uuid": "file-uuid",
  "generation": 0,
  "byte_start": 8192,
  "byte_end": 12345
}
```

This event is internal to the store process. It is not a second shipper/store protocol.

## Message Index Schema

The indexer writes and the read-only surface consumes the message index through this schema. Column names are frozen;
SQLite types may use the closest practical affinity.

Logical session unification is store-side index logic. The indexer first uses parsed
content ids or linking fields when they correctly identify multiple files as one logical
session. When parsed content ids do not unify files, the indexer unifies logical sessions
by overlapping `(entry_type, message_uuid)` across file UUIDs of the same tool; this is
the primary Claude resume path observed in Claude Code v2.1.195, where a captured resume
pair rewrote copied history under the resumed file's own content id and unified only by
141 overlapping message UUIDs. Overlap unification requires at least two overlapping
`(entry_type, message_uuid)` pairs with non-empty `message_uuid`; a single shared UUID is
too weak given the snapshot-id collision class, and empty `message_uuid` rows never
participate. When files unify by overlap, the unified session's `logical_session_id` is
the parsed content id of the earliest file in the unified set by first-ingest order of
generation 0, keeping the value deterministic under reindex and stable when later files
join the set. This rule does not move parsing to the shipper and does not change file
identity.

Table: `sesh_index_messages`

| Column | Meaning |
|---|---|
| `id` | Store-local integer primary key |
| `tool` | `claude` or `codex` |
| `logical_session_id` | Store-derived logical session id after content-id/link-field or overlap unification; falls back to `wire_session_id` only when unavailable |
| `wire_session_id` | Session id claim from the PUT URL |
| `entry_type` | Parsed transcript entry type; opaque string allowed |
| `message_uuid` | Parsed message UUID when present; empty string when absent |
| `file_uuid` | File UUID from the PUT URL |
| `generation` | Store generation number for the file UUID |
| `role` | Parsed role such as `user`, `assistant`, `system`, `tool`, or `unknown` |
| `timestamp_utc` | Parsed timestamp in RFC3339Nano UTC; null when unavailable |
| `file_ordinal` | Monotonic ordinal of the file generation inside the logical session |
| `line_ordinal` | Zero-based complete JSONL line number inside the file generation |
| `byte_start` | Inclusive byte offset of the complete line in the mirror file |
| `byte_end` | Exclusive byte offset of the complete line in the mirror file |
| `quarantine` | Boolean flag; true when this line could not produce a normal parsed row |
| `quarantine_reason` | Stable reason string when `quarantine` is true; empty otherwise |

Dedup key for parsed messages:

```text
(tool, logical_session_id, entry_type, message_uuid)
```

Rows with an empty `message_uuid` are not deduped by that key; they remain ordered by
their file and ordinal. Quarantined rows do not block mirror ACKs, other files, or
subsequent rows. The indexer must parse only complete JSONL lines; trailing partial
lines remain mirrored but absent from this table until completed.

Transcript ordering for the surface is:

```text
(timestamp_utc nulls last, file_ordinal, line_ordinal, file_uuid, generation)
```

Recency for the surface is the maximum parsed `timestamp_utc` for a logical session. A
fully quarantined session falls back to first-ingest time for ordering and renders from
the mirror raw-lines fallback.

## Reindex Contract

`sesh reindex` deletes and rebuilds disposable index rows from the mirror plus store
bookkeeping. It must not mutate mirror bytes, file generations, fact observations, or
high-water ACK state. A reindex that sees the same mirror bytes twice must reproduce the
same `sesh_index_messages` content aside from store-local primary keys.

## Changelog

- 2026-07-09 — Amendment 2: fingerprint-bearing PUTs silently route to the
  highest-numbered generation with the matching recorded fingerprint; `fingerprint_conflict`
  is only returned when opening a new empty generation for a new fingerprint.
- 2026-07-09 — Amendment 1: clamp 200-ACK cursor advancement to the most recently
  observed source size to stop same-prefix truncation loops; pin spanning replay PUTs to
  compare the overlap and append matching excess under fsync-before-ACK; relabel the
  60-second rescan interval as a shipper-local default, not a wire contract.

## Compatibility Rules

- Adding a tool, changing a header name, changing the fingerprint window/algorithm,
  changing an error's shipper reaction, or changing any frozen index column requires an
  amendment to this document before code lands.
- Clients may ignore unknown JSON response fields, but servers must not require fields
  not listed here for v1 clients.
- The wire must remain curl-debuggable HTTP. No streaming side channel, queue protocol,
  node parser, or surface protocol is part of v1.

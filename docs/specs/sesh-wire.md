# sesh Wire and Index Schema

Status: draft for U1 co-author review. After M0 merge, this document is the frozen
shipper-to-store wire contract and binds above the implementation plan.

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
- Rescan interval: 60 seconds.
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
same file UUID. The shipper does not invent generation numbers.

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

The store stamps tailnet identity from the connection at M4. Any client-supplied
tailnet identity or display-owner header is ignored and must not affect storage,
auth, or rendering.

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

`high_water` is the next byte offset the shipper may send for the selected generation.
The shipper advances its cursor only after receiving this response.

### PUT Semantics

- If `offset == high_water`, the store appends the body to the active generation's
  mirror file, fsyncs, records last-PUT time and facts, returns `200`, then publishes an
  internal append event for indexing.
- If `offset < high_water`, the store compares the request bytes with the mirrored bytes
  at that offset.
  - Identical bytes are an idempotent replay and return `200` with the current
    `high_water`.
  - Divergent bytes are never overwritten. The store returns `409 conflict` and opens or
    points at a new generation when the fingerprint proves a recreated file; that
    response carries the new generation and `high_water: 0`. If the same fingerprint
    diverges against already-ACKed bytes, the file is poisoned.
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
| 403 | `out_of_grant` | Tailnet identity is authenticated but not allowed to ship or read | Hold cursor; retry with backoff; surface as auth/config failure |
| 404 | `not_found` | Recovery lookup has no mirror state for this file UUID/fingerprint | Start from offset 0 |
| 409 | `byte_conflict` | Request bytes diverge from mirrored bytes at an already-ACKed offset | Treat source path as recreated; clear local cursor for that file identity; rescan/recover; retry from offset 0 unless response says `poisoned` |
| 409 | `fingerprint_conflict` | Same file UUID now presents a new fingerprint; store has opened or selected a new generation | Clear local cursor for that file identity and retry from offset 0 with the current fingerprint |
| 413 | `body_too_large` | PUT body exceeds 4 MiB | Split the range into smaller PUT bodies and retry without advancing |
| 422 | `offset_gap` | Request offset is beyond the store high-water | Rewind cursor to returned `high_water` and retry |
| 423 | `poisoned_file` | Repeated conflict for the same file UUID/fingerprint/generation | Stop retrying this file; keep other files shipping; surface poisoned state in `sesh status` |
| 500 | `mirror_write_failed` | Store could not durably write or fsync mirror bytes | Do not advance; retry with backoff |
| 503 | `store_unavailable` | Store is temporarily unable to accept bytes | Do not advance; retry with backoff |

`out_of_grant` is used for both PUT and read denial once tailnet auth is enabled. Before
M4, serve mode binds ingest to loopback only; non-loopback ingest attempts must be
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
mirror state for this file UUID; the shipper starts from offset 0. If a UUID-only
recovery response contains multiple generations and the shipper still has no
fingerprint, the shipper starts from offset 0 rather than guessing a high-water from an
unrelated generation.

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

U6 writes and U7 reads the message index through this schema. Column names are frozen;
SQLite types may use the closest practical affinity.

Table: `sesh_index_messages`

| Column | Meaning |
|---|---|
| `id` | Store-local integer primary key |
| `tool` | `claude` or `codex` |
| `logical_session_id` | Parsed session id; falls back to `wire_session_id` only when unavailable |
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

## Compatibility Rules

- Adding a tool, changing a header name, changing the fingerprint window/algorithm,
  changing an error's shipper reaction, or changing any frozen index column requires an
  amendment to this document before code lands.
- Clients may ignore unknown JSON response fields, but servers must not require fields
  not listed here for v1 clients.
- The wire must remain curl-debuggable HTTP. No streaming side channel, queue protocol,
  node parser, or surface protocol is part of v1.

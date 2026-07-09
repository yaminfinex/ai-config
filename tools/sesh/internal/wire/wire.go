// Package wire transcribes docs/specs/sesh-wire.md 1:1. The document is the
// frozen shipper/store contract and binds above the implementation plan;
// nothing here may drift from it without a wire amendment landing first
// (Compatibility Rules). This package is shared vocabulary only — no logic
// beyond the frozen catalog mappings.
package wire

import "time"

// Constants (wire doc "Constants").
const (
	Version                = 1
	APIRoot                = "/v1"
	RescanInterval         = 60 * time.Second
	MaxPUTBody             = 4 << 20 // 4 MiB
	FingerprintAlgorithm   = "sha256-first-1024"
	FingerprintWindowBytes = 1024
	// ContentTypeBytes is the required Content-Type of every PUT body.
	ContentTypeBytes = "application/octet-stream"
)

// Tool is the closed enum of shippable tools. Unknown tools are rejected;
// adding one requires a wire amendment before code lands.
type Tool string

const (
	ToolClaude Tool = "claude"
	ToolCodex  Tool = "codex"
)

// Request headers (wire doc "PUT Bytes"). Fingerprint headers are present
// only once the source file reaches the fingerprint window; the session-owner
// header is omitted when absent or ambiguous, and omission never retracts a
// previously shipped observation. Client-supplied tailnet identity or
// display-owner headers are ignored.
const (
	HeaderWireVersion          = "X-Sesh-Wire-Version"
	HeaderHostname             = "X-Sesh-Hostname"
	HeaderOSUser               = "X-Sesh-OS-User"
	HeaderFingerprintAlgorithm = "X-Sesh-Fingerprint-Algorithm"
	HeaderFingerprint          = "X-Sesh-Fingerprint"
	HeaderSessionOwner         = "X-Sesh-Session-Owner"
)

// Path templates under APIRoot. Order of segments: tool, session_id,
// file_uuid. PUT takes ?offset=N (zero-based, raw file bytes); recovery GET
// takes optional ?fingerprint={lowercase_hex}.
const (
	PathPUTBytesFmt = APIRoot + "/files/%s/%s/%s/bytes"
	PathRecoveryFmt = APIRoot + "/files/%s/%s/%s"
)

// StatusAck is the "status" value of a successful ACK body.
const StatusAck = "ack"

// Ack is the 200 response to PUT bytes. HighWater is the next byte offset the
// store will accept for the selected generation; on any 200 the shipper sets
// its cursor to HighWater exactly — never to offset+len(body). No other
// response advances a cursor. Fingerprint is the generation's recorded value
// (authoritative from mirrored bytes once the mirror reaches the window) or
// null.
type Ack struct {
	WireVersion          int     `json:"wire_version"`
	Status               string  `json:"status"`
	Tool                 Tool    `json:"tool"`
	SessionID            string  `json:"session_id"`
	FileUUID             string  `json:"file_uuid"`
	Generation           int     `json:"generation"`
	HighWater            int64   `json:"high_water"`
	FingerprintAlgorithm string  `json:"fingerprint_algorithm"`
	Fingerprint          *string `json:"fingerprint"`
}

// ErrorCode is the normative field a shipper switches on; ShipperAction in
// the body is informational for logs and operators.
type ErrorCode string

// Error catalog (wire doc "Error Catalog"). The required shipper reaction per
// code is frozen prose in the doc; the doc line is repeated here so shipper
// code cites it in place.
const (
	// ErrMalformedRequest: do not advance; surface in `sesh status`; retry
	// only after local config or code changes.
	ErrMalformedRequest ErrorCode = "malformed_request"
	// ErrUnknownTool: hold every file of that tool; surface prominently; no
	// retry loop — resolution is an upgrade or a wire amendment.
	ErrUnknownTool ErrorCode = "unknown_tool"
	// ErrOutOfGrant: hold cursor; retry with slow jittered backoff; surface
	// as auth/config failure.
	ErrOutOfGrant ErrorCode = "out_of_grant"
	// ErrNotFound (recovery lookup): start from offset 0.
	ErrNotFound ErrorCode = "not_found"
	// ErrByteConflict: re-check local identity — size regression first, then
	// re-fingerprint. Fingerprint changed → resume with the new fingerprint.
	// Unchanged → retry the same PUT once; a second divergence yields
	// generation_opened or poisoned_file.
	ErrByteConflict ErrorCode = "byte_conflict"
	// ErrFingerprintConflict: set cursor to the returned high_water (0 for a
	// fresh generation) and continue with the current fingerprint.
	ErrFingerprintConflict ErrorCode = "fingerprint_conflict"
	// ErrGenerationOpened: set cursor to the returned high_water (0) and
	// re-ship from there so the new generation receives the complete new
	// history from offset 0.
	ErrGenerationOpened ErrorCode = "generation_opened"
	// ErrBodyTooLarge: split the range into smaller PUT bodies and retry
	// without advancing.
	ErrBodyTooLarge ErrorCode = "body_too_large"
	// ErrOffsetGap: rewind cursor to returned high_water and retry.
	ErrOffsetGap ErrorCode = "offset_gap"
	// ErrPoisonedFile: stop retrying this file; freeze its cursor (deletion
	// GC still applies); keep other files shipping; surface poisoned state in
	// `sesh status`.
	ErrPoisonedFile ErrorCode = "poisoned_file"
	// ErrMirrorWriteFailed: do not advance; retry with backoff.
	ErrMirrorWriteFailed ErrorCode = "mirror_write_failed"
	// ErrStoreUnavailable: do not advance; retry with backoff. An unreachable
	// store is treated exactly like this code: hold position, jittered
	// backoff, cursor untouched, no local queue — the source file is the only
	// buffer.
	ErrStoreUnavailable ErrorCode = "store_unavailable"
)

// HTTPStatus returns the frozen HTTP status for a catalog code, or 0 for a
// code not in the catalog.
func (c ErrorCode) HTTPStatus() int {
	switch c {
	case ErrMalformedRequest, ErrUnknownTool:
		return 400
	case ErrOutOfGrant:
		return 403
	case ErrNotFound:
		return 404
	case ErrByteConflict, ErrFingerprintConflict, ErrGenerationOpened:
		return 409
	case ErrBodyTooLarge:
		return 413
	case ErrOffsetGap:
		return 422
	case ErrPoisonedFile:
		return 423
	case ErrMirrorWriteFailed:
		return 500
	case ErrStoreUnavailable:
		return 503
	}
	return 0
}

// Known informational shipper_action values named by the doc.
const (
	ShipperActionRewind        = "rewind"
	ShipperActionStartFromZero = "start_from_zero"
)

// ErrorResponse is the application/json body of every error response.
type ErrorResponse struct {
	WireVersion   int       `json:"wire_version"`
	Code          ErrorCode `json:"error"`
	Message       string    `json:"message"`
	Tool          Tool      `json:"tool"`
	SessionID     string    `json:"session_id"`
	FileUUID      string    `json:"file_uuid"`
	Generation    int       `json:"generation"`
	HighWater     int64     `json:"high_water"`
	ShipperAction string    `json:"shipper_action"`
}

// RecoveryResponse is the 200 body of recovery GET, for a shipper whose
// cursor registry is missing or unreadable. UUID-only lookup is allowed
// before the source file reaches the fingerprint window.
type RecoveryResponse struct {
	WireVersion            int               `json:"wire_version"`
	Tool                   Tool              `json:"tool"`
	SessionID              string            `json:"session_id"`
	FileUUID               string            `json:"file_uuid"`
	FingerprintAlgorithm   string            `json:"fingerprint_algorithm"`
	FingerprintWindowBytes int               `json:"fingerprint_window_bytes"`
	Generations            []GenerationState `json:"generations"`
}

// GenerationState is one generation's recovery state. Generation numbers are
// zero-based per (tool, session_id, file_uuid); the active generation is the
// highest number; the shipper never invents them.
type GenerationState struct {
	Generation      int       `json:"generation"`
	Fingerprint     *string   `json:"fingerprint"`
	HighWater       int64     `json:"high_water"`
	Poisoned        bool      `json:"poisoned"`
	DirtyForReindex bool      `json:"dirty_for_reindex"`
	LastPutAt       time.Time `json:"last_put_at"`
}

// AppendEvent is published in-process by the ingest handler after a
// successful mirror ACK, for the indexer. It is internal to the store
// process — not a second shipper/store protocol.
type AppendEvent struct {
	Tool          Tool   `json:"tool"`
	WireSessionID string `json:"wire_session_id"`
	FileUUID      string `json:"file_uuid"`
	Generation    int    `json:"generation"`
	ByteStart     int64  `json:"byte_start"`
	ByteEnd       int64  `json:"byte_end"`
}

// IndexMessagesTable is the frozen message index table U6 writes and U7
// reads. Column names are frozen; SQLite types may use the closest practical
// affinity.
const IndexMessagesTable = "sesh_index_messages"

// IndexMessage is one row of IndexMessagesTable. Each field's frozen column
// name is its db tag.
//
// Dedup key for parsed messages:
//
//	(tool, logical_session_id, entry_type, message_uuid)
//
// Rows with an empty message_uuid are not deduped by that key. Transcript
// ordering for the surface is (timestamp_utc nulls last, file_ordinal,
// line_ordinal, file_uuid, generation); recency is the maximum parsed
// timestamp_utc for a logical session. The indexer parses only complete JSONL
// lines; trailing partial lines stay mirrored but absent here until
// completed. Logical-session unification (content ids first, then overlap on
// at least two non-empty (entry_type, message_uuid) pairs, earliest file's
// content id by first-ingest order of generation 0) is store-side index
// logic per the doc's Message Index Schema section.
type IndexMessage struct {
	ID               int64      `db:"id"`
	Tool             Tool       `db:"tool"`
	LogicalSessionID string     `db:"logical_session_id"`
	WireSessionID    string     `db:"wire_session_id"`
	EntryType        string     `db:"entry_type"`
	MessageUUID      string     `db:"message_uuid"`
	FileUUID         string     `db:"file_uuid"`
	Generation       int        `db:"generation"`
	Role             string     `db:"role"`
	TimestampUTC     *time.Time `db:"timestamp_utc"`
	FileOrdinal      int64      `db:"file_ordinal"`
	LineOrdinal      int64      `db:"line_ordinal"`
	ByteStart        int64      `db:"byte_start"`
	ByteEnd          int64      `db:"byte_end"`
	Quarantine       bool       `db:"quarantine"`
	QuarantineReason string     `db:"quarantine_reason"`
}

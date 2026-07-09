package wire

import (
	"bytes"
	"encoding/json"
	"testing"
)

// The JSON bodies below are copied verbatim from docs/specs/sesh-wire.md.
// Strict decoding (DisallowUnknownFields) fails if the doc names a field this
// package lacks; the key-set comparison fails if this package emits a field
// the doc does not name. Either failure means the transcription drifted.

const docAckExample = `{
  "wire_version": 1,
  "status": "ack",
  "tool": "claude",
  "session_id": "session-uuid",
  "file_uuid": "file-uuid",
  "generation": 0,
  "high_water": 12345,
  "fingerprint_algorithm": "sha256-first-1024",
  "fingerprint": "lowercase-hex-or-null"
}`

const docErrorExample = `{
  "wire_version": 1,
  "error": "offset_gap",
  "message": "human-readable operator text",
  "tool": "claude",
  "session_id": "session-uuid",
  "file_uuid": "file-uuid",
  "generation": 0,
  "high_water": 8192,
  "shipper_action": "rewind"
}`

const docRecoveryExample = `{
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
}`

const docAppendEventExample = `{
  "tool": "claude",
  "wire_session_id": "session-uuid",
  "file_uuid": "file-uuid",
  "generation": 0,
  "byte_start": 8192,
  "byte_end": 12345
}`

func strictDecode(t *testing.T, doc string, into any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader([]byte(doc)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		t.Fatalf("doc example does not strict-decode into %T: %v", into, err)
	}
}

// keySet returns the top-level JSON keys a marshaled value emits.
func keySet(t *testing.T, v any) map[string]bool {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for k := range m {
		keys[k] = true
	}
	return keys
}

func assertSameKeys(t *testing.T, docExample string, v any) {
	t.Helper()
	var docKeys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(docExample), &docKeys); err != nil {
		t.Fatal(err)
	}
	got := keySet(t, v)
	for k := range docKeys {
		if !got[k] {
			t.Errorf("%T does not emit doc field %q", v, k)
		}
	}
	for k := range got {
		if _, ok := docKeys[k]; !ok {
			t.Errorf("%T emits field %q the doc does not name", v, k)
		}
	}
}

func TestAckMatchesDoc(t *testing.T) {
	var a Ack
	strictDecode(t, docAckExample, &a)
	if a.WireVersion != Version || a.Status != StatusAck || a.Tool != ToolClaude {
		t.Errorf("decoded ack header fields wrong: %+v", a)
	}
	if a.HighWater != 12345 || a.Generation != 0 {
		t.Errorf("decoded ack positions wrong: %+v", a)
	}
	if a.FingerprintAlgorithm != FingerprintAlgorithm {
		t.Errorf("ack fingerprint_algorithm %q != frozen constant %q", a.FingerprintAlgorithm, FingerprintAlgorithm)
	}
	assertSameKeys(t, docAckExample, a)
}

func TestErrorResponseMatchesDoc(t *testing.T) {
	var e ErrorResponse
	strictDecode(t, docErrorExample, &e)
	if e.Code != ErrOffsetGap || e.ShipperAction != ShipperActionRewind {
		t.Errorf("decoded error fields wrong: %+v", e)
	}
	assertSameKeys(t, docErrorExample, e)
}

func TestRecoveryResponseMatchesDoc(t *testing.T) {
	var r RecoveryResponse
	strictDecode(t, docRecoveryExample, &r)
	if r.FingerprintWindowBytes != FingerprintWindowBytes {
		t.Errorf("recovery window %d != frozen constant %d", r.FingerprintWindowBytes, FingerprintWindowBytes)
	}
	if len(r.Generations) != 1 || r.Generations[0].HighWater != 12345 {
		t.Errorf("decoded generations wrong: %+v", r.Generations)
	}
	assertSameKeys(t, docRecoveryExample, r)
	assertSameKeys(t, `{"generation":0,"fingerprint":null,"high_water":0,"poisoned":false,"dirty_for_reindex":false,"last_put_at":"2026-07-09T00:00:00Z"}`, r.Generations[0])
}

func TestAppendEventMatchesDoc(t *testing.T) {
	var ev AppendEvent
	strictDecode(t, docAppendEventExample, &ev)
	if ev.ByteStart != 8192 || ev.ByteEnd != 12345 {
		t.Errorf("decoded append event wrong: %+v", ev)
	}
	assertSameKeys(t, docAppendEventExample, ev)
}

func TestErrorCatalogHTTPStatuses(t *testing.T) {
	frozen := map[ErrorCode]int{
		ErrMalformedRequest:    400,
		ErrUnknownTool:         400,
		ErrOutOfGrant:          403,
		ErrNotFound:            404,
		ErrByteConflict:        409,
		ErrFingerprintConflict: 409,
		ErrGenerationOpened:    409,
		ErrBodyTooLarge:        413,
		ErrOffsetGap:           422,
		ErrPoisonedFile:        423,
		ErrMirrorWriteFailed:   500,
		ErrStoreUnavailable:    503,
	}
	if len(frozen) != 12 {
		t.Fatalf("catalog must hold exactly 12 codes, test lists %d", len(frozen))
	}
	for code, want := range frozen {
		if got := code.HTTPStatus(); got != want {
			t.Errorf("%s: HTTPStatus() = %d, want %d", code, got, want)
		}
	}
	if got := ErrorCode("no_such_code").HTTPStatus(); got != 0 {
		t.Errorf("unknown code HTTPStatus() = %d, want 0", got)
	}
}

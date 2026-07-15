package spawncmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/registry"
)

func TestRecordedBusSessionEvidenceAcceptsFieldRepairShapeThroughWriterAndLoader(t *testing.T) {
	row := writerLoadedRepairRecord(t, nil, "seated", "enroll")

	sid, reason := recordedBusSessionEvidence(&row)
	if sid != "sess-me" || reason != "" {
		t.Fatalf("recordedBusSessionEvidence = (%q, %q), want field SID accepted", sid, reason)
	}
	if row.HcomVerified != nil {
		t.Fatalf("writer-loaded field row HcomVerified = %v, want absent", row.HcomVerified)
	}
}

func TestRecordedBusSessionEvidenceAcceptsCurrentWriterVerifiedShape(t *testing.T) {
	verified := true
	row := writerLoadedRepairRecord(t, &verified, "seated", "spawn")

	sid, reason := recordedBusSessionEvidence(&row)
	if sid != "sess-me" || reason != "" {
		t.Fatalf("recordedBusSessionEvidence = (%q, %q), want verified writer SID accepted", sid, reason)
	}
}

func TestRecordedBusSessionEvidenceFailsClosedForWriterLoadedRows(t *testing.T) {
	falseValue := false
	tests := []struct {
		name         string
		verification *bool
		state        string
		mechanism    string
		wantReason   string
	}{
		{
			name:         "explicit unverified marker",
			verification: &falseValue,
			state:        "seated",
			mechanism:    "enroll",
			wantReason:   "explicitly false",
		},
		{
			name:       "non-enroll provenance",
			state:      "seated",
			mechanism:  "spawn",
			wantReason: `provenance.mechanism is "spawn"`,
		},
		{
			name:       "non-seated state",
			state:      "unseated",
			mechanism:  "enroll",
			wantReason: `row state is "unseated"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := writerLoadedRepairRecord(t, tt.verification, tt.state, tt.mechanism)
			sid, reason := recordedBusSessionEvidence(&row)
			if sid != "" || !strings.Contains(reason, tt.wantReason) {
				t.Fatalf("recordedBusSessionEvidence = (%q, %q), want refusal containing %q", sid, reason, tt.wantReason)
			}
		})
	}
}

// These mutations deliberately create typed/raw combinations that the current
// writer+loader pair cannot emit. They pin recordedBusSessionEvidence's
// fail-closed function contract against old rows and future writer drift; they
// are not presented as independently observable proof from today's writer.
func TestRecordedBusSessionEvidenceFailsClosedForSyntheticPersistedProofDrift(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*registry.Record)
		wantReason string
	}{
		{
			name: "unconfirmed continuity",
			mutate: func(row *registry.Record) {
				row.Raw = replaceRawField(row.Raw, `"continuity":"confirmed"`, `"continuity":"assumed"`)
			},
			wantReason: `continuity is "assumed"`,
		},
		{
			name: "non-harvest sid",
			mutate: func(row *registry.Record) {
				row.Raw = replaceRawField(row.Raw, `"source":"harvest"`, `"source":"observed"`)
			},
			wantReason: "no harvest SID matches",
		},
		{
			name: "typed and persisted bus names diverge",
			mutate: func(row *registry.Record) {
				row.Raw = replaceRawField(row.Raw, `"hcom_name":"me-bus"`, `"hcom_name":"other-bus"`)
			},
			wantReason: "persisted bus name is missing or inconsistent",
		},
		{
			name: "typed and persisted session ids diverge",
			mutate: func(row *registry.Record) {
				row.Raw = replaceRawField(row.Raw, `"tool_session_id":"sess-me"`, `"tool_session_id":"sess-other"`)
			},
			wantReason: "provenance.tool_session_id is inconsistent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := writerLoadedRepairRecord(t, nil, "seated", "enroll")
			before := string(row.Raw)
			tt.mutate(&row)
			if string(row.Raw) == before {
				t.Fatal("synthetic mutation was a no-op")
			}
			sid, reason := recordedBusSessionEvidence(&row)
			if sid != "" || !strings.Contains(reason, tt.wantReason) {
				t.Fatalf("recordedBusSessionEvidence = (%q, %q), want refusal containing %q", sid, reason, tt.wantReason)
			}
		})
	}
}

func TestRecordedBusSessionEvidenceNamesLegacyV1Limitation(t *testing.T) {
	row := loadRegistryRecord(t, []byte(`{"guid":"guid-me-0000","label":"me","agent":"claude","status":"active","terminal_id":"term_ME","pane_id":"w1-2","hcom_name":"me-bus","provenance":{"mechanism":"enroll","tool_session_id":"sess-me"}}`))

	sid, reason := recordedBusSessionEvidence(&row)
	if sid != "" || !strings.Contains(reason, "legacy-v1 rows do not carry the v2 recorded-SID repair proof") {
		t.Fatalf("recordedBusSessionEvidence = (%q, %q), want legacy-v1 attribution", sid, reason)
	}
}

func writerLoadedRepairRecord(t *testing.T, verification *bool, state, mechanism string) registry.Record {
	t.Helper()
	guid, label := "guid-me-0000", "me"
	written := registry.V2FromRecord(registry.Record{
		GUID:         &guid,
		Label:        &label,
		Role:         "worker",
		Agent:        "claude",
		TerminalID:   "term_ME",
		PaneID:       "w1-2",
		HcomName:     "me-bus",
		HcomVerified: verification,
		Provenance: &registry.Provenance{
			Mechanism:     mechanism,
			ToolSessionID: "sess-me",
		},
	}, "recognised", state, "2026-07-15T04:00:00Z")
	raw, err := json.Marshal(written)
	if err != nil {
		t.Fatal(err)
	}
	return loadRegistryRecord(t, raw)
}

func loadRegistryRecord(t *testing.T, raw []byte) registry.Record {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	recs, err := registry.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("registry.Load returned %d rows, want 1", len(recs))
	}
	return recs[0]
}

func replaceRawField(raw []byte, old, replacement string) []byte {
	return []byte(strings.Replace(string(raw), old, replacement, 1))
}

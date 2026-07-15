package spawncmd

import (
	"strings"
	"testing"

	"ai-config/tools/herder/internal/registry"
)

func TestRecordedBusSessionEvidenceAcceptsFieldRepairShape(t *testing.T) {
	row := fieldRepairRecord()

	sid, reason := recordedBusSessionEvidence(&row)
	if sid != "sess-me" || reason != "" {
		t.Fatalf("recordedBusSessionEvidence = (%q, %q), want field SID accepted", sid, reason)
	}
}

func TestRecordedBusSessionEvidenceFailsClosedWithoutCompleteProof(t *testing.T) {
	falseValue := false
	tests := []struct {
		name       string
		mutate     func(*registry.Record)
		wantReason string
	}{
		{
			name: "explicit false",
			mutate: func(row *registry.Record) {
				row.HcomVerified = &falseValue
			},
			wantReason: "explicitly false",
		},
		{
			name: "unconfirmed continuity",
			mutate: func(row *registry.Record) {
				row.Raw = []byte(strings.Replace(string(row.Raw), `"continuity":"confirmed"`, `"continuity":"assumed"`, 1))
			},
			wantReason: `continuity is "assumed"`,
		},
		{
			name: "non-enroll provenance",
			mutate: func(row *registry.Record) {
				row.Raw = []byte(strings.Replace(string(row.Raw), `"mechanism":"enroll"`, `"mechanism":"spawn"`, 1))
			},
			wantReason: `provenance.mechanism is "spawn"`,
		},
		{
			name: "non-harvest sid",
			mutate: func(row *registry.Record) {
				row.Raw = []byte(strings.Replace(string(row.Raw), `"source":"harvest"`, `"source":"observed"`, 1))
			},
			wantReason: "no harvest SID matches",
		},
		{
			name: "different stored bus name",
			mutate: func(row *registry.Record) {
				row.Raw = []byte(strings.Replace(string(row.Raw), `"hcom_name":"me-bus"`, `"hcom_name":"other-bus"`, 1))
			},
			wantReason: "persisted bus name is missing or inconsistent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := fieldRepairRecord()
			tt.mutate(&row)
			sid, reason := recordedBusSessionEvidence(&row)
			if sid != "" || !strings.Contains(reason, tt.wantReason) {
				t.Fatalf("recordedBusSessionEvidence = (%q, %q), want refusal containing %q", sid, reason, tt.wantReason)
			}
		})
	}
}

func fieldRepairRecord() registry.Record {
	return registry.Record{
		HcomName: "me-bus",
		Provenance: &registry.Provenance{
			Mechanism:     "enroll",
			ToolSessionID: "sess-me",
		},
		Raw: []byte(`{"kind":"session","guid":"guid-me-0000","event":"recognised","state":"seated","seat":{"kind":"herdr","hcom_name":"me-bus","confirmed_at":"2026-07-15T04:00:00Z"},"sids":[{"sid":"sess-me","source":"harvest"}],"continuity":"confirmed","provenance":{"mechanism":"enroll","tool_session_id":"sess-me"}}`),
	}
}

package registry

import (
	"encoding/json"
	"testing"

	v2 "ai-config/tools/herder/internal/registry/v2"
)

func TestPiLaunchFactsRoundTripThroughV2Projection(t *testing.T) {
	guid, short, label := "guid-pi", "guid-pi", "worker-pi"
	verified := true
	hooks := true
	history := &v2.VendorVersionHistory{
		Current:  v2.VendorVersionObservation{Version: "0.80.6", ObservedAt: "2026-07-15T01:00:00Z"},
		Previous: &v2.VendorVersionObservation{Version: "0.80.5", ObservedAt: "2026-07-15T00:00:00Z"},
	}
	rec := Record{
		GUID: &guid, ShortGUID: &short, Label: &label,
		Role: "worker", Agent: "pi", PaneID: "p_pi", TerminalID: "term_pi",
		HcomName: "worker-pi", HcomVerified: &verified,
		Provider: "openai", Model: "gpt-test", VendorVersion: history,
		HooksBound: &hooks, TranscriptPath: "/scratch/session.jsonl",
		Provenance: &Provenance{ToolSessionID: "session-pi", TS: "2026-07-15T01:00:00Z"},
	}
	row := V2FromRecord(rec, "registered", v2.StateSeated, "2026-07-15T01:00:00Z")
	if row.Provider != "openai" || row.Model != "gpt-test" || row.VendorVersion == nil || row.VendorVersion.Previous == nil {
		t.Fatalf("launch facts missing from v2 row: %+v", row)
	}
	if row.Seat == nil || !row.Seat.HooksBound || row.Seat.TranscriptPath != "/scratch/session.jsonl" {
		t.Fatalf("bind facts missing from v2 seat: %+v", row.Seat)
	}
	if len(row.SIDs) != 1 || row.SIDs[0].SID != "session-pi" {
		t.Fatalf("session id missing from v2 row: %+v", row.SIDs)
	}

	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &obj); err != nil {
		t.Fatal(err)
	}
	back := recordFromV2SessionObject(obj)
	if back.Provider != rec.Provider || back.Model != rec.Model || back.VendorVersion == nil || back.VendorVersion.Current.Version != "0.80.6" {
		t.Fatalf("v2 projection dropped launch facts: %+v", back)
	}
	if back.HooksBound == nil || !*back.HooksBound || back.TranscriptPath != rec.TranscriptPath {
		t.Fatalf("v2 projection dropped bind facts: %+v", back)
	}
}

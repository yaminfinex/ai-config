package spawncmd

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"ai-config/tools/herder/internal/registry"
)

func TestHcomEntryAcceptsNumericCreatedAt(t *testing.T) {
	var entries []hcomEntry
	data := []byte(`[{"name":"smoke-p5-tuna","tag":"smoke-p5","directory":"/tmp","created_at":1782979094.0}]`)
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got := string(entries[0].CreatedAt); got != "1782979094.0" {
		t.Fatalf("CreatedAt = %q, want numeric value preserved", got)
	}
}

func TestRegistryCapturedNameUsesLatestEnrichmentRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	if err := registry.Append(path, []byte(`{"guid":"guid-1","short_guid":"guid","label":"worker-guid","hcom_name":"","status":"active"}`)); err != nil {
		t.Fatal(err)
	}
	if err := registry.Append(path, []byte(`{"guid":"guid-1","short_guid":"guid","label":"worker-guid","hcom_name":"worker-rive","status":"active","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-1","tag":"worker","batch_id":"","cwd":"/repo","workspace_id":"ws","branch":"","ts":"2026-07-03T00:00:00Z"}}`)); err != nil {
		t.Fatal(err)
	}
	if got := registryCapturedName(path, "guid-1"); got != "worker-rive" {
		t.Fatalf("registryCapturedName = %q, want worker-rive", got)
	}
}

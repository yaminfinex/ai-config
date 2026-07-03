package sidecarcmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/registry"
)

func TestMapStatus(t *testing.T) {
	tests := map[string]struct {
		want string
		ok   bool
	}{
		"active":    {"working", true},
		"listening": {"idle", true},
		"blocked":   {"blocked", true},
		"starting":  {"", false},
		"":          {"", false},
	}
	for input, tt := range tests {
		got, ok := mapStatus(input)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("mapStatus(%q) = (%q, %v), want (%q, %v)", input, got, ok, tt.want, tt.ok)
		}
	}
}

func TestFindRowForPaneWithFlexibleCreatedAt(t *testing.T) {
	fixture := []byte(`[
	  {"name":"other","tool":"codex","tag":"worker","directory":"/tmp","status":"active","created_at":1700000000,"launch_context":{"pane_id":"p_other"}},
	  {"name":"target","tool":"codex","tag":"worker","directory":"/tmp","status":"listening","session_id":"s1","created_at":"2026-07-03T00:00:00Z","launch_context":{"pane_id":"p_target"}}
	]`)
	var rows []hcomRow
	if err := json.Unmarshal(fixture, &rows); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	row := findRowForPane(rows, "p_target")
	if row == nil {
		t.Fatal("findRowForPane returned nil")
	}
	if row.Name != "target" || row.Status != "listening" || row.SessionID != "s1" {
		t.Fatalf("row = %+v, want target/listening/s1", *row)
	}
	if row.CreatedAt == "" {
		t.Fatal("created_at was not retained")
	}
	if got := findRowForPane(rows, "missing"); got != nil {
		t.Fatalf("findRowForPane(missing) = %+v, want nil", *got)
	}
}

func TestFindRowForLaunchFallbackUsesLatestMatchingRow(t *testing.T) {
	rows := []hcomRow{
		{Name: "wrong-tool", Tool: "claude", Tag: "smoke", Directory: "/work", Status: "active"},
		{Name: "first", Tool: "codex", Tag: "smoke", Directory: "/work", Status: "active"},
		{Name: "wrong-dir", Tool: "codex", Tag: "smoke", Directory: "/other", Status: "listening"},
		{Name: "latest", Tool: "codex", Tag: "smoke", Directory: "/work", Status: "listening"},
	}
	row := findRowForLaunchFallback(rows, "codex", "smoke", "/work")
	if row == nil {
		t.Fatal("findRowForLaunchFallback returned nil")
	}
	if row.Name != "latest" || row.Status != "listening" {
		t.Fatalf("row = %+v, want latest/listening", *row)
	}
	if got := findRowForLaunchFallback(rows, "codex", "", "/work"); got != nil {
		t.Fatalf("empty tag matched %+v, want nil", *got)
	}
}

func TestAppendEnrichmentCarriesPriorRowAndSessionID(t *testing.T) {
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	prior := `{"guid":"guid-spawned-0000","short_guid":"guid","label":"worker-guid","role":"worker","agent":"codex","terminal_id":"term_OLD","pane_id":"p_old","status":"active","extra_field":"keep","provenance":{"mechanism":"spawn","spawned_by":"parent-guid","tool_session_id":"","tag":"worker","batch_id":"","cwd":"/old","workspace_id":"ws_old","branch":"","ts":"2026-07-03T00:00:00Z"}}`
	if err := registry.Append(registryPath, []byte(prior)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_GUID", "guid-spawned-0000")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HERDER_SPAWNED_BY", "parent-guid")
	t.Setenv("HCOM_DIR", "/hcom")

	s := &sidecar{tool: "codex", paneID: "p_new", cwd: "/repo", registry: registryPath}
	s.appendEnrichment(&hcomRow{Name: "worker-rive", Tag: "worker", SessionID: "sess-123", Directory: "/repo"})

	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("rows = %d, want 2", len(recs))
	}
	latest := registry.Resolve(recs, "guid-spawned-0000")
	if latest == nil {
		t.Fatal("latest row not found")
	}
	if latest.HcomName != "worker-rive" {
		t.Fatalf("HcomName = %q, want worker-rive", latest.HcomName)
	}
	if latest.Provenance == nil || latest.Provenance.ToolSessionID != "sess-123" || latest.Provenance.Mechanism != "spawn" {
		t.Fatalf("Provenance = %+v, want spawn with sess-123", latest.Provenance)
	}
	if !strings.Contains(string(latest.Raw), `"extra_field":"keep"`) {
		t.Fatalf("enrichment row did not carry prior extra field: %s", latest.Raw)
	}
}

func TestAppendEnrichmentGeneratesManualShimIdentity(t *testing.T) {
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	t.Setenv("HERDER_GUID", "")
	t.Setenv("HERDER_LABEL", "")
	t.Setenv("HERDER_ROLE", "")
	t.Setenv("HERDER_SPAWNED_BY", "")
	t.Setenv("HERDER_SHIM", "1")
	t.Setenv("HCOM_DIR", "/hcom")

	s := &sidecar{tool: "claude", paneID: "p_manual", cwd: "/repo", registry: registryPath}
	s.appendEnrichment(&hcomRow{Name: "manual-rive", Tag: "manual", SessionID: "sess-manual", Directory: "/repo"})

	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("rows = %d, want 1", len(recs))
	}
	rec := recs[0]
	if ptrString(rec.Label) == "" || !strings.HasPrefix(ptrString(rec.Label), "manual-") {
		t.Fatalf("label = %q, want manual-<short>", ptrString(rec.Label))
	}
	if rec.Role != "manual" || rec.Agent != "claude" || rec.HcomName != "manual-rive" {
		t.Fatalf("record = %+v, want manual claude with hcom name", rec)
	}
	if rec.Provenance == nil || rec.Provenance.Mechanism != "shim" || rec.Provenance.SpawnedBy != "user" {
		t.Fatalf("Provenance = %+v, want shim/user", rec.Provenance)
	}
}

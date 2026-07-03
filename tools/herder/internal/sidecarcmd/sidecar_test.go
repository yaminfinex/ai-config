package sidecarcmd

import (
	"encoding/json"
	"testing"
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

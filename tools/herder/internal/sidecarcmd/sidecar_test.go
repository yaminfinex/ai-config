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
	row := findRowForPane(rows, "p_target", "", "")
	if row == nil {
		t.Fatal("findRowForPane returned nil")
	}
	if row.Name != "target" || row.Status != "listening" || row.SessionID != "s1" {
		t.Fatalf("row = %+v, want target/listening/s1", *row)
	}
	if row.CreatedAt == "" {
		t.Fatal("created_at was not retained")
	}
	if got := findRowForPane(rows, "missing", "", ""); got != nil {
		t.Fatalf("findRowForPane(missing) = %+v, want nil", *got)
	}
}

func TestFindRowForPaneSkipsForkParentSession(t *testing.T) {
	rows := []hcomRow{
		{Name: "parent", SessionID: "sess-parent", LaunchContext: struct {
			PaneID string `json:"pane_id"`
		}{PaneID: "p_target"}},
		{Name: "child", SessionID: "sess-child", LaunchContext: struct {
			PaneID string `json:"pane_id"`
		}{PaneID: "p_target"}},
	}
	row := findRowForPane(rows, "p_target", "fork", "sess-parent")
	if row == nil {
		t.Fatal("findRowForPane returned nil")
	}
	if row.Name != "child" {
		t.Fatalf("row = %s, want child", row.Name)
	}
	if got := findRowForPane(rows[:1], "p_target", "fork", "sess-parent"); got != nil {
		t.Fatalf("parent session pane match returned %+v, want nil", *got)
	}
}

func TestFindRowForLaunchFallbackRequiresUniqueMatch(t *testing.T) {
	// A single tag+cwd match (decoys ruled out by tool/dir) is captured.
	unique := []hcomRow{
		{Name: "wrong-tool", Tool: "claude", Tag: "smoke", Directory: "/work", Status: "active"},
		{Name: "wrong-dir", Tool: "codex", Tag: "smoke", Directory: "/other", Status: "listening"},
		{Name: "mine", Tool: "codex", Tag: "smoke", Directory: "/work", Status: "listening"},
	}
	row := findRowForLaunchFallback(unique, "codex", "smoke", "/work", "", "")
	if row == nil || row.Name != "mine" {
		t.Fatalf("unique match = %+v, want mine", row)
	}

	// Two live agents share tool+tag+cwd: no positive correlate → refuse to
	// guess (this is the wrong-guid enrichment path; newest-wins would grab
	// whichever registered last).
	ambiguous := []hcomRow{
		{Name: "first", Tool: "codex", Tag: "smoke", Directory: "/work", Status: "active"},
		{Name: "latest", Tool: "codex", Tag: "smoke", Directory: "/work", Status: "listening"},
	}
	if got := findRowForLaunchFallback(ambiguous, "codex", "smoke", "/work", "", ""); got != nil {
		t.Fatalf("ambiguous tag+cwd matched %+v, want nil (refuse to guess)", *got)
	}

	if got := findRowForLaunchFallback(unique, "codex", "", "/work", "", ""); got != nil {
		t.Fatalf("empty tag matched %+v, want nil", *got)
	}
}

// TestFindRowRefusesAmbiguousFallbackForHeadlessLaunch reproduces the forensic:
// an orchestrator sidecar (its own guid) fails to pane-correlate any hcom row
// (its launch_context drifted across a compaction), and a headless launch
// (calc17-tina) shares tool+tag+cwd. Newest-wins used to attach calc17-tina's
// name onto the orchestrator's guid. The positive-correlate invariant must
// refuse the enrichment entirely rather than guess.
func TestFindRowRefusesAmbiguousFallbackForHeadlessLaunch(t *testing.T) {
	s := &sidecar{tool: "claude", paneID: "p_orch", tag: "orchestrator", cwd: "/repo"}
	rows := []hcomRow{
		{Name: "orchestrator-bumo", Tool: "claude", Tag: "orchestrator", Directory: "/repo", Status: "listening", LaunchContext: struct {
			PaneID string `json:"pane_id"`
		}{PaneID: "p_gone"}},
		{Name: "calc17-tina", Tool: "claude", Tag: "orchestrator", Directory: "/repo", Status: "listening", LaunchContext: struct {
			PaneID string `json:"pane_id"`
		}{PaneID: ""}},
	}
	if got := s.findRow(rows); got != nil {
		t.Fatalf("findRow attached %q despite no positive correlate; want nil", got.Name)
	}

	// Positive control: once the orchestrator's own row carries the matching
	// pane_id, that pane correlate wins and enrichment proceeds.
	rows[0].LaunchContext.PaneID = "p_orch"
	if got := s.findRow(rows); got == nil || got.Name != "orchestrator-bumo" {
		t.Fatalf("pane-correlated findRow = %+v, want orchestrator-bumo", got)
	}
}

func TestFindRowForLaunchFallbackSkipsInactiveAndForkParentSession(t *testing.T) {
	rows := []hcomRow{
		{Name: "inactive-child", Tool: "codex", Tag: "worker", Directory: "/repo", Status: "inactive"},
		{Name: "parent", Tool: "codex", Tag: "worker", Directory: "/repo", Status: "listening", SessionID: "sess-parent"},
		{Name: "child", Tool: "codex", Tag: "worker", Directory: "/repo", Status: "listening", SessionID: "sess-child"},
	}
	row := findRowForLaunchFallback(rows, "codex", "worker", "/repo", "fork", "sess-parent")
	if row == nil {
		t.Fatal("findRowForLaunchFallback returned nil")
	}
	if row.Name != "child" {
		t.Fatalf("row = %s, want child", row.Name)
	}
	onlyParent := rows[:2]
	if got := findRowForLaunchFallback(onlyParent, "codex", "worker", "/repo", "fork", "sess-parent"); got != nil {
		t.Fatalf("parent session fallback matched %+v, want nil", *got)
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

func TestAppendEnrichmentRecognizesResumeBySessionID(t *testing.T) {
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	rows := []string{
		`{"guid":"guid-resume-0000","short_guid":"guid","label":"resume-old","role":"worker","agent":"claude","terminal_id":"term_OLD","pane_id":"p_old","status":"active","provenance":{"mechanism":"spawn","spawned_by":"parent-guid","tool_session_id":"sess-resume","tag":"worker","batch_id":"","cwd":"/old","workspace_id":"ws_old","branch":"old-branch","ts":"2026-07-03T00:00:00Z"}}`,
		`{"guid":"guid-resume-0000","short_guid":"guid","label":"resume-latest","role":"worker","agent":"claude","terminal_id":"term_OLD","pane_id":"p_old","status":"closed","provenance":{"mechanism":"spawn","spawned_by":"parent-guid","tool_session_id":"","tag":"worker","batch_id":"","cwd":"/old","workspace_id":"ws_old","branch":"old-branch","ts":"2026-07-03T00:01:00Z"}}`,
	}
	for _, row := range rows {
		if err := registry.Append(registryPath, []byte(row)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HERDER_GUID", "")
	t.Setenv("HERDER_LABEL", "")
	t.Setenv("HERDER_ROLE", "")
	t.Setenv("HERDER_SPAWNED_BY", "")
	t.Setenv("HERDER_SHIM", "")
	t.Setenv("HCOM_DIR", "/hcom")

	s := &sidecar{tool: "claude", paneID: "p_new", cwd: "/repo", registry: registryPath}
	s.appendEnrichment(&hcomRow{Name: "resume-vire", Tag: "worker", SessionID: "sess-resume", Directory: "/repo"})

	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("rows = %d, want 3", len(recs))
	}
	latest := registry.Resolve(recs, "guid-resume-0000")
	if latest == nil {
		t.Fatal("latest resumed row not found")
	}
	if ptrString(latest.Label) != "resume-latest" || latest.Status != "active" || latest.HcomName != "resume-vire" {
		t.Fatalf("latest = label %q status %q hcom %q, want carried active resume-vire", ptrString(latest.Label), latest.Status, latest.HcomName)
	}
	if latest.Provenance == nil {
		t.Fatal("provenance missing")
	}
	if latest.Provenance.Mechanism != "spawn" || latest.Provenance.ToolSessionID != "sess-resume" || latest.Provenance.ResumedAt == "" {
		t.Fatalf("Provenance = %+v, want spawn sess-resume with resumed_at", latest.Provenance)
	}
	if latest.Provenance.ForkedFrom != "" {
		t.Fatalf("ForkedFrom = %q, want empty", latest.Provenance.ForkedFrom)
	}
}

func TestAppendEnrichmentDoesNotResurrectClosedGUID(t *testing.T) {
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	prior := `{"guid":"guid-closed-0000","short_guid":"closed","label":"closed-worker","role":"worker","agent":"codex","terminal_id":"term_OLD","pane_id":"p_old","status":"closed","hcom_name":"worker-rive","provenance":{"mechanism":"spawn","spawned_by":"parent-guid","tool_session_id":"sess-123","tag":"worker","batch_id":"","cwd":"/old","workspace_id":"ws_old","branch":"","ts":"2026-07-03T00:00:00Z"}}`
	if err := registry.Append(registryPath, []byte(prior)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_GUID", "guid-closed-0000")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HERDER_LABEL", "closed-worker")
	t.Setenv("HCOM_DIR", "/hcom")

	s := &sidecar{tool: "codex", paneID: "p_new", cwd: "/repo", registry: registryPath}
	s.appendEnrichment(&hcomRow{Name: "worker-rive", Tag: "worker", SessionID: "sess-123", Directory: "/repo"})

	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("rows = %d, want 1 closed row only", len(recs))
	}
	latest := registry.Resolve(recs, "guid-closed-0000")
	if latest == nil || latest.Status != "closed" {
		t.Fatalf("latest = %+v, want closed", latest)
	}
}

func TestAppendEnrichmentRefusesActiveLabelCollision(t *testing.T) {
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	prior := `{"guid":"guid-other-0000","short_guid":"other","label":"taken","role":"worker","agent":"claude","terminal_id":"term_OTHER","pane_id":"p_other","status":"active"}`
	if err := registry.Append(registryPath, []byte(prior)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_GUID", "")
	t.Setenv("HERDER_LABEL", "taken")
	t.Setenv("HERDER_ROLE", "manual")
	t.Setenv("HERDER_SPAWNED_BY", "")
	t.Setenv("HCOM_DIR", "/hcom")

	s := &sidecar{tool: "claude", paneID: "p_manual", cwd: "/repo", registry: registryPath}
	s.appendEnrichment(&hcomRow{Name: "manual-rive", Tag: "manual", SessionID: "sess-manual", Directory: "/repo"})

	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("rows = %d, want collision to skip append", len(recs))
	}
	if latest := registry.Resolve(recs, "taken"); latest == nil || ptrString(latest.GUID) != "guid-other-0000" {
		t.Fatalf("latest taken = %+v, want existing owner", latest)
	}
}

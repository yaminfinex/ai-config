package sidecarcmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// TestAppendEnrichmentSelfHealsStaleHcomName pins the AC #2 answer for TASK-033:
// the sidecar SELF-HEALS a wrong row name. spawn no longer tag+cwd-guesses a bus
// name into the row, but if a stale/wrong hcom_name ever sits on the guid's row,
// the sidecar — running in the CHILD's own pane, enriching THIS guid — appends a
// newer row carrying the correct name from its own pane's hcom entry, and
// LatestByGUID (what `herder send <guid>` resolves through) returns the corrected
// name. So a stale-enriched row resolves CORRECTLY after the sidecar runs.
func TestAppendEnrichmentSelfHealsStaleHcomName(t *testing.T) {
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	// Prior row already carries a WRONG name (as a pre-fix tag+cwd guess would).
	stale := `{"guid":"guid-spawned-0000","short_guid":"guid","label":"worker-guid","role":"worker","agent":"claude","terminal_id":"term_NEW","pane_id":"p_new","status":"active","hcom_name":"worker-stale","hcom_tag":"worker","provenance":{"mechanism":"spawn","spawned_by":"parent-guid","tool_session_id":"","tag":"worker","batch_id":"","cwd":"/repo","workspace_id":"ws_1","branch":"","ts":"2026-07-03T00:00:00Z"}}`
	if err := registry.Append(registryPath, []byte(stale)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_GUID", "guid-spawned-0000")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HERDER_SPAWNED_BY", "parent-guid")
	t.Setenv("HCOM_DIR", "/hcom")

	// The sidecar runs in the child's own pane and discovers the child's OWN row
	// (worker-rive) — the correct bus name.
	s := &sidecar{tool: "claude", paneID: "p_new", cwd: "/repo", registry: registryPath}
	s.appendEnrichment(&hcomRow{Name: "worker-rive", Tag: "worker", SessionID: "sess-123", Directory: "/repo"})

	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	// The stale-named row that `herder send guid-spawned-0000` resolves through is
	// now the corrected one.
	latest := registry.Resolve(recs, "guid-spawned-0000")
	if latest == nil {
		t.Fatal("latest row not found")
	}
	if latest.HcomName != "worker-rive" {
		t.Fatalf("HcomName = %q, want worker-rive (self-heal did not override the stale name)", latest.HcomName)
	}
}

// TestSidecarDoesNotEnrichFromStaleUniqueFallback pins the AC #1 invariant on the
// SIDECAR write (the P1 the reviewer surfaced): the tag+cwd guess spawn no longer
// makes must not re-enter through the sidecar's enrichment. A stale same-tool+tag
// +cwd agent is the SOLE roster match and this guid's own row has no pane
// correlate yet — findRowCorrelated returns it flagged non-child-specific, and
// enrichDiscovered writes NOTHING, so the guid's row keeps its empty name (never
// worker-stale). Positive control: once the child's own pane-correlated row shows
// up, the real name enriches.
func TestSidecarDoesNotEnrichFromStaleUniqueFallback(t *testing.T) {
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	// spawn's row for this guid, post-fix: name left EMPTY (no tag+cwd guess).
	prior := `{"guid":"guid-new-0000","short_guid":"guid","label":"worker-guid","role":"worker","agent":"claude","terminal_id":"term_CHILD","pane_id":"p_child","status":"active","hcom_name":"","hcom_tag":"worker","provenance":{"mechanism":"spawn","spawned_by":"parent-guid","tool_session_id":"","tag":"worker","batch_id":"","cwd":"/repo","workspace_id":"ws_1","branch":"","ts":"2026-07-03T00:00:00Z"}}`
	if err := registry.Append(registryPath, []byte(prior)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_GUID", "guid-new-0000")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HERDER_SPAWNED_BY", "parent-guid")
	t.Setenv("HCOM_DIR", "/hcom")

	s := &sidecar{tool: "claude", paneID: "p_child", tag: "worker", cwd: "/repo", registry: registryPath}

	// A stale agent is the ONLY tool+tag+cwd match; its launch pane (p_gone) is not
	// ours, so there is no pane correlate.
	stale := []hcomRow{
		{Name: "worker-stale", Tool: "claude", Tag: "worker", Directory: "/repo", Status: "listening", SessionID: "sess-stale", LaunchContext: struct {
			PaneID string `json:"pane_id"`
		}{PaneID: "p_gone"}},
	}
	row, paneCorrelated := s.findRowCorrelated(stale)
	if row == nil || row.Name != "worker-stale" {
		t.Fatalf("fallback row = %+v, want worker-stale (still returned for status bridging)", row)
	}
	if paneCorrelated {
		t.Fatal("stale unique tag+cwd match reported paneCorrelated=true; want false")
	}
	if s.enrichDiscovered(row, paneCorrelated) {
		t.Fatal("enrichDiscovered wrote from a fallback-only match; want no write")
	}
	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if latest := registry.Resolve(recs, "guid-new-0000"); latest == nil || latest.HcomName != "" {
		t.Fatalf("guid row hcom_name = %q, want \"\" (stale name must not be written)", latest.HcomName)
	}

	// Positive control: the child's own pane-correlated row appears → real name enriches.
	withMine := append(stale, hcomRow{Name: "worker-mine", Tool: "claude", Tag: "worker", Directory: "/repo", Status: "listening", SessionID: "sess-mine", LaunchContext: struct {
		PaneID string `json:"pane_id"`
	}{PaneID: "p_child"}})
	row, paneCorrelated = s.findRowCorrelated(withMine)
	if row == nil || row.Name != "worker-mine" || !paneCorrelated {
		t.Fatalf("pane-correlated match = %+v (paneCorrelated=%v), want worker-mine/true", row, paneCorrelated)
	}
	if !s.enrichDiscovered(row, paneCorrelated) {
		t.Fatal("enrichDiscovered did not write for a pane-correlated match")
	}
	recs, err = registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if latest := registry.Resolve(recs, "guid-new-0000"); latest == nil || latest.HcomName != "worker-mine" {
		t.Fatalf("guid row hcom_name = %q, want worker-mine (pane correlate must enrich)", latest.HcomName)
	}
}

func TestReportAgentSessionOnFirstPaneCorrelatedEnrichment(t *testing.T) {
	logPath := installFakeHerdrForSidecar(t, 0)
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	t.Setenv("HERDER_GUID", "guid-new-0000")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HERDER_LABEL", "worker-guid")
	t.Setenv("HCOM_DIR", "/hcom")

	s := &sidecar{tool: "codex", paneID: "p_child", cwd: "/repo", registry: registryPath}
	row := &hcomRow{Name: "worker-mine", Tool: "codex", Tag: "worker", Directory: "/repo", SessionID: "sess-mine"}

	if !s.enrichDiscovered(row, true) {
		t.Fatal("enrichDiscovered returned false for pane-correlated row")
	}
	got := readReportLog(t, logPath)
	if len(got) != 1 {
		t.Fatalf("report calls = %d (%v), want 1", len(got), got)
	}
	want := "pane report-agent-session p_child --source herder:sidecar --agent codex --agent-session-id sess-mine"
	if got[0] != want {
		t.Fatalf("report call = %q, want %q", got[0], want)
	}
}

func TestReportAgentSessionOnSessionIDChangeOnly(t *testing.T) {
	logPath := installFakeHerdrForSidecar(t, 0)
	s := &sidecar{tool: "claude", paneID: "p_child"}

	s.reportAgentSession(&hcomRow{Tool: "claude", SessionID: "sess-1"}, true)
	s.reportAgentSession(&hcomRow{Tool: "claude", SessionID: "sess-1"}, true)
	s.reportAgentSession(&hcomRow{Tool: "claude", SessionID: "sess-2"}, true)

	got := readReportLog(t, logPath)
	if len(got) != 2 {
		t.Fatalf("report calls = %d (%v), want first sid and changed sid only", len(got), got)
	}
	if !strings.Contains(got[0], "--agent-session-id sess-1") || !strings.Contains(got[1], "--agent-session-id sess-2") {
		t.Fatalf("report calls = %v, want sess-1 then sess-2", got)
	}
}

func TestReportAgentSessionSkipsEmptyPaneEmptySessionAndFallback(t *testing.T) {
	logPath := installFakeHerdrForSidecar(t, 0)
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	t.Setenv("HERDER_GUID", "guid-new-0000")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HCOM_DIR", "/hcom")

	(&sidecar{tool: "codex", paneID: ""}).reportAgentSession(&hcomRow{Tool: "codex", SessionID: "sess-1"}, true)
	(&sidecar{tool: "codex", paneID: "p_child"}).reportAgentSession(&hcomRow{Tool: "codex", SessionID: ""}, true)
	(&sidecar{tool: "codex", paneID: "p_child"}).reportAgentSession(&hcomRow{Tool: "codex", SessionID: "sess-1"}, false)

	s := &sidecar{tool: "codex", paneID: "p_child", cwd: "/repo", registry: registryPath}
	if s.enrichDiscovered(&hcomRow{Name: "stale", Tool: "codex", Tag: "worker", Directory: "/repo", SessionID: "sess-stale"}, false) {
		t.Fatal("enrichDiscovered wrote from fallback-only match; want false")
	}
	if got := readReportLog(t, logPath); len(got) != 0 {
		t.Fatalf("report calls = %v, want none", got)
	}
}

func TestReportAgentSessionFailureIsSwallowed(t *testing.T) {
	logPath := installFakeHerdrForSidecar(t, 9)
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	t.Setenv("HERDER_GUID", "guid-new-0000")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HERDER_LABEL", "worker-guid")
	t.Setenv("HCOM_DIR", "/hcom")

	s := &sidecar{tool: "codex", paneID: "p_child", cwd: "/repo", registry: registryPath}
	if !s.enrichDiscovered(&hcomRow{Name: "worker-mine", Tool: "codex", Tag: "worker", Directory: "/repo", SessionID: "sess-mine"}, true) {
		t.Fatal("enrichDiscovered returned false after report failure; want sidecar to continue")
	}
	if s.lastReportedSID != "" {
		t.Fatalf("lastReportedSID = %q, want empty after failed report", s.lastReportedSID)
	}
	if got := readReportLog(t, logPath); len(got) != 1 {
		t.Fatalf("report attempts = %d (%v), want 1 failed attempt", len(got), got)
	}
}

func TestReportAgentSessionRetriesStableSIDAfterFailure(t *testing.T) {
	logPath := installFakeHerdrForSidecar(t, 0)
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	failOncePath := filepath.Join(t.TempDir(), "fail-once")
	if err := os.WriteFile(failOncePath, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_HERDR_FAIL_ONCE", failOncePath)
	t.Setenv("HERDER_GUID", "guid-new-0000")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HERDER_LABEL", "worker-guid")
	t.Setenv("HCOM_DIR", "/hcom")

	s := &sidecar{tool: "codex", paneID: "p_child", cwd: "/repo", registry: registryPath}
	row := &hcomRow{Name: "worker-mine", Tool: "codex", Tag: "worker", Directory: "/repo", SessionID: "sess-mine"}

	if !s.enrichDiscovered(row, true) {
		t.Fatal("initial pane-correlated enrichment returned false")
	}
	if s.enrichedSessionID != "sess-mine" {
		t.Fatalf("enrichedSessionID = %q, want sess-mine", s.enrichedSessionID)
	}
	if s.lastReportedSID != "" {
		t.Fatalf("lastReportedSID = %q after first failed report, want empty", s.lastReportedSID)
	}
	if got := readReportLog(t, logPath); len(got) != 1 {
		t.Fatalf("report attempts after first tick = %d (%v), want 1", len(got), got)
	}

	// Same sid, same pane-correlated row: enrichment would not append again, but
	// reportAgentSession must retry because lastReportedSID is still empty.
	if row.SessionID != "" && (row.SessionID != s.enrichedSessionID || s.latestSessionMissing(row.SessionID)) {
		s.appendEnrichment(row)
		s.enrichedSessionID = row.SessionID
	}
	s.reportAgentSession(row, true)
	if s.lastReportedSID != "sess-mine" {
		t.Fatalf("lastReportedSID = %q after retry, want sess-mine", s.lastReportedSID)
	}
	if got := readReportLog(t, logPath); len(got) != 2 {
		t.Fatalf("report attempts after retry = %d (%v), want 2", len(got), got)
	}

	s.reportAgentSession(row, true)
	if got := readReportLog(t, logPath); len(got) != 2 {
		t.Fatalf("report attempts after successful stable tick = %d (%v), want still 2", len(got), got)
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

func installFakeHerdrForSidecar(t *testing.T, reportExit int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake herdr is a shell script")
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "report.log")
	script := `#!/bin/sh
if [ "$1" = "pane" ] && [ "$2" = "get" ]; then
  printf '{"result":{"pane":{"pane_id":"%s","terminal_id":"term_TEST","workspace_id":"ws_TEST","cwd":"/repo"}}}\n' "$3"
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "report-agent-session" ]; then
  printf '%s\n' "$*" >> "$FAKE_HERDR_REPORT_LOG"
  if [ -n "$FAKE_HERDR_FAIL_ONCE" ] && [ -f "$FAKE_HERDR_FAIL_ONCE" ]; then
    rm -f "$FAKE_HERDR_FAIL_ONCE"
    exit 9
  fi
  exit "$FAKE_HERDR_REPORT_EXIT"
fi
printf 'fake herdr: unhandled: %s\n' "$*" >&2
exit 1
`
	bin := filepath.Join(dir, "herdr")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_HERDR_REPORT_LOG", logPath)
	t.Setenv("FAKE_HERDR_REPORT_EXIT", fmt.Sprintf("%d", reportExit))
	return logPath
}

func readReportLog(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
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

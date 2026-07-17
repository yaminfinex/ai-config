package sidecarcmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/liveness"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcompletion"
)

func testSeatCompletion(t *testing.T) func(context.Context, *hcomRow, seatcompletion.Request) (seatcompletion.Result, error) {
	t.Helper()
	return func(ctx context.Context, observed *hcomRow, request seatcompletion.Request) (seatcompletion.Result, error) {
		joined := true
		engine := seatcompletion.DefaultEngine()
		engine.HerdrPane = func(context.Context, string) (seatcompletion.LivePane, error) {
			terminalID := request.Seat.TerminalID
			if terminalID == "" {
				terminalID = "terminal-test"
			}
			return seatcompletion.LivePane{PaneID: request.Seat.PaneID, TerminalID: terminalID}, nil
		}
		engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
			return []hcomidentity.Row{{
				Name:      observed.Name,
				Tool:      observed.Tool,
				Joined:    &joined,
				SessionID: observed.SessionID,
				LaunchContext: hcomidentity.LaunchContext{
					PaneID:    observed.LaunchContext.PaneID,
					ProcessID: observed.LaunchContext.ProcessID,
				},
			}}, nil
		}
		engine.RepairLaunchContext = func(_, _, pane string) hcomidentity.LaunchContextRepair {
			return hcomidentity.LaunchContextRepair{Status: "already-present", PaneID: pane}
		}
		return engine.Complete(ctx, request)
	}
}

func TestSidecarMissingBusKeepsPollingUntilHolderExitThenStops(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	outcomes, err := registry.UpdateLocked(path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{
			GUID: "guid-sidecar", Event: "seated", RecordedAt: "2026-07-17T10:00:00Z", State: v2.StateSeated,
			Seat: &v2.Seat{Kind: "herdr", Node: tx.NodeID, TerminalID: "terminal-live", PaneID: "pane-live", HcomName: "bus-live"},
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := registry.SingleOutcome(outcomes)
	if err != nil {
		t.Fatal(err)
	}
	if err := outcome.Err(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_GUID", "guid-sidecar")
	var diagnostic strings.Builder
	s := &sidecar{
		registry: path, diagnostic: &diagnostic,
	}
	for poll := 0; poll < 8; poll++ {
		if !s.observeLiveness(false, nil) {
			t.Fatalf("missing bus stopped sidecar on poll %d while holder remained alive", poll)
		}
	}
	if got := registry.V2ByGUID(mustSidecarProjection(t, path), "guid-sidecar"); got == nil || got.State != v2.StateSeated {
		t.Fatalf("starved keepalive changed seated row: %+v", got)
	}
	if count := strings.Count(diagnostic.String(), "holder alive; expected bus roster row is absent"); count != 1 {
		t.Fatalf("starvation advisory count = %d, output=%q", count, diagnostic.String())
	}
	if s.observeLiveness(true, nil) {
		t.Fatal("holder exit did not terminate sidecar lifetime")
	}
	got := registry.V2ByGUID(mustSidecarProjection(t, path), "guid-sidecar")
	if got == nil || got.State != v2.StateUnseated || got.CloseResult != "observed_dead" || !strings.Contains(got.ObservedVia, "sidecar") {
		t.Fatalf("holder exit did not persist shared death evidence: %+v", got)
	}
}

func mustSidecarProjection(t *testing.T, path string) *v2.Projection {
	t.Helper()
	proj, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return proj
}

func launchContext(paneID, processID string) struct {
	PaneID    string `json:"pane_id"`
	ProcessID string `json:"process_id"`
} {
	return struct {
		PaneID    string `json:"pane_id"`
		ProcessID string `json:"process_id"`
	}{PaneID: paneID, ProcessID: processID}
}

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
		{Name: "parent", SessionID: "sess-parent", LaunchContext: launchContext("p_target", "")},
		{Name: "child", SessionID: "sess-child", LaunchContext: launchContext("p_target", "")},
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
// (its launch_context drifted after a pane id re-key), and a headless launch
// (calc17-tina) shares tool+tag+cwd. Newest-wins used to attach calc17-tina's
// name onto the orchestrator's guid. The positive-correlate invariant must
// refuse the enrichment entirely rather than guess.
func TestFindRowRefusesAmbiguousFallbackForHeadlessLaunch(t *testing.T) {
	s := &sidecar{tool: "claude", paneID: "p_orch", tag: "orchestrator", cwd: "/repo"}
	rows := []hcomRow{
		{Name: "orchestrator-bumo", Tool: "claude", Tag: "orchestrator", Directory: "/repo", Status: "listening", LaunchContext: launchContext("p_gone", "")},
		{Name: "calc17-tina", Tool: "claude", Tag: "orchestrator", Directory: "/repo", Status: "listening", LaunchContext: launchContext("", "")},
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

func TestFindRowCorrelatedUsesProcessIDWhenPaneIDMissing(t *testing.T) {
	t.Setenv("HERDER_GUID", "guid-child-0000")
	scans := 0
	s := &sidecar{
		tool:   "codex",
		paneID: "p_child",
		processEnvirons: func(tool string) []processEnvironmentRead {
			scans++
			if tool != "codex" {
				t.Fatalf("scan tool = %q, want codex", tool)
			}
			return []processEnvironmentRead{{
				env: map[string]string{
					"HERDER_GUID":     "guid-child-0000",
					"HCOM_PROCESS_ID": "proc-child",
				},
			}}
		},
	}
	rows := []hcomRow{
		{Name: "worker-mine", Tool: "codex", Status: "listening", SessionID: "sess-mine", LaunchContext: launchContext("", "proc-child")},
	}

	row, paneCorrelated := s.findRowCorrelated(rows)
	if row == nil || row.Name != "worker-mine" || !paneCorrelated {
		t.Fatalf("process-correlated row = %+v (paneCorrelated=%v), want worker-mine/true", row, paneCorrelated)
	}
	if s.correlatedProcessID != "proc-child" {
		t.Fatalf("cached process id = %q, want proc-child", s.correlatedProcessID)
	}

	row, paneCorrelated = s.findRowCorrelated(rows)
	if row == nil || row.Name != "worker-mine" || !paneCorrelated {
		t.Fatalf("cached process-correlated row = %+v (paneCorrelated=%v), want worker-mine/true", row, paneCorrelated)
	}
	if scans != 1 {
		t.Fatalf("process scan count = %d, want 1 after cached success", scans)
	}
}

func TestFindRowCorrelatedUsesOwnedChildNameWhenLaunchCoordinatesAreEmpty(t *testing.T) {
	t.Setenv("HERDER_GUID", "guid-child-0000")
	s := &sidecar{
		tool:   "codex",
		paneID: "p_child",
		instancePID: func(dir, baseName string) (int, error) {
			if baseName != "mine" {
				t.Fatalf("pid lookup base name = %q, want mine", baseName)
			}
			return 4242, nil
		},
		processEnvirons: func(tool string) []processEnvironmentRead {
			return []processEnvironmentRead{{pid: 4242, env: map[string]string{
				"HERDER_GUID":        "guid-child-0000",
				"HCOM_INSTANCE_NAME": "mine",
				"HCOM_TAG":           "worker",
				"HCOM_PROCESS_ID":    "proc-child",
			}}}
		},
	}
	rows := []hcomRow{{Name: "worker-mine", BaseName: "mine", Tool: "codex", Status: "listening"}}

	row, correlated := s.findRowCorrelated(rows)
	if row == nil || row.Name != "worker-mine" || !correlated {
		t.Fatalf("owned-name match = %+v (correlated=%v), want worker-mine/true", row, correlated)
	}
	if s.correlatedName != "worker-mine" {
		t.Fatalf("cached correlated name = %q, want worker-mine", s.correlatedName)
	}
}

func TestFindRowCorrelatedRejectsReclaimedFrozenOwnedChildName(t *testing.T) {
	t.Setenv("HERDER_GUID", "guid-child-0000")
	s := &sidecar{
		tool:   "codex",
		paneID: "p_child",
		instancePID: func(string, string) (int, error) {
			return 7331, nil
		},
		processEnvirons: func(tool string) []processEnvironmentRead {
			return []processEnvironmentRead{{pid: 4242, env: map[string]string{
				"HERDER_GUID":        "guid-child-0000",
				"HCOM_INSTANCE_NAME": "mine",
				"HCOM_TAG":           "worker",
				"HCOM_PROCESS_ID":    "proc-owned",
			}}}
		},
	}
	rows := []hcomRow{{
		Name: "worker-mine", BaseName: "mine", Tool: "codex", Status: "listening",
		LaunchContext: launchContext("", "proc-stranger"),
	}}

	if row, correlated := s.findRowCorrelated(rows); row != nil || correlated {
		t.Fatalf("reclaimed frozen name = %+v correlated=%v, want fail-closed miss", row, correlated)
	}
}

func TestFindRowCorrelatedReportsPIDSchemaDriftOnce(t *testing.T) {
	t.Setenv("HERDER_GUID", "guid-child-0000")
	var diagnostic strings.Builder
	s := &sidecar{
		tool: "codex", paneID: "p_child", diagnostic: &diagnostic,
		instancePID: func(string, string) (int, error) {
			return 0, fmt.Errorf("%w: instances.pid INTEGER column is missing", hcomidentity.ErrInstancePIDSchemaDrift)
		},
		processEnvirons: func(string) []processEnvironmentRead {
			return []processEnvironmentRead{{pid: 4242, env: map[string]string{
				"HERDER_GUID": "guid-child-0000", "HCOM_INSTANCE_NAME": "zida", "HCOM_TAG": "builder",
			}}}
		},
	}
	rows := []hcomRow{{Name: "builder-zida", BaseName: "zida", Tag: "builder", Tool: "codex", Status: "listening"}}

	for range 2 {
		if row, correlated := s.findRowCorrelated(rows); row != nil || correlated {
			t.Fatalf("schema-drift lookup = %+v correlated=%v, want fail-closed miss", row, correlated)
		}
	}
	got := diagnostic.String()
	if !strings.Contains(got, "refusing hcom PID corroboration: schema drift") ||
		!strings.Contains(got, "refusing exact-name recovery") {
		t.Fatalf("schema-drift diagnostic = %q, want explicit refusal", got)
	}
	if strings.Count(got, "herder sidecar:") != 1 {
		t.Fatalf("schema-drift diagnostic count = %d, want one", strings.Count(got, "herder sidecar:"))
	}
}

func TestOwnedChildNameDisagreementFailsClosed(t *testing.T) {
	t.Setenv("HERDER_GUID", "guid-child-0000")
	s := &sidecar{
		tool:   "codex",
		paneID: "p_child",
		processEnvirons: func(tool string) []processEnvironmentRead {
			return []processEnvironmentRead{
				{env: map[string]string{"HERDER_GUID": "guid-child-0000", "HCOM_INSTANCE_NAME": "mine", "HCOM_TAG": "worker", "HCOM_PROCESS_ID": "proc-child"}},
				{env: map[string]string{"HERDER_GUID": "guid-child-0000", "HCOM_INSTANCE_NAME": "other", "HCOM_TAG": "worker", "HCOM_PROCESS_ID": "proc-child"}},
			}
		},
	}
	rows := []hcomRow{
		{Name: "worker-mine", BaseName: "mine", Tool: "codex", Status: "listening"},
		{Name: "worker-other", BaseName: "other", Tool: "codex", Status: "listening"},
	}

	row, correlated := s.findRowCorrelated(rows)
	if row != nil || correlated || s.correlatedName != "" {
		t.Fatalf("disagreeing owned names = %+v (correlated=%v cached=%q), want fail-closed miss", row, correlated, s.correlatedName)
	}
}

func TestOwnedChildExactNameAmbiguityFailsClosed(t *testing.T) {
	t.Setenv("HERDER_GUID", "guid-child-0000")
	s := &sidecar{
		tool: "codex", paneID: "p_child",
		processEnvirons: func(tool string) []processEnvironmentRead {
			return []processEnvironmentRead{{env: map[string]string{
				"HERDER_GUID": "guid-child-0000", "HCOM_INSTANCE_NAME": "mine", "HCOM_TAG": "worker",
			}}}
		},
	}
	rows := []hcomRow{
		{Name: "worker-mine", Tool: "codex", Status: "listening"},
		{Name: "worker-mine", Tool: "codex", Status: "active"},
	}
	if row, correlated := s.findRowCorrelated(rows); row != nil || correlated {
		t.Fatalf("duplicate exact name = %+v correlated=%v, want fail-closed miss", row, correlated)
	}
}

func TestOwnedChildExactNameIgnoresExplicitlyUnjoinedRow(t *testing.T) {
	joined := true
	unjoined := false
	rows := []hcomRow{
		{Name: "worker-mine", Tool: "codex", Status: "listening", Joined: &unjoined, LaunchContext: launchContext("", "proc-child")},
		{Name: "worker-mine", Tool: "codex", Status: "listening", Joined: &joined, LaunchContext: launchContext("", "proc-child")},
	}
	row := findRowForOwnedChild(rows, ownedChildIdentity{StoredName: "worker-mine", ProcessID: "proc-child"}, "", "", nil)
	if row == nil || row.Joined == nil || !*row.Joined {
		t.Fatalf("owned exact-name match = %+v, want the sole joined row", row)
	}
}

func TestSidecarCompletesEmptyCoordinateRowFromOwnedChildName(t *testing.T) {
	installFakeHerdrForSidecar(t, 0)
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	t.Setenv("HERDER_GUID", "guid-new-0000")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HERDER_LABEL", "worker-guid")
	t.Setenv("HCOM_DIR", "/hcom")
	s := &sidecar{
		tool: "codex", paneID: "p_child", cwd: "/repo", registry: registryPath,
		completeSeat: testSeatCompletion(t),
		instancePID: func(string, string) (int, error) {
			return 4242, nil
		},
		processEnvirons: func(tool string) []processEnvironmentRead {
			return []processEnvironmentRead{{pid: 4242, env: map[string]string{
				"HERDER_GUID": "guid-new-0000", "HCOM_INSTANCE_NAME": "mine", "HCOM_TAG": "worker", "HCOM_PROCESS_ID": "proc-child",
			}}}
		},
	}
	rows := []hcomRow{{Name: "worker-mine", BaseName: "mine", Tool: "codex", Tag: "worker", Directory: "/repo", Status: "listening"}}
	row, correlated := s.findRowCorrelated(rows)
	if row == nil || !correlated {
		t.Fatalf("owned-name discovery = %+v correlated=%v", row, correlated)
	}
	if !s.enrichDiscovered(row, correlated) {
		t.Fatal("sidecar did not complete empty-coordinate row from owned child name")
	}
	if !s.appendEnrichment(row) {
		t.Fatal("identical sidecar replay was not treated as success")
	}
	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	latest := registry.Resolve(recs, "guid-new-0000")
	if latest == nil || latest.HcomName != "worker-mine" {
		t.Fatalf("completed row = %+v, want worker-mine", latest)
	}
	if len(recs) != 1 {
		t.Fatalf("registry session rows = %d, want one after idempotent replay", len(recs))
	}
}

func TestSidecarRejectsUnverifiedNormalizedNoopCompletion(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	t.Setenv("HERDER_GUID", "guid-noop-0000")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HERDER_LABEL", "worker-noop")
	t.Setenv("HCOM_DIR", "/hcom")
	s := &sidecar{
		tool: "codex", paneID: "p_child", cwd: "/repo", registry: registryPath,
		completeSeat: func(context.Context, *hcomRow, seatcompletion.Request) (seatcompletion.Result, error) {
			return seatcompletion.Result{Status: registry.WriteNoop}, nil
		},
	}
	row := &hcomRow{Name: "worker-mine", Tool: "codex", Tag: "worker", Directory: "/repo", Status: "listening"}
	if s.appendEnrichment(row) {
		t.Fatal("unverified normalized noop was treated as successful sidecar completion")
	}
}

func TestSidecarAcceptsNoopAfterCanonicalCompletionIsVerified(t *testing.T) {
	installFakeHerdrForSidecar(t, 0)
	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	t.Setenv("HERDER_GUID", "guid-noop-verified-0000")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HERDER_LABEL", "worker-noop-verified")
	t.Setenv("HCOM_DIR", "/hcom")
	applied := testSeatCompletion(t)
	s := &sidecar{
		tool: "codex", paneID: "p_child", cwd: "/repo", registry: registryPath,
		completeSeat: func(ctx context.Context, row *hcomRow, request seatcompletion.Request) (seatcompletion.Result, error) {
			result, err := applied(ctx, row, request)
			if err != nil || result.Refusal != nil || result.Status != registry.WriteApplied {
				return result, err
			}
			return seatcompletion.Result{Status: registry.WriteNoop}, nil
		},
	}
	row := &hcomRow{Name: "worker-mine", Tool: "codex", Tag: "worker", Directory: "/repo", Status: "listening", LaunchContext: launchContext("p_child", "")}
	if !s.appendEnrichment(row) {
		t.Fatal("noop with a matching canonical registry row was not accepted")
	}
}

func TestSidecarRunCompletesLateEmptyCoordinateRowWithinSteadyPoll(t *testing.T) {
	installFakeHerdrForSidecar(t, 0)
	installFakeHcomRosterForSidecar(t, `[{"name":"worker-mine","base_name":"mine","tool":"codex","tag":"worker","directory":"/repo","status":"listening","session_id":"","launch_context":{}}]`)
	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	t.Setenv("HERDER_GUID", "guid-late-0000")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HERDER_LABEL", "worker-late")
	t.Setenv("HCOM_DIR", "/hcom")
	scans := 0
	var s *sidecar
	var completedAt time.Time
	complete := testSeatCompletion(t)
	s = &sidecar{
		tool: "codex", paneID: "p_child", tag: "worker", cwd: "/repo", registry: registryPath, ppid0: os.Getppid(),
		applyDeath: func(string, string, liveness.SeatAnchor, liveness.Verdict, time.Time, string) (liveness.ApplyResult, error) {
			return liveness.ApplyResult{Status: registry.WriteNoop}, nil
		},
		instancePID: func(string, string) (int, error) {
			return 4242, nil
		},
		processEnvirons: func(tool string) []processEnvironmentRead {
			scans++
			if scans == 1 {
				return nil
			}
			return []processEnvironmentRead{{pid: 4242, env: map[string]string{
				"HERDER_GUID": "guid-late-0000", "HCOM_INSTANCE_NAME": "mine", "HCOM_TAG": "worker", "HCOM_PROCESS_ID": "proc-child",
			}}}
		},
	}
	s.completeSeat = func(ctx context.Context, observed *hcomRow, request seatcompletion.Request) (seatcompletion.Result, error) {
		result, err := complete(ctx, observed, request)
		if err == nil && result.Refusal == nil && result.Status == registry.WriteApplied {
			completedAt = time.Now()
			s.ppid0 = -1
		}
		return result, err
	}
	started := time.Now()
	if code := s.run(); code != 0 {
		t.Fatalf("sidecar run code = %d", code)
	}
	elapsed := completedAt.Sub(started)
	if elapsed < 1500*time.Millisecond || elapsed > 3500*time.Millisecond {
		t.Fatalf("late completion elapsed = %s, want one 2s steady poll", elapsed)
	}
	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	latest := registry.Resolve(recs, "guid-late-0000")
	if latest == nil || latest.HcomName != "worker-mine" {
		t.Fatalf("late completed row = %+v, want worker-mine", latest)
	}
}

func TestFindRowCorrelatedRefusesDifferentHERDERGUID(t *testing.T) {
	t.Setenv("HERDER_GUID", "guid-child-0000")
	s := &sidecar{
		tool:   "codex",
		paneID: "p_child",
		tag:    "worker",
		cwd:    "/repo",
		processEnvirons: func(tool string) []processEnvironmentRead {
			return []processEnvironmentRead{{
				env: map[string]string{
					"HERDER_GUID":     "guid-other-0000",
					"HCOM_PROCESS_ID": "proc-child",
				},
			}}
		},
	}
	rows := []hcomRow{
		{Name: "worker-mine", Tool: "codex", Tag: "worker", Directory: "/repo", Status: "listening", SessionID: "sess-mine", LaunchContext: launchContext("", "proc-child")},
	}

	row, paneCorrelated := s.findRowCorrelated(rows)
	if row == nil || row.Name != "worker-mine" {
		t.Fatalf("fallback row = %+v, want worker-mine for status bridging", row)
	}
	if paneCorrelated {
		t.Fatal("different HERDER_GUID reported child-specific correlation; want false")
	}
	if s.correlatedProcessID != "" {
		t.Fatalf("cached process id = %q, want empty", s.correlatedProcessID)
	}
}

func TestFindRowCorrelatedRefusesUnmatchedProcessID(t *testing.T) {
	t.Setenv("HERDER_GUID", "guid-child-0000")
	s := &sidecar{
		tool:   "codex",
		paneID: "p_child",
		processEnvirons: func(tool string) []processEnvironmentRead {
			return []processEnvironmentRead{{
				env: map[string]string{
					"HERDER_GUID":     "guid-child-0000",
					"HCOM_PROCESS_ID": "proc-missing",
				},
			}}
		},
	}
	rows := []hcomRow{
		{Name: "worker-mine", Tool: "codex", Status: "listening", SessionID: "sess-mine", LaunchContext: launchContext("", "proc-child")},
	}

	row, paneCorrelated := s.findRowCorrelated(rows)
	if row != nil || paneCorrelated {
		t.Fatalf("unmatched process id row = %+v (paneCorrelated=%v), want nil/false", row, paneCorrelated)
	}
	if s.correlatedProcessID != "" {
		t.Fatalf("cached process id = %q, want empty", s.correlatedProcessID)
	}
}

func TestFindRowCorrelatedPaneIDPrecedenceSkipsProcessScan(t *testing.T) {
	t.Setenv("HERDER_GUID", "guid-child-0000")
	scans := 0
	s := &sidecar{
		tool:   "codex",
		paneID: "p_child",
		processEnvirons: func(tool string) []processEnvironmentRead {
			scans++
			return nil
		},
	}
	rows := []hcomRow{
		{Name: "wrong-process", Tool: "codex", Status: "listening", SessionID: "sess-wrong", LaunchContext: launchContext("", "proc-child")},
		{Name: "pane-mine", Tool: "codex", Status: "listening", SessionID: "sess-mine", LaunchContext: launchContext("p_child", "")},
	}

	row, paneCorrelated := s.findRowCorrelated(rows)
	if row == nil || row.Name != "pane-mine" || !paneCorrelated {
		t.Fatalf("pane-correlated row = %+v (paneCorrelated=%v), want pane-mine/true", row, paneCorrelated)
	}
	if scans != 0 {
		t.Fatalf("process scans = %d, want 0 when pane_id matches", scans)
	}
}

func TestFindRowCorrelatedSwallowsProcessEnvironReadFailure(t *testing.T) {
	t.Setenv("HERDER_GUID", "guid-child-0000")
	s := &sidecar{
		tool:   "codex",
		paneID: "p_child",
		tag:    "worker",
		cwd:    "/repo",
		processEnvirons: func(tool string) []processEnvironmentRead {
			return []processEnvironmentRead{{err: errors.New("permission denied")}}
		},
	}
	rows := []hcomRow{
		{Name: "worker-mine", Tool: "codex", Tag: "worker", Directory: "/repo", Status: "listening", SessionID: "sess-mine", LaunchContext: launchContext("", "proc-child")},
	}

	row, paneCorrelated := s.findRowCorrelated(rows)
	if row == nil || row.Name != "worker-mine" {
		t.Fatalf("fallback row after environ failure = %+v, want worker-mine for polling/status bridging", row)
	}
	if paneCorrelated {
		t.Fatal("environ read failure reported child-specific correlation; want false")
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
	prior := `{"guid":"guid-spawned-0000","short_guid":"guid","label":"worker-guid","role":"worker","agent":"codex","terminal_id":"term_OLD","pane_id":"p_old","status":"active","extra_field":"keep","provenance":{"mechanism":"spawn","spawned_by":"parent-guid","tool_session_id":"sess-stale","tag":"worker","batch_id":"","cwd":"/old","workspace_id":"ws_old","branch":"","ts":"2026-07-03T00:00:00Z"}}`
	if err := registry.Append(registryPath, []byte(prior)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_GUID", "guid-spawned-0000")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HERDER_SPAWNED_BY", "parent-guid")
	t.Setenv("HCOM_DIR", "/hcom")

	s := &sidecar{tool: "codex", paneID: "p_new", cwd: "/repo", registry: registryPath, completeSeat: testSeatCompletion(t)}
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
	if latest.HcomVerified == nil || !*latest.HcomVerified {
		t.Fatalf("HcomVerified = %v, want true for pane-correlated enrichment", latest.HcomVerified)
	}
	if latest.Provenance == nil || latest.Provenance.ToolSessionID != "sess-123" || latest.Provenance.Mechanism != "spawn" {
		t.Fatalf("Provenance = %+v, want spawn with sess-123", latest.Provenance)
	}
	if strings.Contains(string(latest.Raw), `"extra_field":"keep"`) {
		t.Fatalf("enrichment row carried unknown legacy field into v2 output: %s", latest.Raw)
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
	s := &sidecar{tool: "claude", paneID: "p_new", cwd: "/repo", registry: registryPath, completeSeat: testSeatCompletion(t)}
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

	s := &sidecar{tool: "claude", paneID: "p_child", tag: "worker", cwd: "/repo", registry: registryPath, completeSeat: testSeatCompletion(t)}

	// A stale agent is the ONLY tool+tag+cwd match; its launch pane (p_gone) is not
	// ours, so there is no pane correlate.
	stale := []hcomRow{
		{Name: "worker-stale", Tool: "claude", Tag: "worker", Directory: "/repo", Status: "listening", SessionID: "sess-stale", LaunchContext: launchContext("p_gone", "")},
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
	withMine := append(stale, hcomRow{Name: "worker-mine", Tool: "claude", Tag: "worker", Directory: "/repo", Status: "listening", SessionID: "sess-mine", LaunchContext: launchContext("p_child", "")})
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

	s := &sidecar{tool: "codex", paneID: "p_child", cwd: "/repo", registry: registryPath, completeSeat: testSeatCompletion(t)}
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

func TestProcessIDCorrelationEnrichesAndReportsAgentSession(t *testing.T) {
	logPath := installFakeHerdrForSidecar(t, 0)
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	t.Setenv("HERDER_GUID", "guid-new-0000")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HERDER_LABEL", "worker-guid")
	t.Setenv("HCOM_DIR", "/hcom")

	s := &sidecar{
		tool:         "codex",
		paneID:       "p_child",
		cwd:          "/repo",
		registry:     registryPath,
		completeSeat: testSeatCompletion(t),
		processEnvirons: func(tool string) []processEnvironmentRead {
			return []processEnvironmentRead{{
				env: map[string]string{
					"HERDER_GUID":     "guid-new-0000",
					"HCOM_PROCESS_ID": "proc-child",
				},
			}}
		},
	}
	rows := []hcomRow{
		{Name: "worker-mine", Tool: "codex", Tag: "worker", Directory: "/repo", SessionID: "sess-mine", LaunchContext: launchContext("", "proc-child")},
	}

	row, paneCorrelated := s.findRowCorrelated(rows)
	if row == nil || row.Name != "worker-mine" || !paneCorrelated {
		t.Fatalf("process-correlated match = %+v (paneCorrelated=%v), want worker-mine/true", row, paneCorrelated)
	}
	if !s.enrichDiscovered(row, paneCorrelated) {
		t.Fatal("enrichDiscovered did not write for process_id-correlated row")
	}

	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	latest := registry.Resolve(recs, "guid-new-0000")
	if latest == nil || latest.HcomName != "worker-mine" || latest.Provenance == nil || latest.Provenance.ToolSessionID != "sess-mine" {
		t.Fatalf("latest registry row = %+v, want hcom worker-mine with sess-mine", latest)
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

func TestFallbackFirstThenEmptySessionProcessCorrelationEnrichesInLoop(t *testing.T) {
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	t.Setenv("HERDER_GUID", "guid-new-0000")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HERDER_LABEL", "worker-guid")
	t.Setenv("HCOM_DIR", "/hcom")

	s := &sidecar{
		tool:         "codex",
		paneID:       "p_child",
		tag:          "worker",
		cwd:          "/repo",
		registry:     registryPath,
		completeSeat: testSeatCompletion(t),
		processEnvirons: func(tool string) []processEnvironmentRead {
			return []processEnvironmentRead{{
				env: map[string]string{
					"HERDER_GUID":     "guid-new-0000",
					"HCOM_PROCESS_ID": "proc-child",
				},
			}}
		},
	}

	fallback := &hcomRow{Name: "worker-mine", Tool: "codex", Tag: "worker", Directory: "/repo", Status: "listening"}
	if s.enrichDiscovered(fallback, false) {
		t.Fatal("fallback-first bootstrap wrote enrichment; want no write")
	}

	rows := []hcomRow{
		{Name: "worker-mine", Tool: "codex", Tag: "worker", Directory: "/repo", Status: "listening", SessionID: "", LaunchContext: launchContext("", "proc-child")},
	}
	row, paneCorrelated := s.findRowCorrelated(rows)
	if row == nil || row.Name != "worker-mine" || row.SessionID != "" || !paneCorrelated {
		t.Fatalf("process-correlated empty-sid row = %+v (paneCorrelated=%v), want worker-mine empty sid/true", row, paneCorrelated)
	}
	if !s.shouldAppendCorrelatedEnrichment(row, paneCorrelated) {
		t.Fatal("empty-sid first correlated poll did not request enrichment")
	}
	s.appendCorrelatedEnrichment(row)

	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	latest := registry.Resolve(recs, "guid-new-0000")
	if latest == nil {
		t.Fatal("latest registry row not found after empty-sid process correlation")
	}
	if latest.HcomName != "worker-mine" {
		t.Fatalf("latest hcom_name = %q, want worker-mine after empty-sid process correlation", latest.HcomName)
	}
	if !s.enrichedCorrelated || s.enrichedSessionID != "" {
		t.Fatalf("enriched state = correlated %v sid %q, want true/empty", s.enrichedCorrelated, s.enrichedSessionID)
	}
	if s.shouldAppendCorrelatedEnrichment(row, paneCorrelated) {
		t.Fatal("same empty-sid correlated row requested duplicate enrichment after first write")
	}
}

func TestNoopFirstCorrelatedAppendKeepsEmptySessionRetryOpen(t *testing.T) {
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	prior := `{"guid":"guid-other-0000","short_guid":"other","label":"taken","role":"worker","agent":"codex","pane_id":"p_other","status":"active"}`
	if err := registry.Append(registryPath, []byte(prior)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_GUID", "guid-new-0000")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HERDER_LABEL", "taken")
	t.Setenv("HCOM_DIR", "/hcom")

	s := &sidecar{tool: "codex", paneID: "p_child", cwd: "/repo", registry: registryPath, completeSeat: testSeatCompletion(t)}
	row := &hcomRow{Name: "worker-mine", Tool: "codex", Tag: "worker", Directory: "/repo", Status: "listening", LaunchContext: launchContext("p_child", "")}

	if !s.shouldAppendCorrelatedEnrichment(row, true) {
		t.Fatal("first empty-sid correlated row did not request enrichment")
	}
	if s.appendCorrelatedEnrichment(row) {
		t.Fatal("label-conflicted append reported success; want no-op")
	}
	if s.enrichedCorrelated || s.enrichedSessionID != "" {
		t.Fatalf("enriched state after no-op = correlated %v sid %q, want false/empty", s.enrichedCorrelated, s.enrichedSessionID)
	}
	if !s.shouldAppendCorrelatedEnrichment(row, true) {
		t.Fatal("empty-sid retry gate closed after no-op append")
	}

	t.Setenv("HERDER_LABEL", "worker-guid")
	if !s.appendCorrelatedEnrichment(row) {
		t.Fatal("append after clearing label conflict reported no write")
	}
	if !s.enrichedCorrelated || s.enrichedSessionID != "" {
		t.Fatalf("enriched state after retry = correlated %v sid %q, want true/empty", s.enrichedCorrelated, s.enrichedSessionID)
	}

	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	latest := registry.Resolve(recs, "guid-new-0000")
	if latest == nil {
		t.Fatal("retried append did not create guid-new-0000 row")
	}
	if latest.HcomName != "worker-mine" {
		t.Fatalf("retried hcom_name = %q, want worker-mine", latest.HcomName)
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

	s := &sidecar{tool: "codex", paneID: "p_child", cwd: "/repo", registry: registryPath, completeSeat: testSeatCompletion(t)}
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

	s := &sidecar{tool: "codex", paneID: "p_child", cwd: "/repo", registry: registryPath, completeSeat: testSeatCompletion(t)}
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

	s := &sidecar{tool: "codex", paneID: "p_child", cwd: "/repo", registry: registryPath, completeSeat: testSeatCompletion(t)}
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

	s := &sidecar{tool: "claude", paneID: "p_manual", cwd: "/repo", registry: registryPath, completeSeat: testSeatCompletion(t)}
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

func installFakeHcomRosterForSidecar(t *testing.T, roster string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake hcom is a shell script")
	}
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "roster.json")
	if err := os.WriteFile(rosterPath, []byte(roster), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncat \"$FAKE_HCOM_ROSTER\"\n"
	if err := os.WriteFile(filepath.Join(dir, "hcom"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_HCOM_ROSTER", rosterPath)
}

func TestCompleteObservedSeatRequiresCorroboratedJoinedNamedUniqueBusRow(t *testing.T) {
	tests := []struct {
		name   string
		roster string
	}{
		{name: "unjoined", roster: `[{"name":"observed","joined":false,"session_id":"session-live","launch_context":{"pane_id":"pane-live"}}]`},
		{name: "empty name", roster: `[{"name":"","joined":true,"session_id":"session-live","launch_context":{"pane_id":"pane-live"}}]`},
		{name: "duplicate pane", roster: `[{"name":"observed-a","joined":true,"launch_context":{"pane_id":"pane-live"}},{"name":"observed-b","joined":true,"launch_context":{"pane_id":"pane-live"}}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installFakeHcomRosterForSidecar(t, tt.roster)
			observed := &hcomRow{Name: "observed", SessionID: "session-live"}
			observed.LaunchContext.PaneID = "pane-live"
			result, err := completeObservedSeat(context.Background(), observed, seatcompletion.Request{
				Origin:       seatcompletion.OriginRecognition,
				RegistryPath: filepath.Join(t.TempDir(), "registry.jsonl"),
				Candidate:    v2.SessionRecord{GUID: "guid-sidecar", Tool: "codex"},
				Seat:         seatcompletion.SeatClaim{Kind: seatcompletion.SeatHerdr, PaneID: "pane-live", TerminalID: "terminal-live"},
				Evidence:     hcomidentity.Evidence{SessionID: "session-live", PaneIDs: []string{"pane-live"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Refusal == nil {
				t.Fatalf("completeObservedSeat() = %+v, want live-roster refusal", result)
			}
		})
	}
}

func TestNoopCompletionDoesNotLatchSidecarEnrichment(t *testing.T) {
	s := &sidecar{
		tool:     "codex",
		paneID:   "pane-live",
		registry: filepath.Join(t.TempDir(), "registry.jsonl"),
		completeSeat: func(context.Context, *hcomRow, seatcompletion.Request) (seatcompletion.Result, error) {
			return seatcompletion.Result{Status: registry.WriteNoop}, nil
		},
	}
	row := &hcomRow{Name: "bus-live", SessionID: "session-live"}
	if s.appendCorrelatedEnrichment(row) {
		t.Fatal("unverified noop completion was treated as a successful replay")
	}
	if s.enrichedCorrelated || s.enrichedSessionID != "" {
		t.Fatalf("unverified noop latched success: correlated=%v session=%q", s.enrichedCorrelated, s.enrichedSessionID)
	}
	if !s.shouldAppendCorrelatedEnrichment(row, true) {
		t.Fatal("unverified noop closed the sidecar retry gate")
	}
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

func TestAppendEnrichmentDoesNotResumeClosedBySessionID(t *testing.T) {
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	rows := []string{
		`{"guid":"guid-resume-0000","short_guid":"guid","label":"resume-old","role":"worker","agent":"claude","terminal_id":"term_OLD","pane_id":"p_old","status":"active","provenance":{"mechanism":"spawn","spawned_by":"parent-guid","tool_session_id":"sess-resume","tag":"worker","batch_id":"","cwd":"/old","workspace_id":"ws_old","branch":"old-branch","ts":"2026-07-03T00:00:00Z"}}`,
		`{"guid":"guid-resume-0000","short_guid":"guid","label":"resume-latest","role":"worker","agent":"claude","terminal_id":"term_OLD","pane_id":"p_old","status":"closed","provenance":{"mechanism":"spawn","spawned_by":"parent-guid","tool_session_id":"","tag":"worker","batch_id":"","cwd":"/old","workspace_id":"ws_old","branch":"old-branch","ts":"2026-07-03T00:01:00Z"}}`,
	}
	if err := os.WriteFile(registryPath, []byte(strings.Join(rows, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_GUID", "")
	t.Setenv("HERDER_LABEL", "")
	t.Setenv("HERDER_ROLE", "")
	t.Setenv("HERDER_SPAWNED_BY", "")
	t.Setenv("HERDER_SHIM", "")
	t.Setenv("HCOM_DIR", "/hcom")

	s := &sidecar{tool: "claude", paneID: "p_new", cwd: "/repo", registry: registryPath, completeSeat: testSeatCompletion(t)}
	s.appendEnrichment(&hcomRow{Name: "resume-vire", Tag: "worker", SessionID: "sess-resume", Directory: "/repo"})

	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("rows = %d, want 2 (closed projection must not be resurrected)", len(recs))
	}
	latest := registry.Resolve(recs, "guid-resume-0000")
	if latest == nil || ptrString(latest.Label) != "resume-latest" || latest.Status != "closed" || latest.HcomName != "" {
		t.Fatalf("latest = %+v, want unchanged closed resume-latest row", latest)
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

	s := &sidecar{tool: "codex", paneID: "p_new", cwd: "/repo", registry: registryPath, completeSeat: testSeatCompletion(t)}
	s.appendEnrichment(&hcomRow{Name: "worker-rive", Tag: "worker", SessionID: "sess-123", Directory: "/repo"})

	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("rows = %d, want 1 closed row only", len(recs))
	}
	latest := registry.Resolve(recs, "guid-closed-0000")
	if latest == nil || !registry.IsTerminal(*latest) {
		t.Fatalf("latest = %+v, want retired", latest)
	}
}

func TestAppendEnrichmentDoesNotResurrectArchivedClosedGUID(t *testing.T) {
	state := t.TempDir()
	registryPath := filepath.Join(state, "registry.jsonl")
	if err := os.WriteFile(registryPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(state, "registry.jsonl.archive", "0002-rotation.jsonl")
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatal(err)
	}
	prior := `{"guid":"guid-closed-0000","short_guid":"closed","label":"closed-worker","role":"worker","agent":"codex","terminal_id":"term_OLD","pane_id":"p_old","status":"closed","hcom_name":"worker-rive","provenance":{"mechanism":"spawn","spawned_by":"parent-guid","tool_session_id":"sess-123","tag":"worker","batch_id":"","cwd":"/old","workspace_id":"ws_old","branch":"","ts":"2026-07-03T00:00:00Z"}}`
	if err := os.WriteFile(archive, []byte(prior+"\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDER_GUID", "guid-closed-0000")
	t.Setenv("HERDER_ROLE", "worker")
	t.Setenv("HERDER_LABEL", "closed-worker")
	t.Setenv("HCOM_DIR", "/hcom")

	s := &sidecar{tool: "codex", paneID: "p_new", cwd: "/repo", registry: registryPath, completeSeat: testSeatCompletion(t)}
	s.appendEnrichment(&hcomRow{Name: "worker-rive", Tag: "worker", SessionID: "sess-123", Directory: "/repo"})

	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("live rows = %+v, want no resurrection append", recs)
	}
	archived, err := registry.LoadWithArchives(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	latest := registry.Resolve(archived, "guid-closed-0000")
	if latest == nil || latest.Status != "closed" || !latest.Archived {
		t.Fatalf("archived latest = %+v, want closed archived row", latest)
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

	s := &sidecar{tool: "claude", paneID: "p_manual", cwd: "/repo", registry: registryPath, completeSeat: testSeatCompletion(t)}
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

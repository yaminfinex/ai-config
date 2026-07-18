package spawncmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/missioncontext"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcompletion"
	"ai-config/tools/herder/internal/seatcred"
	"ai-config/tools/herder/internal/shellquote"
)

type cleanupHerdr struct {
	closed bool
	calls  []string
}

type spawnResponse struct {
	want string
	out  []byte
	rc   int
	err  error
}

type scriptedSpawnClient struct {
	responses []spawnResponse
	calls     []string
}

func (c *scriptedSpawnClient) next(args ...string) (spawnResponse, error) {
	call := strings.Join(args, " ")
	c.calls = append(c.calls, call)
	if len(c.responses) == 0 {
		return spawnResponse{}, errors.New("unexpected call")
	}
	r := c.responses[0]
	c.responses = c.responses[1:]
	if call != r.want {
		return spawnResponse{}, fmt.Errorf("call = %q, want %q", call, r.want)
	}
	return r, nil
}

func (c *scriptedSpawnClient) Combined(args ...string) ([]byte, int, error) {
	r, err := c.next(args...)
	if err != nil {
		return nil, 64, err
	}
	return r.out, r.rc, r.err
}

func (c *scriptedSpawnClient) Output(args ...string) ([]byte, error) {
	r, err := c.next(args...)
	if err != nil {
		return nil, err
	}
	return r.out, r.err
}

func (c *scriptedSpawnClient) Run(args ...string) (int, error) {
	r, err := c.next(args...)
	if err != nil {
		return 64, err
	}
	return r.rc, r.err
}

func assertSpawnScriptConsumed(t *testing.T, client *scriptedSpawnClient) {
	t.Helper()
	if len(client.responses) != 0 {
		t.Fatalf("%d scripted response(s) were not consumed; calls = %v", len(client.responses), client.calls)
	}
}

func (f *cleanupHerdr) Combined(args ...string) ([]byte, int, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if len(args) >= 2 && args[0] == "pane" && args[1] == "get" {
		if f.closed {
			return []byte(`{"error":{"code":"pane_not_found"}}`), 4, nil
		}
		return []byte(`{"result":{"pane":{"pane_id":"p_new","terminal_id":"term_new"}}}`), 0, nil
	}
	if len(args) >= 2 && args[0] == "pane" && args[1] == "close" {
		f.closed = true
		return []byte(`{"result":{"type":"closed"}}`), 0, nil
	}
	return nil, 64, nil
}

func (f *cleanupHerdr) Output(args ...string) ([]byte, error) { return nil, errors.New("unused") }
func (f *cleanupHerdr) Run(args ...string) (int, error)       { return 64, errors.New("unused") }

func TestAbsentChildAllowsCleanupAfterCompletionFailure(t *testing.T) {
	client := &scriptedSpawnClient{responses: []spawnResponse{
		{want: "pane process_info p_new", out: []byte(`{"result":{"process_info":{"foreground_processes":[]}}}`)},
		{want: "pane get p_new", out: []byte(`{"result":{"pane":{"pane_id":"p_new","terminal_id":"term_new"}}}`)},
		{want: "pane list", out: []byte(`{"result":{"panes":[{"pane_id":"p_owner","focused":true}]}}`)},
		{want: "pane close p_new", out: []byte(`{"result":{"type":"ok"}}`)},
		{want: "pane list", out: []byte(`{"result":{"panes":[{"pane_id":"p_owner","focused":true}]}}`)},
		{want: "pane get p_new", out: []byte(`{"error":{"code":"pane_not_found"}}`), rc: 1},
	}}
	var stderr strings.Builder
	r := &runner{
		herdr:  client,
		stderr: &stderr,
	}
	if code := r.handleSeatCompletionFailure("seat completion failed: lock refused", "p_new", "term_new", "ready"); code != 1 {
		t.Fatalf("handleSeatCompletionFailure() = %d, want 1", code)
	}
	assertSpawnScriptConsumed(t, client)
	if !strings.Contains(stderr.String(), "seat completion failed: lock refused") || !strings.Contains(stderr.String(), "cleanup confirmed") {
		t.Fatalf("stderr = %q, want refusal plus confirmed cleanup", stderr.String())
	}
}

func TestCompletionInfrastructureFailurePreservesLiveChild(t *testing.T) {
	client := &scriptedSpawnClient{responses: []spawnResponse{
		{want: "pane get p_new", out: []byte(`{"result":{"pane":{"pane_id":"p_new","terminal_id":"term_new"}}}`)},
		{want: "pane process_info p_new", out: []byte(`{"result":{"process_info":{"foreground_processes":[{"pid":4242,"argv":["codex"]}]}}}`)},
	}}
	var stderr strings.Builder
	r := &runner{
		herdr:      client,
		stderr:     &stderr,
		completion: testSpawnCompletionEngine(t),
		updateRegistry: func(string, registry.LockedUpdateFunc) ([]registry.WriteOutcome, error) {
			return nil, errors.New("lock refused")
		},
	}
	record := spawnRecord{GUID: "guid-new", ShortGUID: "guid-new", Label: "worker-new", Agent: "bash", PaneID: "p_new", TerminalID: "term_new"}
	_, err := r.completeSpawn(filepath.Join(t.TempDir(), "registry.jsonl"), record, "p_new", "")
	if err == nil || !strings.Contains(err.Error(), "lock refused") {
		t.Fatalf("completeSpawn() err = %v, want lock refusal", err)
	}
	if code := r.handleSeatCompletionFailure("seat completion failed: "+err.Error(), "p_new", "term_new", "ready"); code != 1 {
		t.Fatalf("handleSeatCompletionFailure() = %d, want 1", code)
	}
	assertSpawnScriptConsumed(t, client)
	if !strings.Contains(stderr.String(), "child pane remains running") || strings.Contains(strings.Join(client.calls, " "), "pane close") {
		t.Fatalf("live-child failure stderr=%q calls=%v", stderr.String(), client.calls)
	}
}

func TestGrokCompletionRefusalNamesBridgeSupervisorAndPromptHandoff(t *testing.T) {
	client := &scriptedSpawnClient{responses: []spawnResponse{{
		want: "pane process_info p_grok", out: []byte(`{"result":{"process_info":{"foreground_processes":[{"pid":4242,"argv":["grok"]}]}}}`),
	}}}
	var stderr strings.Builder
	r := &runner{herdr: client, stderr: &stderr, opts: options{Agent: "grok"}, pendingPrompt: true}
	if code := r.handleSeatCompletionFailure("seat completion refused [joined_bus_row_missing]", "p_grok", "term_grok", "bound"); code != 1 {
		t.Fatalf("handleSeatCompletionFailure()=%d", code)
	}
	assertSpawnScriptConsumed(t, client)
	got := stderr.String()
	for _, want := range []string{"its bridge supervisor", "complete the seat AND deliver the pending initial prompt automatically"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Grok refusal=%q, want %q", got, want)
		}
	}
	if strings.Contains(got, "its sidecar") {
		t.Fatalf("Grok refusal retained false sidecar promise: %q", got)
	}
}

func TestNoopCompletionIsSuccessfulReplay(t *testing.T) {
	r := &runner{}
	handled, code := r.handleIncompleteSeatCompletion(seatcompletion.Result{Status: registry.WriteNoop}, nil, "p_new", "term_new", "ready")
	if handled || code != 0 {
		t.Fatalf("handleIncompleteSeatCompletion() = (%v, %d), want (false, 0)", handled, code)
	}
}

func TestParseArgsAcceptsMission(t *testing.T) {
	opts, code := parseArgs([]string{"--role", "worker", "--agent", "bash", "--mission", "alpha"}, io.Discard, io.Discard)
	if code != 0 {
		t.Fatalf("parseArgs() code = %d, want 0", code)
	}
	if opts.MissionSlug != "alpha" || !opts.MissionSet {
		t.Fatalf("mission options = (%q, %v), want (alpha, true)", opts.MissionSlug, opts.MissionSet)
	}
}

func TestBindTimeoutDefaultsAreFamilyAwareAndEnvironmentWins(t *testing.T) {
	t.Setenv("HERDER_SPAWN_BIND_MS", "")
	for _, tt := range []struct {
		agent string
		want  int
	}{
		{agent: "claude", want: 60000},
		{agent: "codex", want: 300000},
		{agent: "gemini", want: 300000},
		{agent: "bash", want: 60000},
	} {
		opts, code := parseArgs([]string{"--role", "worker", "--agent", tt.agent}, io.Discard, io.Discard)
		if code != 0 || opts.BindTimeoutMS != tt.want {
			t.Fatalf("agent %s bind timeout = %d code=%d, want %d", tt.agent, opts.BindTimeoutMS, code, tt.want)
		}
	}

	t.Setenv("HERDER_SPAWN_BIND_MS", "12345")
	for _, agent := range []string{"claude", "codex"} {
		opts, code := parseArgs([]string{"--role", "worker", "--agent", agent}, io.Discard, io.Discard)
		if code != 0 || opts.BindTimeoutMS != 12345 {
			t.Fatalf("agent %s override = %d code=%d, want 12345", agent, opts.BindTimeoutMS, code)
		}
	}
}

func TestSpawnJSONMissionWireShape(t *testing.T) {
	withMission, err := json.Marshal(newSpawnJSONRecord(
		missionSpawnRecord(&v2.Mission{Slug: "alpha", Source: missioncontext.SourceExplicit}),
		spawnJSONDetails{},
	))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(withMission, &fields); err != nil {
		t.Fatal(err)
	}
	if got := string(fields["mission"]); got != `{"slug":"alpha","source":"explicit"}` {
		t.Fatalf("mission JSON = %s, want exact slug+source wire shape", got)
	}

	withoutMission, err := json.Marshal(newSpawnJSONRecord(missionSpawnRecord(nil), spawnJSONDetails{}))
	if err != nil {
		t.Fatal(err)
	}
	fields = nil
	if err := json.Unmarshal(withoutMission, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["mission"]; ok {
		t.Fatalf("mission field present without --mission: %s", withoutMission)
	}
}

func TestSpawnMissionRefusalStopsBeforePaneCreation(t *testing.T) {
	stubDir := t.TempDir()
	called := filepath.Join(stubDir, "called")
	stub := "#!/bin/sh\ntouch " + shellquote.Quote(called) + "\nexit 0\n"
	for _, name := range []string{"herdr", "jq"} {
		if err := os.WriteFile(filepath.Join(stubDir, name), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("PATH", stubDir)
	missionsRepo := t.TempDir()
	if err := os.Mkdir(filepath.Join(missionsRepo, "missions"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MISSIONS_REPO", missionsRepo)

	tests := []struct {
		name       string
		slug       string
		unsetRepo  bool
		wantCause  string
		wantRemedy string
	}{
		{name: "invalid slug", slug: "bad--slug", wantCause: "invalid_mission_slug", wantRemedy: "use lowercase letters, digits, and single hyphens, with no trailing hyphen"},
		{name: "empty slug", slug: "", wantCause: "invalid_mission_slug", wantRemedy: "use lowercase letters, digits, and single hyphens, with no trailing hyphen"},
		{name: "missing mission", slug: "missing", wantCause: "mission_not_found", wantRemedy: "check the slug or create the mission"},
		{name: "missions repo unset", slug: "alpha", unsetRepo: true, wantCause: "missions_repo_unset", wantRemedy: "set MISSIONS_REPO to the shared missions repository"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.unsetRepo {
				t.Setenv("MISSIONS_REPO", "")
			}
			if err := os.Remove(called); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			var stderr strings.Builder
			code := Run([]string{"--role", "worker", "--agent", "bash", "--mission", tt.slug}, io.Discard, &stderr)
			if code != 1 {
				t.Fatalf("Run() code = %d, want 1", code)
			}
			for _, want := range []string{"refused [" + tt.wantCause + "]", tt.wantRemedy} {
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("stderr = %q, want %q", stderr.String(), want)
				}
			}
			if _, err := os.Stat(called); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("herdr or jq executed before mission refusal: %v", err)
			}
		})
	}
}

func TestPromptSenderProofAcceptsLivePaneWhenRecordedSessionIsStale(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := `#!/bin/sh
printf '%s\n' '[{"name":"live-self","session_id":"current-session","joined":true,"launch_context":{"pane_id":"pane-self"}}]'
`
	if err := os.WriteFile(filepath.Join(binDir, "hcom"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "pane-self")
	t.Setenv("HERDER_GUID", "guid-self")
	t.Setenv("HCOM_SESSION_ID", "stale-session")

	registryPath := filepath.Join(stateDir, "registry.jsonl")
	if err := registry.Append(registryPath, []byte(`{"guid":"guid-self","short_guid":"guid-sel","label":"dispatcher","pane_id":"pane-self","terminal_id":"term-self","hcom_dir":"/bus","hcom_name":"live-self","status":"active"}`)); err != nil {
		t.Fatal(err)
	}
	client := &scriptedSpawnClient{responses: []spawnResponse{{
		want: "pane get pane-self",
		out:  []byte(`{"result":{"pane":{"pane_id":"pane-self","terminal_id":"term-self","foreground_cwd":"/repo"}}}`),
	}}}
	r := &runner{herdr: client}
	selected := seatcred.Selection{GUID: "guid-self", Row: v2.SessionRecord{GUID: "guid-self", Seat: &v2.Seat{HcomName: "live-self"}}}
	got, err := r.verifyPromptSender(selected, "/bus")
	if err != nil || got != "live-self" {
		t.Fatalf("verifyPromptSender = (%q, %v), want pane-proven live-self", got, err)
	}
	assertSpawnScriptConsumed(t, client)
}

func TestPromptSenderRefusalStopsBeforeChildCreation(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "hcom"), []byte("#!/bin/sh\nprintf '[]\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", root)
	t.Setenv("HERDER_STATE_DIR", stateDir)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "pane-self")
	t.Setenv("HERDER_GUID", "guid-self")
	t.Setenv("HCOM_SESSION_ID", "stale-session")
	t.Setenv("HCOM_PROCESS_ID", "")

	registryPath := filepath.Join(stateDir, "registry.jsonl")
	if err := registry.Append(registryPath, []byte(`{"guid":"guid-self","short_guid":"guid-sel","label":"dispatcher","pane_id":"pane-self","terminal_id":"term-self","hcom_dir":"/bus","hcom_name":"missing-self","status":"active"}`)); err != nil {
		t.Fatal(err)
	}
	client := &scriptedSpawnClient{responses: []spawnResponse{{
		want: "pane get pane-self",
		out:  []byte(`{"result":{"pane":{"pane_id":"pane-self","terminal_id":"term-self","foreground_cwd":"/repo"}}}`),
	}}}
	var stderr strings.Builder
	r := &runner{
		opts:   options{Role: "worker", Agent: "codex", Prompt: "prompt", Worktree: "branch"},
		herdr:  client,
		stdout: io.Discard,
		stderr: &stderr,
	}
	if code := r.run(); code != 2 {
		t.Fatalf("run() code = %d, want sender refusal exit 2; stderr=%s", code, stderr.String())
	}
	assertSpawnScriptConsumed(t, client)
	for _, want := range []string{"sender identity is not verified", "Run `herder enroll`", "Nothing was launched"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestCompleteSpawnWritesInitialExplicitMission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	record := missionSpawnRecord(&v2.Mission{Slug: "alpha", Source: missioncontext.SourceExplicit})
	result, err := completeSpawnRecord(t, path, record)
	if err != nil || result.Refusal != nil || result.Status != registry.WriteApplied {
		t.Fatalf("completeSpawn() = %+v err=%v", result, err)
	}
	projection, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	row := registry.V2ByGUID(projection, record.GUID)
	if row == nil || row.Event != "seated" || row.Mission == nil || row.Mission.Slug != "alpha" || row.Mission.Source != missioncontext.SourceExplicit {
		t.Fatalf("spawn row = %+v, want initial completed row with explicit alpha membership", row)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), `"guid":"guid-mission"`) != 1 || strings.Contains(string(raw), `"event":"mission_joined"`) {
		t.Fatalf("registry = %s, want membership on exactly one initial row", raw)
	}
}

func TestCompleteSpawnRejectsInferredMissionSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	record := missionSpawnRecord(&v2.Mission{Slug: "alpha", Source: missioncontext.SourceCWD})
	result, err := completeSpawnRecord(t, path, record)
	if err != nil || result.Refusal == nil || !strings.Contains(result.Refusal.Cause, "invalid durable mission source") {
		t.Fatalf("completeSpawn() = %+v err=%v, want durable-source refusal", result, err)
	}
	projection, loadErr := v2.LoadFile(path, v2.LoadOptions{})
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if row := registry.V2ByGUID(projection, record.GUID); row != nil {
		t.Fatalf("refused inferred membership persisted: %+v", row)
	}
}

func TestSpawnMissionSurvivesRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.jsonl")
	record := missionSpawnRecord(&v2.Mission{Slug: "alpha", Source: missioncontext.SourceExplicit})
	if result, err := completeSpawnRecord(t, path, record); err != nil || result.Refusal != nil {
		t.Fatalf("completeSpawn() = %+v err=%v", result, err)
	}
	beforeNoise, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for range 4 {
		outcomes, err := registry.UpdateLocked(path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
			current := registry.V2ByGUID(tx.Projection, record.GUID)
			if current == nil {
				t.Fatal("spawn row missing before rotation")
			}
			next := *current
			next.Event = "recognised"
			next.RecordedAt = ""
			next.Mission = nil
			return []v2.SessionRecord{next}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := registry.SingleOutcome(outcomes)
		if err != nil || outcome.Err() != nil {
			t.Fatalf("successor outcome = %+v, err = %v", outcome, err)
		}
	}
	t.Setenv("HERDER_REGISTRY_ROTATE_BYTES", strconv.Itoa(len(beforeNoise)+512))
	outcomes, err := registry.UpdateLocked(path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		current := registry.V2ByGUID(tx.Projection, record.GUID)
		if current == nil {
			t.Fatal("spawn row missing during rotation")
		}
		next := *current
		next.Event = "registered"
		return []v2.SessionRecord{next}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := registry.SingleOutcome(outcomes)
	if err != nil || outcome.Err() != nil {
		t.Fatalf("rotation successor outcome = %+v, err = %v", outcome, err)
	}
	if outcome.Status != registry.WriteNoop {
		t.Fatalf("rotation successor outcome = %+v, want checked no-op", outcome)
	}
	after, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	row := registry.V2ByGUID(after, record.GUID)
	if row == nil || row.Mission == nil || row.Mission.Slug != "alpha" || row.Mission.Source != missioncontext.SourceExplicit {
		t.Fatalf("spawn membership after rotation = %+v", row)
	}
	archives, err := filepath.Glob(filepath.Join(dir, "registry.jsonl.archive", "*-rotation.jsonl"))
	if err != nil || len(archives) != 1 {
		t.Fatalf("rotation archives = %v, err = %v, want one", archives, err)
	}
}

func missionSpawnRecord(mission *v2.Mission) spawnRecord {
	return spawnRecord{
		GUID:       "guid-mission",
		ShortGUID:  "guid-mis",
		Label:      "worker-mission",
		Role:       "worker",
		Agent:      "bash",
		PaneID:     "p_mission",
		TerminalID: "term_mission",
		CWD:        "/repo",
		Status:     "active",
		StartedAt:  "2026-07-15T00:00:00Z",
		Mission:    mission,
	}
}

func testSpawnCompletionEngine(t *testing.T) *seatcompletion.Engine {
	t.Helper()
	ids := 0
	return &seatcompletion.Engine{
		ListBus: func(context.Context, string) ([]hcomidentity.Row, error) {
			t.Fatal("unexpected bus list")
			return nil, nil
		},
		ProcessAlive: func(int) bool { return true },
		Now:          func() time.Time { return time.Date(2026, 7, 17, 1, 2, 3, 0, time.UTC) },
		NewBindingID: func() (string, error) {
			ids++
			return fmt.Sprintf("binding-%d", ids), nil
		},
	}
}

func completeSpawnRecord(t *testing.T, path string, record spawnRecord) (seatcompletion.Result, error) {
	t.Helper()
	client := &scriptedSpawnClient{responses: []spawnResponse{{
		want: "pane get " + record.PaneID,
		out:  []byte(`{"result":{"pane":{"pane_id":"` + record.PaneID + `","terminal_id":"` + record.TerminalID + `"}}}`),
	}}}
	engine := testSpawnCompletionEngine(t)
	if record.Agent != "bash" {
		joined := true
		engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
			return []hcomidentity.Row{{
				Name: "bus-live", Joined: &joined, SessionID: record.Provenance.ToolSessionID,
				LaunchContext: hcomidentity.LaunchContext{PaneID: record.PaneID},
			}}, nil
		}
	}
	r := &runner{herdr: client, completion: engine}
	result, err := r.completeSpawn(path, record, record.PaneID, record.Provenance.ToolSessionID)
	assertSpawnScriptConsumed(t, client)
	return result, err
}

func TestSeedPaneClosePreservesFocus(t *testing.T) {
	client := &scriptedSpawnClient{responses: []spawnResponse{
		{want: "pane get p_seed", out: []byte(`{"result":{"pane":{"pane_id":"p_seed","terminal_id":"term_seed"}}}`)},
		{want: "pane list", out: []byte(`{"result":{"panes":[{"pane_id":"p_owner","focused":true}]}}`)},
		{want: "pane close p_seed", out: []byte(`{"result":{"type":"ok"}}`)},
		{want: "pane list", out: []byte(`{"result":{"panes":[{"pane_id":"p_owner","focused":true}]}}`)},
	}}
	var stderr strings.Builder
	r := &runner{herdr: client, stderr: &stderr}
	if !r.closeSeedPane("p_seed", "term_seed", "term_agent") {
		t.Fatalf("closeSeedPane returned false; stderr = %q", stderr.String())
	}
	assertSpawnScriptConsumed(t, client)
}

// resolveBus calls resolveSpawnerBus discarding the ambiguity signal + warning,
// for the many cases that assert only the resolved name.
func resolveBus(path, notifyTo, spawnedBy, pane, term, childDir string) string {
	name, _ := resolveSpawnerBus(path, notifyTo, spawnedBy, pane, term, childDir, io.Discard)
	return name
}

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

func TestSpawnLabelPrefixReplacement(t *testing.T) {
	if got := spawnLabel("spec", "", "abcd1234"); got != "spec-abcd1234" {
		t.Fatalf("default label = %q, want spec-abcd1234", got)
	}
	if got := spawnLabel("spec", "spec-hera", "abcd1234"); got != "spec-hera-abcd1234" {
		t.Fatalf("prefix label = %q, want spec-hera-abcd1234", got)
	}
}

func TestNewTabMoveArgsCarryResolvedFocus(t *testing.T) {
	for _, tc := range []struct {
		name      string
		focusArgs []string
		wantFocus string
	}{
		{name: "default", wantFocus: "--no-focus"},
		{name: "explicit focus", focusArgs: []string{"--focus"}, wantFocus: "--focus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"--role", "worker", "--agent", "codex", "--new-tab"}, tc.focusArgs...)
			opts, code := parseArgs(args, io.Discard, io.Discard)
			if code != 0 {
				t.Fatalf("parseArgs() code = %d, want 0", code)
			}
			got := strings.Join(newTabMoveArgs("pane-1", "worker", opts.FocusFlag), " ")
			want := "pane move pane-1 --new-tab " + tc.wantFocus + " --label worker"
			if got != want {
				t.Fatalf("move args = %q, want %q", got, want)
			}
		})
	}
}

func TestResolveSpawnerBusMatchesEnrolledPane(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	rows := []string{
		// enrolled orchestrator: pane/terminal identity, bus name, NO spawner guid in play
		`{"guid":"guid-hera","short_guid":"guid-her","label":"orchestrator","pane_id":"p_orch","terminal_id":"term_ORCH","hcom_name":"hera","status":"active"}`,
		// retired session holding the SAME pane id from an older legacy row must not win
		`{"guid":"guid-old","short_guid":"guid-old","label":"old","pane_id":"p_stale","terminal_id":"term_STALE","hcom_name":"stale-name","status":"closed"}`,
	}
	for _, row := range rows {
		if err := registry.Append(path, []byte(row)); err != nil {
			t.Fatal(err)
		}
	}

	// spawner identified only by its pane id (the enrolled case: spawnedBy=user)
	if got := resolveBus(path, "", "user", "p_orch", "", ""); got != "hera" {
		t.Fatalf("pane match = %q, want hera", got)
	}
	// terminal id fallback (notifyTo auto-resolved to the spawner's terminal)
	if got := resolveBus(path, "term_ORCH", "user", "", "", ""); got != "hera" {
		t.Fatalf("terminal match via notifyTo = %q, want hera", got)
	}
	// retired sessions never resolve by pane coordinates
	if got := resolveBus(path, "", "user", "p_stale", "", ""); got != "" {
		t.Fatalf("retired pane match = %q, want empty", got)
	}
	// guid resolution still wins first
	if got := resolveBus(path, "", "guid-hera", "", "", ""); got != "hera" {
		t.Fatalf("guid match = %q, want hera", got)
	}
}

func TestResolveSpawnerBusAcceptsBusNames(t *testing.T) {
	// Stub hcom on PATH so liveOnBus is hermetic: the bus knows only lone-wolf.
	stubDir := t.TempDir()
	stub := "#!/bin/sh\necho '[{\"name\":\"lone-wolf\",\"tag\":\"x\",\"directory\":\"/d\",\"created_at\":\"2026-01-01T00:00:00Z\",\"launch_context\":{\"pane_id\":\"p_9\"}}]'\n"
	if err := os.WriteFile(filepath.Join(stubDir, "hcom"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	path := filepath.Join(t.TempDir(), "registry.jsonl")
	rows := []string{
		`{"guid":"guid-hera","short_guid":"guid-her","label":"orchestrator","pane_id":"p_orch","terminal_id":"term_ORCH","hcom_name":"hera","status":"active"}`,
		`{"guid":"guid-old","short_guid":"guid-old","label":"old","pane_id":"p_stale","terminal_id":"term_STALE","hcom_name":"ghost","status":"closed"}`,
	}
	for _, row := range rows {
		if err := registry.Append(path, []byte(row)); err != nil {
			t.Fatal(err)
		}
	}

	// --notify-to may BE a bus name: a seated session's hcom_name matches (TASK-023)
	if got := resolveBus(path, "hera", "user", "", "", "/no-such-bus"); got != "hera" {
		t.Fatalf("seated hcom_name match = %q, want hera", got)
	}
	// a retired session's bus name must not vouch, and the stub bus doesn't know it
	if got := resolveBus(path, "ghost", "user", "", "", "/no-such-bus"); got != "" {
		t.Fatalf("retired hcom_name match = %q, want empty", got)
	}
	// literal bus name unknown to the registry validates against the child's bus
	if got := resolveBus(path, "lone-wolf", "user", "", "", "/no-such-bus"); got != "lone-wolf" {
		t.Fatalf("literal bus name = %q, want lone-wolf", got)
	}
	// a name live nowhere still refuses
	if got := resolveBus(path, "nosuch", "user", "", "", "/no-such-bus"); got != "" {
		t.Fatalf("unknown name = %q, want empty", got)
	}
	// literal validation works even with NO readable registry (non-bus-env shell)
	if got := resolveBus(filepath.Join(t.TempDir(), "absent.jsonl"), "lone-wolf", "user", "", "", "/no-such-bus"); got != "lone-wolf" {
		t.Fatalf("literal without registry = %q, want lone-wolf", got)
	}
	// an EXPLICIT but unresolvable --notify-to must not fall through to the
	// spawner's own resolution — a typo must never silently redirect reports
	if got := resolveBus(path, "nosuch", "guid-hera", "p_orch", "term_ORCH", "/no-such-bus"); got != "" {
		t.Fatalf("unresolvable notifyTo fell through to spawner = %q, want empty", got)
	}
}

// TestResolveSpawnerBusReusedPaneTiebreaker pins TASK-035 P1-a: notify
// resolution of a reused pane must NOT silently last-pick a stale row. A stub
// `hcom list --json` reports whichever names STUB_JOINED lists as joined; the
// spawner pane p_reuse holds three seated sessions (hera/vore/zero).
func TestResolveSpawnerBusReusedPaneTiebreaker(t *testing.T) {
	stubDir := t.TempDir()
	stub := "#!/bin/sh\n" +
		"printf '['\n" +
		"first=1\n" +
		"for n in $STUB_JOINED; do\n" +
		"  [ $first -eq 1 ] || printf ','\n" +
		"  printf '{\"name\":\"%s\",\"tag\":\"x\",\"directory\":\"/d\",\"created_at\":\"2026-01-01T00:00:00Z\"}' \"$n\"\n" +
		"  first=0\n" +
		"done\n" +
		"printf ']\\n'\n"
	if err := os.WriteFile(filepath.Join(stubDir, "hcom"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	path := filepath.Join(t.TempDir(), "registry.jsonl")
	for _, row := range []string{
		`{"guid":"guid-hera","label":"hera","pane_id":"p_reuse","terminal_id":"term_reuse","hcom_name":"hera-rive","status":"active"}`,
		`{"guid":"guid-vore","label":"vore","pane_id":"p_reuse","terminal_id":"term_reuse","hcom_name":"vore-lilo","status":"active"}`,
		`{"guid":"guid-zero","label":"zero","pane_id":"p_reuse","terminal_id":"term_reuse","hcom_name":"zero-mano","status":"active"}`,
		`{"guid":"guid-solo","label":"solo","pane_id":"p_solo","terminal_id":"term_solo","hcom_name":"solo-teki","status":"active"}`,
	} {
		if err := registry.Append(path, []byte(row)); err != nil {
			t.Fatal(err)
		}
	}

	// One live (@hera) among stale → notify routes to it, not last-in-guid @zero.
	t.Setenv("STUB_JOINED", "hera-rive")
	if name, amb := resolveSpawnerBus(path, "", "user", "p_reuse", "", "/bus", io.Discard); name != "hera-rive" || amb {
		t.Fatalf("one-live reuse = (%q, %v), want (hera-rive, false)", name, amb)
	}

	// Two live at once → ambiguous, warn + skip (best-effort), never guess.
	t.Setenv("STUB_JOINED", "hera-rive zero-mano")
	var warn bytes.Buffer
	if name, amb := resolveSpawnerBus(path, "", "user", "p_reuse", "", "/bus", &warn); name != "" || !amb {
		t.Fatalf("multi-live reuse = (%q, %v), want (\"\", true)", name, amb)
	}
	for _, want := range []string{"ambiguous", "guid-hera", "guid-zero"} {
		if !bytes.Contains(warn.Bytes(), []byte(want)) {
			t.Errorf("ambiguity warning missing %q; got: %s", want, warn.String())
		}
	}

	// None live → also ambiguous skip (can't tell which owns the pane now).
	t.Setenv("STUB_JOINED", "")
	if name, amb := resolveSpawnerBus(path, "", "user", "p_reuse", "", "/bus", io.Discard); name != "" || !amb {
		t.Fatalf("none-live reuse = (%q, %v), want (\"\", true)", name, amb)
	}

	// Single candidate (p_solo) resolves unchanged — no liveness probe, no skip.
	if name, amb := resolveSpawnerBus(path, "", "user", "p_solo", "", "/bus", io.Discard); name != "solo-teki" || amb {
		t.Fatalf("single-candidate solo = (%q, %v), want (solo-teki, false)", name, amb)
	}
	// Explicit --notify-to <pane-id> for the reused pane runs the same tiebreaker.
	t.Setenv("STUB_JOINED", "vore-lilo")
	if name, amb := resolveSpawnerBus(path, "p_reuse", "user", "", "", "/bus", io.Discard); name != "vore-lilo" || amb {
		t.Fatalf("explicit notify-to reuse = (%q, %v), want (vore-lilo, false)", name, amb)
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

func TestCompleteSpawnUsesCapturedNameAsLiveEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	record := spawnRecord{
		GUID: "guid-name", ShortGUID: "guid-nam", Label: "worker-name", Role: "worker", Agent: "codex",
		PaneID: "pane-live", TerminalID: "terminal-live", HcomName: "worker-mine", HcomDir: "/bus", Status: "active",
	}
	client := &scriptedSpawnClient{responses: []spawnResponse{{
		want: "pane get pane-live",
		out:  []byte(`{"result":{"pane":{"pane_id":"pane-live","terminal_id":"terminal-live"}}}`),
	}}}
	joined := true
	engine := testSpawnCompletionEngine(t)
	engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{{Name: "worker-mine", Joined: &joined}}, nil
	}
	engine.RepairLaunchContext = func(_, name, pane string) hcomidentity.LaunchContextRepair {
		if name != "worker-mine" || pane != "pane-live" {
			t.Fatalf("repair inputs = (%q, %q)", name, pane)
		}
		return hcomidentity.LaunchContextRepair{Status: "written", PaneID: pane}
	}
	r := &runner{herdr: client, completion: engine}
	result, err := r.completeSpawn(path, record, "pane-live", "")
	assertSpawnScriptConsumed(t, client)
	if err != nil || result.Refusal != nil || result.Status != registry.WriteApplied {
		t.Fatalf("completeSpawn exact name = %+v err=%v, want applied", result, err)
	}
}

type bindObserverHerdr struct{}

func (bindObserverHerdr) Combined(...string) ([]byte, int, error) {
	return nil, 64, errors.New("unused")
}
func (bindObserverHerdr) Run(...string) (int, error) { return 64, errors.New("unused") }
func (bindObserverHerdr) Output(args ...string) ([]byte, error) {
	call := strings.Join(args, " ")
	switch {
	case call == "pane get pane-live":
		return []byte(`{"result":{"pane":{"pane_id":"pane-live","terminal_id":"terminal-live"}}}`), nil
	case strings.HasPrefix(call, "agent read "):
		return []byte(`{"result":{"read":{"text":""}}}`), nil
	case call == "agent list":
		return []byte(`{"result":{"agents":[]}}`), nil
	default:
		return nil, fmt.Errorf("unexpected herdr call %q", call)
	}
}

func TestSidecarCompletionConvergesWithSpawnBindAndNoopReplay(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "hcom"), []byte("#!/bin/sh\nprintf '[]\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	registryPath := filepath.Join(t.TempDir(), "registry.jsonl")
	record := spawnRecord{
		GUID: "guid-converge", ShortGUID: "guid-con", Label: "worker-converge", Role: "worker", Agent: "codex",
		PaneID: "pane-live", TerminalID: "terminal-live", WorkspaceID: "workspace-live", CWD: "/repo", HcomDir: "/bus", HcomName: "worker-mine",
		HcomTag: "worker", Status: "active", StartedAt: "2026-07-17T00:00:00Z",
		Provenance: registry.BuildProvenance("spawn", "parent-guid", "", "worker", "/repo", "workspace-live"),
	}
	joined := true
	rows := []hcomidentity.Row{{Name: "worker-mine", Tool: "codex", Joined: &joined}}
	newEngine := func() seatcompletion.Engine {
		engine := seatcompletion.DefaultEngine()
		engine.HerdrPane = func(context.Context, string) (seatcompletion.LivePane, error) {
			return seatcompletion.LivePane{PaneID: "pane-live", TerminalID: "terminal-live"}, nil
		}
		engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) { return rows, nil }
		engine.RepairLaunchContext = func(_, _, pane string) hcomidentity.LaunchContextRepair {
			return hcomidentity.LaunchContextRepair{Status: "already-present", PaneID: pane}
		}
		return engine
	}

	completion := make(chan struct {
		at     time.Time
		result seatcompletion.Result
		err    error
	}, 1)
	started := time.Now()
	go func() {
		time.Sleep(2100 * time.Millisecond)
		engine := newEngine()
		result, err := engine.Complete(context.Background(), seatcompletion.Request{
			Origin:       seatcompletion.OriginRecognition,
			RegistryPath: registryPath,
			Candidate:    spawnCompletionCandidate(record),
			Seat:         seatcompletion.SeatClaim{Kind: seatcompletion.SeatHerdr, PaneID: record.PaneID},
			Namespace:    record.HcomDir,
			Evidence:     hcomidentity.Evidence{Name: record.HcomName},
		})
		completion <- struct {
			at     time.Time
			result seatcompletion.Result
			err    error
		}{time.Now(), result, err}
	}()

	r := &runner{opts: options{Agent: "codex", BindTimeoutMS: 60000}, herdr: bindObserverHerdr{}}
	paneID := record.PaneID
	name, reason, blocked, _ := r.awaitBind(&paneID, registryPath, record.GUID, record.HcomDir, record.PaneID, "")
	boundAt := time.Now()
	written := <-completion
	if written.err != nil || written.result.Refusal != nil || written.result.Status != registry.WriteApplied {
		t.Fatalf("sidecar completion = %+v err=%v", written.result, written.err)
	}
	if name != record.HcomName || reason != "bound" || blocked {
		t.Fatalf("awaitBind() = name %q reason %q blocked %v", name, reason, blocked)
	}
	if sidecarElapsed := written.at.Sub(started); sidecarElapsed < 2*time.Second || sidecarElapsed > 3*time.Second {
		t.Fatalf("sidecar completion elapsed = %s, want one 2s steady poll", sidecarElapsed)
	}
	if observeLag := boundAt.Sub(written.at); observeLag < 0 || observeLag > 750*time.Millisecond {
		t.Fatalf("spawn observation lag = %s, want next 500ms poll", observeLag)
	}
	if total := boundAt.Sub(started); total >= 4*time.Second || total >= 60*time.Second {
		t.Fatalf("end-to-end bind = %s, want well inside 60s", total)
	}

	replayEngine := newEngine()
	r.completion = &replayEngine
	replay, err := r.completeSpawn(registryPath, record, record.PaneID, "")
	if err != nil || replay.Refusal != nil || replay.Status != registry.WriteNoop || len(replay.Row) == 0 {
		t.Fatalf("spawn replay = %+v err=%v, want hydrated noop", replay, err)
	}
	if handled, code := r.handleIncompleteSeatCompletion(replay, nil, record.PaneID, record.TerminalID, reason); handled || code != 0 {
		t.Fatalf("spawn noop handling = (%v, %d), want success", handled, code)
	}
	projection, err := v2.LoadFile(registryPath, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	credentialGeneration := ""
	for _, session := range projection.Sessions() {
		if session.GUID == record.GUID {
			count++
			if session.Seat != nil {
				credentialGeneration = session.Seat.CredentialGeneration
			}
		}
	}
	if count != 1 {
		t.Fatalf("canonical rows for %s = %d, want one after noop replay", record.GUID, count)
	}
	if credentialGeneration == "" {
		t.Fatal("spawn idempotent replay stripped the canonical credential generation")
	}
}

func TestGrokCompletionUsesBridgeCapturedExactNameForAdhocEmptyCoordinateRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	record := spawnRecord{
		GUID: "guid-grok-name", ShortGUID: "guid-gro", Label: "reviewer-grok", Role: "reviewer", Agent: "grok",
		PaneID: "pane-live", TerminalID: "terminal-live", HcomName: "reviewer-lote", HcomDir: "/bus", Status: "active",
	}
	client := &scriptedSpawnClient{responses: []spawnResponse{{
		want: "pane get pane-live",
		out:  []byte(`{"result":{"pane":{"pane_id":"pane-live","terminal_id":"terminal-live"}}}`),
	}}}
	joined := true
	engine := testSpawnCompletionEngine(t)
	engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{{Name: "reviewer-lote", Tool: "adhoc", Joined: &joined}}, nil
	}
	engine.RepairLaunchContext = func(_, name, pane string) hcomidentity.LaunchContextRepair {
		if name != "reviewer-lote" || pane != "pane-live" {
			t.Fatalf("repair inputs = (%q, %q)", name, pane)
		}
		return hcomidentity.LaunchContextRepair{Status: "already-present", PaneID: pane}
	}
	r := &runner{herdr: client, completion: engine}
	result, err := r.completeSpawn(path, record, record.PaneID, "")
	assertSpawnScriptConsumed(t, client)
	if err != nil || result.Refusal != nil || result.Status != registry.WriteApplied {
		t.Fatalf("Grok exact-name completion = %+v err=%v, want applied", result, err)
	}
}

func TestSpawnTimeoutCompletionWithEmptyNameStillRefusesEmptyCoordinateRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	record := spawnRecord{
		GUID: "guid-empty", ShortGUID: "guid-emp", Label: "worker-empty", Role: "worker", Agent: "codex",
		PaneID: "pane-live", TerminalID: "terminal-live", HcomDir: "/bus", Status: "active",
	}
	client := &scriptedSpawnClient{responses: []spawnResponse{{
		want: "pane get pane-live",
		out:  []byte(`{"result":{"pane":{"pane_id":"pane-live","terminal_id":"terminal-live"}}}`),
	}}}
	joined := true
	engine := testSpawnCompletionEngine(t)
	engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{{Name: "worker-mine", Tool: "codex", Joined: &joined}}, nil
	}
	r := &runner{herdr: client, completion: engine}
	result, err := r.completeSpawn(path, record, record.PaneID, "")
	assertSpawnScriptConsumed(t, client)
	if err != nil || result.Refusal == nil || result.Refusal.Code != seatcompletion.RefusalBusRowMissing {
		t.Fatalf("empty-name timeout completion = %+v err=%v, want joined_bus_row_missing", result, err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("empty-name refusal created registry file: %v", statErr)
	}
}

// TestResendCommandForOnlySafeResults pins the recovery-command contract
// (TASK-036): the exact `herder send` resend command is surfaced ONLY for the
// delivery results where a resend is provably safe (nothing went on the wire) —
// bind_timeout and ready_match_timeout — and is empty for every other result so
// the omitempty JSON field stays absent where a resend is NOT the remedy.
func TestResendCommandForOnlySafeResults(t *testing.T) {
	const label = "worker-abc1234"
	safe := map[string]bool{"bind_timeout": true, "ready_match_timeout": true}
	for _, result := range []string{
		"bind_timeout", "ready_match_timeout",
		"delivered", "queued", "blocked_trust_modal", "send_failed", "not_attempted",
	} {
		got := resendCommandFor(result, false, label, "do the thing")
		if safe[result] {
			want := resendCommand(label, "do the thing")
			if got != want {
				t.Fatalf("resendCommandFor(%q) = %q, want %q", result, got, want)
			}
		} else if got != "" {
			t.Fatalf("resendCommandFor(%q) = %q, want empty (resend is not the remedy)", result, got)
		}
	}
	if got := resendCommandFor("bind_timeout", true, label, "do the thing"); got != "" {
		t.Fatalf("persisted bind-timeout hand-off resend command = %q, want empty", got)
	}
}

func TestSummaryDescribesPersistedBindTimeoutAsAutomaticHandoff(t *testing.T) {
	var stderr strings.Builder
	r := &runner{
		opts:          options{Prompt: "initial prompt"},
		stderr:        &stderr,
		pendingPrompt: true,
	}
	r.writeSummary(spawnRecord{
		GUID: "child-guid", Label: "worker-label", Agent: "codex", PaneID: "pane-live",
		WorkspaceID: "workspace-live", CWD: "/work", HcomName: "worker", HcomDir: "/bus",
	}, nil, true, false, "", "", "captured", true, false, "bind_timeout", "bind timed out", false, nil)
	got := stderr.String()
	if !strings.Contains(got, "sidecar will deliver it automatically") || strings.Contains(got, "resend is SAFE") || strings.Contains(got, "resend_command") {
		t.Fatalf("persisted bind-timeout summary contradicts hand-off contract:\n%s", got)
	}
}

// TestResendCommandQuotesPromptVerbatim confirms the prompt round-trips through
// shell quoting so a copy-paste resend re-sends the exact bytes — including a
// multi-line brief with the notify appendix already folded in.
func TestResendCommandQuotesPromptVerbatim(t *testing.T) {
	prompt := "review unit X\nread the plan, then the diff\nreport 'findings'"
	cmd := resendCommand("x-review-9f8e", prompt)
	const prefix = "herder send x-review-9f8e "
	if len(cmd) <= len(prefix) || cmd[:len(prefix)] != prefix {
		t.Fatalf("resendCommand = %q, want prefix %q", cmd, prefix)
	}
	// A newline-bearing prompt must be quoted (never emitted raw), so the command
	// stays a single shell word that pastes back intact.
	quoted := cmd[len(prefix):]
	for _, r := range quoted {
		if r == '\n' {
			t.Fatalf("resendCommand left a raw newline in the quoted prompt: %q", cmd)
		}
	}
}

// TestResendCommandQuotesMetacharLabel pins that the LABEL is quoted too: labels
// are built from --label-prefix verbatim and metachar prefixes are accepted, so
// an unquoted label would split/expand/glob when the recovery command is pasted.
func TestResendCommandQuotesMetacharLabel(t *testing.T) {
	label := `quote;$` + "`" + `&<>("* )-abc1234`
	cmd := resendCommand(label, "do the thing")
	want := "herder send " + shellquote.Quote(label) + " " + shellquote.Quote("do the thing")
	if cmd != want {
		t.Fatalf("resendCommand = %q, want %q", cmd, want)
	}
	// The raw metachar label must NOT appear verbatim — proof it was quoted, not
	// pasted through as splittable/expandable shell.
	if strings.Contains(cmd, "herder send "+label) {
		t.Fatalf("resendCommand emitted the label RAW: %q", cmd)
	}
}

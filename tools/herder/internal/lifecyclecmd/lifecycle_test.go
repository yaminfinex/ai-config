package lifecyclecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/grokbridge"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/launchcmd"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
	"ai-config/tools/herder/internal/seatcompletion"
)

func lifecycleCompletionFor(name, sessionID, paneID string) *seatcompletion.Engine {
	joined := true
	engine := seatcompletion.DefaultEngine()
	engine.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{{
			Name: name, SessionID: sessionID, Joined: &joined,
			LaunchContext: hcomidentity.LaunchContext{PaneID: paneID},
		}}, nil
	}
	engine.RepairLaunchContext = func(string, string, string) hcomidentity.LaunchContextRepair {
		return hcomidentity.LaunchContextRepair{Status: "written"}
	}
	return &engine
}

func TestLifecycleReseatCandidateCarriesCredentialGeneration(t *testing.T) {
	base := []byte(`{"kind":"session","guid":"guid-lifecycle","event":"unseated","state":"unseated","credential_generation":"generation-lifecycle","seat":{"kind":"herdr","terminal_id":"term-old","pane_id":"pane-old","credential_generation":"generation-lifecycle"}}`)
	row, err := registry.UpdateRawObject(base, map[string]any{"terminal_id": "term-new", "pane_id": "pane-new", "status": "active"})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := registry.SessionEventFromJSON(row, "seated", v2.StateSeated)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Seat == nil || candidate.Seat.CredentialGeneration != "generation-lifecycle" {
		t.Fatalf("lifecycle candidate stripped credential generation: %+v", candidate.Seat)
	}
}

func configureLifecycleTest(t *testing.T, stateDir string) {
	t.Helper()
	mockBin := t.TempDir()
	for _, tool := range []string{"herdr", "hcom"} {
		if err := os.WriteFile(filepath.Join(mockBin, tool), []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", mockBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "p_self")
	t.Setenv("HERDER_STATE_DIR", stateDir)
	t.Setenv("HERDER_LIFECYCLE_SETTLE_MS", "0")
	t.Setenv("HERDER_ADDENDUM_SETTLE_MS", "0")
}

type noLaunchHerdr struct {
	startCalls int
}

func (f *noLaunchHerdr) Combined(args ...string) ([]byte, int, error) {
	if len(args) >= 2 && args[0] == "agent" && args[1] == "start" {
		f.startCalls++
	}
	return []byte(`{"result":{"workspaces":[]}}`), 0, nil
}

func (f *noLaunchHerdr) Output(args ...string) ([]byte, error) {
	return []byte(`{"result":{"agents":[]}}`), nil
}

func TestResumeMissingWorkingDirectoryRefusesBeforeLaunch(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "removed-worktree")
	row := strings.ReplaceAll(`{"guid":"guid-missing","short_guid":"missing","label":"missing","role":"worker","agent":"claude","terminal_id":"term_old","pane_id":"p_old","hcom_dir":"/hcom","status":"closed","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-missing","tag":"worker","cwd":"<CWD>","workspace_id":"ws_old","ts":"2026-07-08T00:00:00Z"}}
`, "<CWD>", missing)
	if err := os.WriteFile(filepath.Join(dir, "registry.jsonl"), []byte(row), 0o644); err != nil {
		t.Fatal(err)
	}
	configureLifecycleTest(t, dir)
	client := &noLaunchHerdr{}
	var stdout, stderr strings.Builder
	rc := (&runner{stdout: &stdout, stderr: &stderr, herdr: client}).resume(resumeOptions{target: "missing"})
	if rc == 0 {
		t.Fatalf("resume rc = 0, want refusal\nstderr:\n%s", stderr.String())
	}
	if client.startCalls != 0 {
		t.Fatalf("agent start called %d times, want zero", client.startCalls)
	}
	for _, want := range []string{"[cwd_unavailable]", missing, "pass --cwd", "recreate the removed worktree"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr missing %q: %s", want, stderr.String())
		}
	}
	var typed *cwdUnavailableError
	if err := preflightCWD("resume", missing); !errors.As(err, &typed) {
		t.Fatalf("preflightCWD() error = %T %v, want *cwdUnavailableError", err, err)
	}
}

type vanishedPaneHerdr struct{}

func (vanishedPaneHerdr) Combined(args ...string) ([]byte, int, error) {
	if len(args) >= 2 && args[0] == "agent" && args[1] == "start" {
		return []byte(`{"result":{"agent":{"pane_id":"p_vanished","terminal_id":"term_vanished","workspace_id":"ws_work","cwd":"/work"}}}`), 0, nil
	}
	if len(args) >= 2 && args[0] == "pane" && args[1] == "get" {
		return []byte(`{"error":{"code":"pane_not_found","message":"pane was closed"}}`), 4, nil
	}
	return []byte(`{"result":{}}`), 0, nil
}

func (vanishedPaneHerdr) Output(args ...string) ([]byte, error) {
	return []byte(`{"result":{"agents":[]}}`), nil
}

func TestSettleFailureIncludesLaunchAndExitDiagnostics(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERDER_LIFECYCLE_SETTLE_MS", "1")
	var stdout, stderr strings.Builder
	r := &runner{stdout: &stdout, stderr: &stderr, herdr: vanishedPaneHerdr{}}
	_, code := r.startAndAppend(startSpec{
		Mode:          "resume",
		GUID:          "guid-diag",
		Short:         "diag",
		Label:         "diagnostic-worker",
		Role:          "worker",
		Agent:         "codex",
		HcomDir:       "/hcom",
		VehicleTarget: "session-diag",
		RegistryPath:  filepath.Join(dir, "registry.jsonl"),
		BaseRaw:       []byte(`{}`),
		CWD:           dir,
		Workspace:     "ws_work",
		Split:         "down",
	})
	if code == 0 {
		t.Fatalf("startAndAppend() code = 0, want failure\nstderr:\n%s", stderr.String())
	}
	for _, want := range []string{"seat completion refused [live_seat_missing]", "pane lookup exited 4", "pane was closed", "launched pane cleanup confirmed"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr missing %q: %s", want, stderr.String())
		}
	}
}

func TestNewTabMoveArgsCarryResolvedFocus(t *testing.T) {
	for _, tc := range []struct {
		name      string
		focusFlag string
		wantFocus string
	}{
		{name: "default", wantFocus: "--no-focus"},
		{name: "explicit focus", focusFlag: "--focus", wantFocus: "--focus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(newTabMoveArgs("pane-1", "worker", tc.focusFlag), " ")
			want := "pane move pane-1 --new-tab " + tc.wantFocus + " --label worker"
			if got != want {
				t.Fatalf("move args = %q, want %q", got, want)
			}
		})
	}
}

func TestResolveTargetWithArchiveFallbackSkipsArchivesOnLiveHit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.jsonl")
	archDir := filepath.Join(dir, "registry.jsonl.archive")
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-target", filepath.Join(archDir, "0002-rotation.jsonl")); err != nil {
		t.Fatal(err)
	}
	guid := "guid-live-0000"
	label := "live"
	live := []registry.Record{{GUID: &guid, Label: &label, Status: "active"}}

	recs, rec, err := resolveTargetWithArchiveFallback(live, path, "live")
	if err != nil {
		t.Fatalf("live hit consulted broken archive: %v", err)
	}
	if rec == nil || rec.GUID == nil || *rec.GUID != guid {
		t.Fatalf("rec = %+v, want live row", rec)
	}
	if len(recs) != 1 {
		t.Fatalf("records = %d, want live-only set", len(recs))
	}

	if _, _, err := resolveTargetWithArchiveFallback(live, path, "missing"); err == nil {
		t.Fatal("live miss did not consult broken archive")
	}
}

func TestResumeRefusesRetiredSession(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.jsonl")
	outcomes, err := registry.UpdateLocked(registryPath, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{
			Kind:       v2.KindSession,
			GUID:       "guid-retired",
			Event:      "retired",
			RecordedAt: "2026-07-08T00:00:00Z",
			State:      v2.StateRetired,
			Label:      "old",
			Tool:       "codex",
			SIDs:       []v2.SID{{SID: "session-retired", Source: "harvest"}},
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			t.Fatal(err)
		}
	}
	mockBin := t.TempDir()
	for _, tool := range []string{"herdr", "hcom"} {
		if err := os.WriteFile(filepath.Join(mockBin, tool), []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", mockBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDER_STATE_DIR", dir)

	var stdout, stderr strings.Builder
	rc := (&runner{stdout: &stdout, stderr: &stderr, herdr: fakeHerdrClient{}}).resume(resumeOptions{target: "old"})
	if rc == 0 {
		t.Fatalf("resume rc = 0, want refusal\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "session is retired") || !strings.Contains(stderr.String(), "herder reopen") {
		t.Fatalf("stderr = %q, want reopen guidance", stderr.String())
	}
}

func TestResumeAllowsLegacyClosedSession(t *testing.T) {
	dir := t.TempDir()
	row := strings.ReplaceAll(`{"guid":"guid-legacy","short_guid":"legacy","label":"legacy","role":"worker","agent":"claude","terminal_id":"term_old","pane_id":"p_old","hcom_dir":"/hcom","status":"closed","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sess-legacy","tag":"worker","cwd":"<CWD>","workspace_id":"ws_1","ts":"2026-07-08T00:00:00Z"}}
`, "<CWD>", dir)
	if err := os.WriteFile(filepath.Join(dir, "registry.jsonl"), []byte(row), 0o644); err != nil {
		t.Fatal(err)
	}
	mockBin := t.TempDir()
	for _, tool := range []string{"herdr", "hcom"} {
		if err := os.WriteFile(filepath.Join(mockBin, tool), []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", mockBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "p_self")
	t.Setenv("HERDER_STATE_DIR", dir)
	t.Setenv("HERDER_LIFECYCLE_SETTLE_MS", "0")
	t.Setenv("HERDER_ADDENDUM_SETTLE_MS", "0")

	var stdout, stderr strings.Builder
	rc := (&runner{stdout: &stdout, stderr: &stderr, herdr: fakeHerdrClient{}, completion: lifecycleCompletionFor("legacy-bus", "sess-legacy", "p_resumed")}).resume(resumeOptions{target: "legacy"})
	if rc != 0 {
		t.Fatalf("resume legacy closed rc = %d, want 0\nstdout:\n%s\nstderr:\n%s", rc, stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "session is retired") {
		t.Fatalf("stderr = %q, want no retired refusal for legacy closed row", stderr.String())
	}
}

func TestResumeTargetSIDWinsAfterPriorProvenanceCarry(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.jsonl")
	rows := strings.ReplaceAll(`{"guid":"guid-resume-carry","short_guid":"resume","label":"resume-carry","role":"worker","agent":"claude","status":"closed","provenance":{"mechanism":"spawn","spawned_by":"user","tool_session_id":"sid-target","cwd":"<CWD>","ts":"2026-07-08T00:00:00Z"}}
{"guid":"guid-resume-carry","short_guid":"resume","label":"resume-carry","role":"worker","agent":"claude","status":"closed","provenance":{"mechanism":"spawn","spawned_by":"user","cwd":"<CWD>","ts":"2026-07-08T00:01:00Z"}}
`, "<CWD>", dir)
	if err := os.WriteFile(registryPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	configureLifecycleTest(t, dir)
	t.Setenv("HCOM_SESSION_ID", "sid-caller")

	var stdout, stderr strings.Builder
	rc := (&runner{stdout: &stdout, stderr: &stderr, herdr: fakeHerdrClient{}, completion: lifecycleCompletionFor("target-bus", "sid-target", "p_resumed")}).resume(resumeOptions{target: "resume-carry"})
	if rc != 0 {
		t.Fatalf("resume rc = %d, want 0\nstdout:\n%s\nstderr:\n%s", rc, stdout.String(), stderr.String())
	}
	projection, err := v2.LoadFile(registryPath, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := registry.V2ByGUID(projection, "guid-resume-carry")
	if got == nil || got.Provenance.ToolSessionID != "sid-target" {
		t.Fatalf("resumed provenance = %+v, want target SID after prior-provenance carry", got)
	}
	if len(got.SIDs) != 1 || got.SIDs[0].SID != "sid-target" || got.Continuity != "confirmed" {
		t.Fatalf("resumed identity evidence = sids %+v, continuity %q; want explicit target SID confirmed", got.SIDs, got.Continuity)
	}
	if len(got.Bindings) != 2 || got.Bindings[0].Field != v2.BindingFieldSeat || got.Bindings[1].Field != v2.BindingFieldHcomName {
		t.Fatalf("resumed binding history = %+v, want seat and bus facts from shared completion", got.Bindings)
	}
}

type fakeHerdrClient struct{}

func (fakeHerdrClient) Combined(args ...string) ([]byte, int, error) {
	if len(args) >= 2 && args[0] == "agent" && args[1] == "start" {
		return []byte(`{"result":{"type":"agent_started","agent":{"pane_id":"p_resumed","terminal_id":"term_resumed","workspace_id":"ws_resumed","cwd":"/repo"}}}`), 0, nil
	}
	if len(args) >= 3 && args[0] == "pane" && args[1] == "get" {
		return []byte(`{"result":{"pane":{"pane_id":"p_resumed","terminal_id":"term_resumed"}}}`), 0, nil
	}
	return []byte(`{"result":{"type":"ok"}}`), 0, nil
}

func (fakeHerdrClient) Output(args ...string) ([]byte, error) {
	return []byte(`{"result":{"agents":[]}}`), nil
}

type liveLifecycleFailureHerdr struct {
	closeCalls int
}

func (f *liveLifecycleFailureHerdr) Combined(args ...string) ([]byte, int, error) {
	if len(args) >= 2 && args[0] == "agent" && args[1] == "start" {
		return []byte(`{"result":{"type":"agent_started","agent":{"pane_id":"p_live","terminal_id":"term_live","workspace_id":"ws_live","cwd":"/repo"}}}`), 0, nil
	}
	if len(args) >= 2 && args[0] == "pane" && args[1] == "get" {
		return []byte(`{"result":{"pane":{"pane_id":"p_live","terminal_id":"term_live"}}}`), 0, nil
	}
	if len(args) >= 2 && args[0] == "pane" && args[1] == "close" {
		f.closeCalls++
		return []byte(`{"result":{"type":"pane_closed"}}`), 0, nil
	}
	if len(args) >= 2 && args[0] == "pane" && args[1] == "list" {
		return []byte(`{"result":{"panes":[]}}`), 0, nil
	}
	return []byte(`{"result":{"type":"ok"}}`), 0, nil
}

func (f *liveLifecycleFailureHerdr) Output(args ...string) ([]byte, error) {
	return []byte(`{"result":{"agents":[]}}`), nil
}

func TestLifecycleIncompleteCompletionPreservesLiveChild(t *testing.T) {
	for _, tt := range []struct {
		name   string
		update func(string, registry.LockedUpdateFunc) ([]registry.WriteOutcome, error)
		want   string
	}{
		{
			name: "registry lock failure",
			update: func(string, registry.LockedUpdateFunc) ([]registry.WriteOutcome, error) {
				return nil, errors.New("lock refused")
			},
			want: "seat completion failed: lock refused",
		},
		{
			name: "registry noop",
			update: func(string, registry.LockedUpdateFunc) ([]registry.WriteOutcome, error) {
				return []registry.WriteOutcome{{Status: registry.WriteNoop}}, nil
			},
			want: "seat completion wrote no registry row",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			state := t.TempDir()
			configureLifecycleTest(t, state)
			client := &liveLifecycleFailureHerdr{}
			engine := seatcompletion.DefaultEngine()
			engine.UpdateRegistry = tt.update
			var stderr strings.Builder
			_, code := (&runner{stderr: &stderr, herdr: client, completion: &engine}).startAndAppend(startSpec{
				Mode:         "resume",
				GUID:         "guid-live-failure",
				Short:        "live",
				Label:        "live-worker",
				Role:         "worker",
				Agent:        "bash",
				RegistryPath: filepath.Join(state, "registry.jsonl"),
				BaseRaw:      []byte(`{}`),
				CWD:          state,
			})
			if code != 1 {
				t.Fatalf("startAndAppend() code = %d, want 1", code)
			}
			if client.closeCalls != 0 {
				t.Fatalf("live child close calls = %d, want 0; stderr=%s", client.closeCalls, stderr.String())
			}
			for _, want := range []string{tt.want, "launched child is live", "preserved without a registry row"} {
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("stderr = %q, want %q", stderr.String(), want)
				}
			}
			if strings.Contains(stderr.String(), "invalid registry row") {
				t.Fatalf("noop reached registry-row unmarshal: %s", stderr.String())
			}
		})
	}
}

func TestGrokLifecycleCompletionFailureNamesOnlyRealRecoveryOwner(t *testing.T) {
	for _, tt := range []struct {
		mode    string
		want    string
		notWant string
	}{
		{mode: "fork", want: "bridge supervisor will retry", notWant: "sidecar will retry"},
		{mode: "resume", want: "No automatic resume completion owner is armed", notWant: "will retry this same completion step"},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			var stderr strings.Builder
			r := &runner{stderr: &stderr, herdr: &liveLifecycleFailureHerdr{}}
			r.handleLifecycleCompletionFailure("seat completion refused", "p_live", "term_live", "grok", tt.mode, false)
			got := stderr.String()
			if !strings.Contains(got, tt.want) || strings.Contains(got, tt.notWant) {
				t.Fatalf("Grok %s recovery text=%q, want %q and not %q", tt.mode, got, tt.want, tt.notWant)
			}
		})
	}
}

func TestLifecycleInvalidAppliedRowPreservesLiveChild(t *testing.T) {
	state := t.TempDir()
	configureLifecycleTest(t, state)
	client := &liveLifecycleFailureHerdr{}
	engine := seatcompletion.DefaultEngine()
	engine.UpdateRegistry = func(string, registry.LockedUpdateFunc) ([]registry.WriteOutcome, error) {
		return []registry.WriteOutcome{{Status: registry.WriteApplied, Row: []byte(`{`)}}, nil
	}
	var stderr strings.Builder
	_, code := (&runner{stderr: &stderr, herdr: client, completion: &engine}).startAndAppend(startSpec{
		Mode:         "fork",
		GUID:         "guid-invalid-applied-row",
		Short:        "invalid",
		Label:        "invalid-worker",
		Role:         "worker",
		Agent:        "bash",
		RegistryPath: filepath.Join(state, "registry.jsonl"),
		BaseRaw:      []byte(`{}`),
		CWD:          state,
	})
	if code != 1 {
		t.Fatalf("startAndAppend() code = %d, want 1", code)
	}
	if client.closeCalls != 0 {
		t.Fatalf("live child close calls = %d, want 0; stderr=%s", client.closeCalls, stderr.String())
	}
	for _, want := range []string{"invalid registry row", "launched child is live", "registry completion was already applied"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

type lifecycleBridge struct {
	b      *grokbridge.Binder
	cancel context.CancelFunc
	done   chan error
}

func startLifecycleBridge(t *testing.T, state, seat, sessionID, busName string) *lifecycleBridge {
	t.Helper()
	hcom := filepath.Join(t.TempDir(), "hcom")
	body := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = start ]; then printf '%%s\\n' '[hcom:%[1]s]'; exit 0; fi\nif [ \"$1\" = list ]; then printf '%%s\\n' '{\"name\":\"%[1]s\"}'; exit 0; fi\ncase \" $* \" in *' --wait '*) exec sleep 60;; esac\nexit 0\n", busName)
	if err := os.WriteFile(hcom, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := grokbridge.OpenBinder(grokbridge.BinderConfig{Seat: seat, StateDir: state, HcomBin: hcom, SessionID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Serve(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if st, statErr := os.Stat(grokbridge.SocketPath(state, seat)); statErr == nil && st.Mode()&os.ModeSocket != 0 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			b.Close()
			t.Fatal("Grok bridge socket did not become ready")
		}
		time.Sleep(time.Millisecond)
	}
	return &lifecycleBridge{b: b, cancel: cancel, done: done}
}

func (b *lifecycleBridge) close() {
	b.cancel()
	select {
	case <-b.done:
	case <-time.After(time.Second):
	}
	_ = b.b.Close()
}

func seedPendingJournal(t *testing.T, state, seat string, id int64) {
	t.Helper()
	j, err := grokbridge.OpenJournal(filepath.Join(grokbridge.SeatDir(state, seat), "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.AdvanceGeneration(); err != nil {
		t.Fatal(err)
	}
	event := map[string]any{
		"id": id, "type": "message", "ts": "2026-07-13T00:00:00Z",
		"data": map[string]any{"from": "peer", "text": "pending", "intent": "request", "delivered_to": []string{"seat"}},
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if _, added, err := j.Queue(raw); err != nil || !added {
		t.Fatalf("seed pending added=%v err=%v", added, err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
}

type grokLifecycleHerdr struct {
	mode       string
	parentSID  string
	childSID   string
	childGUID  string
	paneID     string
	terminalID string
	onStart    func(guid, sid string) error
	startErr   error
	startCmd   string
}

var lifecycleGUIDRE = regexp.MustCompile(`HERDER_GUID=([0-9a-f-]{36})`)
var lifecycleSIDRE = regexp.MustCompile(`HERDER_GROK_SESSION_ID=([0-9a-f-]{36})`)

func (f *grokLifecycleHerdr) Combined(args ...string) ([]byte, int, error) {
	if len(args) >= 2 && args[0] == "workspace" && args[1] == "list" {
		return []byte(`{"result":{"workspaces":[]}}`), 0, nil
	}
	if len(args) >= 2 && args[0] == "agent" && args[1] == "start" {
		f.startCmd = strings.Join(args, " ")
		if match := lifecycleGUIDRE.FindStringSubmatch(f.startCmd); len(match) == 2 {
			f.childGUID = match[1]
		}
		if match := lifecycleSIDRE.FindStringSubmatch(f.startCmd); len(match) == 2 {
			f.childSID = match[1]
		}
		if f.onStart != nil {
			f.startErr = f.onStart(f.childGUID, f.childSID)
			if f.startErr != nil {
				return []byte(`{"error":{"message":"fixture start failed"}}`), 1, f.startErr
			}
		}
		return []byte(fmt.Sprintf(`{"result":{"agent":{"pane_id":%q,"terminal_id":%q,"cwd":%q}}}`, f.paneID, f.terminalID, os.TempDir())), 0, nil
	}
	if len(args) >= 3 && args[0] == "pane" && args[1] == "get" {
		return []byte(fmt.Sprintf(`{"result":{"pane":{"pane_id":%q,"terminal_id":%q,"cwd":%q}}}`, args[2], f.terminalID, os.TempDir())), 0, nil
	}
	if len(args) >= 3 && args[0] == "pane" && args[1] == "process_info" {
		argv := []string{"/pinned/grok-linux-x86_64", "--no-subagents"}
		if f.mode == "resume" {
			argv = append(argv, "--resume", f.childSID)
		} else {
			argv = append(argv, "--resume", f.parentSID, "--fork-session", "--session-id", f.childSID)
		}
		payload, _ := json.Marshal(map[string]any{"result": map[string]any{"process_info": map[string]any{"foreground_processes": []any{map[string]any{"pid": 4242, "argv": argv}}}}})
		return payload, 0, nil
	}
	if len(args) >= 2 && args[0] == "pane" && args[1] == "close" {
		return []byte(`{"result":{"type":"pane_closed"}}`), 0, nil
	}
	return []byte(`{"result":{}}`), 0, nil
}

func (f *grokLifecycleHerdr) Output(args ...string) ([]byte, error) {
	return []byte(`{"result":{"agents":[]}}`), nil
}

func appendGrokSession(t *testing.T, path, guid, label, sid string) {
	t.Helper()
	outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{
			GUID: guid, Event: "registered", State: v2.StateUnseated, Label: label, Role: "worker", Tool: "grok",
			SIDs: []v2.SID{{SID: sid, Source: "harvest"}}, Continuity: "confirmed",
			Provenance: v2.Provenance{Mechanism: "spawn", ToolSessionID: sid, CWD: os.TempDir()},
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			t.Fatal(err)
		}
	}
}

func shortLifecycleState(t *testing.T) string {
	t.Helper()
	state, err := os.MkdirTemp("/tmp", "grok-life-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(state) })
	return state
}

func TestT14GrokResumeKeepsSeatSessionSpoolAndBusIdentity(t *testing.T) {
	state := shortLifecycleState(t)
	ownerHome := filepath.Join(state, "owner-home")
	t.Setenv("HOME", ownerHome)
	registryPath := filepath.Join(state, "registry.jsonl")
	guid, err := registry.NewGUID()
	if err != nil {
		t.Fatal(err)
	}
	sid, err := launchcmd.NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	appendGrokSession(t, registryPath, guid, "resume-grok", sid)
	if err := os.MkdirAll(filepath.Join(ownerHome, ".grok", "sessions", "%2Fresume", sid), 0o700); err != nil {
		t.Fatal(err)
	}
	seedPendingJournal(t, state, guid, 14)
	bridge := startLifecycleBridge(t, state, guid, sid, "resume-bus")
	defer bridge.close()
	busBefore, err := os.ReadFile(filepath.Join(state, "grok", guid, "bus-name"))
	if err != nil {
		t.Fatal(err)
	}

	configureLifecycleTest(t, state)
	t.Setenv("HERDER_LIFECYCLE_SETTLE_MS", "1500")
	fake := &grokLifecycleHerdr{mode: "resume", childSID: sid, paneID: "pane-resume", terminalID: "term-resume"}
	var stdout, stderr strings.Builder
	rc := (&runner{stdout: &stdout, stderr: &stderr, herdr: fake, completion: lifecycleCompletionFor("resume-bus", sid, "pane-resume")}).resume(resumeOptions{target: guid, cwd: os.TempDir()})
	if rc != 0 {
		t.Fatalf("resume rc=%d stdout=%s stderr=%s", rc, stdout.String(), stderr.String())
	}
	proj, err := v2.LoadFile(registryPath, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := registry.V2ByGUID(proj, guid)
	if got == nil || got.State != v2.StateSeated || got.Seat == nil || got.Seat.PID != 0 || got.Seat.HcomName != "resume-bus" || got.Capabilities == nil || got.Capabilities.BinderPID <= 0 || got.Capabilities.Pending != 1 || got.Capabilities.Wake != "degraded" || got.Provenance.ToolSessionID != sid {
		t.Fatalf("resumed row=%+v", got)
	}
	if len(got.SIDs) != 1 || got.SIDs[0].SID != sid {
		t.Fatalf("resume sids=%+v", got.SIDs)
	}
	busAfter, err := os.ReadFile(filepath.Join(state, "grok", guid, "bus-name"))
	if err != nil || string(busAfter) != string(busBefore) {
		t.Fatalf("bus before=%q after=%q err=%v", busBefore, busAfter, err)
	}
	status, err := grokBridgeCall(state, guid, sid, "status")
	if err != nil || status.Status == nil || status.Status.Pending != 1 {
		t.Fatalf("resumed bridge status=%+v err=%v", status.Status, err)
	}
}

func TestT15GrokForkGetsFreshSeatSpoolNameAndLineage(t *testing.T) {
	state := shortLifecycleState(t)
	ownerHome := filepath.Join(state, "owner-home")
	t.Setenv("HOME", ownerHome)
	registryPath := filepath.Join(state, "registry.jsonl")
	parentGUID, err := registry.NewGUID()
	if err != nil {
		t.Fatal(err)
	}
	parentSID, err := launchcmd.NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	appendGrokSession(t, registryPath, parentGUID, "parent-grok", parentSID)
	parentDir := filepath.Join(ownerHome, ".grok", "sessions", "%2Fparent", parentSID)
	if err := os.MkdirAll(parentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	parentHistory := filepath.Join(parentDir, "chat_history.jsonl")
	if err := os.WriteFile(parentHistory, []byte("parent-stays\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	seedPendingJournal(t, state, parentGUID, 15)
	parentBridge := startLifecycleBridge(t, state, parentGUID, parentSID, "parent-bus")
	defer parentBridge.close()

	configureLifecycleTest(t, state)
	t.Setenv("HERDER_LIFECYCLE_SETTLE_MS", "1500")
	fake := &grokLifecycleHerdr{mode: "fork", parentSID: parentSID, paneID: "pane-child", terminalID: "term-child"}
	var childBridge *lifecycleBridge
	fake.onStart = func(guid, sid string) error {
		if guid == "" || sid == "" {
			return errors.New("missing preassigned child identity")
		}
		if err := os.MkdirAll(filepath.Join(ownerHome, ".grok", "sessions", "%2Fchild", sid), 0o700); err != nil {
			return err
		}
		childBridge = startLifecycleBridge(t, state, guid, sid, "child-bus")
		return nil
	}
	defer func() {
		if childBridge != nil {
			childBridge.close()
		}
	}()
	var stdout, stderr strings.Builder
	completion := lifecycleCompletionFor("child-bus", "", "pane-child")
	completion.ListBus = func(context.Context, string) ([]hcomidentity.Row, error) {
		joined := true
		return []hcomidentity.Row{{Name: "child-bus", SessionID: fake.childSID, Joined: &joined, LaunchContext: hcomidentity.LaunchContext{PaneID: "pane-child"}}}, nil
	}
	rc := (&runner{stdout: &stdout, stderr: &stderr, herdr: fake, completion: completion}).fork(forkOptions{target: parentGUID, label: "child-grok", cwd: os.TempDir()})
	if rc != 0 {
		t.Fatalf("fork rc=%d stdout=%s stderr=%s startErr=%v", rc, stdout.String(), stderr.String(), fake.startErr)
	}
	if fake.childGUID == "" || fake.childSID == "" || fake.childGUID == parentGUID || fake.childSID == parentSID {
		t.Fatalf("child guid=%q sid=%q parent guid=%q sid=%q", fake.childGUID, fake.childSID, parentGUID, parentSID)
	}
	proj, err := v2.LoadFile(registryPath, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	parent := registry.V2ByGUID(proj, parentGUID)
	child := registry.V2ByGUID(proj, fake.childGUID)
	if parent == nil || parent.State != v2.StateUnseated || len(parent.SIDs) != 1 || parent.SIDs[0].SID != parentSID {
		t.Fatalf("parent row=%+v", parent)
	}
	if child == nil || child.Lineage.ForkedFrom != parentGUID || child.Provenance.ToolSessionID != fake.childSID || child.Seat == nil || child.Seat.HcomName != "child-bus" || child.Capabilities == nil || child.Capabilities.Pending != 0 {
		t.Fatalf("child row=%+v", child)
	}
	if child.Seat.HcomName == "parent-bus" || grokbridge.SeatDir(state, fake.childGUID) == grokbridge.SeatDir(state, parentGUID) {
		t.Fatal("fork reused parent bus name or spool path")
	}
	parentStatus, err := grokBridgeCall(state, parentGUID, parentSID, "status")
	if err != nil || parentStatus.Status == nil || parentStatus.Status.Pending != 1 {
		t.Fatalf("parent pending status=%+v err=%v", parentStatus.Status, err)
	}
	if got, err := os.ReadFile(parentHistory); err != nil || string(got) != "parent-stays\n" {
		t.Fatalf("parent history=%q err=%v", got, err)
	}
}

func TestT16GrokLifecycleProcessEvidenceRejectsSubagentAndForeignIdentity(t *testing.T) {
	parent, err := launchcmd.NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := launchcmd.NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := launchcmd.NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	spec := startSpec{Mode: "fork", Agent: "grok", ParentSession: parent, GrokSessionID: owner}
	cases := []struct {
		name string
		argv []string
	}{
		{name: "subagent boundary absent", argv: []string{"grok", "--resume", parent, "--fork-session", "--session-id", owner}},
		{name: "foreign child identity", argv: []string{"grok", "--no-subagents", "--resume", parent, "--fork-session", "--session-id", foreign}},
		{name: "synthetic subagent process", argv: []string{"grok", "--no-subagents", "--session-id", foreign}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if pid := matchingGrokProcess([]herdrcli.Process{{PID: 99, Argv: tc.argv}}, spec); pid != 0 {
				t.Fatalf("foreign/subagent process accepted with pid %d", pid)
			}
		})
	}
	valid := []string{"grok", "--no-subagents", "--resume", parent, "--fork-session", "--session-id", owner}
	if pid := matchingGrokProcess([]herdrcli.Process{{PID: 101, Argv: valid}}, spec); pid != 101 {
		t.Fatalf("owning process pid=%d, want 101", pid)
	}
}

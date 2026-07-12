package lifecyclecmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

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
	for _, want := range []string{"mode=resume", "agent=codex", "label=diagnostic-worker", "cwd=" + dir, "workspace=ws_work", "pane=p_vanished", "pane lookup exit=4", "pane was closed", "process exit code/signal unavailable"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr missing %q: %s", want, stderr.String())
		}
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
	rc := (&runner{stdout: &stdout, stderr: &stderr, herdr: fakeHerdrClient{}}).resume(resumeOptions{target: "legacy"})
	if rc != 0 {
		t.Fatalf("resume legacy closed rc = %d, want 0\nstdout:\n%s\nstderr:\n%s", rc, stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "session is retired") {
		t.Fatalf("stderr = %q, want no retired refusal for legacy closed row", stderr.String())
	}
}

type fakeHerdrClient struct{}

func (fakeHerdrClient) Combined(args ...string) ([]byte, int, error) {
	if len(args) >= 2 && args[0] == "agent" && args[1] == "start" {
		return []byte(`{"result":{"type":"agent_started","agent":{"pane_id":"p_resumed","terminal_id":"term_resumed","workspace_id":"ws_resumed","cwd":"/repo"}}}`), 0, nil
	}
	return []byte(`{"result":{"type":"ok"}}`), 0, nil
}

func (fakeHerdrClient) Output(args ...string) ([]byte, error) {
	return []byte(`{"result":{"agents":[]}}`), nil
}

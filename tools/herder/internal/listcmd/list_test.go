package listcmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
)

func TestJoinShowsExactMatchAndBothGapDirections(t *testing.T) {
	snapshot := herdrcli.Snapshot{
		Panes: []herdrcli.Pane{
			{PaneID: "pane-a", Agent: "codex", AgentStatus: "active", AgentSession: "session-a"},
			{PaneID: "pane-b", Agent: "claude", AgentStatus: "idle", AgentSession: "session-b"},
			{PaneID: "shell-only"},
		},
		Agents: []herdrcli.Agent{
			{PaneID: "pane-a", Name: "mavu", Agent: "codex", Status: "active"},
			{PaneID: "pane-b", Name: "zira", Agent: "claude", Status: "idle"},
		},
	}
	roster := []hcomidentity.Row{
		{Name: "mavu", Tool: "codex", Status: "listening", LaunchContext: hcomidentity.LaunchContext{PaneID: "pane-a"}},
		{Name: "vile", Tool: "claude", Status: "active", LaunchContext: hcomidentity.LaunchContext{PaneID: "missing-pane"}},
	}

	want := []Row{
		{Pane: "-", Agent: "vile", Tool: "claude", HerdrStatus: "-", BusStatus: "active", Gap: "no visible pane"},
		{Pane: "pane-a", Agent: "mavu", Tool: "codex", HerdrStatus: "active", BusStatus: "listening", Gap: "-"},
		{Pane: "pane-b", Agent: "zira", Tool: "claude", HerdrStatus: "idle", BusStatus: "-", Gap: "no bus row"},
	}
	got := Join(snapshot, roster)
	if len(got) != len(want) {
		t.Fatalf("Join rows = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Join row %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func seedStaleSnapshot(t *testing.T) (string, string, []byte, time.Time) {
	t.Helper()
	state := t.TempDir()
	t.Setenv("HERDER_STATE_DIR", state)
	s := agentstore.Open(state, nil)
	at := time.Date(2026, 9, 9, 5, 0, 0, 0, time.UTC)
	if _, err := s.Append(agentstore.Event{ID: agentstore.NewID(at), At: at, Kind: agentstore.KindLaunchReady, By: "ziru", Name: "mavu", Pane: "p1"}); err != nil {
		t.Fatal(err)
	}
	if proj, err := s.Load(); err != nil || proj.SnapshotErr != nil {
		t.Fatalf("seed snapshot: proj=%+v err=%v", proj, err)
	}
	path := s.SnapshotPath()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(agentstore.Event{ID: agentstore.NewID(at.Add(time.Second)), At: at.Add(time.Second), Kind: agentstore.KindReparent, By: "ziru", Name: "mavu", Manager: "vara"}); err != nil {
		t.Fatal(err)
	}
	return state, path, before, info.ModTime()
}

func TestListFoldsSnapshotTailWithoutRewritingSnapshot(t *testing.T) {
	_, snapshotPath, before, beforeModTime := seedStaleSnapshot(t)
	deps := dependencies{
		snapshot: func() (herdrcli.Snapshot, error) {
			return herdrcli.Snapshot{Panes: []herdrcli.Pane{{PaneID: "p1"}}, Agents: []herdrcli.Agent{{PaneID: "p1", Name: "mavu", Agent: "claude"}}}, nil
		},
		roster: func() ([]hcomidentity.Row, error) {
			return []hcomidentity.Row{{Name: "mavu", Tool: "claude", Status: "listening", CreatedAt: time.Date(2026, 9, 9, 4, 59, 0, 0, time.UTC), LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"}}}, nil
		},
		store: liveDependencies.store,
	}
	var out, errBuf bytes.Buffer
	if code := run(nil, &out, &errBuf, deps); code != 0 || errBuf.Len() != 0 || !strings.Contains(lineFor(out.String(), "mavu"), "vara") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errBuf.String())
	}
	after, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) || !info.ModTime().Equal(beforeModTime) {
		t.Fatalf("list rewrote snapshot: content_equal=%t mtime_before=%s mtime_after=%s", bytes.Equal(after, before), beforeModTime, info.ModTime())
	}
}

func TestListReadsCorrectProjectionWhenStateDirectoryIsReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	state, _, _, _ := seedStaleSnapshot(t)
	agentsDir := filepath.Join(state, "agents")
	if err := os.Chmod(state, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(agentsDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(state, 0o700)
		_ = os.Chmod(agentsDir, 0o700)
	})
	deps := dependencies{
		snapshot: func() (herdrcli.Snapshot, error) {
			return herdrcli.Snapshot{Panes: []herdrcli.Pane{{PaneID: "p1"}}, Agents: []herdrcli.Agent{{PaneID: "p1", Name: "mavu", Agent: "claude"}}}, nil
		},
		roster: func() ([]hcomidentity.Row, error) {
			return []hcomidentity.Row{{Name: "mavu", Tool: "claude", Status: "listening", CreatedAt: time.Date(2026, 9, 9, 4, 59, 0, 0, time.UTC), LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"}}}, nil
		},
		store: liveDependencies.store,
	}
	var out, errBuf bytes.Buffer
	if code := run(nil, &out, &errBuf, deps); code != 0 || errBuf.Len() != 0 || !strings.Contains(lineFor(out.String(), "mavu"), "vara") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errBuf.String())
	}
}

func TestJoinDoesNotInferPlacementFromMatchingName(t *testing.T) {
	snapshot := herdrcli.Snapshot{Agents: []herdrcli.Agent{{PaneID: "pane-live", Name: "same", Agent: "codex", Status: "active"}}}
	for name, paneID := range map[string]string{
		"missing pane coordinate": "",
		"stale pane coordinate":   "pane-stale",
	} {
		t.Run(name, func(t *testing.T) {
			roster := []hcomidentity.Row{{
				Name: "same", Tool: "codex", Status: "active",
				LaunchContext: hcomidentity.LaunchContext{PaneID: paneID},
			}}
			rows := Join(snapshot, roster)
			if len(rows) != 2 || rows[0].Gap != "no visible pane" || rows[1].Gap != "no bus row" {
				t.Fatalf("matching names erased placement gap: %#v", rows)
			}
		})
	}
}

func TestJoinDoesNotClaimPaneVisibilityFromAgentRow(t *testing.T) {
	rows := Join(herdrcli.Snapshot{
		Agents: []herdrcli.Agent{{PaneID: "pane-agent-only", Name: "mavu", Agent: "codex"}},
	}, nil)
	if len(rows) != 1 || rows[0].HerdrStatus != "-" {
		t.Fatalf("agent-only row claims pane visibility: %#v", rows)
	}
}

func TestRunReadsSocketSnapshotBeforeRosterAndPrintsTable(t *testing.T) {
	window := int64(258400)
	percent := 31.733746
	var calls []string
	deps := dependencies{
		snapshot: func() (herdrcli.Snapshot, error) {
			calls = append(calls, "snapshot")
			return herdrcli.Snapshot{Agents: []herdrcli.Agent{{PaneID: "p1", Name: "mavu", Agent: "codex", Status: "active"}}}, nil
		},
		roster: func() ([]hcomidentity.Row, error) {
			calls = append(calls, "roster")
			return []hcomidentity.Row{{Name: "mavu", Tool: "codex", Status: "listening", LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"}}}, nil
		},
		vitals: func(hcomidentity.Row) (claudesession.Vitals, string, time.Time, error) {
			return claudesession.Vitals{Model: "gpt-5.6-sol", ContextUsage: &claudesession.ContextUsage{UsedTokens: 82000, WindowTokens: &window, UsedPercent: &percent}}, "", time.Time{}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("run code = %d, stderr = %q", code, stderr.String())
	}
	if strings.Join(calls, ",") != "snapshot,roster" {
		t.Fatalf("calls = %v", calls)
	}
	for _, text := range []string{"PANE", "AGENT", "HERDR", "BUS", "MODEL", "CONTEXT", "p1", "mavu", "listening", "gpt-5.6-sol", "82k/258k 32%"} {
		if !strings.Contains(stdout.String(), text) {
			t.Errorf("output missing %q:\n%s", text, stdout.String())
		}
	}
}

func TestRunPrintsDashVitalsForGapRow(t *testing.T) {
	deps := dependencies{
		snapshot: func() (herdrcli.Snapshot, error) {
			return herdrcli.Snapshot{Agents: []herdrcli.Agent{{PaneID: "p1", Name: "pane-only", Agent: "claude"}}}, nil
		},
		roster: func() ([]hcomidentity.Row, error) { return nil, nil },
		vitals: func(hcomidentity.Row) (claudesession.Vitals, string, time.Time, error) {
			t.Fatal("vitals called for gap")
			return claudesession.Vitals{}, "", time.Time{}, nil
		},
	}
	var out, errBuf bytes.Buffer
	if code := run(nil, &out, &errBuf, deps); code != 0 {
		t.Fatalf("code=%d err=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "MODEL") || !strings.Contains(out.String(), "CONTEXT") {
		t.Fatalf("gap output = %q", out.String())
	}
}

func TestContextLabelMissingAndPartialUsage(t *testing.T) {
	tests := []struct {
		name  string
		usage *claudesession.ContextUsage
		want  string
	}{
		{"nil", nil, "-"},
		{"empty", &claudesession.ContextUsage{}, "-"},
		{"used only", &claudesession.ContextUsage{UsedTokens: 820}, "820/- -"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := contextLabel(tc.usage); got != tc.want {
				t.Fatalf("contextLabel() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRunReportsHerdrFailureWithoutReadingRoster(t *testing.T) {
	rosterCalled := false
	deps := dependencies{
		snapshot: func() (herdrcli.Snapshot, error) { return herdrcli.Snapshot{}, errors.New("socket refused") },
		roster: func() ([]hcomidentity.Row, error) {
			rosterCalled = true
			return nil, nil
		},
	}
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr, deps); code != 1 {
		t.Fatalf("run code = %d, want 1", code)
	}
	if rosterCalled {
		t.Fatal("roster read after herdr failure")
	}
	if !strings.Contains(stderr.String(), "cannot read live herdr snapshot: socket refused") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunReportsHcomFailure(t *testing.T) {
	deps := dependencies{
		snapshot: func() (herdrcli.Snapshot, error) { return herdrcli.Snapshot{}, nil },
		roster:   func() ([]hcomidentity.Row, error) { return nil, errors.New("bus unavailable") },
	}
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr, deps); code != 1 {
		t.Fatalf("run code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "cannot read live hcom roster: bus unavailable") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunHelpAndUnknownArgument(t *testing.T) {
	deps := dependencies{
		snapshot: func() (herdrcli.Snapshot, error) { t.Fatal("snapshot called"); return herdrcli.Snapshot{}, nil },
		roster:   func() ([]hcomidentity.Row, error) { t.Fatal("roster called"); return nil, nil },
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "join live herdr placement") {
		t.Fatalf("help: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--json"}, &stdout, &stderr, deps); code != 2 || !strings.Contains(stderr.String(), "unknown argument") {
		t.Fatalf("unknown: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunFoldsStoreColumnsAndPrintsUnregistered(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HERDER_STATE_DIR", state)
	s := agentstore.Open(state, nil)
	at := time.Date(2026, 9, 9, 5, 0, 0, 0, time.UTC)
	for _, e := range []agentstore.Event{
		{ID: agentstore.NewID(at), At: at, Kind: agentstore.KindLaunchReady, By: "ziru", ByKind: "agent", Name: "mavu", Pane: "p1"},
		{ID: agentstore.NewID(at), At: at, Kind: agentstore.KindAssign, By: "ziru", Name: "mavu", Mission: "fleet-refit"},
		{ID: agentstore.NewID(at), At: at, Kind: agentstore.KindReparent, By: "ziru", Name: "mavu", Manager: "vara"},
		{ID: agentstore.NewID(at), At: at, Kind: agentstore.KindMirrorReady, By: "riko", ByKind: "mirror", Name: "vile"},
		{ID: agentstore.NewID(at), At: at, Kind: agentstore.KindLaunchReady, By: "ziru", Name: "funa", Session: "claimed-session-1"},
	} {
		if _, err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	deps := dependencies{
		snapshot: func() (herdrcli.Snapshot, error) {
			return herdrcli.Snapshot{Panes: []herdrcli.Pane{{PaneID: "p1"}, {PaneID: "p9", Agent: "claude", AgentSession: "s9"}}, Agents: []herdrcli.Agent{{PaneID: "p1", Name: "mavu", Agent: "codex", Status: "active"}}}, nil
		},
		roster: func() ([]hcomidentity.Row, error) {
			return []hcomidentity.Row{
				{Name: "mavu", Tool: "codex", Status: "listening", CreatedAt: at.Add(-time.Minute), LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"}},
				{Name: "vile", Tool: "claude", Status: "active"},
				{Name: "funa", Tool: "codex", Status: "listening", SessionID: "roster-session-2"},
				{Name: "nobody", Tool: "codex", Status: "listening"},
			}, nil
		},
		store: liveDependencies.store,
	}
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr, deps); code != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	text := stdout.String()
	if !strings.Contains(text, "LAUNCHER") || !strings.Contains(text, "MANAGER") || strings.Contains(text, "MISSION") || !strings.Contains(text, "BINDING") {
		t.Fatalf("columns missing:\n%s", text)
	}
	for _, row := range []struct{ agent, launcher, manager string }{
		{"mavu", "ziru", "vara"},
		{"vile", "mirrored: riko", "riko"},
		{"funa", "ziru", "ziru"},
		{"nobody", "unregistered", "-"},
	} {
		line := lineFor(text, row.agent)
		fields := strings.Fields(line)
		if len(fields) < 7 || fields[5] != row.launcher && fields[5]+" "+fields[6] != row.launcher {
			t.Errorf("%s row = %q, want launcher %q manager %q", row.agent, line, row.launcher, row.manager)
			continue
		}
		if !strings.Contains(line, row.manager) {
			t.Errorf("%s row = %q, want manager %q", row.agent, line, row.manager)
		}
	}
	// The assign event is folded into Row.Mission but never printed (owner ruling 2026-09-09).
	if strings.Contains(lineFor(text, "mavu"), "fleet-refit") {
		t.Errorf("mission printed in list: %q", lineFor(text, "mavu"))
	}
	if line := lineFor(text, "p9"); !strings.Contains(line, "no bus row") || strings.Contains(line, "unregistered") {
		t.Errorf("pane without a bus row must not print unregistered: %q", line)
	}
	if line := lineFor(text, "funa"); !strings.Contains(line, "conflict claimed-≠roster-s") || !strings.Contains(line, "no visible pane") {
		t.Errorf("binding conflict must show without changing GAP: %q", line)
	}
	if line := lineFor(text, "mavu"); !strings.Contains(line, "  -  ") && !strings.Contains(line, "\t-\t") && strings.Contains(line, "conflict") {
		t.Errorf("mavu has no claim and must not show a binding: %q", line)
	}
}

func TestRunPrintsRowsWhenStoreImportCannotWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	state := t.TempDir()
	os.WriteFile(filepath.Join(state, "launch-edges.jsonl"), []byte(`{"name":"mavu","launcher":"web","time":"2026-09-08T22:21:01Z"}`+"\n"), 0o600)
	os.Chmod(state, 0o500)
	t.Cleanup(func() { os.Chmod(state, 0o700) })
	t.Setenv("HERDER_STATE_DIR", state)
	deps := dependencies{
		snapshot: func() (herdrcli.Snapshot, error) { return herdrcli.Snapshot{}, nil },
		roster: func() ([]hcomidentity.Row, error) {
			return []hcomidentity.Row{{Name: "mavu", Tool: "codex", Status: "active"}}, nil
		},
		store: liveDependencies.store,
	}
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(lineFor(stdout.String(), "mavu"), "unregistered") || strings.Count(stderr.String(), "\n") != 1 || !strings.Contains(stderr.String(), "unregistered") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunPrintsWhenAgentStoreDirectoryIsReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	state := t.TempDir()
	t.Setenv("HERDER_STATE_DIR", state)
	s := agentstore.Open(state, nil)
	at := time.Now().UTC()
	if _, err := s.Append(agentstore.Event{ID: agentstore.NewID(at), At: at, Kind: agentstore.KindLaunchReady, By: "ziru", Name: "mavu"}); err != nil {
		t.Fatal(err)
	}
	os.Chmod(filepath.Join(state, "agents"), 0o500)
	t.Cleanup(func() { os.Chmod(filepath.Join(state, "agents"), 0o700) })
	deps := dependencies{
		snapshot: func() (herdrcli.Snapshot, error) { return herdrcli.Snapshot{}, nil },
		roster: func() ([]hcomidentity.Row, error) {
			return []hcomidentity.Row{{Name: "mavu", Tool: "codex", Status: "active"}}, nil
		},
		store: liveDependencies.store,
	}
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr, deps); code != 0 || stderr.Len() != 0 || !strings.Contains(lineFor(stdout.String(), "mavu"), "ziru") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func lineFor(text, needle string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

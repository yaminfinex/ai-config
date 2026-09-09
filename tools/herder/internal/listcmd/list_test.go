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
	}
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("run code = %d, stderr = %q", code, stderr.String())
	}
	if strings.Join(calls, ",") != "snapshot,roster" {
		t.Fatalf("calls = %v", calls)
	}
	for _, text := range []string{"PANE", "AGENT", "HERDR", "BUS", "p1", "mavu", "listening"} {
		if !strings.Contains(stdout.String(), text) {
			t.Errorf("output missing %q:\n%s", text, stdout.String())
		}
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
	if !strings.Contains(text, "LAUNCHER") || !strings.Contains(text, "MANAGER") || !strings.Contains(text, "MISSION") || !strings.Contains(text, "BINDING") {
		t.Fatalf("columns missing:\n%s", text)
	}
	for _, row := range []struct{ agent, launcher, manager, mission string }{
		{"mavu", "ziru", "vara", "fleet-refit"},
		{"vile", "mirrored: riko", "riko", "-"},
		{"funa", "ziru", "ziru", "-"},
		{"nobody", "unregistered", "-", "-"},
	} {
		line := lineFor(text, row.agent)
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[5] != row.launcher && fields[5]+" "+fields[6] != row.launcher {
			t.Errorf("%s row = %q, want launcher %q manager %q mission %q", row.agent, line, row.launcher, row.manager, row.mission)
			continue
		}
		if !strings.Contains(line, row.manager) || !strings.Contains(line, row.mission) {
			t.Errorf("%s row = %q, want manager %q mission %q", row.agent, line, row.manager, row.mission)
		}
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
	if _, err := os.Stat(filepath.Join(state, "agents", "snapshot.json")); err != nil {
		t.Fatalf("list did not refresh the snapshot: %v", err)
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

func TestRunPrintsWhenSnapshotCannotBeWritten(t *testing.T) {
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

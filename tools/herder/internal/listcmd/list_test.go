package listcmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

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

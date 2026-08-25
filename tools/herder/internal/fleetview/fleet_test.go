package fleetview

import (
	"testing"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
)

func TestBuildPreservesHierarchyAndBothGapDirections(t *testing.T) {
	snapshot := herdrcli.Snapshot{
		Workspaces: []herdrcli.Workspace{{WorkspaceID: "w1", Label: "repo", TabCount: 1, PaneCount: 2}},
		Tabs:       []herdrcli.Tab{{TabID: "t1", WorkspaceID: "w1", Label: "agents", PaneCount: 2}},
		Panes: []herdrcli.Pane{
			{PaneID: "p1", WorkspaceID: "w1", TabID: "t1", Agent: "codex", AgentStatus: "working", AgentSession: "s1"},
			{PaneID: "p2", WorkspaceID: "w1", TabID: "t1", Agent: "claude", AgentStatus: "idle", AgentSession: "s2"},
			{PaneID: "p3", WorkspaceID: "w1", TabID: "t1"},
		},
		Agents: []herdrcli.Agent{{PaneID: "p1", Name: "dore", Agent: "codex", Status: "working"}, {PaneID: "p2", Name: "kumo", Agent: "claude", Status: "idle"}},
	}
	roster := []hcomidentity.Row{
		{Name: "dore", Tool: "codex", Status: "active", LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"}},
		{Name: "vile", Tool: "claude", Status: "listening", LaunchContext: hcomidentity.LaunchContext{PaneID: "missing"}},
	}
	board := Build(snapshot, roster)
	if len(board.Workspaces) != 1 || len(board.Workspaces[0].Tabs) != 1 || len(board.Workspaces[0].Tabs[0].Panes) != 3 {
		t.Fatalf("board hierarchy = %#v", board)
	}
	panes := board.Workspaces[0].Tabs[0].Panes
	if panes[0].Agent != "dore" || panes[0].Tool != "codex" || panes[0].Gap != "-" {
		t.Fatalf("joined pane = %#v", panes[0])
	}
	if panes[1].Agent != "kumo" || panes[1].Gap != "no bus row" {
		t.Fatalf("herdr-only pane = %#v", panes[1])
	}
	if panes[2].PaneID != "p3" || panes[2].Agent != "-" || panes[2].Gap != "-" {
		t.Fatalf("plain terminal pane was not preserved: %#v", panes[2])
	}
	if len(board.Unplaced) != 1 || board.Unplaced[0].Agent != "vile" || board.Unplaced[0].Gap != "no visible pane" {
		t.Fatalf("unplaced = %#v", board.Unplaced)
	}
}

func TestValidateSnapshotRejectsHierarchyGaps(t *testing.T) {
	for name, snapshot := range map[string]herdrcli.Snapshot{
		"missing tab":       {Panes: []herdrcli.Pane{{PaneID: "p1", WorkspaceID: "w1", TabID: "missing"}}},
		"missing workspace": {Tabs: []herdrcli.Tab{{TabID: "t1", WorkspaceID: "missing"}}},
		"missing pane":      {Agents: []herdrcli.Agent{{PaneID: "missing", Name: "dore"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateSnapshot(snapshot); err == nil {
				t.Fatal("ValidateSnapshot accepted inconsistent hierarchy")
			}
		})
	}
}

func TestValidateRosterRejectsDuplicatePaneClaims(t *testing.T) {
	roster := []hcomidentity.Row{
		{Name: "dore", LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"}},
		{Name: "kumo", LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"}},
	}
	if err := ValidateRoster(roster); err == nil {
		t.Fatal("ValidateRoster accepted duplicate pane claim")
	}
}

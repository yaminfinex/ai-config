package fleetview

import (
	"testing"
	"time"

	"ai-config/tools/herder/internal/agentstore"
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
			{PaneID: "p3", WorkspaceID: "w1", TabID: "t1", Label: "shell", CurrentCommand: "htop"},
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
	if panes[2].CurrentCommand != "htop" || panes[2].Label != "shell" {
		t.Fatalf("plain terminal process was not carried honestly: %#v", panes[2])
	}
	if len(board.Unplaced) != 1 || board.Unplaced[0].Agent != "vile" || board.Unplaced[0].Gap != "no visible pane" {
		t.Fatalf("unplaced = %#v", board.Unplaced)
	}
}

func TestBuildCarriesOnlySuppliedWorktreeParent(t *testing.T) {
	snapshot := herdrcli.Snapshot{Workspaces: []herdrcli.Workspace{
		{WorkspaceID: "root", Label: "repo"},
		{WorkspaceID: "linked", Label: "feature"},
		{WorkspaceID: "orphan", Label: "other"},
	}}
	board := Build(snapshot, nil, map[string]string{"linked": "root"})
	if board.Workspaces[0].WorktreeOf != "" || board.Workspaces[1].WorktreeOf != "root" || board.Workspaces[2].WorktreeOf != "" {
		t.Fatalf("worktree parents = %#v", board.Workspaces)
	}
}

func TestBuildDoesNotInferPlacementFromMatchingName(t *testing.T) {
	snapshot := herdrcli.Snapshot{
		Workspaces: []herdrcli.Workspace{{WorkspaceID: "w1", TabCount: 1, PaneCount: 1}},
		Tabs:       []herdrcli.Tab{{TabID: "t1", WorkspaceID: "w1", PaneCount: 1}},
		Panes:      []herdrcli.Pane{{PaneID: "p1", WorkspaceID: "w1", TabID: "t1", Agent: "codex", AgentStatus: "working", AgentSession: "s1"}},
		Agents:     []herdrcli.Agent{{PaneID: "p1", Name: "dore", Agent: "codex", Status: "working"}},
	}
	roster := []hcomidentity.Row{{
		Name: "dore", Tool: "codex", Status: "active",
		LaunchContext: hcomidentity.LaunchContext{PaneID: "different-pane"},
	}}

	board := Build(snapshot, roster)
	pane := board.Workspaces[0].Tabs[0].Panes[0]
	if pane.Agent != "dore" || pane.BusStatus != "-" || pane.Gap != "no bus row" {
		t.Fatalf("same-name herdr pane was incorrectly joined: %#v", pane)
	}
	if len(board.Unplaced) != 1 || board.Unplaced[0].Agent != "dore" || board.Unplaced[0].Pane != "-" || board.Unplaced[0].Gap != "no visible pane" {
		t.Fatalf("different-pane bus row was incorrectly placed: %#v", board.Unplaced)
	}
}

func TestBuildKeepsNativeHcomDoubleGapWithoutJoinEvidence(t *testing.T) {
	// Captured from the pane-less native hcom probe on 2026-08-28: the bus row
	// carried a real session and process but no pane claim, while Herdr exposed
	// the terminal without an agent_session. Its matching title is not evidence.
	snapshot := sessionSnapshot(herdrcli.Pane{PaneID: "w4R:p3", Label: "pjsbare-kole"})
	snapshot.Panes[0].AgentStatus = ""
	roster := []hcomidentity.Row{{
		Name: "pjsbare-kole", Tool: "claude", SessionID: "d84ed22c-8af1-4636-aa97-45f638e53ec4", Status: "active",
	}}

	board := Build(snapshot, roster)
	pane := board.Workspaces[0].Tabs[0].Panes[0]
	if pane.PaneID != "w4R:p3" || pane.Agent != "-" || pane.Tool != "-" || pane.Gap != "-" {
		t.Fatalf("unattributed terminal was assigned by display name: %#v", pane)
	}
	if len(board.Unplaced) != 1 || board.Unplaced[0].Agent != "pjsbare-kole" || board.Unplaced[0].Gap != "no visible pane" {
		t.Fatalf("pane-less bus row did not remain unplaced: %#v", board.Unplaced)
	}
}

func TestBuildHealsMissingPaneClaimFromUniqueToolAndSession(t *testing.T) {
	for name, claim := range map[string]string{"absent claim": "", "stale claim": "gone"} {
		t.Run(name, func(t *testing.T) {
			snapshot := sessionSnapshot(
				herdrcli.Pane{PaneID: "p1", Agent: "codex", AgentSession: "session-dore"},
			)
			roster := []hcomidentity.Row{{
				Name: "dore", Tool: "codex", SessionID: "session-dore", Status: "active",
				LaunchContext: hcomidentity.LaunchContext{PaneID: claim},
			}}

			board := Build(snapshot, roster)
			pane := board.Workspaces[0].Tabs[0].Panes[0]
			if pane.Agent != "dore" || pane.BusStatus != "active" || pane.Gap != "-" || len(board.Unplaced) != 0 {
				t.Fatalf("session-healed board = %#v", board)
			}
		})
	}
}

func TestBuildKeepsGapWhenSessionIdentityIsAmbiguous(t *testing.T) {
	t.Run("same session in multiple panes", func(t *testing.T) {
		snapshot := sessionSnapshot(
			herdrcli.Pane{PaneID: "p1", Agent: "codex", AgentSession: "shared"},
			herdrcli.Pane{PaneID: "p2", Agent: "codex", AgentSession: "shared"},
		)
		board := Build(snapshot, []hcomidentity.Row{{Name: "dore", Tool: "codex", SessionID: "shared"}})
		if len(board.Unplaced) != 1 || board.Unplaced[0].Agent != "dore" {
			t.Fatalf("ambiguous panes placed a row: %#v", board)
		}
	})
	t.Run("multiple rows match one pane", func(t *testing.T) {
		snapshot := sessionSnapshot(
			herdrcli.Pane{PaneID: "p1", Agent: "codex", AgentSession: "shared"},
		)
		roster := []hcomidentity.Row{
			{Name: "dore", Tool: "codex", SessionID: "shared"},
			{Name: "kumo", Tool: "codex", SessionID: "shared"},
		}
		board := Build(snapshot, roster)
		if len(board.Unplaced) != 2 || board.Workspaces[0].Tabs[0].Panes[0].BusStatus != "-" {
			t.Fatalf("ambiguous rows placed a row: %#v", board)
		}
	})
}

func TestBuildNeverUsesSessionFallbackForLivePaneClaim(t *testing.T) {
	snapshot := sessionSnapshot(
		herdrcli.Pane{PaneID: "p1", Agent: "codex", AgentSession: "session-dore"},
		herdrcli.Pane{PaneID: "p2"},
	)
	snapshot.Panes[1].AgentStatus = ""
	roster := []hcomidentity.Row{{
		Name: "dore", Tool: "codex", SessionID: "session-dore", Status: "active",
		LaunchContext: hcomidentity.LaunchContext{PaneID: "p2"},
	}}
	board := Build(snapshot, roster)
	claimed := board.Workspaces[0].Tabs[0].Panes[1]
	if len(board.Unplaced) != 0 || board.Workspaces[0].Tabs[0].Panes[0].BusStatus != "-" || claimed.Agent != "dore" || claimed.BusStatus != "active" || claimed.Gap != "pane not bound" {
		t.Fatalf("live pane claim used session fallback: %#v", board)
	}
}

func TestBuildLeavesTwoClaimsOnUnboundLivePaneUnplaced(t *testing.T) {
	snapshot := sessionSnapshot(herdrcli.Pane{PaneID: "p1"})
	snapshot.Panes[0].AgentStatus = ""
	roster := []hcomidentity.Row{
		{Name: "dore", Tool: "codex", Status: "active", LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"}},
		{Name: "kumo", Tool: "claude", Status: "listening", LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"}},
	}
	board := Build(snapshot, roster)
	pane := board.Workspaces[0].Tabs[0].Panes[0]
	if pane.Agent != "-" || pane.Gap != "-" {
		t.Fatalf("contested unbound pane gained a placement: %#v", pane)
	}
	if len(board.Unplaced) != 2 {
		t.Fatalf("unplaced=%#v", board.Unplaced)
	}
	for _, row := range board.Unplaced {
		if row.Pane != "-" || row.Gap != "no visible pane" {
			t.Fatalf("contested claimant gained pane evidence: %#v", row)
		}
	}
}

func TestBuildNestsOnlyExplicitUnambiguousSubagents(t *testing.T) {
	snapshot := sessionSnapshot(herdrcli.Pane{PaneID: "p1", Agent: "claude", AgentSession: "parent-session"})
	roster := []hcomidentity.Row{
		{Name: "probe-fame", BaseName: "fame", Tool: "claude", Status: "active", SessionID: "parent-session", LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"}},
		{Name: "probe-fame_general_purpose_1", BaseName: "fame_general_purpose_1", ParentName: "fame", AgentID: "a35b593a6be7a9ba5", Tool: "claude", Status: "active"},
	}
	board := Build(snapshot, roster)
	pane := board.Workspaces[0].Tabs[0].Panes[0]
	if len(board.Unplaced) != 0 || len(pane.Subagents) != 1 || pane.Subagents[0].Agent != "probe-fame_general_purpose_1" || pane.Subagents[0].ParentAgent != "probe-fame" {
		t.Fatalf("nested board = %#v", board)
	}
}

func TestBuildLeavesUnprovableSubagentsUnplaced(t *testing.T) {
	for name, roster := range map[string][]hcomidentity.Row{
		"missing parent": {
			{Name: "probe-child", BaseName: "child", ParentName: "missing", AgentID: "a35b593a6be7a9ba5", Tool: "claude"},
		},
		"ambiguous parent": {
			{Name: "probe-parent-one", BaseName: "parent", Tool: "claude"},
			{Name: "probe-parent-two", BaseName: "parent", Tool: "claude"},
			{Name: "probe-child", BaseName: "child", ParentName: "parent", AgentID: "a35b593a6be7a9ba5", Tool: "claude"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			board := Build(herdrcli.Snapshot{}, roster)
			found := false
			for _, row := range board.Unplaced {
				found = found || row.Agent == "probe-child"
			}
			if !found {
				t.Fatalf("child disappeared from honest gap: %#v", board)
			}
		})
	}
}

func sessionSnapshot(panes ...herdrcli.Pane) herdrcli.Snapshot {
	for i := range panes {
		panes[i].WorkspaceID = "w1"
		panes[i].TabID = "t1"
		panes[i].AgentStatus = first(panes[i].AgentStatus, "working")
	}
	return herdrcli.Snapshot{
		Workspaces: []herdrcli.Workspace{{WorkspaceID: "w1", TabCount: 1, PaneCount: len(panes)}},
		Tabs:       []herdrcli.Tab{{TabID: "t1", WorkspaceID: "w1", PaneCount: len(panes)}},
		Panes:      panes,
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

func TestFoldStoreMatchesByNameAndIncarnation(t *testing.T) {
	proj := agentstore.NewProjection()
	at := time.Date(2026, 9, 9, 5, 0, 0, 0, time.UTC)
	apply := func(e agentstore.Event) { e.ID = agentstore.NewID(at); proj.Apply(e, 0) }
	apply(agentstore.Event{At: at, Kind: agentstore.KindLaunchReady, By: "ziru", Name: "mavu"})
	apply(agentstore.Event{At: at.Add(time.Second), Kind: agentstore.KindAssign, By: "ziru", Name: "mavu", Mission: "old"})
	apply(agentstore.Event{At: at.Add(2 * time.Second), Kind: agentstore.KindCulled, By: "ziru", Name: "mavu", Pane: "p1", Close: "managed"})
	apply(agentstore.Event{At: at.Add(10 * time.Second), Kind: agentstore.KindLaunchReady, By: "vara", Name: "mavu"})
	rows := []Row{
		{Pane: "p1", Agent: "mavu", BusStatus: "listening"},
		{Pane: "p2", Agent: "zira", BusStatus: "-", Gap: "no bus row"},
		{Pane: "-", Agent: "funa", BusStatus: "active"},
	}
	roster := []hcomidentity.Row{{Name: "mavu", CreatedAt: at.Add(9 * time.Second)}, {Name: "funa"}}
	got := FoldStore(rows, roster, proj)
	if got[0].Launcher != "vara" || got[0].Manager != "vara" || got[0].Mission != "-" || got[0].Provenance == nil || got[0].Provenance.Kind != "registered" {
		t.Fatalf("reused name folded the old incarnation: %+v", got[0])
	}
	if got[1].Launcher != "-" || got[1].Provenance != nil {
		t.Fatalf("pane without bus row: %+v", got[1])
	}
	if got[2].Launcher != "unregistered" || got[2].Manager != "-" || got[2].Provenance.Kind != "unregistered" {
		t.Fatalf("bus row without events: %+v", got[2])
	}
	if rows[0].Launcher != "" {
		t.Fatal("FoldStore mutated its input")
	}
	roster[0].CreatedAt = at.Add(-time.Minute)
	if old := FoldStore(rows, roster, proj); old[0].Launcher != "ziru" || old[0].Mission != "old" {
		t.Fatalf("earlier created_at must fold the old incarnation: %+v", old[0])
	}
	if nilProj := FoldStore(rows, roster, nil); nilProj[0].Launcher != "unregistered" {
		t.Fatalf("nil projection: %+v", nilProj[0])
	}
	// No close event recorded (raw hcom kill): a later created_at must not inherit.
	apply(agentstore.Event{At: at.Add(20 * time.Second), Kind: agentstore.KindAssign, By: "vara", Name: "mavu", Mission: "second-life"})
	roster[0].CreatedAt = at.Add(time.Minute)
	roster[0].SessionID = "brand-new"
	if noClose := FoldStore(rows, roster, proj); noClose[0].Launcher != "unregistered" || noClose[0].Mission != "-" || noClose[0].Manager != "-" {
		t.Fatalf("reused name without a close event inherited: %+v", noClose[0])
	}
	// Binding folds into the row without touching placement/GAP.
	apply(agentstore.Event{At: at.Add(30 * time.Second), Kind: agentstore.KindLaunchReady, By: "riko", Name: "funa", Session: "claimed"})
	roster[1].SessionID = "roster-other"
	folded := FoldStore(rows, roster, proj)
	if folded[2].Binding == nil || folded[2].Binding.State != "conflict" || folded[2].Binding.Claimed != "claimed" || folded[2].Binding.Roster != "roster-other" || folded[2].Gap != rows[2].Gap || folded[2].Pane != "-" {
		t.Fatalf("binding fold = %+v", folded[2])
	}
}

func TestFoldStoreFallsBackToUniqueBaseName(t *testing.T) {
	proj := agentstore.NewProjection()
	at := time.Date(2026, 9, 10, 5, 0, 0, 0, time.UTC)
	proj.Apply(agentstore.Event{ID: agentstore.NewID(at), At: at, Kind: agentstore.KindMirrorReady, By: "hamo", ByKind: "mirror", Name: "kele"}, 0)
	rows := []Row{{Pane: "p1", Agent: "sesh-kele", BusStatus: "listening"}}
	roster := []hcomidentity.Row{{Name: "sesh-kele", BaseName: "kele", CreatedAt: at.Add(-time.Second)}}

	got := FoldStore(rows, roster, proj)
	if got[0].Manager != "hamo" || got[0].Launcher != "mirrored: hamo" {
		t.Fatalf("unique base fallback = %+v", got[0])
	}

	roster = append(roster, hcomidentity.Row{Name: "other-kele", BaseName: "kele", CreatedAt: at.Add(-time.Second)})
	got = FoldStore(rows, roster, proj)
	if got[0].Manager != "-" || got[0].Launcher != "unregistered" {
		t.Fatalf("ambiguous base fallback = %+v", got[0])
	}
}

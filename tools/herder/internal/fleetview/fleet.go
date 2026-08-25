// Package fleetview builds the shared, live herdr/hcom fleet join used by
// both the terminal list and the web API. It stores and caches nothing.
package fleetview

import (
	"fmt"
	"sort"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
)

// Row is one honest placement/bus join result. Gap is empty only when a live
// pane coordinate and an hcom roster row agree by exact pane ID.
type Row struct {
	Pane        string `json:"pane_id"`
	Agent       string `json:"agent"`
	Tool        string `json:"tool"`
	HerdrStatus string `json:"herdr_status"`
	BusStatus   string `json:"bus_status"`
	Gap         string `json:"gap"`
}

type Board struct {
	Workspaces []Workspace `json:"workspaces"`
	Unplaced   []Row       `json:"unplaced"`
}

type Workspace struct {
	WorkspaceID string `json:"workspace_id"`
	Number      int    `json:"number"`
	Label       string `json:"label"`
	Focused     bool   `json:"focused"`
	PaneCount   int    `json:"pane_count"`
	TabCount    int    `json:"tab_count"`
	ActiveTabID string `json:"active_tab_id"`
	AgentStatus string `json:"agent_status"`
	Tabs        []Tab  `json:"tabs"`
}

type Tab struct {
	TabID       string `json:"tab_id"`
	Number      int    `json:"number"`
	Label       string `json:"label"`
	Focused     bool   `json:"focused"`
	PaneCount   int    `json:"pane_count"`
	AgentStatus string `json:"agent_status"`
	Panes       []Pane `json:"panes"`
}

type Pane struct {
	PaneID       string `json:"pane_id"`
	Label        string `json:"label,omitempty"`
	AgentSession string `json:"agent_session,omitempty"`
	Agent        string `json:"agent"`
	Tool         string `json:"tool"`
	HerdrStatus  string `json:"herdr_status"`
	BusStatus    string `json:"bus_status"`
	Gap          string `json:"gap"`
}

type placement struct {
	pane, name, tool, status string
}

// ValidateSnapshot rejects hierarchy gaps that would otherwise turn a live
// placement into an apparently empty or incomplete web board.
func ValidateSnapshot(snapshot herdrcli.Snapshot) error {
	workspaces := make(map[string]bool, len(snapshot.Workspaces))
	for _, workspace := range snapshot.Workspaces {
		workspaces[workspace.WorkspaceID] = true
	}
	tabs := make(map[string]herdrcli.Tab, len(snapshot.Tabs))
	for _, tab := range snapshot.Tabs {
		if !workspaces[tab.WorkspaceID] {
			return fmt.Errorf("tab %s references missing workspace %s", tab.TabID, tab.WorkspaceID)
		}
		tabs[tab.TabID] = tab
	}
	panes := make(map[string]bool, len(snapshot.Panes))
	for _, pane := range snapshot.Panes {
		tab, ok := tabs[pane.TabID]
		if !ok {
			return fmt.Errorf("pane %s references missing tab %s", pane.PaneID, pane.TabID)
		}
		if pane.WorkspaceID != tab.WorkspaceID {
			return fmt.Errorf("pane %s workspace %s disagrees with tab workspace %s", pane.PaneID, pane.WorkspaceID, tab.WorkspaceID)
		}
		panes[pane.PaneID] = true
	}
	for _, agent := range snapshot.Agents {
		if agent.PaneID != "" && !panes[agent.PaneID] {
			return fmt.Errorf("agent %s references missing pane %s", agent.Name, agent.PaneID)
		}
	}
	return nil
}

// ValidateRoster rejects contested pane claims rather than letting the web
// projection silently pick one agent while the terminal list shows both.
func ValidateRoster(roster []hcomidentity.Row) error {
	claimed := make(map[string]string)
	for _, row := range roster {
		paneID := row.LaunchContext.PaneID
		if paneID == "" {
			continue
		}
		if previous, ok := claimed[paneID]; ok {
			return fmt.Errorf("pane %s is claimed by multiple bus rows (%s, %s)", paneID, previous, row.Name)
		}
		claimed[paneID] = row.Name
	}
	return nil
}

// JoinRows correlates only exact pane IDs. Names and session IDs are display
// evidence, never placement evidence.
func JoinRows(snapshot herdrcli.Snapshot, roster []hcomidentity.Row) []Row {
	agents := make(map[string]herdrcli.Agent, len(snapshot.Agents))
	for _, agent := range snapshot.Agents {
		if agent.PaneID != "" {
			agents[agent.PaneID] = agent
		}
	}
	placements := make(map[string]placement)
	for _, pane := range snapshot.Panes {
		agent, hasAgent := agents[pane.PaneID]
		if !hasAgent && pane.Agent == "" && pane.AgentSession == "" && pane.AgentStatus == "" {
			continue
		}
		placements[pane.PaneID] = placement{pane.PaneID, first(agent.Name, pane.Label), first(agent.Agent, pane.Agent), first(agent.Status, pane.AgentStatus, "visible")}
	}
	for paneID, agent := range agents {
		if _, ok := placements[paneID]; !ok {
			placements[paneID] = placement{paneID, agent.Name, agent.Agent, agent.Status}
		}
	}

	byPane := make(map[string][]int)
	for i, bus := range roster {
		if bus.LaunchContext.PaneID != "" {
			byPane[bus.LaunchContext.PaneID] = append(byPane[bus.LaunchContext.PaneID], i)
		}
	}
	matched := make(map[int]bool)
	paneIDs := make([]string, 0, len(placements))
	for paneID := range placements {
		paneIDs = append(paneIDs, paneID)
	}
	sort.Strings(paneIDs)
	rows := make([]Row, 0, len(placements)+len(roster))
	for _, paneID := range paneIDs {
		place := placements[paneID]
		matches := byPane[paneID]
		if len(matches) == 0 {
			rows = append(rows, Row{paneID, display(place.name), display(place.tool), display(place.status), "-", "no bus row"})
			continue
		}
		for _, i := range matches {
			matched[i] = true
			bus := roster[i]
			rows = append(rows, Row{paneID, display(first(bus.Name, place.name)), display(first(bus.Tool, place.tool)), display(place.status), display(bus.Status), "-"})
		}
	}
	for i, bus := range roster {
		if !matched[i] {
			rows = append(rows, Row{"-", display(bus.Name), display(bus.Tool), "-", display(bus.Status), "no visible pane"})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Pane != rows[j].Pane {
			return rows[i].Pane < rows[j].Pane
		}
		return rows[i].Agent < rows[j].Agent
	})
	return rows
}

// Build preserves the herdr workspace/tab/pane hierarchy and enriches agent
// panes with the exact-pane-ID join. Bus-only rows remain top-level gaps.
func Build(snapshot herdrcli.Snapshot, roster []hcomidentity.Row) Board {
	rows := JoinRows(snapshot, roster)
	byPane := make(map[string]Row)
	board := Board{Workspaces: []Workspace{}, Unplaced: []Row{}}
	for _, row := range rows {
		if row.Pane == "-" {
			board.Unplaced = append(board.Unplaced, row)
		} else {
			byPane[row.Pane] = row
		}
	}
	panesByTab := make(map[string][]Pane)
	for _, source := range snapshot.Panes {
		row, joined := byPane[source.PaneID]
		if !joined {
			if source.Agent == "" && source.AgentSession == "" && source.AgentStatus == "" {
				// Plain terminal panes are part of herdr's structure but are not
				// placement gaps: no agent is expected on the bus.
				row = Row{Pane: source.PaneID, Agent: "-", Tool: "-", HerdrStatus: "-", BusStatus: "-", Gap: "-"}
			} else {
				row = Row{Pane: source.PaneID, Agent: display(source.Label), Tool: display(source.Agent), HerdrStatus: display(first(source.AgentStatus, "visible")), BusStatus: "-", Gap: "no bus row"}
			}
		}
		panesByTab[source.TabID] = append(panesByTab[source.TabID], Pane{
			PaneID: source.PaneID, Label: source.Label, AgentSession: source.AgentSession,
			Agent: row.Agent, Tool: row.Tool, HerdrStatus: row.HerdrStatus, BusStatus: row.BusStatus, Gap: row.Gap,
		})
	}
	tabsByWorkspace := make(map[string][]Tab)
	for _, source := range snapshot.Tabs {
		panes := panesByTab[source.TabID]
		if panes == nil {
			panes = []Pane{}
		}
		tabsByWorkspace[source.WorkspaceID] = append(tabsByWorkspace[source.WorkspaceID], Tab{
			TabID: source.TabID, Number: source.Number, Label: source.Label, Focused: source.Focused,
			PaneCount: source.PaneCount, AgentStatus: source.AgentStatus, Panes: panes,
		})
	}
	for _, source := range snapshot.Workspaces {
		tabs := tabsByWorkspace[source.WorkspaceID]
		if tabs == nil {
			tabs = []Tab{}
		}
		board.Workspaces = append(board.Workspaces, Workspace{
			WorkspaceID: source.WorkspaceID, Number: source.Number, Label: source.Label, Focused: source.Focused,
			PaneCount: source.PaneCount, TabCount: source.TabCount, ActiveTabID: source.ActiveTabID,
			AgentStatus: source.AgentStatus, Tabs: tabs,
		})
	}
	return board
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func display(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

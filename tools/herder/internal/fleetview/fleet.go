// Package fleetview builds the shared, live herdr/hcom fleet join used by
// both the terminal list and the web API. It stores and caches nothing.
package fleetview

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/repoctx"
)

// Row is one honest placement/bus join result. Gap is empty only when a live
// pane coordinate and an hcom roster row agree by exact pane ID or by the
// unambiguous live tool/session fallback described in JoinRows.
type Row struct {
	Pane        string `json:"pane_id"`
	Agent       string `json:"agent"`
	Tool        string `json:"tool"`
	HerdrStatus string `json:"herdr_status"`
	BusStatus   string `json:"bus_status"`
	Gap         string `json:"gap"`
	ParentAgent string `json:"parent_agent,omitempty"`
	Subagents   *Rows  `json:"subagents,omitempty"`
	// Store-folded columns (FoldStore). Launcher is immutable provenance,
	// Manager the mutable hierarchy pointer, Mission the current assignment.
	// A bus row with no store record prints "unregistered"; a pane with no
	// bus row prints "-".
	Launcher string `json:"launcher,omitempty"`
	Manager  string `json:"manager,omitempty"`
	// ManagerState is derived at fold time from the manager's own standing:
	// operator (a human seeded the edge), live (a live roster row), ended
	// (a store record that is closed or no longer on the roster), unknown
	// (no manager, or a name with no record and no roster row).
	ManagerState string                 `json:"manager_state,omitempty"`
	CreatedAt    string                 `json:"created_at,omitempty"` // roster created_at, RFC3339 UTC
	Mission      string                 `json:"mission,omitempty"`
	Provenance   *ProvenanceSummary     `json:"provenance,omitempty"`
	Binding      *agentstore.Binding    `json:"binding,omitempty"` // claimed vs roster session; never touches placement
	Title        string                 `json:"title,omitempty"`
	Vitals       *agentstore.VitalsView `json:"vitals,omitempty"`
}

// ProvenanceSummary is the row-sized slice of an AgentView's provenance.
type ProvenanceSummary struct {
	Kind     string `json:"kind"` // registered | mirrored | unregistered
	Launcher string `json:"launcher,omitempty"`
}

// FoldStore is the ONE place the agent store joins the fleet rows, used by
// the terminal list now and by the serve's board later. Rows are matched by
// (name, incarnation): the roster row's created_at picks the incarnation. The
// join itself is untouched; a nil projection folds every bus row as
// unregistered.
func FoldStore(rows []Row, roster []hcomidentity.Row, proj *agentstore.Projection) []Row {
	byName := make(map[string]*hcomidentity.Row, len(roster))
	for i := range roster {
		byName[roster[i].Name] = &roster[i]
	}
	out := append([]Row(nil), rows...)
	for i := range out {
		row := &out[i]
		if row.BusStatus == "-" {
			row.Launcher, row.Manager, row.Mission = "-", "-", "-"
			continue
		}
		var view *agentstore.AgentView
		if bus, ok := byName[row.Agent]; ok {
			if !bus.CreatedAt.IsZero() {
				row.CreatedAt = bus.CreatedAt.UTC().Format(time.RFC3339)
			}
			if proj != nil {
				view = proj.ViewForRoster(bus, roster)
			}
		}
		if view == nil {
			row.ManagerState = ManagerUnknown
			row.Launcher, row.Manager, row.Mission = "unregistered", "-", "-"
			row.Provenance = &ProvenanceSummary{Kind: "unregistered"}
			continue
		}
		pr := view.Provenance
		row.Provenance = &ProvenanceSummary{Kind: pr.Kind, Launcher: pr.Launcher}
		switch pr.Kind {
		case "registered":
			row.Launcher = display(pr.Launcher)
		case "mirrored":
			row.Launcher = "mirrored: " + display(pr.Launcher)
		default:
			row.Launcher = "unregistered"
		}
		manager, state := ManagerEdge(view, roster, proj)
		row.Manager, row.ManagerState = display(manager), state
		row.Binding = view.Binding
		row.Mission = "-"
		if view.Assignment != nil {
			row.Mission = display(view.Assignment.Mission)
		}
		if view.Annotation != nil {
			row.Title = view.Annotation.Title
		}
		if view.Session != nil {
			vitals := view.Session.Vitals
			row.Vitals = &vitals
		}
	}
	return out
}

// Manager states carried on Row.ManagerState.
const (
	ManagerOperator = "operator"
	ManagerLive     = "live"
	ManagerEnded    = "ended"
	ManagerUnknown  = "unknown"
)

// ManagerEdge resolves a folded record's manager pointer to the name the tree
// hangs the row under and the standing of that manager. A human seed (hcom's
// literal "user", a registered user/web launcher that still seeds the edge,
// or a web identity with no record) is the operator. A manager string that is
// a unique roster base_name resolves to the full roster name (the same rule
// the life mirror applies). A live roster row is live whether or not it has a
// record; a record that is closed or whose name is off the roster is ended;
// anything else is unknown. Nothing here writes an edge.
func ManagerEdge(view *agentstore.AgentView, roster []hcomidentity.Row, proj *agentstore.Projection) (string, string) {
	manager := view.Manager
	if manager == "" || manager == "unknown" {
		return manager, ManagerUnknown
	}
	seededByHuman := view.ManagerAt == nil && (view.Provenance.LauncherKind == "user" || view.Provenance.LauncherKind == "web")
	if manager == "user" || seededByHuman {
		return manager, ManagerOperator
	}
	var bus *hcomidentity.Row
	for i := range roster {
		if roster[i].Name == manager {
			bus = &roster[i]
			break
		}
	}
	if bus == nil {
		if owner, unique := hcomidentity.ByUniqueBaseName(roster, manager); unique {
			manager, bus = owner.Name, &owner
		}
	}
	if bus != nil {
		return manager, ManagerLive
	}
	if proj != nil && proj.Latest(manager) != nil {
		return manager, ManagerEnded
	}
	if strings.HasPrefix(manager, "web-") {
		return manager, ManagerOperator
	}
	return manager, ManagerUnknown
}

// FoldBoard folds every row of a built board (workspace panes, unplaced rows
// and nested subagents) through FoldStore and copies the supervision fields
// onto them. The board's placement rows are untouched otherwise: the payload
// gains manager, manager_state, title and created_at and nothing else. A
// manager the operator seeded reads "operator"; an empty manager is omitted.
func FoldBoard(board *Board, roster []hcomidentity.Row, proj *agentstore.Projection) {
	fold := func(agent, busStatus string) Row {
		folded := FoldStore([]Row{{Agent: agent, BusStatus: busStatus}}, roster, proj)[0]
		if folded.ManagerState == ManagerOperator {
			folded.Manager = ManagerOperator
		}
		if folded.Manager == "-" || folded.Manager == "unknown" {
			folded.Manager = ""
		}
		return folded
	}
	var foldRows func(rows []Row)
	foldRows = func(rows []Row) {
		for i := range rows {
			if rows[i].BusStatus == "-" {
				continue
			}
			folded := fold(rows[i].Agent, rows[i].BusStatus)
			rows[i].Manager, rows[i].ManagerState, rows[i].Title, rows[i].CreatedAt = folded.Manager, folded.ManagerState, folded.Title, folded.CreatedAt
			if rows[i].Subagents != nil {
				foldRows(*rows[i].Subagents)
			}
		}
	}
	for w := range board.Workspaces {
		for t := range board.Workspaces[w].Tabs {
			panes := board.Workspaces[w].Tabs[t].Panes
			for p := range panes {
				if panes[p].BusStatus == "-" {
					continue
				}
				folded := fold(panes[p].Agent, panes[p].BusStatus)
				panes[p].Manager, panes[p].ManagerState, panes[p].Title, panes[p].CreatedAt = folded.Manager, folded.ManagerState, folded.Title, folded.CreatedAt
				foldRows(panes[p].Subagents)
			}
		}
	}
	foldRows(board.Unplaced)
}

type Rows []Row

type Board struct {
	Workspaces []Workspace `json:"workspaces"`
	Unplaced   []Row       `json:"unplaced"`
}

type Workspace struct {
	WorkspaceID string       `json:"workspace_id"`
	WorktreeOf  string       `json:"worktree_of,omitempty"`
	Number      int          `json:"number"`
	Label       string       `json:"label"`
	Focused     bool         `json:"focused"`
	PaneCount   int          `json:"pane_count"`
	TabCount    int          `json:"tab_count"`
	ActiveTabID string       `json:"active_tab_id"`
	AgentStatus string       `json:"agent_status"`
	CWD         string       `json:"cwd,omitempty"`
	Git         *repoctx.Git `json:"git,omitempty"`
	Tabs        []Tab        `json:"tabs"`
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
	PaneID         string `json:"pane_id"`
	Label          string `json:"label,omitempty"`
	CurrentCommand string `json:"current_command,omitempty"`
	AgentSession   string `json:"agent_session,omitempty"`
	Agent          string `json:"agent"`
	Tool           string `json:"tool"`
	HerdrStatus    string `json:"herdr_status"`
	BusStatus      string `json:"bus_status"`
	Gap            string `json:"gap"`
	Manager        string `json:"manager,omitempty"`
	ManagerState   string `json:"manager_state,omitempty"`
	Title          string `json:"title,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
	Subagents      []Row  `json:"subagents,omitempty"`
}

type placement struct {
	pane, name, tool, session, status string
}

type sessionIdentity struct {
	tool, session string
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

// JoinRows correlates exact pane IDs first. A roster row without a live pane
// claim may then use an unambiguous live tool/session identity. Names remain
// display evidence and never place a row.
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
		if !hasAgent && pane.Agent == "" && pane.AgentSession == "" {
			continue
		}
		placements[pane.PaneID] = placement{pane.PaneID, first(agent.Name, pane.Label), first(agent.Agent, pane.Agent), pane.AgentSession, first(agent.Status, pane.AgentStatus, "visible")}
	}
	for paneID, agent := range agents {
		if _, ok := placements[paneID]; !ok {
			placements[paneID] = placement{pane: paneID, name: agent.Name, tool: agent.Agent, status: agent.Status}
		}
	}

	byPane := make(map[string][]int)
	livePanes := make(map[string]bool, len(snapshot.Panes))
	for _, pane := range snapshot.Panes {
		livePanes[pane.PaneID] = true
	}
	for i, bus := range roster {
		if bus.LaunchContext.PaneID != "" {
			byPane[bus.LaunchContext.PaneID] = append(byPane[bus.LaunchContext.PaneID], i)
		}
	}
	// A fleet launch may have created its requested pane even when Herdr has not
	// yet bound agent metadata to it. Preserve the exact launch-context claim as
	// a visible, explicitly degraded placement; never infer one from a name.
	for paneID := range livePanes {
		if _, visible := placements[paneID]; !visible && len(byPane[paneID]) == 1 {
			placements[paneID] = placement{pane: paneID}
		}
	}
	panesBySession := make(map[sessionIdentity][]string)
	for paneID, place := range placements {
		if place.tool != "" && place.session != "" {
			key := sessionIdentity{tool: place.tool, session: place.session}
			panesBySession[key] = append(panesBySession[key], paneID)
		}
	}
	rowsBySession := make(map[sessionIdentity][]int)
	for i, bus := range roster {
		if bus.Tool == "" || bus.SessionID == "" || livePanes[bus.LaunchContext.PaneID] {
			continue
		}
		key := sessionIdentity{tool: bus.Tool, session: bus.SessionID}
		rowsBySession[key] = append(rowsBySession[key], i)
	}
	bySessionPane := make(map[string]int)
	for key, paneMatches := range panesBySession {
		rowMatches := rowsBySession[key]
		if len(paneMatches) == 1 && len(rowMatches) == 1 && len(byPane[paneMatches[0]]) == 0 {
			bySessionPane[paneMatches[0]] = rowMatches[0]
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
			if match, ok := bySessionPane[paneID]; ok {
				matches = []int{match}
			}
		}
		if len(matches) == 0 {
			rows = append(rows, Row{Pane: paneID, Agent: display(place.name), Tool: display(place.tool), HerdrStatus: display(place.status), BusStatus: "-", Gap: "no bus row"})
			continue
		}
		for _, i := range matches {
			matched[i] = true
			bus := roster[i]
			gap := "-"
			if place.name == "" && place.tool == "" && place.session == "" && place.status == "" {
				gap = "pane not bound"
			}
			rows = append(rows, Row{Pane: paneID, Agent: display(first(bus.Name, place.name)), Tool: display(first(bus.Tool, place.tool)), HerdrStatus: display(place.status), BusStatus: display(bus.Status), Gap: gap})
		}
	}
	for i, bus := range roster {
		if !matched[i] {
			rows = append(rows, Row{Pane: "-", Agent: display(bus.Name), Tool: display(bus.Tool), HerdrStatus: "-", BusStatus: display(bus.Status), Gap: "no visible pane"})
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
func Build(snapshot herdrcli.Snapshot, roster []hcomidentity.Row, worktreeParents ...map[string]string) Board {
	rows := JoinRows(snapshot, roster)
	rowsByAgent := make(map[string]Row, len(rows))
	for _, row := range rows {
		if row.BusStatus != "-" {
			rowsByAgent[row.Agent] = row
		}
	}
	children := make(map[string][]string)
	provenChildren := make(map[string]bool)
	for _, child := range roster {
		parent, ok := hcomidentity.Parent(roster, child)
		if !ok {
			continue
		}
		row, childVisible := rowsByAgent[child.Name]
		_, parentVisible := rowsByAgent[parent.Name]
		if !childVisible || !parentVisible {
			continue
		}
		row.ParentAgent = parent.Name
		rowsByAgent[child.Name] = row
		children[parent.Name] = append(children[parent.Name], child.Name)
		provenChildren[child.Name] = true
	}
	var attachChildren func(Row, map[string]bool) Row
	attachChildren = func(row Row, ancestors map[string]bool) Row {
		if ancestors[row.Agent] {
			return row
		}
		next := make(map[string]bool, len(ancestors)+1)
		for name := range ancestors {
			next[name] = true
		}
		next[row.Agent] = true
		var nested Rows
		for _, childName := range children[row.Agent] {
			child := attachChildren(rowsByAgent[childName], next)
			nested = append(nested, child)
		}
		sort.SliceStable(nested, func(i, j int) bool { return nested[i].Agent < nested[j].Agent })
		if len(nested) > 0 {
			row.Subagents = &nested
		}
		return row
	}
	byPane := make(map[string]Row)
	board := Board{Workspaces: []Workspace{}, Unplaced: []Row{}}
	for _, row := range rows {
		if provenChildren[row.Agent] && row.BusStatus != "-" {
			continue
		}
		if row.BusStatus != "-" {
			row = attachChildren(row, nil)
		}
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
			if source.Agent == "" && source.AgentSession == "" {
				// Plain terminal panes are part of herdr's structure but are not
				// placement gaps: no agent is expected on the bus.
				row = Row{Pane: source.PaneID, Agent: "-", Tool: "-", HerdrStatus: "-", BusStatus: "-", Gap: "-"}
			} else {
				row = Row{Pane: source.PaneID, Agent: display(source.Label), Tool: display(source.Agent), HerdrStatus: display(first(source.AgentStatus, "visible")), BusStatus: "-", Gap: "no bus row"}
			}
		}
		panesByTab[source.TabID] = append(panesByTab[source.TabID], Pane{
			PaneID: source.PaneID, Label: source.Label, CurrentCommand: source.CurrentCommand, AgentSession: source.AgentSession,
			Agent: row.Agent, Tool: row.Tool, HerdrStatus: row.HerdrStatus, BusStatus: row.BusStatus, Gap: row.Gap,
			Subagents: rowsValue(row.Subagents),
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
		worktreeOf := ""
		if len(worktreeParents) > 0 {
			worktreeOf = worktreeParents[0][source.WorkspaceID]
		}
		board.Workspaces = append(board.Workspaces, Workspace{
			WorkspaceID: source.WorkspaceID, Number: source.Number, Label: source.Label, Focused: source.Focused,
			WorktreeOf: worktreeOf,
			PaneCount:  source.PaneCount, TabCount: source.TabCount, ActiveTabID: source.ActiveTabID,
			AgentStatus: source.AgentStatus, Tabs: tabs,
		})
	}
	return board
}

func rowsValue(rows *Rows) []Row {
	if rows == nil {
		return nil
	}
	return []Row(*rows)
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

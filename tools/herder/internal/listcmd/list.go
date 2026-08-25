// Package listcmd renders a live, read-only join of herdr placement and the
// hcom roster. It deliberately owns no persisted state.
package listcmd

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
)

type dependencies struct {
	snapshot func() (herdrcli.Snapshot, error)
	roster   func(string) ([]hcomidentity.Row, error)
}

var liveDependencies = dependencies{
	snapshot: herdrcli.LiveSnapshot,
	roster:   hcomidentity.List,
}

// Row is one honest placement/bus join result. Gap is empty only when a live
// pane coordinate and an hcom roster row agree by exact pane ID.
type Row struct {
	Pane        string
	Agent       string
	Tool        string
	HerdrStatus string
	BusStatus   string
	Gap         string
}

func Run(args []string, stdout, stderr io.Writer) int {
	return run(args, stdout, stderr, liveDependencies)
}

func run(args []string, stdout, stderr io.Writer, deps dependencies) int {
	if len(args) > 0 {
		if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
			printHelp(stdout)
			return 0
		}
		fmt.Fprintf(stderr, "herder list: unknown argument %q\n", args[0])
		return 2
	}

	// Placement is read first on purpose: a missing herdr socket must never be
	// disguised as an empty fleet or inferred from hcom's recorded launch data.
	snapshot, err := deps.snapshot()
	if err != nil {
		fmt.Fprintf(stderr, "herder list: cannot read live herdr snapshot: %v\n", err)
		return 1
	}
	roster, err := deps.roster("")
	if err != nil {
		fmt.Fprintf(stderr, "herder list: cannot read live hcom roster: %v\n", err)
		return 1
	}

	writeTable(stdout, Join(snapshot, roster))
	return 0
}

func printHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `herder list — join live herdr placement with the hcom roster.

Usage:
  herder list

Rows are joined only by an exact pane ID. A bus agent without a visible pane
and a visible agent pane without a bus row are shown explicitly as gaps.
`)
}

type placement struct {
	pane   string
	name   string
	tool   string
	status string
}

// Join correlates only exact pane IDs. Names and session IDs are display
// evidence, not placement evidence, so they never erase a gap.
func Join(snapshot herdrcli.Snapshot, roster []hcomidentity.Row) []Row {
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
		placements[pane.PaneID] = placement{
			pane:   pane.PaneID,
			name:   first(agent.Name, pane.Label),
			tool:   first(agent.Agent, pane.Agent),
			status: first(agent.Status, pane.AgentStatus, "visible"),
		}
	}
	// An agent row carrying a pane coordinate is placement evidence even if a
	// concurrently captured pane array omitted that coordinate.
	for paneID, agent := range agents {
		if _, ok := placements[paneID]; ok {
			continue
		}
		placements[paneID] = placement{
			pane: paneID, name: agent.Name, tool: agent.Agent,
			status: first(agent.Status, "visible"),
		}
	}

	byPane := make(map[string][]int)
	for i, bus := range roster {
		if bus.LaunchContext.PaneID != "" {
			byPane[bus.LaunchContext.PaneID] = append(byPane[bus.LaunchContext.PaneID], i)
		}
	}
	matchedBus := make(map[int]bool)
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
			rows = append(rows, Row{
				Pane: paneID, Agent: display(place.name), Tool: display(place.tool),
				HerdrStatus: display(place.status), BusStatus: "-", Gap: "no bus row",
			})
			continue
		}
		for _, index := range matches {
			matchedBus[index] = true
			bus := roster[index]
			rows = append(rows, Row{
				Pane: paneID, Agent: display(first(bus.Name, place.name)),
				Tool: display(first(bus.Tool, place.tool)), HerdrStatus: display(place.status),
				BusStatus: display(bus.Status), Gap: "-",
			})
		}
	}

	for i, bus := range roster {
		if matchedBus[i] {
			continue
		}
		rows = append(rows, Row{
			Pane: "-", Agent: display(bus.Name), Tool: display(bus.Tool),
			HerdrStatus: "-", BusStatus: display(bus.Status), Gap: "no visible pane",
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Pane != rows[j].Pane {
			return rows[i].Pane < rows[j].Pane
		}
		return rows[i].Agent < rows[j].Agent
	})
	return rows
}

func writeTable(out io.Writer, rows []Row) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PANE\tAGENT\tTOOL\tHERDR\tBUS\tGAP")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Pane, row.Agent, row.Tool, row.HerdrStatus, row.BusStatus, row.Gap)
	}
	_ = w.Flush()
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

// Package listcmd renders a live, read-only join of herdr placement and the
// hcom roster. It deliberately owns no persisted state.
package listcmd

import (
	"fmt"
	"io"
	"text/tabwriter"

	"ai-config/tools/herder/internal/fleetview"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
)

type dependencies struct {
	snapshot func() (herdrcli.Snapshot, error)
	roster   func() ([]hcomidentity.Row, error)
}

var liveDependencies = dependencies{
	snapshot: herdrcli.LiveSnapshot,
	roster:   hcomidentity.List,
}

// Row is one honest placement/bus join result. Gap is empty only when a live
// pane coordinate and an hcom roster row agree by exact pane ID.
type Row = fleetview.Row

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
	roster, err := deps.roster()
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

// Join correlates only exact pane IDs. Names and session IDs are display
// evidence, not placement evidence, so they never erase a gap.
func Join(snapshot herdrcli.Snapshot, roster []hcomidentity.Row) []Row {
	return fleetview.JoinRows(snapshot, roster)
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

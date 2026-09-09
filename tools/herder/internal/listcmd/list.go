// Package listcmd renders a live, read-only join of herdr placement and the
// hcom roster, folded with the agent store's provenance columns. The join
// owns no persisted state; the store is read (and its snapshot refreshed)
// but a store failure only degrades the columns, never the list.
package listcmd

import (
	"fmt"
	"io"
	"text/tabwriter"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/fleetview"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herderstate"
	"ai-config/tools/herder/internal/herdrcli"
)

type dependencies struct {
	snapshot func() (herdrcli.Snapshot, error)
	roster   func() ([]hcomidentity.Row, error)
	store    func(stderr io.Writer) *agentstore.Projection
}

var liveDependencies = dependencies{
	snapshot: herdrcli.LiveSnapshot,
	roster:   hcomidentity.List,
	store:    loadStore,
}

// loadStore never fails a list: an unwritable first-open import or an
// unreadable log prints one warning and folds every row as unregistered; a
// snapshot rewrite failure is silent (full replay still happened).
func loadStore(stderr io.Writer) *agentstore.Projection {
	stateDir, err := herderstate.Dir()
	if err != nil {
		fmt.Fprintf(stderr, "herder list: agent store unavailable (%v); rows shown as unregistered\n", err)
		return nil
	}
	store := agentstore.Open(stateDir, stderr)
	if store.ImportErr != nil {
		fmt.Fprintf(stderr, "herder list: agent store unavailable (%v); rows shown as unregistered\n", store.ImportErr)
		return nil
	}
	proj, err := store.Load()
	if err != nil {
		fmt.Fprintf(stderr, "herder list: cannot read agent store (%v); rows shown as unregistered\n", err)
		return nil
	}
	return proj
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

	var proj *agentstore.Projection
	if deps.store != nil {
		proj = deps.store(stderr)
	}
	rows := fleetview.FoldStore(Join(snapshot, roster), roster, proj)
	writeTable(stdout, rows)
	return 0
}

func printHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `herder list — join live herdr placement with the hcom roster.

Usage:
  herder list

Rows are joined only by an exact pane ID. A bus agent without a visible pane
and a visible agent pane without a bus row are shown explicitly as gaps.

LAUNCHER, MANAGER, MISSION and BINDING come from the agent store
($HERDER_STATE_DIR/agents), folded by (name, hcom created_at). A bus row with
no store record prints "unregistered"; BINDING shows "conflict A≠B" when a
registered session claim disagrees with the roster (the roster stays
current). The store never gates a lifecycle action.
`)
}

// Join correlates only exact pane IDs. Names and session IDs are display
// evidence, not placement evidence, so they never erase a gap.
func Join(snapshot herdrcli.Snapshot, roster []hcomidentity.Row) []Row {
	return fleetview.JoinRows(snapshot, roster)
}

func writeTable(out io.Writer, rows []Row) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PANE\tAGENT\tTOOL\tHERDR\tBUS\tLAUNCHER\tMANAGER\tMISSION\tBINDING\tGAP")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Pane, row.Agent, row.Tool, row.HerdrStatus, row.BusStatus, orDash(row.Launcher), orDash(row.Manager), orDash(row.Mission), bindingLabel(row.Binding), row.Gap)
	}
	_ = w.Flush()
}

// bindingLabel is "-" when the store made no session claim, "verified" or
// "pending" when it agrees or waits, and "conflict <claimed>≠<roster>" (first
// 8 chars of each id) when the roster disagrees. It never changes GAP.
func bindingLabel(b *agentstore.Binding) string {
	if b == nil {
		return "-"
	}
	if b.State != "conflict" {
		return b.State
	}
	return fmt.Sprintf("conflict %s≠%s", short(b.Claimed), short(b.Roster))
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

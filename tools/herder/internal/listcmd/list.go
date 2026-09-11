// Package listcmd renders a live, read-only join of herdr placement and the
// hcom roster, folded with the agent store's provenance columns. The join
// owns no persisted state and never maintains the store snapshot; a store
// failure only degrades the columns, never the list.
package listcmd

import (
	"fmt"
	"io"
	"text/tabwriter"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/fleetview"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herderstate"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/sessionvitals"
)

type dependencies struct {
	snapshot func() (herdrcli.Snapshot, error)
	roster   func() ([]hcomidentity.Row, error)
	store    func(stderr io.Writer) *agentstore.Projection
	// vitals is sessionvitals.Read: the serve's socket cache first, then the
	// direct transcript read. Tests substitute a fake.
	vitals func(hcomidentity.Row) (sessionvitals.Result, error)
}

var liveDependencies = dependencies{
	snapshot: herdrcli.LiveSnapshot,
	roster:   hcomidentity.List,
	store:    loadStore,
	vitals:   sessionvitals.Read,
}

// loadStore never fails a list: an unwritable first-open import or an
// unreadable log prints one warning and folds every row as unregistered.
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
	proj, err := store.LoadNoSnapshot()
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
	vitals := make(map[string]claudesession.Vitals, len(roster))
	if deps.vitals != nil {
		for _, row := range roster {
			// Sequential per row (no goroutine per row). sessionvitals.Read asks a
			// running serve's socket first and reads the transcript itself when no
			// serve answers; a failed row prints "-" rather than failing the list.
			read, _ := deps.vitals(row)
			vitals[row.Name] = read.Vitals
		}
	}
	writeTable(stdout, rows, vitals)
	return 0
}

func printHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `herder list — join live herdr placement with the hcom roster.

Usage:
  herder list

Rows are joined only by an exact pane ID. A bus agent without a visible pane
and a visible agent pane without a bus row are shown explicitly as gaps.

LAUNCHER, MANAGER, GROUP and BINDING come from the agent store. MODEL and CONTEXT come
from a running herder serve's cache over its local socket when one answers,
else from each current session transcript; CONTEXT is used/window
and percent used. Missing live vitals print "-".

The store lives at ($HERDER_STATE_DIR/agents), folded by (name, hcom
created_at). A bus row with no store record prints "unregistered"; BINDING
shows "conflict A≠B" when a registered session claim disagrees with the roster
(the roster stays current). The store never gates a lifecycle action.
`)
}

// Join correlates only exact pane IDs. Names and session IDs are display
// evidence, not placement evidence, so they never erase a gap.
func Join(snapshot herdrcli.Snapshot, roster []hcomidentity.Row) []Row {
	return fleetview.JoinRows(snapshot, roster)
}

func writeTable(out io.Writer, rows []Row, vitals map[string]claudesession.Vitals) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PANE\tAGENT\tTOOL\tHERDR\tBUS\tLAUNCHER\tMANAGER\tGROUP\tBINDING\tMODEL\tCONTEXT\tGAP")
	for _, row := range rows {
		rowVitals := vitals[row.Agent]
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Pane, row.Agent, row.Tool, row.HerdrStatus, row.BusStatus, orDash(row.Launcher), orDash(row.Manager), orDash(row.Group), bindingLabel(row.Binding), orDash(rowVitals.Model), contextLabel(rowVitals.ContextUsage), row.Gap)
	}
	_ = w.Flush()
}

func contextLabel(usage *claudesession.ContextUsage) string {
	if usage == nil || usage.UsedTokens <= 0 {
		return "-"
	}
	window, percent := "-", "-"
	if usage.WindowTokens != nil && *usage.WindowTokens > 0 {
		window = sessionvitals.Kilo(*usage.WindowTokens)
	}
	if usage.UsedPercent != nil {
		percent = fmt.Sprintf("%.0f%%", *usage.UsedPercent)
	}
	return fmt.Sprintf("%s/%s %s", sessionvitals.Kilo(usage.UsedTokens), window, percent)
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

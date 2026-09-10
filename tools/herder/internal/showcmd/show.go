// Package showcmd is `herder show <name> [--json]`: the AgentView for one
// agent, folded with the live roster when hcom answers (store-only otherwise).
package showcmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herderstate"
	"ai-config/tools/herder/internal/sessionvitals"
)

type dependencies struct {
	roster func() ([]hcomidentity.Row, error)
	vitals func(hcomidentity.Row) (claudesession.Vitals, string, time.Time, error)
}

var liveDependencies = dependencies{roster: hcomidentity.List, vitals: sessionvitals.Read}

type showVitals struct {
	Model        string                      `json:"model,omitempty"`
	ContextUsage *claudesession.ContextUsage `json:"context_usage,omitempty"`
	ObservedAt   *time.Time                  `json:"observed_at,omitempty"`
	SessionFile  string                      `json:"session_file,omitempty"`
}

type showOutput struct {
	*agentstore.AgentView
	Vitals      showVitals `json:"vitals"`
	VitalsError string     `json:"vitals_error,omitempty"`
}

func Run(args []string, stdout, stderr io.Writer) int {
	return run(args, stdout, stderr, liveDependencies)
}

func run(args []string, stdout, stderr io.Writer, deps dependencies) int {
	name, sessionID, asJSON := "", "", false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			fmt.Fprint(stdout, help)
			return 0
		case arg == "--json":
			asJSON = true
		case arg == "--session":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				fmt.Fprintln(stderr, "herder show: --session requires an id")
				return 2
			}
			i++
			sessionID = args[i]
		case strings.HasPrefix(arg, "--session="):
			sessionID = strings.TrimPrefix(arg, "--session=")
			if sessionID == "" {
				fmt.Fprintln(stderr, "herder show: --session requires an id")
				return 2
			}
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(stderr, "herder show: unknown flag %q\n", arg)
			return 2
		case name == "":
			name = arg
		default:
			fmt.Fprintf(stderr, "herder show: unexpected argument %q\n", arg)
			return 2
		}
	}
	if name != "" && sessionID != "" {
		fmt.Fprintln(stderr, "herder show: agent name and --session are mutually exclusive")
		return 2
	}
	if name == "" && sessionID == "" {
		fmt.Fprint(stderr, "herder show: an agent name or --session is required\n"+help)
		return 2
	}
	var rows []hcomidentity.Row
	rows, rosterErr := deps.roster()
	if rosterErr == nil {
		rows = hcomidentity.WithParents(rows)
	}
	if sessionID != "" {
		if rosterErr != nil {
			fmt.Fprintf(stderr, "herder show: cannot read live hcom roster: %v\n", rosterErr)
			return 1
		}
		for i := range rows {
			if rows[i].SessionID == sessionID {
				name = rows[i].Name
				break
			}
		}
		if name == "" {
			fmt.Fprintf(stderr, "herder show: session %q not found\n", sessionID)
			return 1
		}
	}
	stateDir, err := herderstate.Dir()
	if err != nil {
		fmt.Fprintf(stderr, "herder show: cannot resolve state dir: %v\n", err)
		return 1
	}
	store := agentstore.Open(stateDir, stderr)
	var proj *agentstore.Projection
	if store.ImportErr != nil {
		fmt.Fprintf(stderr, "herder show: agent store unavailable (%v); showing %s as unregistered\n", store.ImportErr, name)
		proj = agentstore.NewProjection()
	} else if proj, err = store.Load(); err != nil {
		fmt.Fprintf(stderr, "herder show: cannot read agent store (%v); showing %s as unregistered\n", err, name)
		proj = agentstore.NewProjection()
	}
	var rosterRow *hcomidentity.Row
	if rosterErr != nil {
		fmt.Fprintf(stderr, "herder show: live hcom roster unavailable (%v); store-only view\n", rosterErr)
	} else {
		for i := range rows {
			if rows[i].Name == name {
				rosterRow = &rows[i]
				break
			}
		}
	}
	view := proj.View(name, rosterRow)
	if view == nil {
		view = &agentstore.AgentView{Name: name, Provenance: agentstore.Provenance{Kind: "unregistered"}, Events: []agentstore.Event{}}
		if rosterRow != nil {
			view.Tool, view.Tag, view.BaseName, view.Incarnation = rosterRow.Tool, rosterRow.Tag, rosterRow.BaseName, rosterRow.CreatedAt
			if rosterRow.SessionID != "" {
				view.Session = &agentstore.SessionView{SessionID: rosterRow.SessionID, Tool: rosterRow.Tool, Path: rosterRow.TranscriptPath}
			}
		}
	}
	var outputVitals showVitals
	var vitalsErr string
	if rosterRow != nil && deps.vitals != nil {
		// This is today's on-demand reverse transcript scan. Once the
		// observer/daemon exists, it becomes a central context cache read over the
		// local socket; the transcript remains the authority behind that cache.
		vitals, path, observedAt, readErr := deps.vitals(*rosterRow)
		outputVitals.Model, outputVitals.ContextUsage, outputVitals.SessionFile = vitals.Model, vitals.ContextUsage, path
		if !observedAt.IsZero() {
			observedAt = observedAt.UTC()
			outputVitals.ObservedAt = &observedAt
		}
		if readErr != nil {
			vitalsErr = readErr.Error()
		}
	}
	if asJSON {
		encoded, err := json.MarshalIndent(showOutput{AgentView: view, Vitals: outputVitals, VitalsError: vitalsErr}, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "herder show: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s\n", encoded)
		return 0
	}
	writeText(stdout, view, outputVitals)
	return 0
}

const help = `herder show — the agent store's view of one agent.

Usage:
  herder show <name> [--json]
  herder show --session <id> [--json]

Prints provenance (launcher, requested model/effort/placement), the mutable
manager pointer, assignment, annotation, session history and the last 32
store events and live model/context vitals. Folds the live hcom roster when
available (binding: verified, pending or conflict); the roster's session is
always current. --session matches that current roster session exactly.
`

func writeText(out io.Writer, v *agentstore.AgentView, vitals showVitals) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	field := func(label, value string) {
		if value == "" {
			value = "-"
		}
		fmt.Fprintf(w, "%s\t%s\n", label, value)
	}
	stamp := func(t *time.Time) string {
		if t == nil || t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}
	field("name", v.Name)
	field("tool", v.Tool)
	field("tag", v.Tag)
	field("incarnation", stamp(&v.Incarnation))
	pr := v.Provenance
	field("provenance", pr.Kind)
	field("launcher", launcherLabel(pr))
	field("launcher_kind", pr.LauncherKind)
	field("manager", v.Manager)
	if v.ManagerAt != nil {
		field("manager_set", stamp(v.ManagerAt)+" by "+v.ManagerBy)
	}
	field("state", pr.State)
	field("model_requested", pr.ModelRequested)
	field("effort", pr.Effort)
	field("workspace", pr.Workspace)
	field("pane_requested", pr.PaneRequested)
	field("pane", pr.Pane)
	field("cwd", pr.Cwd)
	field("batch", pr.Batch)
	field("request", pr.Request)
	field("requested_at", stamp(pr.RequestedAt))
	field("ready_at", stamp(pr.ReadyAt))
	field("parent", v.Parent)
	field("from_name", v.FromName)
	if v.Closed != nil {
		field("closed", stamp(v.Closed)+" "+v.CloseReason)
	}
	if v.Assignment != nil {
		a := v.Assignment
		field("mission", a.Mission)
		field("brief", a.Brief)
		field("thread", a.Thread)
		field("task", a.Task)
		field("assigned", stamp(&a.At)+" by "+a.By)
	} else {
		field("mission", "")
	}
	if v.Annotation != nil {
		field("title", v.Annotation.Title)
		field("note", v.Annotation.Note)
	}
	if v.Binding != nil {
		b := v.Binding
		switch b.State {
		case "conflict":
			field("binding", fmt.Sprintf("conflict {claimed: %s, roster: %s}", b.Claimed, b.Roster))
		default:
			field("binding", b.State)
		}
	}
	if v.Session != nil {
		field("session", v.Session.SessionID)
	} else {
		field("session", "")
	}
	_ = w.Flush()
	fmt.Fprintln(out, "vitals:")
	w = tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	field = func(label, value string) {
		if value == "" {
			value = "-"
		}
		fmt.Fprintf(w, "  %s\t%s\n", label, value)
	}
	field("model", vitals.Model)
	if vitals.ContextUsage == nil {
		field("context_used", "")
		field("context_window", "")
		field("context_percent", "")
	} else {
		field("context_used", tokenCount(vitals.ContextUsage.UsedTokens))
		if vitals.ContextUsage.WindowTokens == nil {
			field("context_window", "")
		} else {
			field("context_window", tokenCount(*vitals.ContextUsage.WindowTokens))
		}
		if vitals.ContextUsage.UsedPercent == nil {
			field("context_percent", "")
		} else {
			field("context_percent", fmt.Sprintf("%.0f%% used", *vitals.ContextUsage.UsedPercent))
		}
	}
	if vitals.ObservedAt == nil {
		field("observed_at", "")
	} else {
		field("observed_at", vitals.ObservedAt.UTC().Format(time.RFC3339))
	}
	field("session_file", vitals.SessionFile)
	_ = w.Flush()
	if len(v.Sessions) > 0 {
		fmt.Fprintln(out, "\nsessions (newest first):")
		for _, s := range v.Sessions {
			end := "open"
			if s.Ended != nil {
				end = stamp(s.Ended) + " " + s.EndReason
			}
			fmt.Fprintf(out, "  %s\t%s\t%s\t%s\n", s.SessionID, s.Tool, stamp(s.Started), end)
		}
	}
	fmt.Fprintf(out, "\nevents (last %d of %d):\n", len(v.Events), v.EventCount)
	for _, e := range v.Events {
		fmt.Fprintf(out, "  %s  %-18s by %s  %s\n", e.At.UTC().Format(time.RFC3339), e.Kind, e.By, summary(e))
	}
}

func tokenCount(value int64) string {
	if value <= 0 {
		return ""
	}
	if value < 1000 {
		return fmt.Sprintf("%d tokens", value)
	}
	return fmt.Sprintf("%dk tokens", (value+500)/1000)
}

func launcherLabel(pr agentstore.Provenance) string {
	switch pr.Kind {
	case "registered":
		return pr.Launcher
	case "mirrored":
		return "mirrored: " + pr.Launcher
	default:
		return "unregistered"
	}
}

func summary(e agentstore.Event) string {
	var parts []string
	add := func(k, v string) {
		if v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	add("request", e.Request)
	add("tool", e.Tool)
	add("model", e.Model)
	add("pane", e.Pane)
	add("session", e.Session)
	add("reason", e.Reason)
	add("from", e.FromName)
	add("mission", e.Mission)
	add("manager", e.Manager)
	add("title", e.Title)
	if e.Placement != nil {
		add("workspace", e.Placement.Workspace)
		add("split_from", e.Placement.SplitFrom)
		add("pane", e.Placement.Pane)
	}
	return strings.Join(parts, " ")
}

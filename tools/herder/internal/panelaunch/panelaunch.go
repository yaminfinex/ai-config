// Package panelaunch adapts herder's spawner to herdr 0.7.5, which split pane
// creation away from `herdr agent start`. Under 0.7.5 `agent start` starts a
// supported agent KIND in an EXISTING pane and no longer creates one, so the
// spawner now creates the pane itself (a fresh tab or a split), runs the
// login-shell wrapper in it via `pane run`, and reports the agent working via
// `pane report-agent`. This is the single seam both spawners (spawncmd and
// lifecyclecmd) share, so future herdr drift on pane creation lands here.
package panelaunch

import (
	"fmt"
	"strings"

	"ai-config/tools/herder/internal/herdrcli"
)

// Client is the slice of the herdr CLI this package drives. Both spawners'
// herdrClient satisfy it.
type Client interface {
	Combined(args ...string) ([]byte, int, error)
}

// Spec is the placement plus launch intent for one agent pane.
type Spec struct {
	Label     string   // herdr label; also report-agent --agent
	CWD       string   // child working directory (empty lets herdr pick)
	Workspace string   // target workspace: tab create --workspace; split falls back to pane move --workspace
	Tab       string   // existing tab id to place into (split a pane already in it)
	BasePane  string   // anchor pane for a split (falls back to --current)
	Split     string   // right|down
	NewTab    bool     // fresh tab — the dominant path
	FocusFlag string   // --focus | --no-focus (default --no-focus)
	Argv      []string // command tokens run in the pane (the login-shell wrapper)
	Source    string   // report-agent --source (default herder:spawn)
}

// Launch creates the pane, runs Argv in it, and reports the agent working. On
// failure it returns the pane created so far (PaneID empty if creation itself
// failed) so callers can tear it down.
func Launch(c Client, spec Spec) (herdrcli.Pane, error) {
	pane, err := createPane(c, spec)
	if err != nil {
		return pane, err
	}
	if pane.PaneID == "" {
		return pane, fmt.Errorf("herdr pane creation returned no pane id")
	}
	runArgs := append([]string{"pane", "run", pane.PaneID}, spec.Argv...)
	if out, rc, runErr := c.Combined(runArgs...); runErr != nil || rc != 0 {
		return pane, fmt.Errorf("herdr pane run exited %d: %s", rc, compact(out, runErr))
	}
	// Report the agent working immediately so the seat shows live without
	// waiting for the sidecar's first tick. Best-effort: the sidecar keeps
	// reporting under its own source, so a transient failure here is not fatal.
	_, _, _ = c.Combined("pane", "report-agent", pane.PaneID,
		"--source", firstNonEmpty(spec.Source, "herder:spawn"),
		"--agent", spec.Label,
		"--state", "working")
	return refresh(c, pane), nil
}

func createPane(c Client, spec Spec) (herdrcli.Pane, error) {
	if spec.NewTab {
		args := []string{"tab", "create"}
		if spec.Workspace != "" {
			args = append(args, "--workspace", spec.Workspace)
		}
		if spec.CWD != "" {
			args = append(args, "--cwd", spec.CWD)
		}
		args = append(args, "--label", spec.Label, focus(spec.FocusFlag))
		out, rc, err := c.Combined(args...)
		if err != nil || rc != 0 {
			return herdrcli.Pane{}, fmt.Errorf("herdr tab create exited %d: %s", rc, compact(out, err))
		}
		pane, perr := herdrcli.ParseTabCreateRootPane(out)
		if perr != nil {
			return herdrcli.Pane{}, fmt.Errorf("herdr tab create payload: %w", perr)
		}
		return pane, nil
	}

	// Split path. Placing "into a tab" (an explicit --tab, or --worktree's seed
	// tab) means splitting a pane already in that tab, so the tab wins over any
	// caller anchor; otherwise anchor on the explicit base pane, else the
	// current pane. pane split has no --workspace, so a specific workspace
	// request is honored with a follow-up pane move.
	var base string
	if spec.Tab != "" {
		base = firstPaneInTab(c, spec.Tab)
		if base == "" {
			return herdrcli.Pane{}, fmt.Errorf("no live pane found in tab %s to split into", spec.Tab)
		}
	} else {
		base = spec.BasePane
	}
	args := []string{"pane", "split"}
	if base != "" {
		args = append(args, base)
	} else {
		args = append(args, "--current")
	}
	if spec.Split != "" {
		args = append(args, "--direction", spec.Split)
	}
	if spec.CWD != "" {
		args = append(args, "--cwd", spec.CWD)
	}
	args = append(args, focus(spec.FocusFlag))
	out, rc, err := c.Combined(args...)
	if err != nil || rc != 0 {
		return herdrcli.Pane{}, fmt.Errorf("herdr pane split exited %d: %s", rc, compact(out, err))
	}
	pane, perr := herdrcli.ParsePaneGet(out) // pane split returns .result.pane
	if perr != nil {
		return herdrcli.Pane{}, fmt.Errorf("herdr pane split payload: %w", perr)
	}
	if spec.Workspace != "" && pane.WorkspaceID != spec.Workspace {
		mvOut, mvRC, mvErr := c.Combined("pane", "move", pane.PaneID, "--workspace", spec.Workspace, focus(spec.FocusFlag))
		if mvErr != nil || mvRC != 0 {
			return pane, fmt.Errorf("herdr pane move --workspace exited %d: %s", mvRC, compact(mvOut, mvErr))
		}
		pane = refresh(c, pane)
	}
	return pane, nil
}

func firstPaneInTab(c Client, tabID string) string {
	out, rc, err := c.Combined("pane", "list")
	if err != nil || rc != 0 {
		return ""
	}
	panes, perr := herdrcli.ParsePaneList(out)
	if perr != nil {
		return ""
	}
	for _, p := range panes {
		if p.TabID == tabID {
			return p.PaneID
		}
	}
	return ""
}

// refresh re-reads the pane so coordinates reflect any post-create move,
// merging non-empty fields over what creation already reported. On any error it
// keeps the pane it has (creation's payload is authoritative until proven
// otherwise), so a partial or failed pane get never blanks a known coordinate.
func refresh(c Client, pane herdrcli.Pane) herdrcli.Pane {
	out, rc, err := c.Combined("pane", "get", pane.PaneID)
	if err != nil || rc != 0 {
		return pane
	}
	got, perr := herdrcli.ParsePaneGet(out)
	if perr != nil || got.PaneID == "" {
		return pane
	}
	pane.PaneID = firstNonEmpty(got.PaneID, pane.PaneID)
	pane.TerminalID = firstNonEmpty(got.TerminalID, pane.TerminalID)
	pane.WorkspaceID = firstNonEmpty(got.WorkspaceID, pane.WorkspaceID)
	pane.TabID = firstNonEmpty(got.TabID, pane.TabID)
	pane.CWD = firstNonEmpty(got.CWD, pane.CWD)
	return pane
}

func focus(flag string) string {
	if flag == "" {
		return "--no-focus"
	}
	return flag
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func compact(out []byte, err error) string {
	msg := strings.TrimSpace(strings.ReplaceAll(string(out), "\n", " "))
	if msg == "" && err != nil {
		return err.Error()
	}
	return msg
}

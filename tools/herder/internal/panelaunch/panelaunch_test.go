package panelaunch

import (
	"fmt"
	"strings"
	"testing"
)

// recorder is a scriptable herdr stub that records every Combined call and
// answers per command prefix.
type recorder struct {
	calls    []string
	tabCreate func() ([]byte, int, error)
	paneSplit func() ([]byte, int, error)
	paneList  func() ([]byte, int, error)
	paneGet   func() ([]byte, int, error)
	fallback  func() ([]byte, int, error)
}

func (r *recorder) Combined(args ...string) ([]byte, int, error) {
	r.calls = append(r.calls, strings.Join(args, " "))
	switch {
	case len(args) >= 2 && args[0] == "tab" && args[1] == "create" && r.tabCreate != nil:
		return r.tabCreate()
	case len(args) >= 2 && args[0] == "pane" && args[1] == "split" && r.paneSplit != nil:
		return r.paneSplit()
	case len(args) >= 2 && args[0] == "pane" && args[1] == "list" && r.paneList != nil:
		return r.paneList()
	case len(args) >= 2 && args[0] == "pane" && args[1] == "get" && r.paneGet != nil:
		return r.paneGet()
	case r.fallback != nil:
		return r.fallback()
	}
	return []byte(`{"result":{"type":"ok"}}`), 0, nil
}

func (r *recorder) called(prefix string) bool {
	for _, c := range r.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func ok() ([]byte, int, error) { return []byte(`{"result":{"type":"ok"}}`), 0, nil }

func TestLaunchNewTabUsesTabCreateNotPaneMove(t *testing.T) {
	rec := &recorder{
		tabCreate: func() ([]byte, int, error) {
			return []byte(`{"result":{"type":"tab_created","root_pane":{"pane_id":"p1","terminal_id":"t1","workspace_id":"ws1","tab_id":"tab1","cwd":"/repo"},"tab":{"tab_id":"tab1"}}}`), 0, nil
		},
		paneGet: ok,
	}
	pane, err := Launch(rec, Spec{Label: "worker", CWD: "/repo", Workspace: "ws1", NewTab: true, Argv: []string{"bash", "-lic", "exec claude"}})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if pane.PaneID != "p1" || pane.WorkspaceID != "ws1" || pane.TabID != "tab1" {
		t.Fatalf("pane = %+v", pane)
	}
	if !rec.called("tab create --workspace ws1 --cwd /repo --label worker --no-focus") {
		t.Fatalf("tab create not issued with workspace/cwd/label/focus: %v", rec.calls)
	}
	if rec.called("pane move") {
		t.Fatalf("new-tab path must not issue a separate pane move: %v", rec.calls)
	}
	if !rec.called("pane run p1 bash -lic exec claude") {
		t.Fatalf("wrapper not run in the created pane: %v", rec.calls)
	}
	if !rec.called("pane report-agent p1 --source herder:spawn --agent worker --state working") {
		t.Fatalf("initial working report not emitted: %v", rec.calls)
	}
}

func TestLaunchSplitAnchorsOnBasePane(t *testing.T) {
	rec := &recorder{
		paneSplit: func() ([]byte, int, error) {
			return []byte(`{"result":{"type":"pane_info","pane":{"pane_id":"p2","terminal_id":"t2","workspace_id":"wsA","tab_id":"tab2"}}}`), 0, nil
		},
		paneGet: ok,
	}
	pane, err := Launch(rec, Spec{Label: "w", Split: "down", BasePane: "p_self", FocusFlag: "--focus", Argv: []string{"bash", "-lic", "x"}, Source: "herder:resume"})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if pane.PaneID != "p2" {
		t.Fatalf("pane = %+v", pane)
	}
	if !rec.called("pane split p_self --direction down --cwd") && !rec.called("pane split p_self --direction down --focus") {
		t.Fatalf("split did not anchor on base pane with direction: %v", rec.calls)
	}
	if !rec.called("pane report-agent p2 --source herder:resume --agent w --state working") {
		t.Fatalf("report used wrong source/label: %v", rec.calls)
	}
}

func TestLaunchSplitFallsBackToCurrent(t *testing.T) {
	rec := &recorder{
		paneSplit: func() ([]byte, int, error) {
			return []byte(`{"result":{"pane":{"pane_id":"p3","terminal_id":"t3","workspace_id":"wsB"}}}`), 0, nil
		},
		paneGet: ok,
	}
	if _, err := Launch(rec, Spec{Label: "w", Split: "right", Argv: []string{"bash"}}); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if !rec.called("pane split --current --direction right") {
		t.Fatalf("no base pane must fall back to --current: %v", rec.calls)
	}
}

func TestLaunchExistingTabSplitsPaneInThatTab(t *testing.T) {
	rec := &recorder{
		paneList: func() ([]byte, int, error) {
			return []byte(`{"result":{"panes":[{"pane_id":"seed","tab_id":"twork"},{"pane_id":"other","tab_id":"tother"}]}}`), 0, nil
		},
		paneSplit: func() ([]byte, int, error) {
			return []byte(`{"result":{"pane":{"pane_id":"p4","terminal_id":"t4","workspace_id":"wtws","tab_id":"twork"}}}`), 0, nil
		},
		paneGet: ok,
	}
	// BasePane is the caller's pane in another workspace; the tab must win.
	if _, err := Launch(rec, Spec{Label: "w", Tab: "twork", Workspace: "wtws", BasePane: "caller", Split: "right", Argv: []string{"env", "X=1", "claude"}}); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if !rec.called("pane split seed --direction right") {
		t.Fatalf("existing tab must split a pane in that tab (seed), not the caller: %v", rec.calls)
	}
}

func TestLaunchSplitMovesToWorkspaceWhenMismatched(t *testing.T) {
	moved := false
	rec := &recorder{
		paneSplit: func() ([]byte, int, error) {
			return []byte(`{"result":{"pane":{"pane_id":"p5","terminal_id":"t5","workspace_id":"wrong"}}}`), 0, nil
		},
		paneGet: ok,
		fallback: func() ([]byte, int, error) { return ok() },
	}
	// Intercept the move via the recorder's call log rather than a dedicated hook.
	_, err := Launch(rec, Spec{Label: "w", Split: "right", BasePane: "b", Workspace: "want", Argv: []string{"bash"}})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	for _, c := range rec.calls {
		if strings.HasPrefix(c, "pane move p5 --workspace want") {
			moved = true
		}
	}
	if !moved {
		t.Fatalf("split landing in the wrong workspace must be moved: %v", rec.calls)
	}
}

func TestLaunchTabCreateFailurePropagates(t *testing.T) {
	rec := &recorder{
		tabCreate: func() ([]byte, int, error) {
			return []byte(`{"error":{"code":"boom","message":"no workspace"}}`), 64, nil
		},
	}
	pane, err := Launch(rec, Spec{Label: "w", NewTab: true, Argv: []string{"bash"}})
	if err == nil {
		t.Fatal("Launch() error = nil, want tab create failure")
	}
	if pane.PaneID != "" {
		t.Fatalf("failed creation must not yield a pane: %+v", pane)
	}
	if rec.called("pane run") {
		t.Fatalf("must not run a wrapper after creation failure: %v", rec.calls)
	}
	if !strings.Contains(err.Error(), "tab create") {
		t.Fatalf("error = %v, want tab create context", err)
	}
}

func TestLaunchPaneRunFailureReturnsCreatedPane(t *testing.T) {
	rec := &recorder{
		tabCreate: func() ([]byte, int, error) {
			return []byte(`{"result":{"root_pane":{"pane_id":"p6","terminal_id":"t6"},"tab":{"tab_id":"tab6"}}}`), 0, nil
		},
		fallback: func() ([]byte, int, error) {
			return []byte(`{"error":{"message":"run refused"}}`), 1, nil
		},
	}
	pane, err := Launch(rec, Spec{Label: "w", NewTab: true, Argv: []string{"bash"}})
	if err == nil {
		t.Fatal("Launch() error = nil, want pane run failure")
	}
	// The caller needs the created pane id to tear it down.
	if pane.PaneID != "p6" {
		t.Fatalf("pane = %+v, want created p6 for cleanup", pane)
	}
	if !strings.Contains(err.Error(), "pane run") {
		t.Fatalf("error = %v, want pane run context", err)
	}
}

func TestLaunchExistingTabWithNoLivePaneFails(t *testing.T) {
	rec := &recorder{
		paneList: func() ([]byte, int, error) {
			return []byte(`{"result":{"panes":[{"pane_id":"x","tab_id":"different"}]}}`), 0, nil
		},
	}
	_, err := Launch(rec, Spec{Label: "w", Tab: "missing", Split: "right", Argv: []string{"bash"}})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("tab %s", "missing")) {
		t.Fatalf("error = %v, want no-pane-in-tab failure", err)
	}
}

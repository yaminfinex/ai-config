package herdrcli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeHerdr writes an executable that prints script-controlled stdout/stderr
// and exits with the given code, standing in for the herdr CLI the same way
// the hermetic suites' mocks do.
func fakeHerdr(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake herdr is a shell script")
	}
	path := filepath.Join(t.TempDir(), "herdr")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOutputDiscardsStderr(t *testing.T) {
	c := &Client{Bin: fakeHerdr(t, `echo '{"result":{}}'; echo noise >&2`)}
	out, err := c.Output("agent", "list")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "{\"result\":{}}\n" {
		t.Fatalf("stdout = %q, want payload without stderr noise", out)
	}
}

func TestOutputNonZeroExitReturnsError(t *testing.T) {
	c := &Client{Bin: fakeHerdr(t, `echo partial; exit 3`)}
	out, err := c.Output("agent", "list")
	if err == nil {
		t.Fatal("want error on exit 3")
	}
	if string(out) != "partial\n" {
		t.Fatalf("stdout on failure = %q, want partial output preserved", out)
	}
}

func TestCombinedInterleavesAndReportsExitCode(t *testing.T) {
	c := &Client{Bin: fakeHerdr(t, `echo out; echo err >&2; exit 5`)}
	out, code, err := c.Combined("pane", "get", "p_1")
	if err != nil {
		t.Fatal(err)
	}
	if code != 5 {
		t.Fatalf("exit code = %d, want 5", code)
	}
	if string(out) != "out\nerr\n" {
		t.Fatalf("combined = %q, want stdout+stderr", out)
	}
}

func TestRunReturnsExitCodeOnly(t *testing.T) {
	c := &Client{Bin: fakeHerdr(t, `exit 1`)}
	code, err := c.Run("wait", "agent-status", "p_1")
	if err != nil {
		t.Fatal(err)
	}
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestMissingBinary(t *testing.T) {
	c := &Client{Bin: filepath.Join(t.TempDir(), "no-such-herdr")}
	if c.Available() {
		t.Error("Available() = true for missing binary")
	}
	if _, code, err := c.Combined("agent", "list"); err == nil || code != -1 {
		t.Errorf("Combined on missing binary = (code %d, err %v), want (-1, error)", code, err)
	}
}

func TestParseAgentList(t *testing.T) {
	payload := []byte(`{"id":1,"result":{"agents":[
		{"terminal_id":"term_A","pane_id":"p_1","agent_status":"idle","extra":"kept"},
		{"terminal_id":null,"pane_id":"p_2","agent_status":"working"},
		{"pane_id":"p_3","agent_status":"working"}
	]}}`)
	agents, err := ParseAgentList(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 3 {
		t.Fatalf("got %d agents, want 3", len(agents))
	}
	if agents[0].TerminalID == nil || *agents[0].TerminalID != "term_A" ||
		agents[0].PaneID != "p_1" || agents[0].Status != "idle" {
		t.Errorf("agents[0] = %+v", agents[0])
	}
	// reconcile's `select(.terminal_id != null)` needs null and missing to
	// stay distinguishable from "".
	if agents[1].TerminalID != nil || agents[2].TerminalID != nil {
		t.Errorf("null/missing terminal_id decoded non-nil: %+v, %+v", agents[1], agents[2])
	}
	// Raw carries the whole object — reconcile embeds it as `live` verbatim.
	if want := `"extra":"kept"`; !strings.Contains(string(agents[0].Raw), want) {
		t.Errorf("agents[0].Raw = %s, want member %s preserved", agents[0].Raw, want)
	}
}

func TestParseAgentListEmptyShapes(t *testing.T) {
	// `.result.agents[]?` tolerates every one of these without erroring.
	for _, payload := range []string{
		`{"result":{"agents":[]}}`,
		`{"result":{}}`,
		`{"result":{"agents":null}}`,
		`{}`,
	} {
		agents, err := ParseAgentList([]byte(payload))
		if err != nil {
			t.Errorf("ParseAgentList(%s) error: %v", payload, err)
		}
		if len(agents) != 0 {
			t.Errorf("ParseAgentList(%s) = %+v, want empty", payload, agents)
		}
	}
	if _, err := ParseAgentList([]byte("not json")); err == nil {
		t.Error("want error on invalid JSON")
	}
}

func TestParsePaneListAndGet(t *testing.T) {
	panes, err := ParsePaneList([]byte(`{"result":{"panes":[
		{"pane_id":"pane_7","terminal_id":"term_A"},
		{"pane_id":"pane_8","terminal_id":"term_B"}
	]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 2 || panes[0].PaneID != "pane_7" || panes[1].TerminalID != "term_B" {
		t.Errorf("panes = %+v", panes)
	}

	pane, err := ParsePaneGet([]byte(`{"result":{"pane":{"pane_id":"pane_9","terminal_id":"term_C","workspace_id":"ws_1","tab_id":"tab_2"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if pane.PaneID != "pane_9" || pane.TerminalID != "term_C" || pane.WorkspaceID != "ws_1" || pane.TabID != "tab_2" {
		t.Errorf("pane = %+v", pane)
	}
	// Error payloads have no .result.pane; `// empty` becomes the zero Pane.
	pane, err = ParsePaneGet([]byte(`{"error":{"message":"pane not found"}}`))
	if err != nil || pane != (Pane{}) {
		t.Errorf("ParsePaneGet(error payload) = (%+v, %v), want zero Pane", pane, err)
	}
}

func TestParseWorkspaceTabAgentStart(t *testing.T) {
	wss, err := ParseWorkspaceList([]byte(`{"result":{"workspaces":[{"workspace_id":"ws_1"},{"workspace_id":"ws_2"}]}}`))
	if err != nil || len(wss) != 2 || wss[1].WorkspaceID != "ws_2" {
		t.Errorf("workspaces = (%+v, %v)", wss, err)
	}

	tab, err := ParseTabCreate([]byte(`{"result":{"tab":{"tab_id":"tab_3"}}}`))
	if err != nil || tab.TabID != "tab_3" {
		t.Errorf("tab = (%+v, %v)", tab, err)
	}

	start, err := ParseAgentStart([]byte(`{"id":4,"result":{
		"agent":{"pane_id":"pane_5","workspace_id":"ws_1","tab_id":"tab_3","terminal_id":"term_D"},
		"argv":["zsh","-lc","exec claude"],
		"type":"agent_started"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if start.Agent.PaneID != "pane_5" || start.Agent.TerminalID != "term_D" ||
		start.Type != "agent_started" || len(start.Argv) != 3 {
		t.Errorf("start = %+v", start)
	}
}

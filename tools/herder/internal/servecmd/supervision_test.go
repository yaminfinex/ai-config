package servecmd

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/fleetview"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"github.com/fsnotify/fsnotify"
)

// supervisionDeps is a fleet with every row source the board folds: two
// placed panes, an unplaced row, a Task subagent, a terminal pane; a store
// with a human-launched root, an agent-launched seat, a culled manager, a
// base-name manager and an annotation title.
func supervisionDeps(t *testing.T) dependencies {
	t.Helper()
	deps := fixtureDeps()
	created := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	deps.snapshot = func() (herdrcli.Snapshot, error) {
		return herdrcli.Snapshot{
			Workspaces: []herdrcli.Workspace{{WorkspaceID: "w1", Label: "repo", TabCount: 1, PaneCount: 3}},
			Tabs:       []herdrcli.Tab{{TabID: "t1", WorkspaceID: "w1", Label: "agents", PaneCount: 3}},
			Panes: []herdrcli.Pane{
				{PaneID: "p1", WorkspaceID: "w1", TabID: "t1", Agent: "claude", AgentStatus: "working", AgentSession: "s-ziru"},
				{PaneID: "p2", WorkspaceID: "w1", TabID: "t1", Agent: "claude", AgentStatus: "working", AgentSession: "s-kolo"},
				{PaneID: "p3", WorkspaceID: "w1", TabID: "t1"},
			},
			Agents: []herdrcli.Agent{{PaneID: "p1", Name: "ziru", Agent: "claude", Status: "working"}, {PaneID: "p2", Name: "impl-kolo", Agent: "claude", Status: "working"}},
		}, nil
	}
	deps.roster = func() ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{
			{Name: "ziru", BaseName: "ziru", Tool: "claude", Status: "listening", SessionID: "s-ziru", CreatedAt: created, LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"}},
			{Name: "impl-kolo", BaseName: "kolo", Tool: "claude", Status: "active", SessionID: "s-kolo", CreatedAt: created.Add(time.Minute), LaunchContext: hcomidentity.LaunchContext{PaneID: "p2"}},
			{Name: "kolo_general_purpose_1", BaseName: "kolo_general_purpose_1", ParentName: "kolo", AgentID: "a1", Tool: "claude", Status: "active", CreatedAt: created.Add(2 * time.Minute)},
			{Name: "sesh-nabi", BaseName: "nabi", Tool: "claude", Status: "listening", SessionID: "s-nabi", CreatedAt: created.Add(3 * time.Minute)},
			{Name: "orch-hamo", BaseName: "hamo", Tool: "claude", Status: "listening", SessionID: "s-hamo", CreatedAt: created.Add(4 * time.Minute)},
			{Name: "sesh-kele", BaseName: "kele", Tool: "claude", Status: "listening", SessionID: "s-kele", CreatedAt: created.Add(5 * time.Minute)},
			{Name: "lone", BaseName: "lone", Tool: "codex", Status: "listening", SessionID: "s-lone", CreatedAt: created.Add(6 * time.Minute)},
		}, nil
	}
	state := t.TempDir()
	store := agentstore.Open(state, nil)
	at := created.Add(10 * time.Minute)
	events := []agentstore.Event{
		{ID: agentstore.DerivedID([]byte("sup-e1")), At: at, Kind: agentstore.KindLaunchReady, Name: "ziru", By: "ubuntu", ByKind: "user"},
		{ID: agentstore.DerivedID([]byte("sup-e2")), At: at.Add(time.Second), Kind: agentstore.KindLaunchReady, Name: "impl-kolo", By: "ziru", ByKind: "agent"},
		{ID: agentstore.DerivedID([]byte("sup-e3")), At: at.Add(2 * time.Second), Kind: agentstore.KindAnnotate, Name: "impl-kolo", By: "ziru", ByKind: "agent", Title: "payload builder"},
		{ID: agentstore.DerivedID([]byte("sup-e4")), At: at.Add(3 * time.Second), Kind: agentstore.KindLaunchReady, Name: "orch-dead", By: "ziru", ByKind: "agent"},
		{ID: agentstore.DerivedID([]byte("sup-e5")), At: at.Add(4 * time.Second), Kind: agentstore.KindLaunchReady, Name: "sesh-nabi", By: "orch-dead", ByKind: "agent"},
		{ID: agentstore.DerivedID([]byte("sup-e6")), At: at.Add(5 * time.Second), Kind: agentstore.KindCulled, Name: "orch-dead", Pane: "p9", Close: "managed", By: "ziru", ByKind: "agent"},
		{ID: agentstore.DerivedID([]byte("sup-e7")), At: at.Add(6 * time.Second), Kind: agentstore.KindLaunchReady, Name: "sesh-kele", By: "hamo", ByKind: "agent"},
		{ID: agentstore.DerivedID([]byte("sup-e8")), At: at.Add(7 * time.Second), Kind: agentstore.KindLaunchReady, Name: "kolo_general_purpose_1", By: "impl-kolo", ByKind: "agent"},
	}
	for _, event := range events {
		if _, err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	deps.store = store
	return deps
}

func TestBoardCarriesManager(t *testing.T) {
	deps := supervisionDeps(t)
	board, err := readBoard(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	panes := board.Workspaces[0].Tabs[0].Panes
	if len(panes) != 3 {
		t.Fatalf("panes = %d", len(panes))
	}
	type edge struct{ manager, state, title, created string }
	got := map[string]edge{}
	for _, pane := range panes {
		got[pane.Agent] = edge{pane.Manager, pane.ManagerState, pane.Title, pane.CreatedAt}
		for _, child := range pane.Subagents {
			got[child.Agent] = edge{child.Manager, child.ManagerState, child.Title, child.CreatedAt}
		}
	}
	for _, row := range board.Unplaced {
		got[row.Agent] = edge{row.Manager, row.ManagerState, row.Title, row.CreatedAt}
	}
	want := map[string]edge{
		"ziru":                   {"operator", fleetview.ManagerOperator, "", "2026-09-10T08:00:00Z"},
		"impl-kolo":              {"ziru", fleetview.ManagerLive, "payload builder", "2026-09-10T08:01:00Z"},
		"kolo_general_purpose_1": {"impl-kolo", fleetview.ManagerLive, "", "2026-09-10T08:02:00Z"},
		"sesh-nabi":              {"orch-dead", fleetview.ManagerEnded, "", "2026-09-10T08:03:00Z"},
		"orch-hamo":              {"", fleetview.ManagerUnknown, "", "2026-09-10T08:04:00Z"},
		"sesh-kele":              {"orch-hamo", fleetview.ManagerLive, "", "2026-09-10T08:05:00Z"},
		"lone":                   {"", fleetview.ManagerUnknown, "", "2026-09-10T08:06:00Z"},
		"-":                      {},
	}
	for name, expected := range want {
		if got[name] != expected {
			t.Errorf("%s = %#v, want %#v", name, got[name], expected)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %#v", got)
	}
	encoded, _ := json.Marshal(board)
	for _, forbidden := range []string{`"launcher"`, `"mission"`, `"provenance"`, `"binding"`, `"vitals"`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("board leaks %s: %s", forbidden, encoded)
		}
	}
}

func TestBoardWithoutStoreFoldsUnknown(t *testing.T) {
	board, err := readBoard(context.Background(), fixtureDeps())
	if err != nil {
		t.Fatal(err)
	}
	pane := board.Workspaces[0].Tabs[0].Panes[0]
	if pane.Manager != "" || pane.ManagerState != fleetview.ManagerUnknown {
		t.Fatalf("pane = %#v", pane)
	}
}

func TestStoreAppendReemitsFleet(t *testing.T) {
	deps := supervisionDeps(t)
	deps.poll = time.Hour
	deps.fileWatcher = fsnotify.NewWatcher
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	for {
		event, data := readEvent(t, reader)
		if event == "fleet" {
			if !strings.Contains(data, `"manager":"ziru","manager_state":"live","title":"payload builder"`) || !strings.Contains(data, `"manager":"operator","manager_state":"operator"`) {
				t.Fatalf("initial fleet = %s", data)
			}
			break
		}
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := deps.store.Append(agentstore.Event{ID: agentstore.DerivedID([]byte("sup-e9")), At: time.Now().UTC(), Kind: agentstore.KindReparent, Name: "impl-kolo", Manager: "sesh-nabi", By: "ziru", ByKind: "agent"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	got := make(chan string, 1)
	go func() {
		for {
			event, data := readEvent(t, reader)
			if event == "fleet" {
				got <- data
				return
			}
		}
	}()
	select {
	case data := <-got:
		if !strings.Contains(data, `"agent":"impl-kolo"`) || !strings.Contains(data, `"manager":"sesh-nabi","manager_state":"live"`) {
			t.Fatalf("fleet after append = %s", data)
		}
	case <-deadline:
		t.Fatal("store append did not re-emit fleet within the debounce (poll is an hour)")
	}
}

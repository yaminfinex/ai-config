package servecmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ai-config/tools/herder/internal/hcomevents"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
)

func fixtureDeps() dependencies {
	return dependencies{
		snapshot: func() (herdrcli.Snapshot, error) {
			return herdrcli.Snapshot{
				Workspaces: []herdrcli.Workspace{{WorkspaceID: "w1", Label: "repo", TabCount: 1, PaneCount: 1}},
				Tabs:       []herdrcli.Tab{{TabID: "t1", WorkspaceID: "w1", Label: "agents", PaneCount: 1}},
				Panes:      []herdrcli.Pane{{PaneID: "p1", WorkspaceID: "w1", TabID: "t1", Agent: "codex", AgentStatus: "working", AgentSession: "s1"}},
				Agents:     []herdrcli.Agent{{PaneID: "p1", Name: "dore", Agent: "codex", Status: "working"}},
			}, nil
		},
		roster: func() ([]hcomidentity.Row, error) {
			return []hcomidentity.Row{{Name: "dore", Tool: "codex", Status: "active", LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"}}}, nil
		},
		messages: func(ctx context.Context, cursor *hcomevents.Cursor, emit func(hcomevents.Message) error, healthy func() error) error {
			<-ctx.Done()
			return nil
		},
		poll: 10 * time.Millisecond,
	}
}

func TestFleetEndpointPinsPathAndBoardJSONShape(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/fleet", nil)
	response := httptest.NewRecorder()
	newHandler(fixtureDeps()).ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	workspaces, ok := body["workspaces"].([]any)
	if !ok || len(workspaces) != 1 {
		t.Fatalf("workspaces = %#v", body["workspaces"])
	}
	workspace := workspaces[0].(map[string]any)
	tabs := workspace["tabs"].([]any)
	panes := tabs[0].(map[string]any)["panes"].([]any)
	pane := panes[0].(map[string]any)
	for key, want := range map[string]string{"pane_id": "p1", "agent": "dore", "tool": "codex", "herdr_status": "working", "bus_status": "active", "gap": "-"} {
		if pane[key] != want {
			t.Errorf("pane[%s] = %#v, want %q", key, pane[key], want)
		}
	}
	if unplaced, ok := body["unplaced"].([]any); !ok || len(unplaced) != 0 {
		t.Fatalf("unplaced = %#v", body["unplaced"])
	}
}

func TestRefusalsUseOneShapeAndHonestStatuses(t *testing.T) {
	deps := fixtureDeps()
	deps.snapshot = func() (herdrcli.Snapshot, error) {
		return herdrcli.Snapshot{}, errors.New("connect missing.sock: refused")
	}
	for name, test := range map[string]struct {
		method, path string
		status       int
		detail       string
	}{
		"dead socket":      {http.MethodGet, "/api/fleet", http.StatusBadGateway, "connect missing.sock: refused"},
		"bad method":       {http.MethodPost, "/api/fleet", http.StatusBadRequest, "GET required"},
		"unknown endpoint": {http.MethodGet, "/api/agents/dore", http.StatusNotFound, "unknown endpoint"},
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			newHandler(deps).ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			if response.Code != test.status {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			errorText, errorOK := body["error"].(string)
			detail, detailOK := body["detail"].(string)
			if len(body) != 2 || !errorOK || errorText == "" || !detailOK || !strings.Contains(detail, test.detail) {
				t.Fatalf("refusal = %#v", body)
			}
		})
	}
}

func TestFleetRefusesFailingHcomRoster(t *testing.T) {
	deps := fixtureDeps()
	deps.roster = func() ([]hcomidentity.Row, error) { return nil, errors.New("hcom list failed") }
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/fleet", nil))
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), `"error":"substrate unreachable"`) || !strings.Contains(response.Body.String(), "hcom list failed") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestFleetRefusesInconsistentHierarchyAndRoster(t *testing.T) {
	for name, test := range map[string]struct {
		mutate func(*dependencies)
		detail string
	}{
		"hierarchy": {func(deps *dependencies) {
			deps.snapshot = func() (herdrcli.Snapshot, error) {
				return herdrcli.Snapshot{Panes: []herdrcli.Pane{{PaneID: "p1", TabID: "missing"}}}, nil
			}
		}, "invalid session hierarchy"},
		"roster": {func(deps *dependencies) {
			deps.roster = func() ([]hcomidentity.Row, error) {
				return []hcomidentity.Row{
					{Name: "dore", LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"}},
					{Name: "kumo", LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"}},
				}, nil
			}
		}, "invalid roster"},
	} {
		t.Run(name, func(t *testing.T) {
			deps := fixtureDeps()
			test.mutate(&deps)
			response := httptest.NewRecorder()
			newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/fleet", nil))
			if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), test.detail) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestTestScopedListenersNeverUseWildcard(t *testing.T) {
	t.Setenv("HERDER_SERVE_TEST_LOOPBACK_ONLY", "1")
	listeners, warnings, err := liveListeners(0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	if len(listeners) != 1 || len(warnings) != 1 {
		t.Fatalf("listeners=%d warnings=%v", len(listeners), warnings)
	}
	address := listeners[0].Addr().(*net.TCPAddr)
	if !address.IP.Equal(net.ParseIP("127.0.0.1")) || address.IP.IsUnspecified() {
		t.Fatalf("listener address = %s", address)
	}
}

func TestTailscaleBindFailureKeepsLoopbackListener(t *testing.T) {
	listeners, warnings, err := openListeners([]string{"127.0.0.1", "192.0.2.1"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	if len(listeners) != 1 || len(warnings) != 1 || !strings.Contains(warnings[0], "tailscale address") {
		t.Fatalf("listeners=%d warnings=%v", len(listeners), warnings)
	}
}

func TestOpenListenersRejectsWildcard(t *testing.T) {
	listeners, _, err := openListeners([]string{"0.0.0.0"}, 0)
	for _, listener := range listeners {
		_ = listener.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "unspecified") {
		t.Fatalf("openListeners wildcard error = %v", err)
	}
}

func TestEventsSendsFleetThenMessage(t *testing.T) {
	deps := fixtureDeps()
	readSnapshot := deps.snapshot
	started := make(chan struct{})
	deps.snapshot = func() (herdrcli.Snapshot, error) {
		select {
		case <-started:
			return readSnapshot()
		case <-time.After(time.Second):
			return herdrcli.Snapshot{}, errors.New("hcom subscription did not start before initial snapshot")
		}
	}
	deps.messages = func(ctx context.Context, cursor *hcomevents.Cursor, emit func(hcomevents.Message) error, healthy func() error) error {
		close(started)
		if err := healthy(); err != nil {
			return err
		}
		return emit(hcomevents.Message{ID: 7, From: "vile", To: []string{"dore"}, Thread: "web-serve", Text: "done?"})
	}
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	event, data := readEvent(t, reader)
	if event != "fleet" || !strings.Contains(data, `"workspaces"`) {
		t.Fatalf("first event = %q %s", event, data)
	}
	event, data = readEvent(t, reader)
	if event != "message" || !strings.Contains(data, `"id":7`) || !strings.Contains(data, `"to":["dore"]`) {
		t.Fatalf("second event = %q %s", event, data)
	}
}

func TestEventsReportsUnreachableAndRecoveredWithoutEmptyFleet(t *testing.T) {
	deps := fixtureDeps()
	goodSnapshot := deps.snapshot
	var calls atomic.Int32
	deps.snapshot = func() (herdrcli.Snapshot, error) {
		if calls.Add(1) == 1 {
			return herdrcli.Snapshot{}, errors.New("socket refused")
		}
		return goodSnapshot()
	}
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	event, data := readEvent(t, reader)
	if event != "substrate" || !strings.Contains(data, `"source":"herdr"`) || !strings.Contains(data, `"status":"unreachable"`) || strings.Contains(data, `"workspaces":[]`) {
		t.Fatalf("unreachable event = %q %s", event, data)
	}
	event, data = readEvent(t, reader)
	if event != "substrate" || !strings.Contains(data, `"status":"recovered"`) {
		t.Fatalf("recovery event = %q %s", event, data)
	}
	event, data = readEvent(t, reader)
	if event != "fleet" || !strings.Contains(data, `"workspace_id":"w1"`) {
		t.Fatalf("fleet after recovery = %q %s", event, data)
	}
}

func TestEventsReportsHcomSubscriptionFailureAndRecovery(t *testing.T) {
	deps := fixtureDeps()
	var calls atomic.Int32
	deps.messages = func(ctx context.Context, cursor *hcomevents.Cursor, emit func(hcomevents.Message) error, healthy func() error) error {
		if calls.Add(1) == 1 {
			cursor.ID = 7
			return errors.New("hcom wait failed")
		}
		if cursor.ID != 7 {
			return errors.New("subscription cursor reset across retry")
		}
		if err := healthy(); err != nil {
			return err
		}
		if err := emit(hcomevents.Message{ID: 8, From: "vile", To: []string{"dore"}, Text: "recovered"}); err != nil {
			return err
		}
		<-ctx.Done()
		return nil
	}
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if event, _ := readEvent(t, reader); event != "fleet" {
		t.Fatalf("first event = %q", event)
	}
	if event, data := readEvent(t, reader); event != "substrate" || !strings.Contains(data, `"source":"hcom"`) || !strings.Contains(data, `"status":"unreachable"`) {
		t.Fatalf("failure event = %q %s", event, data)
	}
	if event, data := readEvent(t, reader); event != "substrate" || !strings.Contains(data, `"status":"recovered"`) {
		t.Fatalf("recovery event = %q %s", event, data)
	}
	if event, data := readEvent(t, reader); event != "message" || !strings.Contains(data, `"id":8`) {
		t.Fatalf("message event = %q %s", event, data)
	}
}

func readEvent(t *testing.T, reader *bufio.Reader) (string, string) {
	t.Helper()
	type result struct {
		event, data string
		err         error
	}
	done := make(chan result, 1)
	go func() {
		var got result
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				got.err = err
				done <- got
				return
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "event: ") {
				got.event = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") {
				got.data = strings.TrimPrefix(line, "data: ")
			}
			if line == "" && got.event != "" {
				done <- got
				return
			}
		}
	}()
	select {
	case got := <-done:
		if got.err != nil && got.err != io.EOF {
			t.Fatal(got.err)
		}
		return got.event, got.data
	case <-time.After(time.Second):
		t.Fatal("timed out reading SSE event")
		return "", ""
	}
}

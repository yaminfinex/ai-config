package servecmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"ai-config/tools/herder/internal/hcommessage"
	"ai-config/tools/herder/internal/hcomtranscript"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/webidentity"
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
			return []hcomidentity.Row{{Name: "dore", Tool: "codex", Status: "active", SessionID: "session-dore", LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"}}}, nil
		},
		messages: func(ctx context.Context, cursor *hcomevents.Cursor, emit func(hcomevents.Message) error, healthy func() error) error {
			<-ctx.Done()
			return nil
		},
		transcript: func(context.Context, string, int, int, hcomtranscript.Detail) ([]hcomtranscript.Exchange, error) {
			return fixtureExchanges(1), nil
		},
		latest: func(context.Context, string, hcomtranscript.Detail) (hcomtranscript.Exchange, bool, error) {
			return fixtureExchanges(1)[0], true, nil
		},
		rangeRead: func(_ context.Context, _ string, start, end int, _ hcomtranscript.Detail) ([]hcomtranscript.Exchange, error) {
			positions := make([]int, 0, end-start+1)
			for position := start; position <= end; position++ {
				positions = append(positions, position)
			}
			return fixtureExchanges(positions...), nil
		},
		sender: func(context.Context, string) (string, error) { return "web-alice-example-com", nil },
		send:   func(context.Context, string, string, string) error { return nil },
		poll:   10 * time.Millisecond,
	}
}

func fixtureExchanges(positions ...int) []hcomtranscript.Exchange {
	raw := bytes.NewBufferString("[")
	for index, position := range positions {
		if index > 0 {
			raw.WriteByte(',')
		}
		fmt.Fprintf(raw, `{"position":%d,"user":"prompt %d","action":"reply %d"}`, position, position, position)
	}
	raw.WriteByte(']')
	var exchanges []hcomtranscript.Exchange
	if err := json.Unmarshal(raw.Bytes(), &exchanges); err != nil {
		panic(err)
	}
	return exchanges
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

func TestAgentEndpointReturnsJoinedDetailAnd404sUnknownBusName(t *testing.T) {
	deps := fixtureDeps()
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/agents/dore", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("detail response = %d %s", response.Code, response.Body.String())
	}
	var detail agentDetail
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Name != "dore" || detail.Tool != "codex" || detail.HerdrStatus != "working" || detail.BusStatus != "active" || detail.Gap != "-" || detail.Pane == nil || detail.Pane.PaneID != "p1" || detail.LaunchContext.PaneID != "p1" {
		t.Fatalf("agent detail = %#v", detail)
	}

	response = httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/agents/missing", nil))
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"error":"unknown agent"`) {
		t.Fatalf("unknown response = %d %s", response.Code, response.Body.String())
	}
}

func TestTranscriptWindowsPageBackwardByExchangeAndPinDetail(t *testing.T) {
	deps := fixtureDeps()
	type call struct {
		before, limit int
		detail        hcomtranscript.Detail
	}
	var calls []call
	deps.transcript = func(_ context.Context, agent string, before, limit int, detail hcomtranscript.Detail) ([]hcomtranscript.Exchange, error) {
		if agent != "dore" {
			t.Fatalf("agent = %q", agent)
		}
		calls = append(calls, call{before, limit, detail})
		if before == 0 {
			return fixtureExchanges(4, 5), nil
		}
		return fixtureExchanges(2, 3), nil
	}

	first := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/agents/dore/transcript?limit=2", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first = %d %s", first.Code, first.Body.String())
	}
	var page transcriptPage
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Exchanges) != 2 || page.Exchanges[0].Position != 4 || page.Exchanges[1].Position != 5 || page.Cursor == "" {
		t.Fatalf("first page = %#v", page)
	}

	olderPath := "/api/agents/dore/transcript?limit=2&before=" + page.Cursor
	older := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(older, httptest.NewRequest(http.MethodGet, olderPath, nil))
	var olderPage transcriptPage
	if err := json.Unmarshal(older.Body.Bytes(), &olderPage); err != nil {
		t.Fatal(err)
	}
	if older.Code != http.StatusOK || len(olderPage.Exchanges) != 2 || olderPage.Exchanges[0].Position != 2 || olderPage.Exchanges[1].Position != 3 {
		t.Fatalf("older page = %d %#v", older.Code, olderPage)
	}
	repeat := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(repeat, httptest.NewRequest(http.MethodGet, olderPath, nil))
	var repeatPage transcriptPage
	if err := json.Unmarshal(repeat.Body.Bytes(), &repeatPage); err != nil {
		t.Fatal(err)
	}
	if repeatPage.Cursor != olderPage.Cursor {
		t.Fatalf("cursor changed for stable window: %q != %q", repeatPage.Cursor, olderPage.Cursor)
	}

	full := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(full, httptest.NewRequest(http.MethodGet, "/api/agents/dore/transcript?limit=2&detail=full", nil))
	if full.Code != http.StatusOK || len(calls) != 4 || calls[0] != (call{0, 2, hcomtranscript.Exchanges}) || calls[1].before != 4 || calls[3].detail != hcomtranscript.Full {
		t.Fatalf("calls = %#v full=%d %s", calls, full.Code, full.Body.String())
	}
	staleCursor := encodeTranscriptCursor(transcriptCursor{
		Version: 1, Kind: "page", Agent: "dore", Session: "older-session", Detail: hcomtranscript.Exchanges, Position: 4,
	})
	stale := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(stale, httptest.NewRequest(http.MethodGet, "/api/agents/dore/transcript?before="+staleCursor, nil))
	if stale.Code != http.StatusBadRequest || len(calls) != 4 {
		t.Fatalf("stale cursor response=%d %s calls=%#v", stale.Code, stale.Body.String(), calls)
	}
}

func TestTranscriptStreamResumesAfterLastEventID(t *testing.T) {
	deps := fixtureDeps()
	deps.latest = func(context.Context, string, hcomtranscript.Detail) (hcomtranscript.Exchange, bool, error) {
		return fixtureExchanges(5)[0], true, nil
	}
	var rangeStart, rangeEnd int
	deps.rangeRead = func(_ context.Context, _ string, start, end int, _ hcomtranscript.Detail) ([]hcomtranscript.Exchange, error) {
		rangeStart, rangeEnd = start, end
		return fixtureExchanges(3, 4, 5), nil
	}
	lastID := encodeTranscriptCursor(transcriptCursor{Version: 1, Kind: "stream", Agent: "dore", Session: "session-dore", Detail: hcomtranscript.Exchanges, Position: 2})
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/agents/dore/transcript/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Last-Event-ID", lastID)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	event, data := readEvent(t, bufio.NewReader(response.Body))
	if response.StatusCode != http.StatusOK || event != "exchange" || !strings.Contains(data, `"position":3`) || rangeStart != 3 || rangeEnd != 5 {
		t.Fatalf("resume = status=%d event=%q data=%s range=%d-%d", response.StatusCode, event, data, rangeStart, rangeEnd)
	}
}

func TestMessageWritePinsAttributionRefusalsAndConfirmationShape(t *testing.T) {
	requestBody := func() *bytes.Buffer { return bytes.NewBufferString(`{"text":"please inspect"}`) }

	deps := fixtureDeps()
	deps.sender = func(context.Context, string) (string, error) {
		return "", errors.New("loopback peer has no tailnet identity")
	}
	unresolved := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/agents/dore/message", requestBody())
	newHandler(deps).ServeHTTP(unresolved, request)
	if unresolved.Code != http.StatusConflict || !strings.Contains(unresolved.Body.String(), `"error":"attribution required"`) {
		t.Fatalf("unresolved = %d %s", unresolved.Code, unresolved.Body.String())
	}

	deps = fixtureDeps()
	roster := deps.roster
	deps.roster = func() ([]hcomidentity.Row, error) {
		rows, _ := roster()
		return append(rows, hcomidentity.Row{Name: "web-alice-example-com", Status: "active"}), nil
	}
	collision := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/agents/dore/message", requestBody())
	newHandler(deps).ServeHTTP(collision, request)
	if collision.Code != http.StatusConflict || !strings.Contains(collision.Body.String(), `"error":"sender refused"`) {
		t.Fatalf("collision = %d %s", collision.Code, collision.Body.String())
	}

	deps = fixtureDeps()
	var target, sender, text string
	deps.send = func(_ context.Context, gotTarget, gotSender, gotText string) error {
		target, sender, text = gotTarget, gotSender, gotText
		return nil
	}
	confirmed := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/agents/dore/message", requestBody())
	newHandler(deps).ServeHTTP(confirmed, request)
	if confirmed.Code != http.StatusOK || target != "dore" || sender != "web-alice-example-com" || text != "please inspect" || !strings.Contains(confirmed.Body.String(), `"sent":true`) || !strings.Contains(confirmed.Body.String(), `"intent":"request"`) {
		t.Fatalf("confirmed=%d %s send=(%q,%q,%q)", confirmed.Code, confirmed.Body.String(), target, sender, text)
	}
}

func TestMessageWriteMapsWhoisInfrastructureTo502AndSemanticAttributionTo409(t *testing.T) {
	for name, test := range map[string]struct {
		err       error
		status    int
		errorText string
	}{
		"infrastructure": {fmt.Errorf("%w: daemon unavailable", webidentity.ErrUnavailable), http.StatusBadGateway, "substrate unreachable"},
		"semantic":       {errors.New("tailscale whois returned no user login"), http.StatusConflict, "attribution required"},
	} {
		t.Run(name, func(t *testing.T) {
			deps := fixtureDeps()
			deps.sender = func(context.Context, string) (string, error) { return "", test.err }
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/agents/dore/message", bytes.NewBufferString(`{"text":"hello"}`))
			newHandler(deps).ServeHTTP(response, request)
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"error":"`+test.errorText+`"`) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestMessageWriteMapsSendInfrastructureTo502AndSemanticRefusalTo409(t *testing.T) {
	for name, test := range map[string]struct {
		err       error
		status    int
		errorText string
	}{
		"infrastructure": {fmt.Errorf("%w: hcom missing", hcommessage.ErrUnavailable), http.StatusBadGateway, "substrate unreachable"},
		"semantic":       {errors.New("target refused message"), http.StatusConflict, "refused by substrate"},
	} {
		t.Run(name, func(t *testing.T) {
			deps := fixtureDeps()
			deps.send = func(context.Context, string, string, string) error { return test.err }
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/agents/dore/message", bytes.NewBufferString(`{"text":"hello"}`))
			newHandler(deps).ServeHTTP(response, request)
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"error":"`+test.errorText+`"`) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
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
		"unknown endpoint": {http.MethodGet, "/api/not-an-endpoint", http.StatusNotFound, "unknown endpoint"},
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

func TestServesEmbeddedUIAndSPAWithoutWeakeningAPINamespace(t *testing.T) {
	for name, test := range map[string]struct {
		path        string
		status      int
		contentType string
		contains    string
	}{
		"root":             {"/", http.StatusOK, "text/html", "<title>Herder fleet</title>"},
		"spa fallback":     {"/future/agent/dore", http.StatusOK, "text/html", "<title>Herder fleet</title>"},
		"unknown api":      {"/api/future", http.StatusNotFound, "application/json", `"error":"not found"`},
		"api root refusal": {"/api", http.StatusNotFound, "application/json", `"detail":"unknown endpoint"`},
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			newHandler(fixtureDeps()).ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != test.status || !strings.Contains(response.Header().Get("Content-Type"), test.contentType) || !strings.Contains(response.Body.String(), test.contains) {
				t.Fatalf("response = %d content-type=%q body=%s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
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

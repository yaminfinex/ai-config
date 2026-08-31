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
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/quick"
	"time"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/fileindex"
	"ai-config/tools/herder/internal/fileresolver"
	"ai-config/tools/herder/internal/fleetview"
	"ai-config/tools/herder/internal/hcomevents"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/hcommessage"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/repoctx"
	"ai-config/tools/herder/internal/webaction"
	"ai-config/tools/herder/internal/webidentity"
	"github.com/fsnotify/fsnotify"
)

func fixtureDeps() dependencies {
	return dependencies{
		buildIdentity: "source:fixture731",
		snapshot: func() (herdrcli.Snapshot, error) {
			return herdrcli.Snapshot{
				Workspaces: []herdrcli.Workspace{{WorkspaceID: "w1", Label: "repo", TabCount: 1, PaneCount: 1}},
				Tabs:       []herdrcli.Tab{{TabID: "t1", WorkspaceID: "w1", Label: "agents", PaneCount: 1}},
				Panes:      []herdrcli.Pane{{PaneID: "p1", WorkspaceID: "w1", TabID: "t1", Agent: "codex", AgentStatus: "working", AgentSession: "s1"}},
				Agents:     []herdrcli.Agent{{PaneID: "p1", Name: "dore", Agent: "codex", Status: "working"}},
			}, nil
		},
		worktrees:        func([]herdrcli.Workspace) (map[string]string, error) { return map[string]string{}, nil },
		paneProcessNames: func([]string) (map[string]string, error) { return map[string]string{}, nil },
		roster: func() ([]hcomidentity.Row, error) {
			return []hcomidentity.Row{{Name: "dore", Tool: "codex", Status: "active", SessionID: "session-dore", LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"}}}, nil
		},
		stopped: func(string) (hcomidentity.Row, error) { return hcomidentity.Row{}, hcomidentity.ErrStoppedNotFound },
		messages: func(ctx context.Context, cursor *hcomevents.Cursor, emit func(hcomevents.Message) error, healthy func() error) error {
			<-ctx.Done()
			return nil
		},
		recentMessages: func(context.Context, int) ([]hcomevents.Message, error) { return []hcomevents.Message{}, nil },
		latestDelivery: func(context.Context, string) (hcomevents.DeliveryWatermark, bool, error) {
			return hcomevents.DeliveryWatermark{}, false, nil
		},
		entryEnd: func(hcomidentity.Row) (int64, error) { return 0, nil },
		entryTail: func(row hcomidentity.Row, cursor claudesession.Cursor, _ int) (claudesession.TailResult, error) {
			return claudesession.TailResult{Cursor: claudesession.Cursor{SessionID: row.SessionID, Offset: cursor.Offset}}, nil
		},
		agentQueueExclusions: func(hcomidentity.Row, map[string]queueCandidate) (map[string]bool, error) {
			return map[string]bool{}, nil
		},
		agentVitals: func(hcomidentity.Row) (claudesession.Vitals, error) { return claudesession.Vitals{}, nil },
		sender:      func(context.Context, string) (string, error) { return "web-alice-example-com", nil },
		send:        func(context.Context, string, string, string) error { return nil },
		spawn: func(context.Context, []string) (webaction.Result, error) {
			return webaction.Result{Name: "new-vava", Pane: "p-new"}, nil
		},
		poll:             10 * time.Millisecond,
		heartbeat:        time.Second,
		transcriptSafety: time.Hour,
		screens:          func() (screenSource, error) { return &fixtureScreenSource{}, nil },
		roots:            buildRootSet,
		fileResolver:     fileresolver.New(fileindex.New(fileindex.Options{})),
		repoContext:      repoctx.Read,
		now:              time.Now,
		audit:            func(string, ...any) {},
		inputSerial:      &paneInputSerial{},
	}
}

type fixtureScreenSource struct {
	reads            map[string]int
	readCh           chan string
	text             string
	history          string
	historyTruncated bool
	inputs           []herdrcli.PaneInput
	inputErr         error
}

func (s *fixtureScreenSource) SendInput(input herdrcli.PaneInput) error {
	s.inputs = append(s.inputs, input)
	return s.inputErr
}

func (s *fixtureScreenSource) ReadVisible(paneID string) (herdrcli.VisibleScreen, error) {
	if s.reads != nil {
		s.reads[paneID]++
	}
	if s.readCh != nil {
		s.readCh <- paneID
	}
	return herdrcli.VisibleScreen{PaneID: paneID, Text: s.text}, nil
}

func (s *fixtureScreenSource) ReadHistory(paneID string, _ int) (herdrcli.VisibleScreen, error) {
	return herdrcli.VisibleScreen{PaneID: paneID, Text: s.history, Truncated: s.historyTruncated}, nil
}

func TestPaneInputPinsValidationAttributionForwardingAndAudit(t *testing.T) {
	source := &fixtureScreenSource{}
	deps := fixtureDeps()
	deps.screens = func() (screenSource, error) { return source, nil }
	var audits []string
	deps.audit = func(format string, values ...any) { audits = append(audits, fmt.Sprintf(format, values...)) }

	for _, body := range []string{`{}`, `{"text":"x","keys":["up"]}`, `{"text":"","keys":[]}`, `{"bogus":"x"}`} {
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/panes/p1/input", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %s = %d %s", body, response.Code, response.Body.String())
		}
	}
	oversized := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(oversized, httptest.NewRequest(http.MethodPost, "/api/panes/p1/input", strings.NewReader(`{"text":"`+strings.Repeat("x", 9<<10)+`"}`)))
	if oversized.Code != http.StatusBadRequest || !strings.Contains(oversized.Body.String(), "8 KiB") {
		t.Fatalf("oversized = %d %s", oversized.Code, oversized.Body.String())
	}

	for _, body := range []string{`{"text":"\u0003\u001b[A"}`, `{"keys":["ctrl+c","up"]}`} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/panes/p1/input", strings.NewReader(body))
		request.RemoteAddr = "100.64.0.8:4400"
		newHandler(deps).ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"sent":true`) || !strings.Contains(response.Body.String(), `"viewer":"web-alice-example-com"`) {
			t.Fatalf("success = %d %s", response.Code, response.Body.String())
		}
	}
	if len(source.inputs) != 2 || source.inputs[0].Text != "\x03\x1b[A" || fmt.Sprint(source.inputs[1].Keys) != "[ctrl+c up]" {
		t.Fatalf("inputs = %#v", source.inputs)
	}
	if len(audits) != 2 || !strings.Contains(audits[0], "viewer=web-alice-example-com") || !strings.Contains(audits[0], "pane=p1") || strings.Contains(strings.Join(audits, "\n"), "ctrl+c") {
		t.Fatalf("audits = %#v", audits)
	}
}

func TestPaneInputPins404409And502Refusals(t *testing.T) {
	unknown := httptest.NewRecorder()
	newHandler(fixtureDeps()).ServeHTTP(unknown, httptest.NewRequest(http.MethodPost, "/api/panes/missing/input", strings.NewReader(`{"text":"x"}`)))
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown = %d %s", unknown.Code, unknown.Body.String())
	}

	for name, test := range map[string]struct {
		err    error
		status int
	}{
		"gone mid-write":    {herdrcli.ErrPaneGone, http.StatusConflict},
		"substrate failure": {errors.New("socket unavailable"), http.StatusBadGateway},
	} {
		t.Run(name, func(t *testing.T) {
			deps := fixtureDeps()
			deps.screens = func() (screenSource, error) { return &fixtureScreenSource{inputErr: test.err}, nil }
			response := httptest.NewRecorder()
			newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/panes/p1/input", strings.NewReader(`{"text":"x"}`)))
			if response.Code != test.status {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func decodedScreenFrame(t *testing.T, wire []byte) screenFrame {
	t.Helper()
	parts := bytes.SplitN(wire, []byte("\n"), 2)
	if len(parts) != 2 {
		t.Fatalf("invalid SSE wire %q", wire)
	}
	data := bytes.TrimSuffix(bytes.TrimPrefix(parts[1], []byte("data: ")), []byte("\n\n"))
	var decoded screenFrame
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func ansiPrefixIsGround(text string) bool {
	const (
		ground = iota
		escape
		csi
		stringEscape
		stringEscapeEnd
	)
	state := ground
	for index := 0; index < len(text); index++ {
		value := text[index]
		switch state {
		case ground:
			if value == 0x1b {
				state = escape
			}
		case escape:
			switch value {
			case '[':
				state = csi
			case ']', 'P', 'X', '^', '_':
				state = stringEscape
			default:
				state = ground
			}
		case csi:
			if value >= 0x40 && value <= 0x7e {
				state = ground
			}
		case stringEscape:
			if value == 0x07 {
				state = ground
			} else if value == 0x1b {
				state = stringEscapeEnd
			}
		case stringEscapeEnd:
			if value == '\\' {
				state = ground
			} else if value != 0x1b {
				state = stringEscape
			}
		}
	}
	return state == ground
}

func TestScreenFrameUsesRealFixtureAndHardSerializedBudget(t *testing.T) {
	if maxScreenFrameBytes != 64<<10 {
		t.Fatalf("screen frame budget=%d", maxScreenFrameBytes)
	}
	realScreen, err := os.ReadFile("testdata/real-terminal-screen.txt")
	if err != nil {
		t.Fatal(err)
	}
	wire, err := encodeScreenEvent("w1:p1", screenFrame{PaneID: "w1:p1", Revision: 731, Status: "available", Text: string(realScreen)})
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodedScreenFrame(t, wire)
	if len(wire) > maxScreenFrameBytes || decoded.Text != string(realScreen) {
		t.Fatalf("real screen frame bytes=%d", len(wire))
	}
	huge := strings.Repeat("\x1b[38;2;115;204;255mcolored row\x1b[0m\n", 10_000)
	wire, err = encodeScreenEvent("w1:p1", screenFrame{PaneID: "w1:p1", Status: "available", Text: huge})
	decoded = decodedScreenFrame(t, wire)
	if err != nil || len(wire) > maxScreenFrameBytes || !decoded.Truncated || !strings.HasSuffix(decoded.Text, "\n") || !ansiPrefixIsGround(decoded.Text) {
		t.Fatalf("budgeted frame bytes=%d err=%v", len(wire), err)
	}
}

func TestScreenFrameTruncationNeverSplitsANSIOrLinesProperty(t *testing.T) {
	property := func(seed uint16) bool {
		payload := fmt.Sprintf("\x1b]8;;https://example.test/%d\x1b\\linked-%d\x1b]8;;\x1b\\\n\x1b[3%dmcolor-%d\x1b[0m\n", seed, seed, seed%8, seed)
		source := strings.Repeat(payload, 5000+int(seed%50))
		wire, err := encodeScreenEvent("w1:p1", screenFrame{PaneID: "w1:p1", Status: "available", Text: source})
		if err != nil || len(wire) > maxScreenFrameBytes {
			return false
		}
		decoded := decodedScreenFrame(t, wire)
		return decoded.Truncated && strings.HasPrefix(source, decoded.Text) && (decoded.Text == "" || strings.HasSuffix(decoded.Text, "\n")) && ansiPrefixIsGround(decoded.Text)
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatal(err)
	}
}

func TestScreenFrameOversizedSingleLineFallsBackToEmptyPrefix(t *testing.T) {
	wire, err := encodeScreenEvent("w1:p1", screenFrame{PaneID: "w1:p1", Status: "available", Text: "\x1b[31m" + strings.Repeat("x", maxScreenFrameBytes*2) + "\x1b[0m"})
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodedScreenFrame(t, wire)
	if decoded.Text != "" || !decoded.Truncated {
		t.Fatalf("single-line truncation=%#v", decoded)
	}
}

func TestScreenFrameCarriesHerdrSnapshotGeometry(t *testing.T) {
	snapshot, err := herdrcli.ParseSessionSnapshotResult([]byte(`{"protocol":19,"workspaces":[{"workspace_id":"w1"}],"tabs":[{"tab_id":"t1","workspace_id":"w1"}],"panes":[{"pane_id":"p1","workspace_id":"w1","tab_id":"t1"}],"layouts":[{"workspace_id":"w1","tab_id":"t1","panes":[{"pane_id":"p1","rect":{"width":103,"height":56,"x":0,"y":0}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	deps := fixtureDeps()
	deps.snapshot = func() (herdrcli.Snapshot, error) { return snapshot, nil }
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/events?screens=p1")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	readEvent(t, reader)
	readEvent(t, reader)
	if event, data := readEvent(t, reader); event != "screen:p1" || !strings.Contains(data, `"cols":103`) || !strings.Contains(data, `"rows":56`) {
		t.Fatalf("geometry frame=%q %s", event, data)
	}
}

func TestFocusedScreenMustNameARequestedScreen(t *testing.T) {
	server := httptest.NewServer(newHandler(fixtureDeps()))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/events?screens=p1&focused_screen=missing")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("focused screen status=%d", response.StatusCode)
	}
}

func TestPaneHistoryIsBoundedANSIAndHonest(t *testing.T) {
	deps := fixtureDeps()
	deps.now = func() time.Time { return time.Date(2026, 8, 29, 12, 34, 56, 731, time.UTC) }
	deps.screens = func() (screenSource, error) {
		return &fixtureScreenSource{history: "\x1b[31mold output\x1b[0m", historyTruncated: true}, nil
	}
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/panes/p1/history", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"text":"\u001b[31mold output\u001b[0m"`) || !strings.Contains(response.Body.String(), `"truncated":true`) || !strings.Contains(response.Body.String(), `"fetched_at":"2026-08-29T12:34:56.000000731Z"`) {
		t.Fatalf("history response=%d %s", response.Code, response.Body.String())
	}
}

func TestScreensPollOnlyRequestedLivePanesAtBoundedCadence(t *testing.T) {
	deps := fixtureDeps()
	reads := make(chan string, 20)
	source := &fixtureScreenSource{readCh: reads, text: "real screen"}
	deps.screens = func() (screenSource, error) { return source, nil }
	deps.poll = time.Hour
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()

	response, err := http.Get(server.URL + "/api/events?screens=p1")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if event, _ := readEvent(t, reader); event != "hello" {
		t.Fatalf("hello=%q", event)
	}
	if event, _ := readEvent(t, reader); event != "fleet" {
		t.Fatalf("fleet=%q", event)
	}
	if event, data := readEvent(t, reader); event != "screen:p1" || !strings.Contains(data, `"status":"available"`) {
		t.Fatalf("screen=%q %s", event, data)
	}
	if pane := <-reads; pane != "p1" {
		t.Fatalf("initial read=%q", pane)
	}

	readTimes := make([]time.Time, 0, 3)
	for len(readTimes) < 3 {
		select {
		case pane := <-reads:
			if pane != "p1" {
				t.Fatalf("polled unrequested pane %q", pane)
			}
			readTimes = append(readTimes, time.Now())
		case <-time.After(time.Second):
			t.Fatal("requested pane was not polled")
		}
	}
	for i := 1; i < len(readTimes); i++ {
		if elapsed := readTimes[i].Sub(readTimes[i-1]); elapsed < 200*time.Millisecond {
			t.Fatalf("screen reads were not throttled: %s", elapsed)
		}
	}
}

func TestUnknownScreenPaneEmitsUnavailableWithoutReading(t *testing.T) {
	deps := fixtureDeps()
	reads := make(chan string, 1)
	deps.screens = func() (screenSource, error) { return &fixtureScreenSource{readCh: reads}, nil }
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/events?screens=not-a-live-pane")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	readEvent(t, reader)
	readEvent(t, reader)
	if event, data := readEvent(t, reader); event != "screen:not-a-live-pane" || !strings.Contains(data, `"status":"unavailable"`) || !strings.Contains(data, "not reported by Herdr") {
		t.Fatalf("screen=%q %s", event, data)
	}
	select {
	case pane := <-reads:
		t.Fatalf("unknown pane was read: %s", pane)
	default:
	}
}

func TestEventsWithoutScreensHaveZeroScreenSourceCost(t *testing.T) {
	deps := fixtureDeps()
	var calls atomic.Int32
	deps.screens = func() (screenSource, error) {
		calls.Add(1)
		return nil, errors.New("screen source must stay dark")
	}
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(response.Body)
	readEvent(t, reader)
	readEvent(t, reader)
	response.Body.Close()
	if got := calls.Load(); got != 0 {
		t.Fatalf("screen source calls without screens = %d", got)
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
	if _, present := workspace["worktree_of"]; present {
		t.Fatalf("root workspace unexpectedly has worktree_of: %#v", workspace)
	}
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

func TestFleetEndpointNamesPlainTerminalFromHerdrProcessWithLabelFallback(t *testing.T) {
	for _, test := range []struct {
		name        string
		processName string
		processErr  error
		wantCommand bool
	}{
		{name: "foreground leader", processName: "htop", wantCommand: true},
		{name: "lookup failure", processErr: errors.New("process info unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			deps := fixtureDeps()
			deps.snapshot = func() (herdrcli.Snapshot, error) {
				return herdrcli.Snapshot{
					Workspaces: []herdrcli.Workspace{{WorkspaceID: "w1", Label: "repo", TabCount: 1, PaneCount: 1}},
					Tabs:       []herdrcli.Tab{{TabID: "t1", WorkspaceID: "w1", Label: "terminals", PaneCount: 1}},
					Panes:      []herdrcli.Pane{{PaneID: "p-shell", WorkspaceID: "w1", TabID: "t1", Label: "shell", AgentStatus: "unknown"}},
				}, nil
			}
			deps.roster = func() ([]hcomidentity.Row, error) { return nil, nil }
			deps.paneProcessNames = func(paneIDs []string) (map[string]string, error) {
				if len(paneIDs) != 1 || paneIDs[0] != "p-shell" {
					t.Fatalf("process lookup panes = %#v", paneIDs)
				}
				if test.processErr != nil {
					return nil, test.processErr
				}
				return map[string]string{"p-shell": test.processName}, nil
			}
			request := httptest.NewRequest(http.MethodGet, "/api/fleet", nil)
			response := httptest.NewRecorder()
			newHandler(deps).ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
			var body struct {
				Workspaces []struct {
					Tabs []struct {
						Panes []fleetview.Pane `json:"panes"`
					} `json:"tabs"`
				} `json:"workspaces"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			pane := body.Workspaces[0].Tabs[0].Panes[0]
			if pane.Label != "shell" || pane.CurrentCommand != test.processName || (pane.CurrentCommand != "") != test.wantCommand {
				t.Fatalf("terminal pane = %#v", pane)
			}
		})
	}
}

func TestAgentEndpointReturnsJoinedDetailAnd404sUnknownBusName(t *testing.T) {
	deps := fixtureDeps()
	usedPercent := 43.75
	deps.agentVitals = func(hcomidentity.Row) (claudesession.Vitals, error) {
		return claudesession.Vitals{Model: "invented-codex-model", ContextUsage: &claudesession.ContextUsage{
			UsedTokens: 112000, InputTokens: 112000, CachedInputTokens: int64Pointer(101000),
			OutputTokens: int64Pointer(731), WindowTokens: int64Pointer(256000), UsedPercent: &usedPercent,
		}}, nil
	}
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/agents/dore", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("detail response = %d %s", response.Code, response.Body.String())
	}
	var detail agentDetail
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Name != "dore" || detail.Tool != "codex" || detail.HerdrStatus != "working" || detail.BusStatus != "active" || detail.Gap != "-" || detail.Pane == nil || detail.Pane.PaneID != "p1" || detail.LaunchContext.PaneID != "p1" || detail.Model != "invented-codex-model" || detail.ContextUsage == nil || detail.ContextUsage.UsedTokens != 112000 {
		t.Fatalf("agent detail = %#v", detail)
	}

	response = httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/agents/missing", nil))
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"error":"unknown agent"`) {
		t.Fatalf("unknown response = %d %s", response.Code, response.Body.String())
	}
}

func TestAgentEndpointUsesLiveFirstThenRetainedStoppedEvidence(t *testing.T) {
	retired := hcomidentity.Row{Name: "dore", BaseName: "dore", Tool: "codex", Status: "retired", Directory: "/retained", SessionID: fixtureSessionID}

	deps := fixtureDeps()
	stoppedCalls := 0
	deps.stopped = func(string) (hcomidentity.Row, error) {
		stoppedCalls++
		return retired, nil
	}
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/agents/dore", nil))
	if response.Code != http.StatusOK || stoppedCalls != 0 || !strings.Contains(response.Body.String(), `"bus_status":"active"`) {
		t.Fatalf("live-first detail = %d calls=%d %s", response.Code, stoppedCalls, response.Body.String())
	}

	deps.roster = func() ([]hcomidentity.Row, error) { return nil, nil }
	queueReads := 0
	deps.recentMessages = func(context.Context, int) ([]hcomevents.Message, error) {
		queueReads++
		return []hcomevents.Message{{ID: 731}}, nil
	}
	response = httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/agents/dore", nil))
	var detail map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || detail["bus_status"] != "retired" || detail["pane"] != nil || detail["session_id"] != fixtureSessionID || queueReads != 0 {
		t.Fatalf("retired detail = %d queueReads=%d %#v", response.Code, queueReads, detail)
	}
	if _, present := detail["queued"]; present {
		t.Fatalf("retired detail falsely emitted queued: %#v", detail["queued"])
	}
}

func TestRetiredMessageIsRefusedBeforeAttributionOrSend(t *testing.T) {
	deps := fixtureDeps()
	deps.roster = func() ([]hcomidentity.Row, error) { return nil, nil }
	deps.stopped = func(name string) (hcomidentity.Row, error) {
		return hcomidentity.Row{Name: name, Tool: "codex", Status: "retired", SessionID: fixtureSessionID}, nil
	}
	deps.sender = func(context.Context, string) (string, error) {
		t.Fatal("retired send attempted attribution")
		return "", nil
	}
	deps.send = func(context.Context, string, string, string) error { t.Fatal("retired send reached hcom"); return nil }
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/agents/dore/message", strings.NewReader(`{"text":"hello"}`))
	newHandler(deps).ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"retired agent"`) || !strings.Contains(response.Body.String(), "read-only") {
		t.Fatalf("retired send = %d %s", response.Code, response.Body.String())
	}
}

func TestAgentEndpointCarriesOnlyProvenSubagentParent(t *testing.T) {
	deps := fixtureDeps()
	deps.roster = func() ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{
			{Name: "probe-fame", BaseName: "fame", Tool: "claude", Status: "active", SessionID: fixtureSessionID},
			{Name: "probe-child", BaseName: "child", ParentName: "fame", AgentID: "a35b593a6be7a9ba5", Tool: "claude", Status: "active"},
		}, nil
	}
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/agents/probe-child", nil))
	var detail agentDetail
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || detail.ParentAgent != "probe-fame" || detail.Pane != nil {
		t.Fatalf("subagent detail = %d %#v", response.Code, detail)
	}
}

func TestAgentDetailShowsBusMessageUntilTranscriptProvesDelivery(t *testing.T) {
	deps := fixtureDeps()
	deps.recentMessages = func(context.Context, int) ([]hcomevents.Message, error) {
		return []hcomevents.Message{
			{ID: 731, From: "web-owner", To: []string{"dore"}, Intent: "request", Text: webMessage("web-owner", "operator question"), SentAt: "2026-08-27T04:00:00Z"},
			{ID: 733, From: "vile", To: []string{"dore"}, Intent: "request", Text: "agent question", SentAt: "2026-08-27T04:00:01Z"},
			{ID: 732, From: "vile", To: []string{"someone-else"}, Intent: "inform", Text: "not for dore", SentAt: "2026-08-27T04:00:01Z"},
		}, nil
	}
	delivered := false
	deps.agentQueueExclusions = func(_ hcomidentity.Row, candidates map[string]queueCandidate) (map[string]bool, error) {
		if len(candidates) != 1 || candidates["731"].Sender != "web-owner" {
			t.Fatalf("operator candidates = %#v", candidates)
		}
		if !delivered {
			return map[string]bool{}, nil
		}
		return map[string]bool{"731": true}, nil
	}

	read := func() agentDetail {
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/agents/dore", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("detail response = %d %s", response.Code, response.Body.String())
		}
		var detail agentDetail
		if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
			t.Fatal(err)
		}
		return detail
	}

	queued := read().Queued
	if len(queued) != 1 || queued[0].ID != 731 || queued[0].Sender != "web-owner" || queued[0].Intent != "request" || queued[0].Preview != "operator question" || queued[0].SentAt != "2026-08-27T04:00:00Z" || !queued[0].Operator {
		t.Fatalf("queued before injection = %#v", queued)
	}
	delivered = true
	if queued = read().Queued; len(queued) != 0 {
		t.Fatalf("queued after transcript delivery = %#v", queued)
	}
}

func TestQueuedOperatorAttributionRequiresExactEnvelope(t *testing.T) {
	preview, operator := queuedPresentation("plain message from a web-looking sender")
	if preview != "plain message from a web-looking sender" || operator {
		t.Fatalf("plain queued presentation = %q, operator=%v", preview, operator)
	}
}

func TestOperatorQueueCandidatesResolveBusBaseNameToFullAgentRecipient(t *testing.T) {
	messages := []hcomevents.Message{{
		ID: 731, From: "web-owner", To: []string{"bulo"}, Intent: "request", Thread: "violet",
		Text: webMessage("web-owner", "operator question"), SentAt: "2026-08-27T04:00:00Z",
	}}
	candidates := operatorQueueCandidates("qlive-bulo", "bulo", messages, nil)
	if len(candidates) != 1 || candidates["731"].Recipient != "qlive-bulo" {
		t.Fatalf("tagged agent candidates = %#v", candidates)
	}
}

func TestOperatorQueueCandidatesResolveUniqueSenderBaseName(t *testing.T) {
	messages := []hcomevents.Message{{
		ID: 731, From: "nero", To: []string{"bulo"}, Intent: "request",
		Text: webMessage("web-owner", "operator question"),
	}}
	roster := []hcomidentity.Row{{Name: "impl-nero", BaseName: "nero"}, {Name: "qlive-bulo", BaseName: "bulo"}}
	candidates := operatorQueueCandidates("qlive-bulo", "bulo", messages, roster)
	if candidates["731"].Sender != "impl-nero" {
		t.Fatalf("resolved sender = %#v", candidates["731"])
	}
}

func TestQueueProofRequiresBusMetadataAgreement(t *testing.T) {
	candidates := map[string]queueCandidate{
		"731": {Sender: "web-owner", Recipient: "dore", Intent: "request", Thread: "violet"},
	}
	proof := newQueueProof(false)
	for _, payload := range []string{
		`{"deliveries":[{"message_id":"731","sender":"attacker","recipient":"dore","intent":"request","thread":"violet"}]}`,
		`{"deliveries":[{"message_id":"731","sender":"web-owner","recipient":"other","intent":"request","thread":"violet"}]}`,
		`{"deliveries":[{"message_id":"731","sender":"web-owner","recipient":"dore","intent":"inform","thread":"violet"}]}`,
		`{"deliveries":[{"message_id":"731","sender":"web-owner","recipient":"dore","intent":"request","thread":"other"}]}`,
	} {
		if err := proof.observe([]claudesession.Entry{{Kind: claudesession.KindHcomDelivery, Payload: json.RawMessage(payload)}}, candidates); err != nil {
			t.Fatal(err)
		}
	}
	if proof.excluded["731"] {
		t.Fatal("mismatched transcript metadata forged queue exclusion")
	}
	matching := claudesession.Entry{Kind: claudesession.KindHcomDelivery, Payload: json.RawMessage(`{"deliveries":[{"message_id":"731","sender":"web-owner","recipient":"dore","intent":"request","thread":"violet"}]}`)}
	if err := proof.observe([]claudesession.Entry{matching}, candidates); err != nil || !proof.excluded["731"] {
		t.Fatalf("matching delivery not proven: excluded=%#v err=%v", proof.excluded, err)
	}
}

func TestQueueProofAcceptsResolvedRecipientBaseAlias(t *testing.T) {
	candidates := map[string]queueCandidate{
		"731": {Sender: "web-owner", Recipient: "qlive-bulo", RecipientBase: "bulo", Intent: "request", Thread: "violet"},
	}
	entry := claudesession.Entry{Kind: claudesession.KindHcomDelivery, Payload: json.RawMessage(`{"deliveries":[{"message_id":"731","sender":"web-owner","recipient":"bulo","intent":"request","thread":"violet"}]}`)}
	proof := newQueueProof(false)
	if err := proof.observe([]claudesession.Entry{entry}, candidates); err != nil || !proof.excluded["731"] {
		t.Fatalf("base recipient alias not accepted: excluded=%#v err=%v", proof.excluded, err)
	}
}

func TestAgentDetailUsesRecipientWatermarkWithoutSessionScan(t *testing.T) {
	for _, test := range []struct {
		name             string
		position         int64
		wantQueued       bool
		rotateBeforeRead bool
	}{
		{name: "just-advanced cursor clears delivered candidate", position: 731},
		{name: "candidate beyond cursor remains queued", position: 730, wantQueued: true},
		{name: "rotation and resume do not invalidate bus proof", position: 730, wantQueued: true, rotateBeforeRead: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			deps := fixtureDeps()
			row := hcomidentity.Row{Name: "dore", BaseName: "dore-base", Tool: "claude", Status: "active", SessionID: fixtureSessionID}
			deps.roster = func() ([]hcomidentity.Row, error) { return []hcomidentity.Row{row}, nil }
			deps.recentMessages = func(context.Context, int) ([]hcomevents.Message, error) {
				return []hcomevents.Message{{
					ID: 731, From: "web-owner", To: []string{"dore-base"}, Intent: "request",
					Text: webMessage("web-owner", "operator question"), SentAt: "2026-08-31T04:00:00Z",
				}}, nil
			}
			deps.latestDelivery = func(context.Context, string) (hcomevents.DeliveryWatermark, bool, error) {
				return hcomevents.DeliveryWatermark{Recipient: "dore", Position: test.position}, true, nil
			}
			deps.agentQueueExclusions = func(hcomidentity.Row, map[string]queueCandidate) (map[string]bool, error) {
				t.Fatal("recipient watermark proof fell through to session scan")
				return nil, nil
			}
			if test.rotateBeforeRead {
				row.SessionID = "73200000-0000-4000-8000-000000000732"
			}

			detail, err := readAgent(context.Background(), deps, "dore")
			if err != nil {
				t.Fatal(err)
			}
			if got := len(detail.Queued) == 1; got != test.wantQueued {
				t.Fatalf("queued = %#v, want present %v", detail.Queued, test.wantQueued)
			}
		})
	}
}

func TestAgentDetailFallsBackToSessionForWatermarkRecipientMismatch(t *testing.T) {
	deps := fixtureDeps()
	deps.recentMessages = func(context.Context, int) ([]hcomevents.Message, error) {
		return []hcomevents.Message{{
			ID: 731, From: "web-owner", To: []string{"dore"}, Intent: "request",
			Text: webMessage("web-owner", "operator question"), SentAt: "2026-08-31T04:00:00Z",
		}}, nil
	}
	deps.latestDelivery = func(context.Context, string) (hcomevents.DeliveryWatermark, bool, error) {
		return hcomevents.DeliveryWatermark{Recipient: "someone-else", Position: 999}, true, nil
	}
	scans := 0
	deps.agentQueueExclusions = func(_ hcomidentity.Row, candidates map[string]queueCandidate) (map[string]bool, error) {
		scans++
		if len(candidates) != 1 || candidates["731"].Recipient != "dore" {
			t.Fatalf("fallback candidates = %#v", candidates)
		}
		return map[string]bool{"731": true}, nil
	}

	detail, err := readAgent(context.Background(), deps, "dore")
	if err != nil || scans != 1 || len(detail.Queued) != 0 {
		t.Fatalf("mismatched-recipient fallback = detail:%#v scans:%d err:%v", detail, scans, err)
	}
}

func TestAgentDetailWatermarkFailureOmitsQueueWithoutSessionScan(t *testing.T) {
	deps := fixtureDeps()
	deps.recentMessages = func(context.Context, int) ([]hcomevents.Message, error) {
		return []hcomevents.Message{{
			ID: 731, From: "web-owner", To: []string{"dore"}, Intent: "request",
			Text: webMessage("web-owner", "operator question"), SentAt: "2026-08-31T04:00:00Z",
		}}, nil
	}
	deps.latestDelivery = func(context.Context, string) (hcomevents.DeliveryWatermark, bool, error) {
		return hcomevents.DeliveryWatermark{}, false, errors.New("delivery cursor unavailable")
	}
	scans := 0
	deps.agentQueueExclusions = func(hcomidentity.Row, map[string]queueCandidate) (map[string]bool, error) {
		scans++
		return map[string]bool{}, nil
	}

	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/agents/dore", nil))
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || scans != 0 {
		t.Fatalf("watermark failure = status:%d scans:%d body:%s", response.Code, scans, response.Body.String())
	}
	if _, present := body["queued"]; present {
		t.Fatalf("unproven queue leaked on watermark failure: %#v", body["queued"])
	}
}

func TestCapturedDeliveryClearingUsesTranscriptOrRecipientWatermark(t *testing.T) {
	for _, test := range []struct {
		name, fixture, id, recipient, recipientBase, sentAt, msgTS string
		position                                                   int64
		transcriptProof                                            bool
		wantDeliveries                                             int
	}{
		{
			name: "post-tool task history batch", fixture: "claude-posttool-task-history.jsonl",
			id: "161485", recipient: "ziru", sentAt: "2026-08-30T20:13:31.118862+00:00",
			position: 161498, msgTS: "2026-08-30T20:17:49.539986+00:00",
		},
		{
			name: "stop hook idle wake", fixture: "claude-stop-hook-feedback.jsonl",
			id: "161525", recipient: "zuma", sentAt: "2026-08-30T20:20:14.553415+00:00",
			position: 161525, msgTS: "2026-08-30T20:20:14.553415+00:00", transcriptProof: true, wantDeliveries: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			read, err := claudesession.ReadWindow(filepath.Join("testdata", test.fixture), 0, 20)
			if err != nil {
				t.Fatal(err)
			}
			deliveries := 0
			for _, entry := range read.Entries {
				if entry.Kind == claudesession.KindHcomDelivery {
					deliveries++
				}
			}
			if deliveries != test.wantDeliveries {
				t.Fatalf("structural deliveries = %d, want %d", deliveries, test.wantDeliveries)
			}
			candidates := map[string]queueCandidate{
				test.id: {
					SentAt: test.sentAt, Sender: "web-operator", Recipient: test.recipient,
					RecipientBase: test.recipientBase, Intent: "request",
				},
			}
			proof := newQueueProof(false)
			if err := proof.observe(read.Entries, candidates); err != nil {
				t.Fatal(err)
			}
			if proof.excluded[test.id] != test.transcriptProof {
				t.Fatalf("transcript proof exclusion = %v, want %v", proof.excluded[test.id], test.transcriptProof)
			}
			watermark := hcomevents.DeliveryWatermark{Recipient: test.recipient, Position: test.position, MessageTimestamp: test.msgTS}
			excluded, _ := partitionDeliveryCandidates(candidates, watermark)
			if !excluded[test.id] {
				t.Fatalf("recipient watermark did not clear %s", test.id)
			}
			if test.id == "161485" && watermark.MessageTimestamp == candidates[test.id].SentAt {
				t.Fatal("batched fixture no longer guards against msg_ts equality matching")
			}
		})
	}
}

func TestQueueProofOmitsPreCompactionCandidatesButKeepsNewer(t *testing.T) {
	entries := []claudesession.Entry{
		{
			Kind:      claudesession.KindHcomDelivery,
			Timestamp: "2026-08-26T08:07:00Z",
			Payload:   json.RawMessage(`{"deliveries":[{"message_id":"731","sender":"web-owner","recipient":"dore","intent":"request","thread":"violet"}]}`),
		},
		{Kind: claudesession.KindCompactDivider, Timestamp: "2026-08-26T08:14:50Z"},
	}
	candidates := map[string]queueCandidate{
		"730": {SentAt: "2026-08-26T08:06:58Z", Sender: "web-owner", Recipient: "dore", Intent: "request", Thread: "violet"},
		"731": {SentAt: "2026-08-26T08:07:00Z", Sender: "web-owner", Recipient: "dore", Intent: "request", Thread: "violet"},
		"732": {SentAt: "2026-08-26T08:15:00Z", Sender: "web-owner", Recipient: "dore", Intent: "request", Thread: "violet", Preview: "newer"},
	}
	proof := newQueueProof(true)
	if err := proof.observe(entries, candidates); err != nil {
		t.Fatal(err)
	}
	excluded, err := proof.exclusions(candidates)
	if err != nil {
		t.Fatal(err)
	}
	messages := []hcomevents.Message{
		{ID: 730, From: "web-owner", To: []string{"dore"}, Intent: "request", Thread: "violet", SentAt: candidates["730"].SentAt},
		{ID: 731, From: "web-owner", To: []string{"dore"}, Intent: "request", Thread: "violet", SentAt: candidates["731"].SentAt},
		{ID: 732, From: "web-owner", To: []string{"dore"}, Intent: "request", Thread: "violet", SentAt: candidates["732"].SentAt},
	}
	queued := diffQueuedMessages(messages, candidates, excluded)
	if len(queued) != 1 || queued[0].ID != 732 {
		t.Fatalf("queued across compaction = %#v", queued)
	}
}

func TestAgentDetailOmitsQueuedWhenDiffCannotBeProven(t *testing.T) {
	for _, mutate := range []func(*dependencies){
		func(deps *dependencies) {
			deps.recentMessages = func(context.Context, int) ([]hcomevents.Message, error) { return nil, errors.New("bus unreadable") }
		},
		func(deps *dependencies) {
			deps.agentQueueExclusions = func(hcomidentity.Row, map[string]queueCandidate) (map[string]bool, error) {
				return nil, errors.New("session unreadable")
			}
		},
		func(deps *dependencies) {
			deps.latestDelivery = func(context.Context, string) (hcomevents.DeliveryWatermark, bool, error) {
				return hcomevents.DeliveryWatermark{}, false, errors.New("delivery events unreadable")
			}
		},
	} {
		deps := fixtureDeps()
		deps.recentMessages = func(context.Context, int) ([]hcomevents.Message, error) {
			return []hcomevents.Message{{ID: 731, From: "web-owner", To: []string{"dore"}, Text: "question"}}, nil
		}
		deps.agentQueueExclusions = func(hcomidentity.Row, map[string]queueCandidate) (map[string]bool, error) {
			return map[string]bool{}, nil
		}
		mutate(&deps)
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/agents/dore", nil))
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK {
			t.Fatalf("honest omission response = %d %s", response.Code, response.Body.String())
		}
		if _, present := body["queued"]; present {
			t.Fatalf("unprovable queued field present: %#v", body["queued"])
		}
	}
}

func int64Pointer(value int64) *int64 { return &value }

func TestAgentEndpointUsesSessionHealedPane(t *testing.T) {
	deps := fixtureDeps()
	baseRoster := deps.roster
	deps.roster = func() ([]hcomidentity.Row, error) {
		rows, err := baseRoster()
		rows[0].SessionID = "s1"
		rows[0].LaunchContext.PaneID = "stale"
		return rows, err
	}
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/agents/dore", nil))
	var detail agentDetail
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || detail.Pane == nil || detail.Pane.PaneID != "p1" || detail.Gap != "-" || detail.LaunchContext.PaneID != "stale" {
		t.Fatalf("session-healed detail = %d %#v", response.Code, detail)
	}
}

func TestLegacyExchangeEndpointsAreStructured404s(t *testing.T) {
	for _, path := range []string{
		"/api/agents/dore/transcript",
		"/api/agents/dore/transcript/stream",
	} {
		response := httptest.NewRecorder()
		newHandler(fixtureDeps()).ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound || response.Body.String() != "{\"error\":\"not found\",\"detail\":\"unknown endpoint\"}\n" {
			t.Fatalf("%s = %d %s", path, response.Code, response.Body.String())
		}
	}
}

func TestMessageWritePinsAttributionRefusalsAndConfirmationShape(t *testing.T) {
	original := "please inspect --flag 'quotes'\nsecond line"
	requestBody := func() *bytes.Buffer {
		body, err := json.Marshal(messageRequest{Text: original})
		if err != nil {
			t.Fatal(err)
		}
		return bytes.NewBuffer(body)
	}

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
	sendCalls := 0
	deps.send = func(_ context.Context, gotTarget, gotSender, gotText string) error {
		sendCalls++
		target, sender, text = gotTarget, gotSender, gotText
		return nil
	}
	confirmed := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/agents/dore/message", requestBody())
	newHandler(deps).ServeHTTP(confirmed, request)
	wantText := "[HERDER_WEB_OPERATOR_NOTE_BEGIN]\n[This message came from a web operator named web-alice-example-com via the fleet web view. They cannot receive hcom messages; do not reply with `hcom send`. Answer in your normal chat turn; they are watching the session transcript live.]\n[HERDER_WEB_OPERATOR_NOTE_END]\n\n" + original
	wantResponse := "{\"sent\":true,\"to\":\"dore\",\"from\":\"web-alice-example-com\",\"intent\":\"request\"}\n"
	if confirmed.Code != http.StatusOK || sendCalls != 1 || target != "dore" || sender != "web-alice-example-com" || text != wantText || confirmed.Body.String() != wantResponse {
		t.Fatalf("confirmed=%d %s send=(%q,%q,%q)", confirmed.Code, confirmed.Body.String(), target, sender, text)
	}
}

func TestViewerEndpointResolvesWithoutWritingAndSharesAttributionRefusals(t *testing.T) {
	deps := fixtureDeps()
	sendCalls := 0
	deps.send = func(context.Context, string, string, string) error {
		sendCalls++
		return nil
	}
	resolved := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/viewer", nil)
	request.RemoteAddr = "100.64.0.8:44321"
	newHandler(deps).ServeHTTP(resolved, request)
	if resolved.Code != http.StatusOK || resolved.Body.String() != "{\"viewer\":\"web-alice-example-com\"}\n" || sendCalls != 0 {
		t.Fatalf("resolved = %d %s send calls=%d", resolved.Code, resolved.Body.String(), sendCalls)
	}

	for name, test := range map[string]struct {
		err       error
		status    int
		errorText string
	}{
		"unresolved":     {errors.New("tailscale whois returned no user login"), http.StatusConflict, "attribution required"},
		"infrastructure": {fmt.Errorf("%w: daemon unavailable", webidentity.ErrUnavailable), http.StatusBadGateway, "substrate unreachable"},
	} {
		t.Run(name, func(t *testing.T) {
			refusing := fixtureDeps()
			refusing.sender = func(context.Context, string) (string, error) { return "", test.err }
			response := httptest.NewRecorder()
			newHandler(refusing).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/viewer", nil))
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"error":"`+test.errorText+`"`) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}

	collision := fixtureDeps()
	baseRoster := collision.roster
	collision.roster = func() ([]hcomidentity.Row, error) {
		rows, err := baseRoster()
		return append(rows, hcomidentity.Row{Name: "web-alice-example-com"}), err
	}
	response := httptest.NewRecorder()
	newHandler(collision).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/viewer", nil))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"sender refused"`) {
		t.Fatalf("collision = %d %s", response.Code, response.Body.String())
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

func TestSpawnMapsEveryShapeToThePinnedFleetFlags(t *testing.T) {
	for name, test := range map[string]struct {
		body string
		want []string
	}{
		"same tab":       {`{"from_pane":"p1","shape":"pane","tool":"codex","tag":"api","prompt":"hello"}`, []string{"codex", "--tag", "api", "--split-from", "p1", "--prompt", "hello"}},
		"same workspace": {`{"from_pane":"p1","shape":"tab","tool":"claude","tag":"api","prompt":"hello"}`, []string{"claude", "--tag", "api", "--workspace", "w1", "--prompt", "hello"}},
		"worktree":       {`{"from_pane":"p1","shape":"worktree","tool":"codex","tag":"api","prompt":"hello","branch":"feature/web"}`, []string{"codex", "--tag", "api", "--worktree-branch", "feature/web", "--repo", "/repo/checkout", "--prompt", "hello"}},
	} {
		t.Run(name, func(t *testing.T) {
			deps := fixtureDeps()
			baseSnapshot := deps.snapshot
			deps.snapshot = func() (herdrcli.Snapshot, error) {
				snapshot, _ := baseSnapshot()
				snapshot.Workspaces[0].Worktree = &herdrcli.WorkspaceWorktree{RepoRoot: "/repo", CheckoutPath: "/repo/checkout"}
				return snapshot, nil
			}
			var got []string
			deps.spawn = func(_ context.Context, args []string) (webaction.Result, error) {
				got = append([]string(nil), args...)
				return webaction.Result{Name: "api-vava", Pane: "p9"}, nil
			}
			response := httptest.NewRecorder()
			newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewBufferString(test.body)))
			if response.Code != http.StatusOK || fmt.Sprint(got) != fmt.Sprint(test.want) || !strings.Contains(response.Body.String(), `"name":"api-vava"`) || !strings.Contains(response.Body.String(), `"pane":"p9"`) {
				t.Fatalf("response=%d %s args=%q want=%q", response.Code, response.Body.String(), got, test.want)
			}
		})
	}
}

func TestSpawnPreservesPromptAsOneArgumentAndPinsStrictBody(t *testing.T) {
	deps := fixtureDeps()
	prompt := "--review 'quotes'\nsecond line"
	var got []string
	deps.spawn = func(_ context.Context, args []string) (webaction.Result, error) {
		got = args
		return webaction.Result{Name: "api-vava", Pane: "p9"}, nil
	}
	body, _ := json.Marshal(map[string]string{"from_pane": "p1", "shape": "pane", "tool": "codex", "tag": "api", "prompt": prompt})
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewReader(body)))
	if response.Code != http.StatusOK || got[len(got)-1] != prompt {
		t.Fatalf("response=%d %s args=%q", response.Code, response.Body.String(), got)
	}

	for name, body := range map[string]string{
		"unknown field":  `{"from_pane":"p1","shape":"pane","tool":"codex","tag":"api","prompt":"x","extra":true}`,
		"missing prompt": `{"from_pane":"p1","shape":"pane","tool":"codex","tag":"api"}`,
		"branch on pane": `{"from_pane":"p1","shape":"pane","tool":"codex","tag":"api","prompt":"x","branch":"no"}`,
		"bad tag":        `{"from_pane":"p1","shape":"pane","tool":"codex","tag":"bad tag","prompt":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			newHandler(fixtureDeps()).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewBufferString(body)))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestSpawnPinsAttributionAndSubstrateRefusals(t *testing.T) {
	valid := `{"from_pane":"p1","shape":"pane","tool":"codex","tag":"api","prompt":"x"}`
	for name, test := range map[string]struct {
		mutate func(*dependencies)
		status int
		detail string
	}{
		"unattributed": {func(d *dependencies) {
			d.sender = func(context.Context, string) (string, error) { return "", errors.New("peer not found") }
		}, 409, "peer not found"},
		"whois unavailable": {func(d *dependencies) {
			d.sender = func(context.Context, string) (string, error) {
				return "", fmt.Errorf("%w: tailscaled down", webidentity.ErrUnavailable)
			}
		}, 502, "tailscaled down"},
		"unknown pane": {func(d *dependencies) {}, 409, "fleet spawn: pane does not exist: missing"},
		"script refusal": {func(d *dependencies) {
			d.spawn = func(context.Context, []string) (webaction.Result, error) {
				return webaction.Result{}, errors.New("fleet spawn: pane is busy")
			}
		}, 409, "fleet spawn: pane is busy"},
		"script unavailable": {func(d *dependencies) {
			d.spawn = func(context.Context, []string) (webaction.Result, error) {
				return webaction.Result{}, fmt.Errorf("%w: missing", webaction.ErrUnavailable)
			}
		}, 502, "missing"},
	} {
		t.Run(name, func(t *testing.T) {
			deps := fixtureDeps()
			test.mutate(&deps)
			body := valid
			if name == "unknown pane" {
				body = strings.Replace(valid, `"p1"`, `"missing"`, 1)
			}
			response := httptest.NewRecorder()
			newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewBufferString(body)))
			if response.Code != test.status || !strings.Contains(response.Body.String(), test.detail) {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestSpawnRejectsWorktreeWithoutRepoAndInvalidBranch(t *testing.T) {
	for name, test := range map[string]struct {
		body   string
		status int
		detail string
	}{
		"missing repo":   {`{"from_pane":"p1","shape":"worktree","tool":"codex","tag":"api","prompt":"x","branch":"feature/web"}`, http.StatusConflict, "workspace w1 has no repository"},
		"invalid branch": {`{"from_pane":"p1","shape":"worktree","tool":"codex","tag":"api","prompt":"x","branch":"bad branch"}`, http.StatusBadRequest, "branch must start"},
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			newHandler(fixtureDeps()).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewBufferString(test.body)))
			if response.Code != test.status || !strings.Contains(response.Body.String(), test.detail) {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
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
	if event != "hello" || data != `{"buildIdentity":"source:fixture731"}` {
		t.Fatalf("handshake event = %q %s", event, data)
	}
	event, data = readEvent(t, reader)
	if event != "fleet" || !strings.Contains(data, `"workspaces"`) {
		t.Fatalf("first event = %q %s", event, data)
	}
	event, data = readEvent(t, reader)
	if event != "message" || !strings.Contains(data, `"id":7`) || !strings.Contains(data, `"to":["dore"]`) {
		t.Fatalf("second event = %q %s", event, data)
	}
}

func TestEventsMultiplexesOnlySubscribedAgentTranscripts(t *testing.T) {
	deps := fixtureDeps()
	deps.transcriptSafety = 10 * time.Millisecond
	baseRoster := deps.roster
	deps.roster = func() ([]hcomidentity.Row, error) {
		rows, _ := baseRoster()
		return append(rows,
			hcomidentity.Row{Name: "kumo", Tool: "claude", Status: "active", SessionID: "session-kumo"},
			hcomidentity.Row{Name: "veno", Tool: "codex", Status: "active", SessionID: "session-veno"},
		), nil
	}
	calls := map[string]int{}
	deps.entryEnd = func(hcomidentity.Row) (int64, error) { return 0, nil }
	deps.entryTail = func(row hcomidentity.Row, cursor claudesession.Cursor, _ int) (claudesession.TailResult, error) {
		name := row.Name
		calls[name]++
		result := claudesession.TailResult{Cursor: claudesession.Cursor{SessionID: row.SessionID, Offset: cursor.Offset}}
		if calls[name] > 1 {
			result.Read = claudesession.ReadResult{
				Entries:    []claudesession.Entry{{UUID: "invented-" + name, Line: 1, ByteOffset: 0, Timestamp: "2026-01-02T03:04:05Z", Kind: claudesession.KindAssistantText, Payload: json.RawMessage(`{"invented":true}`)}},
				NextOffset: 73,
			}
			result.Cursor.Offset = 73
		}
		return result, nil
	}
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/events?agents=dore,kumo,dore")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if event, _ := readEvent(t, reader); event != "hello" {
		t.Fatalf("handshake event = %q", event)
	}
	if event, _ := readEvent(t, reader); event != "fleet" {
		t.Fatalf("first event = %q", event)
	}
	seen := map[string]bool{}
	for len(seen) < 2 {
		event, data := readEvent(t, reader)
		if event == "entry:dore" || event == "entry:kumo" {
			seen[event] = strings.Contains(data, `"kind":"assistant_text"`) && strings.Contains(data, `"byteOffset":0`)
		}
	}
	if !seen["entry:dore"] || !seen["entry:kumo"] || calls["veno"] != 0 {
		t.Fatalf("seen=%v latest calls=%v", seen, calls)
	}
}

func TestEventsContinuesRetiredTranscriptWithoutSubstrateFault(t *testing.T) {
	deps := fixtureDeps()
	deps.transcriptSafety = 10 * time.Millisecond
	baseRoster := deps.roster
	var rosterCalls atomic.Int32
	deps.roster = func() ([]hcomidentity.Row, error) {
		if rosterCalls.Add(1) == 1 {
			return baseRoster()
		}
		return nil, nil
	}
	deps.stopped = func(name string) (hcomidentity.Row, error) {
		return hcomidentity.Row{Name: name, BaseName: name, Tool: "codex", Status: "retired", SessionID: "session-dore"}, nil
	}
	deps.entryTail = func(row hcomidentity.Row, cursor claudesession.Cursor, _ int) (claudesession.TailResult, error) {
		result := claudesession.TailResult{Cursor: claudesession.Cursor{SessionID: row.SessionID, Offset: cursor.Offset}}
		if row.Status == "retired" && cursor.Offset == 0 {
			result.Read = claudesession.ReadResult{Entries: []claudesession.Entry{{UUID: "retained", Kind: claudesession.KindAssistantText, Payload: json.RawMessage(`{"text":"retained"}`)}}, NextOffset: 73}
			result.Cursor.Offset = 73
		}
		return result, nil
	}
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/events?agents=dore")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if event, _ := readEvent(t, reader); event != "hello" {
		t.Fatalf("hello = %q", event)
	}
	if event, _ := readEvent(t, reader); event != "fleet" {
		t.Fatalf("fleet = %q", event)
	}
	for {
		event, data := readEvent(t, reader)
		if event == "substrate" && strings.Contains(data, "transcript:dore") {
			t.Fatalf("retirement emitted transcript fault: %s", data)
		}
		if event == "entry:dore" {
			if !strings.Contains(data, `"uuid":"retained"`) {
				t.Fatalf("retained entry = %s", data)
			}
			break
		}
	}
}

func TestEventsRewindowsWhenEntryTailResets(t *testing.T) {
	deps := fixtureDeps()
	deps.transcriptSafety = 10 * time.Millisecond
	endCalls := 0
	deps.entryEnd = func(hcomidentity.Row) (int64, error) {
		endCalls++
		if endCalls > 1 {
			return 25, nil
		}
		return 0, nil
	}
	tailCalls := 0
	deps.entryTail = func(row hcomidentity.Row, cursor claudesession.Cursor, _ int) (claudesession.TailResult, error) {
		tailCalls++
		if tailCalls > 1 {
			return claudesession.TailResult{
				Cursor: claudesession.Cursor{SessionID: row.SessionID},
				Reset:  &claudesession.Reset{Reason: claudesession.ResetTruncated, SessionID: row.SessionID, PreviousOffset: cursor.Offset},
			}, nil
		}
		return claudesession.TailResult{Cursor: claudesession.Cursor{SessionID: row.SessionID, Offset: cursor.Offset}}, nil
	}
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/events?agents=dore")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if event, _ := readEvent(t, reader); event != "hello" {
		t.Fatalf("handshake event = %q", event)
	}
	if event, _ := readEvent(t, reader); event != "fleet" {
		t.Fatalf("first event = %q", event)
	}
	for {
		event, data := readEvent(t, reader)
		if event != "rewindow" {
			continue
		}
		if data != `{"agent":"dore"}` || endCalls < 2 {
			t.Fatalf("rewindow = %s end calls=%d", data, endCalls)
		}
		break
	}
}

func TestEventStreamEmitsHeartbeatComments(t *testing.T) {
	if HeartbeatCadence != 15*time.Second {
		t.Fatalf("heartbeat cadence = %s, want 15s", HeartbeatCadence)
	}
	deps := fixtureDeps()
	deps.poll = time.Hour
	deps.heartbeat = 10 * time.Millisecond
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()

	eventsResponse, err := http.Get(server.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	eventsReader := bufio.NewReader(eventsResponse.Body)
	if event, _ := readEvent(t, eventsReader); event != "hello" {
		t.Fatalf("handshake event = %q", event)
	}
	if event, _ := readEvent(t, eventsReader); event != "fleet" {
		t.Fatalf("first events frame = %q", event)
	}
	if line := readSSELine(t, eventsReader, ": ping"); line != ": ping" {
		t.Fatalf("events heartbeat = %q", line)
	}
	eventsResponse.Body.Close()
}

func TestEventsDoNotTailTranscriptsOnFleetPolls(t *testing.T) {
	deps := fixtureDeps()
	deps.poll = 5 * time.Millisecond
	deps.transcriptSafety = 80 * time.Millisecond
	tailCalled := make(chan struct{}, 1)
	deps.entryTail = func(row hcomidentity.Row, cursor claudesession.Cursor, _ int) (claudesession.TailResult, error) {
		select {
		case tailCalled <- struct{}{}:
		default:
		}
		return claudesession.TailResult{Cursor: cursor}, nil
	}
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/events?agents=dore")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if event, _ := readEvent(t, reader); event != "hello" {
		t.Fatalf("hello=%q", event)
	}
	if event, _ := readEvent(t, reader); event != "fleet" {
		t.Fatalf("fleet=%q", event)
	}
	time.Sleep(40 * time.Millisecond)
	select {
	case <-tailCalled:
		t.Fatal("2s-equivalent fleet poll tailed a quiet transcript")
	default:
	}
	select {
	case <-tailCalled:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("30s-equivalent transcript safety sweep did not run")
	}
}

func TestTranscriptWatcherSetupFailureFallsBackToSafetySweep(t *testing.T) {
	deps := fixtureDeps()
	deps.poll = time.Hour
	deps.transcriptSafety = 10 * time.Millisecond
	deps.entryPath = func(hcomidentity.Row) (string, error) { return filepath.Join(t.TempDir(), "session.jsonl"), nil }
	deps.transcriptWatcher = func() (*fsnotify.Watcher, error) { return nil, errors.New("watch limit reached") }
	audits := make(chan string, 8)
	deps.audit = func(format string, values ...any) { audits <- fmt.Sprintf(format, values...) }
	call := 0
	deps.entryTail = func(row hcomidentity.Row, cursor claudesession.Cursor, _ int) (claudesession.TailResult, error) {
		call++
		result := claudesession.TailResult{Cursor: cursor}
		if call == 1 {
			result.Read = claudesession.ReadResult{Entries: []claudesession.Entry{{UUID: "safety", Kind: claudesession.KindAssistantText, Payload: json.RawMessage(`{"text":"safety"}`)}}, NextOffset: 73}
			result.Cursor.Offset = 73
		}
		return result, nil
	}
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/events?agents=dore")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if event, _ := readEvent(t, reader); event != "hello" {
		t.Fatalf("hello=%q", event)
	}
	if event, _ := readEvent(t, reader); event != "fleet" {
		t.Fatalf("fleet=%q", event)
	}
	if event, data := readEvent(t, reader); event != "entry:dore" || !strings.Contains(data, `"uuid":"safety"`) {
		t.Fatalf("fallback event=%q data=%s", event, data)
	}
	select {
	case audit := <-audits:
		if !strings.Contains(audit, "using safety sweep") {
			t.Fatalf("watch failure audit=%q", audit)
		}
	case <-time.After(time.Second):
		t.Fatal("watch setup failure was not audited")
	}
}

func TestTranscriptWatcherRuntimeFailureFallsBackToSafetySweep(t *testing.T) {
	deps := fixtureDeps()
	deps.poll = time.Hour
	deps.transcriptSafety = 100 * time.Millisecond
	root := t.TempDir()
	path := filepath.Join(root, "session.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	deps.entryPath = func(hcomidentity.Row) (string, error) { return path, nil }
	var watcher *fsnotify.Watcher
	deps.transcriptWatcher = func() (*fsnotify.Watcher, error) {
		var err error
		watcher, err = fsnotify.NewWatcher()
		return watcher, err
	}
	audits := make(chan string, 8)
	deps.audit = func(format string, values ...any) { audits <- fmt.Sprintf(format, values...) }
	call := 0
	deps.entryTail = func(row hcomidentity.Row, cursor claudesession.Cursor, _ int) (claudesession.TailResult, error) {
		call++
		result := claudesession.TailResult{Cursor: cursor}
		if call == 1 {
			result.Read = claudesession.ReadResult{Entries: []claudesession.Entry{{UUID: "runtime-safety", Kind: claudesession.KindAssistantText, Payload: json.RawMessage(`{"text":"runtime safety"}`)}}, NextOffset: 73}
			result.Cursor.Offset = 73
		}
		return result, nil
	}
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/events?agents=dore")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	readUntilEvent(t, reader, "fleet")
	if watcher == nil {
		t.Fatal("runtime watcher was not started")
	}
	if err := watcher.Close(); err != nil {
		t.Fatal(err)
	}
	if data := readUntilEvent(t, reader, "entry:dore"); !strings.Contains(data, `"uuid":"runtime-safety"`) {
		t.Fatalf("runtime fallback entry=%s", data)
	}
	select {
	case audit := <-audits:
		if !strings.Contains(audit, "using safety sweep") {
			t.Fatalf("runtime watcher failure audit=%q", audit)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime watcher failure was not audited")
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
	if event != "hello" {
		t.Fatalf("handshake event = %q %s", event, data)
	}
	event, data = readEvent(t, reader)
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
	if event, _ := readEvent(t, reader); event != "hello" {
		t.Fatalf("handshake event = %q", event)
	}
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

func readSSELine(t *testing.T, reader *bufio.Reader, want string) string {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				done <- ""
				return
			}
			line = strings.TrimSpace(line)
			if line == want {
				done <- line
				return
			}
		}
	}()
	select {
	case line := <-done:
		return line
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for SSE line %q", want)
		return ""
	}
}

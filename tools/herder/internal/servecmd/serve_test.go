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

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/hcomevents"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/hcommessage"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/webaction"
	"ai-config/tools/herder/internal/webidentity"
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
		worktrees: func([]herdrcli.Workspace) (map[string]string, error) { return map[string]string{}, nil },
		roster: func() ([]hcomidentity.Row, error) {
			return []hcomidentity.Row{{Name: "dore", Tool: "codex", Status: "active", SessionID: "session-dore", LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"}}}, nil
		},
		messages: func(ctx context.Context, cursor *hcomevents.Cursor, emit func(hcomevents.Message) error, healthy func() error) error {
			<-ctx.Done()
			return nil
		},
		entryEnd: func(hcomidentity.Row) (int64, error) { return 0, nil },
		entryTail: func(row hcomidentity.Row, cursor claudesession.Cursor, _ int) (claudesession.TailResult, error) {
			return claudesession.TailResult{Cursor: claudesession.Cursor{SessionID: row.SessionID, Offset: cursor.Offset}}, nil
		},
		sender: func(context.Context, string) (string, error) { return "web-alice-example-com", nil },
		send:   func(context.Context, string, string, string) error { return nil },
		spawn: func(context.Context, []string) (webaction.Result, error) {
			return webaction.Result{Name: "new-vava", Pane: "p-new"}, nil
		},
		poll:      10 * time.Millisecond,
		heartbeat: time.Second,
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

func TestEventsRewindowsWhenEntryTailResets(t *testing.T) {
	deps := fixtureDeps()
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

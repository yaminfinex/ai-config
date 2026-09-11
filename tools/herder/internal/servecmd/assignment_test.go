package servecmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/hcomidentity"
	"github.com/fsnotify/fsnotify"
)

func TestAssignmentEndpointWritesOneEvent(t *testing.T) {
	deps := supervisionDeps(t)
	request := httptest.NewRequest(http.MethodPost, "/api/agents/impl-kolo/assignment", strings.NewReader(`{"manager":" sesh-nabi ","group":" fleet-refit "}`))
	request.SetPathValue("busName", "impl-kolo")
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, request)
	var body assignmentResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || body != (assignmentResponse{Name: "impl-kolo", Manager: "sesh-nabi", Group: "fleet-refit", By: "web-alice-example-com"}) {
		t.Fatalf("status=%d body=%+v", response.Code, body)
	}
	projection, err := deps.store.Replay()
	if err != nil {
		t.Fatal(err)
	}
	latest := projection.Latest("impl-kolo")
	event := latest.Events[len(latest.Events)-1]
	if event.Kind != agentstore.KindAssign || event.Manager != "sesh-nabi" || event.Group != "fleet-refit" || event.By != "web-alice-example-com" || event.ByKind != "web" || latest.Manager != "sesh-nabi" || latest.Assignment == nil || latest.Assignment.Group != "fleet-refit" {
		t.Fatalf("latest=%+v event=%+v", latest, event)
	}
}

func TestAssignmentEndpointGroupClear(t *testing.T) {
	deps := supervisionDeps(t)
	for _, body := range []string{`{"group":"fleet-refit"}`, `{"group":""}`} {
		request := httptest.NewRequest(http.MethodPost, "/api/agents/impl-kolo/assignment", strings.NewReader(body))
		request.SetPathValue("busName", "impl-kolo")
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("body=%s status=%d response=%s", body, response.Code, response.Body.String())
		}
	}
	projection, err := deps.store.Replay()
	if err != nil || projection.Latest("impl-kolo").Assignment != nil {
		t.Fatalf("err=%v latest=%+v", err, projection.Latest("impl-kolo"))
	}
}

func TestAssignmentEndpointReemitsFleetForManagerAndHuman(t *testing.T) {
	deps := supervisionDeps(t)
	deps.poll = time.Hour
	deps.fileWatcher = fsnotify.NewWatcher
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps = startStoreProjection(ctx, deps)
	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	stream, err := http.Get(server.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	reader := bufio.NewReader(stream.Body)
	for event, _ := readEvent(t, reader); event != "fleet"; event, _ = readEvent(t, reader) {
	}

	post := func(body, want string) {
		t.Helper()
		response, err := http.Post(server.URL+"/api/agents/impl-kolo/assignment", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", response.StatusCode)
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
			if !strings.Contains(data, want) {
				t.Fatalf("fleet=%s want=%s", data, want)
			}
		case <-deadline:
			t.Fatal("assignment did not re-emit fleet within the debounce")
		}
	}
	post(`{"manager":"sesh-nabi","group":"delivery"}`, `"manager":"sesh-nabi","manager_state":"live","group":"delivery"`)
	post(`{"manager":"human"}`, `"manager":"operator","manager_state":"operator","group":"delivery"`)
}

func TestAssignmentEndpointRefusals(t *testing.T) {
	tests := map[string]struct {
		mutate func(*dependencies)
		name   string
		body   string
		status int
		detail string
	}{
		"missing fields": {name: "impl-kolo", body: `{}`, status: 400, detail: "manager or group required"},
		"empty manager":  {name: "impl-kolo", body: `{"manager":" "}`, status: 400},
		"self":           {name: "impl-kolo", body: `{"manager":"impl-kolo"}`, status: 400, detail: "an agent cannot manage itself"},
		"unknown target": {name: "impl-kolo", body: `{"manager":"missing"}`, status: 400, detail: `manager \"missing\" is not a live agent`},
		"tombstone":      {name: "impl-kolo", body: `{"manager":"orch-dead"}`, status: 400, detail: `manager \"orch-dead\" is not a live agent`},
		"long group":     {name: "impl-kolo", body: `{"group":"` + strings.Repeat("界", 81) + `"}`, status: 400},
		"retired": {mutate: func(deps *dependencies) {
			deps.stopped = func(string) (hcomidentity.Row, error) { return hcomidentity.Row{Name: "old"}, nil }
		}, name: "missing", body: `{"group":"x"}`, status: 409},
		"unattributed": {mutate: func(deps *dependencies) {
			deps.sender = func(context.Context, string) (string, error) {
				return "", fmt.Errorf("no viewer")
			}
		}, name: "impl-kolo", body: `{"group":"x"}`, status: 409},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			deps := supervisionDeps(t)
			if test.mutate != nil {
				test.mutate(&deps)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/agents/"+test.name+"/assignment", bytes.NewBufferString(test.body))
			request.SetPathValue("busName", test.name)
			response := httptest.NewRecorder()
			newHandler(deps).ServeHTTP(response, request)
			if response.Code != test.status || test.detail != "" && !strings.Contains(response.Body.String(), test.detail) {
				t.Fatalf("status=%d body=%s want status=%d detail=%q", response.Code, response.Body.String(), test.status, test.detail)
			}
		})
	}
}

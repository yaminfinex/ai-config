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

	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/webidentity"
	"github.com/fsnotify/fsnotify"
)

func TestAnnotationEndpoint(t *testing.T) {
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
	for {
		event, _ := readEvent(t, reader)
		if event == "fleet" {
			break
		}
	}

	response, err := http.Post(server.URL+"/api/agents/impl-kolo/annotation", "application/json", strings.NewReader(`{"title":"  payload lead  "}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body annotationResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || body != (annotationResponse{Name: "impl-kolo", Title: "payload lead", By: "web-alice-example-com"}) {
		t.Fatalf("status=%d body=%+v", response.StatusCode, body)
	}
	projection, err := deps.store.Replay()
	if err != nil {
		t.Fatal(err)
	}
	latest := projection.Latest("impl-kolo")
	if latest.Annotation.Title != "payload lead" || latest.Annotation.By != "web-alice-example-com" || latest.Events[len(latest.Events)-1].ByKind != "web" {
		t.Fatalf("latest = %+v", latest)
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
		if !strings.Contains(data, `"agent":"impl-kolo"`) || !strings.Contains(data, `"title":"payload lead"`) {
			t.Fatalf("fleet=%s", data)
		}
	case <-deadline:
		t.Fatal("annotation append did not re-emit fleet")
	}
}

func TestAnnotationEndpointRefusals(t *testing.T) {
	tests := map[string]struct {
		mutate     func(*dependencies)
		name, body string
		detail     string
		status     int
	}{
		"unknown": {name: "missing", body: `{"title":"x"}`, status: 404},
		"retired": {mutate: func(d *dependencies) {
			d.stopped = func(string) (hcomidentity.Row, error) { return hcomidentity.Row{Name: "old"}, nil }
		}, name: "missing", body: `{"title":"x"}`, status: 409},
		"collision": {mutate: func(d *dependencies) {
			old := d.roster
			d.roster = func() ([]hcomidentity.Row, error) {
				rows, err := old()
				return append(rows, hcomidentity.Row{Name: "web-alice-example-com"}), err
			}
		}, name: "impl-kolo", body: `{"title":"x"}`, status: 409},
		"whois unavailable": {mutate: func(d *dependencies) {
			d.sender = func(context.Context, string) (string, error) {
				return "", fmt.Errorf("%w: down", webidentity.ErrUnavailable)
			}
		}, name: "impl-kolo", body: `{"title":"x"}`, status: 502},
		"storeless":     {mutate: func(d *dependencies) { d.store = nil }, name: "impl-kolo", body: `{"title":"x"}`, status: 502},
		"unknown field": {name: "impl-kolo", body: `{"title":"x","note":"no"}`, status: 400},
		"empty":         {name: "impl-kolo", body: `{"title":"  "}`, status: 400, detail: "title must not be empty"},
		"81 runes":      {name: "impl-kolo", body: `{"title":"` + strings.Repeat("界", 81) + `"}`, status: 400},
		"control":       {name: "impl-kolo", body: `{"title":"bad\tname"}`, status: 400},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			deps := supervisionDeps(t)
			if test.mutate != nil {
				test.mutate(&deps)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/agents/"+test.name+"/annotation", bytes.NewBufferString(test.body))
			request.SetPathValue("busName", test.name)
			response := httptest.NewRecorder()
			newHandler(deps).ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if test.detail != "" && !strings.Contains(response.Body.String(), test.detail) {
				t.Fatalf("body=%s, want detail %q", response.Body.String(), test.detail)
			}
		})
	}
}

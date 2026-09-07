package servecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"ai-config/tools/herder/internal/webaction"
	"ai-config/tools/herder/internal/webidentity"
)

func TestLaunchMapsWorkspaceToFleetArgv(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want []string
	}{
		{
			name: "selected workspace",
			body: `{"tool":"claude","model":"claude-fable-5-1","effort":"max","tag":"impl","workspace":"w1"}`,
			want: []string{"claude", "--model", "claude-fable-5-1", "--effort", "max", "--tag", "impl", "--workspace", "w1"},
		},
		{
			name: "Codex default model",
			body: `{"tool":"codex","model":"","effort":" ","tag":"impl","workspace":"w1"}`,
			want: []string{"codex", "--tag", "impl", "--workspace", "w1"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			deps := fixtureDeps()
			deps.now = func() time.Time { return time.Date(2026, 9, 2, 3, 4, 5, 0, time.UTC) }
			var got []string
			deps.spawn = func(_ context.Context, args []string) (webaction.Result, error) {
				got = append([]string(nil), args...)
				return webaction.Result{Name: "impl-vava", Pane: "p9", OutputTail: "launch ready"}, nil
			}
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewBufferString(test.body))
			request.RemoteAddr = "100.64.0.8:4400"
			newHandler(deps).ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("argv=%q want=%q", got, test.want)
			}
			var result struct {
				Names      []string `json:"names"`
				Pane       string   `json:"pane"`
				OutputTail string   `json:"output_tail"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(result.Names, []string{"impl-vava"}) || result.Pane != "p9" || result.OutputTail == "" {
				t.Fatalf("launch result=%#v", result)
			}
		})
	}
}

func TestLaunchReturnsSpawnStderrVerbatim(t *testing.T) {
	deps := fixtureDeps()
	want := "fleet spawn: branch already exists\nretry with a different branch"
	deps.spawn = func(context.Context, []string) (webaction.Result, error) {
		return webaction.Result{}, errors.New(want)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewBufferString(`{"tool":"codex","tag":"impl","workspace":"w1"}`))
	request.RemoteAddr = "100.64.0.8:4400"
	newHandler(deps).ServeHTTP(response, request)
	var refusal refusal
	if err := json.Unmarshal(response.Body.Bytes(), &refusal); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusConflict || refusal.Detail != want {
		t.Fatalf("response=%d refusal=%#v", response.Code, refusal)
	}
}

func TestLaunchAppendsAttributedEdgeAfterSuccess(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HERDER_STATE_DIR", state)
	deps := fixtureDeps()
	deps.now = func() time.Time { return time.Date(2026, 9, 2, 3, 4, 5, 6, time.UTC) }
	deps.recordLaunch = appendLaunchEdge
	deps.spawn = func(context.Context, []string) (webaction.Result, error) {
		return webaction.Result{Name: "impl-vava", Pane: "p9", OutputTail: "launch ready"}, nil
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewBufferString(`{"tool":"claude","model":"opus","effort":"high","tag":"impl","workspace":"w1"}`))
	request.RemoteAddr = "100.64.0.8:4400"
	newHandler(deps).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
	encoded, err := os.ReadFile(filepath.Join(state, "launch-edges.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var edge map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(encoded), &edge); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"name":      "impl-vava",
		"launcher":  "web-alice-example-com",
		"tool":      "claude",
		"model":     "opus",
		"effort":    "high",
		"tag":       "impl",
		"workspace": "w1",
		"pane":      "p9",
		"time":      "2026-09-02T03:04:05.000000006Z",
	}
	if !reflect.DeepEqual(edge, want) {
		t.Fatalf("edge=%s\nwant=%s", encoded, fmt.Sprint(want))
	}
}

func TestLaunchPinsAttributionValidationAndInfrastructureRefusals(t *testing.T) {
	valid := `{"tool":"codex","model":"gpt-5.4","tag":"impl","workspace":"w1"}`
	for _, test := range []struct {
		name   string
		body   string
		mutate func(*dependencies)
		status int
		detail string
	}{
		{name: "unattributed", body: valid, mutate: func(deps *dependencies) {
			deps.sender = func(context.Context, string) (string, error) { return "", errors.New("peer not found") }
		}, status: http.StatusConflict, detail: "peer not found"},
		{name: "whois unavailable", body: valid, mutate: func(deps *dependencies) {
			deps.sender = func(context.Context, string) (string, error) {
				return "", fmt.Errorf("%w: tailscaled down", webidentity.ErrUnavailable)
			}
		}, status: http.StatusBadGateway, detail: "tailscaled down"},
		{name: "script unavailable", body: valid, mutate: func(deps *dependencies) {
			deps.spawn = func(context.Context, []string) (webaction.Result, error) {
				return webaction.Result{}, fmt.Errorf("%w: missing", webaction.ErrUnavailable)
			}
		}, status: http.StatusBadGateway, detail: "missing"},
		{name: "unknown field", body: `{"tool":"codex","workspace":"w1","prompt":"unsafe"}`, status: http.StatusBadRequest, detail: "documented fields"},
		{name: "missing workspace", body: `{"tool":"codex"}`, status: http.StatusBadRequest, detail: "workspace is required"},
		{name: "unknown workspace", body: `{"tool":"codex","workspace":"missing"}`, status: http.StatusConflict, detail: "workspace is not live"},
		{name: "multiline model", body: `{"tool":"codex","workspace":"w1","model":"one\ntwo"}`, status: http.StatusBadRequest, detail: "one non-empty line"},
		{name: "unknown Claude effort", body: `{"tool":"claude","workspace":"w1","effort":"bogus"}`, status: http.StatusBadRequest, detail: "effort for claude must be one of: low, medium, high, xhigh, max"},
		{name: "unsupported Codex max effort", body: `{"tool":"codex","workspace":"w1","effort":"max"}`, status: http.StatusBadRequest, detail: "effort for codex must be one of: low, medium, high, xhigh"},
	} {
		t.Run(test.name, func(t *testing.T) {
			deps := fixtureDeps()
			if test.mutate != nil {
				test.mutate(&deps)
			}
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewBufferString(test.body))
			request.RemoteAddr = "100.64.0.8:4400"
			newHandler(deps).ServeHTTP(response, request)
			if response.Code != test.status || !bytes.Contains(response.Body.Bytes(), []byte(test.detail)) {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
		})
	}
}

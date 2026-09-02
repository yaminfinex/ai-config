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

func TestLaunchMapsRepoAndOptionalWorktreeToFleetArgv(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want []string
	}{
		{
			name: "generated worktree branch",
			body: `{"tool":"claude","model":"claude-fable-5-1","effort":"max","tag":"impl","repo":"/repo/root"}`,
			want: []string{"claude", "--model", "claude-fable-5-1", "--effort", "max", "--tag", "impl", "--worktree-branch", "launch-claude-20260902-030405", "--repo", "/repo/root"},
		},
		{
			name: "new worktree",
			body: `{"tool":"codex","model":"gpt-5.4-mini","effort":"xhigh","tag":"review","repo":"/repo/root","branch":"feature/web"}`,
			want: []string{"codex", "--model", "gpt-5.4-mini", "--effort", "xhigh", "--tag", "review", "--worktree-branch", "feature/web", "--repo", "/repo/root"},
		},
		{
			name: "Codex default model",
			body: `{"tool":"codex","model":"","effort":" ","tag":"impl","repo":"/repo/root"}`,
			want: []string{"codex", "--tag", "impl", "--worktree-branch", "launch-codex-20260902-030405", "--repo", "/repo/root"},
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
				OutputTail string   `json:"output_tail"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(result.Names, []string{"impl-vava"}) || result.OutputTail == "" {
				t.Fatalf("launch result=%#v", result)
			}
		})
	}
}

func TestLaunchGeneratedBranchesAddCollisionSuffixes(t *testing.T) {
	deps := fixtureDeps()
	deps.now = func() time.Time { return time.Date(2026, 9, 2, 3, 4, 5, 0, time.UTC) }
	branches := make(map[string]bool)
	deps.branchExists = func(_ context.Context, _, branch string) (bool, error) {
		return branches[branch], nil
	}
	var got []string
	deps.spawn = func(_ context.Context, args []string) (webaction.Result, error) {
		for index, arg := range args {
			if arg == "--worktree-branch" && index+1 < len(args) {
				got = append(got, args[index+1])
				branches[args[index+1]] = true
			}
		}
		return webaction.Result{Name: "impl-vava", Pane: "p9"}, nil
	}
	handler := newHandler(deps)
	for range 2 {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewBufferString(`{"tool":"codex","repo":"/repo/root"}`))
		request.RemoteAddr = "100.64.0.8:4400"
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("response=%d %s", response.Code, response.Body.String())
		}
	}
	want := []string{"launch-codex-20260902-030405", "launch-codex-20260902-030405-2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("branches=%q want=%q", got, want)
	}
}

func TestLaunchReturnsSpawnStderrVerbatim(t *testing.T) {
	deps := fixtureDeps()
	want := "fleet spawn: branch already exists\nretry with a different branch"
	deps.spawn = func(context.Context, []string) (webaction.Result, error) {
		return webaction.Result{}, errors.New(want)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewBufferString(`{"tool":"codex","tag":"impl","repo":"/repo/root"}`))
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
	request := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewBufferString(`{"tool":"claude","model":"opus","effort":"high","tag":"impl","repo":"/repo/root"}`))
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
		"name":     "impl-vava",
		"launcher": "web-alice-example-com",
		"tool":     "claude",
		"model":    "opus",
		"effort":   "high",
		"tag":      "impl",
		"repo":     "/repo/root",
		"time":     "2026-09-02T03:04:05.000000006Z",
	}
	if !reflect.DeepEqual(edge, want) {
		t.Fatalf("edge=%s\nwant=%s", encoded, fmt.Sprint(want))
	}
}

func TestLaunchPinsAttributionValidationAndInfrastructureRefusals(t *testing.T) {
	valid := `{"tool":"codex","model":"gpt-5.4","tag":"impl","repo":"/repo/root"}`
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
		{name: "unknown field", body: `{"tool":"codex","repo":"/repo/root","prompt":"unsafe"}`, status: http.StatusBadRequest, detail: "documented fields"},
		{name: "relative repo", body: `{"tool":"codex","repo":"relative"}`, status: http.StatusBadRequest, detail: "absolute path"},
		{name: "invalid branch", body: `{"tool":"codex","repo":"/repo/root","branch":"bad branch"}`, status: http.StatusBadRequest, detail: "branch must start"},
		{name: "multiline model", body: `{"tool":"codex","repo":"/repo/root","model":"one\ntwo"}`, status: http.StatusBadRequest, detail: "one non-empty line"},
		{name: "unknown Claude effort", body: `{"tool":"claude","repo":"/repo/root","effort":"bogus"}`, status: http.StatusBadRequest, detail: "effort for claude must be one of: low, medium, high, xhigh, max"},
		{name: "unsupported Codex max effort", body: `{"tool":"codex","repo":"/repo/root","effort":"max"}`, status: http.StatusBadRequest, detail: "effort for codex must be one of: low, medium, high, xhigh"},
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

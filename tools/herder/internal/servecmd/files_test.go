package servecmd

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/fileapi"
	"ai-config/tools/herder/internal/fileindex"
	"ai-config/tools/herder/internal/fileresolver"
	"ai-config/tools/herder/internal/fileroots"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/repoctx"
)

func TestResolveEndpointUsesLiveAgentThenConfiguredRootPreference(t *testing.T) {
	agentRoot := newFileAPIGitRepo(t)
	configuredRoot := newFileAPIGitRepo(t)
	for _, root := range []string{agentRoot, configuredRoot} {
		writeFileAPIFixture(t, root, "docs/README.md", "fixture\n")
		fileAPIGit(t, root, "add", ".")
		fileAPIGit(t, root, "commit", "-m", "fixture")
	}
	deps := fileAPIDeps(t, []string{configuredRoot}, []hcomidentity.Row{{Name: "dore", Tool: "codex", Status: "active", Directory: agentRoot}})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/resolve?q=README.md&agent=dore", nil)
	newHandler(deps).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("resolve = %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Candidates []fileresolver.Result      `json:"candidates"`
		Roots      []fileresolver.RootOutcome `json:"roots"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Candidates) != 2 || body.Candidates[0].Root != agentRoot || body.Candidates[1].Root != configuredRoot || body.Candidates[0].Tier != fileresolver.TierSuffix {
		t.Fatalf("candidates = %#v", body.Candidates)
	}
	if len(body.Roots) != 2 || body.Roots[0].Status != fileresolver.RootComplete || body.Roots[1].Status != fileresolver.RootComplete {
		t.Fatalf("roots = %#v", body.Roots)
	}

	response = httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?q=README.md&agent=missing", nil))
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"error":"unknown agent"`) {
		t.Fatalf("unknown agent = %d %s", response.Code, response.Body.String())
	}
}

func TestResolveEndpointKeepsHealthyAndDegradedResultsWhenAnotherRootFails(t *testing.T) {
	healthy := newFileAPIGitRepo(t)
	writeFileAPIFixture(t, healthy, "healthy-needle.md", "healthy\n")
	fileAPIGit(t, healthy, "add", ".")
	fileAPIGit(t, healthy, "commit", "-m", "fixture")

	degraded := t.TempDir()
	writeFileAPIFixture(t, degraded, "degraded-needle.md", "partial\n")
	writeFileAPIFixture(t, degraded, "blocked/secret.md", "secret\n")
	blocked := filepath.Join(degraded, "blocked")
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })

	failed := t.TempDir()
	if err := os.Chmod(failed, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(failed, 0o755) })

	deps := fileAPIDeps(t, []string{healthy, degraded, failed}, nil)
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?q=needle", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("resolve = %d %s", response.Code, response.Body.String())
	}
	var body resolveResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Candidates) != 2 || body.Candidates[0].Root != healthy || body.Candidates[1].Root != degraded {
		t.Fatalf("candidates = %#v", body.Candidates)
	}
	wantStatuses := []fileresolver.RootStatus{fileresolver.RootComplete, fileresolver.RootDegraded, fileresolver.RootFailed}
	if len(body.Roots) != len(wantStatuses) {
		t.Fatalf("roots = %#v", body.Roots)
	}
	for i, want := range wantStatuses {
		if body.Roots[i].Status != want {
			t.Errorf("roots[%d] = %#v, want status %q", i, body.Roots[i], want)
		}
	}
	if body.Roots[0].Detail != "" || !strings.Contains(body.Roots[1].Detail, "Permission denied") || body.Roots[2].Detail == "" {
		t.Fatalf("root details = %#v", body.Roots)
	}
}

func TestFileAndTreeEndpointsServeRealRootWithPinnedShapes(t *testing.T) {
	root := newFileAPIGitRepo(t)
	writeFileAPIFixture(t, root, ".hidden", "hidden\n")
	writeFileAPIFixture(t, root, "docs/readme.md", "hello\n")
	fileAPIGit(t, root, "add", ".")
	fileAPIGit(t, root, "commit", "-m", "fixture")
	fetched := time.Date(2026, 8, 28, 2, 30, 0, 731, time.UTC)
	deps := fileAPIDeps(t, []string{root}, nil)
	deps.now = func() time.Time { return fetched }

	fileURL := "/api/files?root=" + url.QueryEscape(root) + "&path=docs%2Freadme.md"
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, fileURL, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("file = %d %s", response.Code, response.Body.String())
	}
	var file fileapi.File
	if err := json.Unmarshal(response.Body.Bytes(), &file); err != nil {
		t.Fatal(err)
	}
	if file.Root != root || file.Path != "docs/readme.md" || file.Content == nil || *file.Content != "hello\n" || file.Truncated == nil || *file.Truncated || !file.FetchedAt.Equal(fetched) {
		t.Fatalf("file = %#v", file)
	}

	response = httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/files/tree?root="+url.QueryEscape(root), nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":".hidden"`) || !strings.Contains(response.Body.String(), `"name":"docs"`) {
		t.Fatalf("tree = %d %s", response.Code, response.Body.String())
	}
}

func TestFileEndpointsPinMissingHardCapGitAndSymlinkRefusals(t *testing.T) {
	root := newFileAPIGitRepo(t)
	outside := t.TempDir()
	writeFileAPIFixture(t, outside, "outside.md", "outside\n")
	if err := os.Symlink(filepath.Join(outside, "outside.md"), filepath.Join(root, "escape.md")); err != nil {
		t.Fatal(err)
	}
	large, err := os.Create(filepath.Join(root, "large.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := large.Truncate(fileapi.HardCap + 1); err != nil {
		t.Fatal(err)
	}
	if err := large.Close(); err != nil {
		t.Fatal(err)
	}
	deps := fileAPIDeps(t, []string{root}, nil)
	rootQuery := url.QueryEscape(root)

	tests := []struct {
		path       string
		status     int
		errorShape string
		detail     []string
	}{
		{"/api/files?root=" + rootQuery + "&path=missing.md", http.StatusNotFound, `"error":"not found"`, nil},
		{"/api/files?root=" + rootQuery + "&path=.git%2Fconfig", http.StatusConflict, `"error":"refused by substrate"`, []string{".git"}},
		{"/api/files?root=" + rootQuery + "&path=large.md", http.StatusConflict, `"error":"refused by substrate"`, []string{"4 MiB"}},
		{"/api/files?root=" + rootQuery + "&path=escape.md", http.StatusConflict, `"error":"refused by substrate"`, []string{root, outside}},
		{"/api/files?root=" + url.QueryEscape(t.TempDir()) + "&path=x", http.StatusNotFound, `"error":"unknown root"`, nil},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.errorShape) {
			t.Errorf("%s = %d %s", test.path, response.Code, response.Body.String())
		}
		for _, detail := range test.detail {
			if !strings.Contains(response.Body.String(), detail) {
				t.Errorf("%s detail missing %q: %s", test.path, detail, response.Body.String())
			}
		}
	}
}

func TestFileEndpointsRejectMissingDuplicateAndWrongMethodParameters(t *testing.T) {
	root := newFileAPIGitRepo(t)
	deps := fileAPIDeps(t, []string{root}, nil)
	tests := []string{
		"/api/resolve",
		"/api/resolve?q=a&q=b",
		"/api/files?root=" + url.QueryEscape(root),
		"/api/files/tree?root=" + url.QueryEscape(root) + "&root=" + url.QueryEscape(root),
	}
	for _, path := range tests {
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"error":"bad request"`) {
			t.Errorf("%s = %d %s", path, response.Code, response.Body.String())
		}
	}
}

func TestFileEndpointRootUniverseSurvivesMissingGit(t *testing.T) {
	root := t.TempDir()
	writeFileAPIFixture(t, root, "readme.md", "fixture\n")
	t.Setenv("PATH", t.TempDir())
	deps := fileAPIDeps(t, nil, []hcomidentity.Row{{Name: "dore", Tool: "codex", Status: "active", Directory: root}})
	rootQuery := url.QueryEscape(root)
	tests := []struct {
		path string
		want string
	}{
		{"/api/resolve?q=readme", `"status":"failed"`},
		{"/api/files?root=" + rootQuery + "&path=readme.md", `"content":"fixture\n"`},
		{"/api/files/tree?root=" + rootQuery, `"name":"readme.md"`},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), test.want) {
			t.Errorf("%s = %d %s", test.path, response.Code, response.Body.String())
		}
	}
}

func TestRootFlagIsRepeatableAndInvalidConfiguredRootFailsBeforeServe(t *testing.T) {
	var roots rootFlags
	if err := roots.Set("/first"); err != nil {
		t.Fatal(err)
	}
	if err := roots.Set("/second"); err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 || roots[0] != "/first" || roots[1] != "/second" {
		t.Fatalf("root flags = %q", roots)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	var stdout, stderr strings.Builder
	if code := Run([]string{"--root", missing}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "invalid root") || stdout.Len() != 0 {
		t.Fatalf("run = %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestAgentAndFleetAttachLiveRepoContextAtPinnedLocations(t *testing.T) {
	root := newFileAPIGitRepo(t)
	fileAPIGit(t, root, "remote", "add", "origin", "https://example.invalid/repo.git")
	writeFileAPIFixture(t, root, "tracked.md", "fixture\n")
	fileAPIGit(t, root, "add", ".")
	fileAPIGit(t, root, "commit", "-m", "fixture")
	deps := fileAPIDeps(t, nil, []hcomidentity.Row{{
		Name: "dore", Tool: "codex", Status: "active", Directory: root,
		LaunchContext: hcomidentity.LaunchContext{PaneID: "p1"},
	}})
	deps.snapshot = func() (herdrcli.Snapshot, error) {
		return herdrcli.Snapshot{
			Workspaces: []herdrcli.Workspace{{WorkspaceID: "w1", Worktree: &herdrcli.WorkspaceWorktree{RepoRoot: root, CheckoutPath: root}}},
			Tabs:       []herdrcli.Tab{{TabID: "t1", WorkspaceID: "w1"}},
			Panes:      []herdrcli.Pane{{PaneID: "p1", WorkspaceID: "w1", TabID: "t1", Agent: "codex", AgentSession: "session"}},
			Agents:     []herdrcli.Agent{{PaneID: "p1", Name: "dore", Agent: "codex", Status: "active"}},
		}, nil
	}

	agentResponse := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(agentResponse, httptest.NewRequest(http.MethodGet, "/api/agents/dore", nil))
	var agent map[string]any
	if err := json.Unmarshal(agentResponse.Body.Bytes(), &agent); err != nil {
		t.Fatal(err)
	}
	gitContext, ok := agent["git"].(map[string]any)
	if agentResponse.Code != http.StatusOK || agent["cwd"] != root || !ok || gitContext["branch"] != "main" || gitContext["remote_url"] != "https://example.invalid/repo.git" {
		t.Fatalf("agent = %d %#v", agentResponse.Code, agent)
	}

	fleetResponse := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(fleetResponse, httptest.NewRequest(http.MethodGet, "/api/fleet", nil))
	var fleet map[string]any
	if err := json.Unmarshal(fleetResponse.Body.Bytes(), &fleet); err != nil {
		t.Fatal(err)
	}
	workspace := fleet["workspaces"].([]any)[0].(map[string]any)
	workspaceGit, ok := workspace["git"].(map[string]any)
	if fleetResponse.Code != http.StatusOK || workspace["cwd"] != root || !ok || workspaceGit["branch"] != "main" || workspaceGit["remote_url"] != "https://example.invalid/repo.git" {
		t.Fatalf("workspace = %d %#v", fleetResponse.Code, workspace)
	}

	server := httptest.NewServer(newHandler(deps))
	defer server.Close()
	eventsResponse, err := http.Get(server.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer eventsResponse.Body.Close()
	reader := bufio.NewReader(eventsResponse.Body)
	if event, _ := readEvent(t, reader); event != "hello" {
		t.Fatalf("event = %q", event)
	}
	if event, data := readEvent(t, reader); event != "fleet" || !strings.Contains(data, `"cwd":"`+root+`"`) || !strings.Contains(data, `"remote_url":"https://example.invalid/repo.git"`) {
		t.Fatalf("fleet event = %q %s", event, data)
	}
}

func fileAPIDeps(t *testing.T, configured []string, roster []hcomidentity.Row) dependencies {
	t.Helper()
	deps := fixtureDeps()
	deps.configuredRoots = configured
	deps.roster = func() ([]hcomidentity.Row, error) { return roster, nil }
	deps.roots = func(ctx context.Context, configured []string, rows []hcomidentity.Row) (fileroots.Set, error) {
		agents := make([]fileroots.Agent, 0, len(rows))
		for _, row := range rows {
			agents = append(agents, fileroots.Agent{Name: row.Name, CWD: row.Directory})
		}
		return fileroots.Build(ctx, configured, agents)
	}
	deps.fileResolver = fileresolver.New(fileindex.New(fileindex.Options{}))
	deps.repoContext = repoctx.Read
	deps.now = time.Now
	return deps
}

func newFileAPIGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	fileAPIGit(t, root, "init", "-q", "-b", "main")
	fileAPIGit(t, root, "config", "user.name", "Fixture")
	fileAPIGit(t, root, "config", "user.email", "fixture@example.invalid")
	return root
}

func fileAPIGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeFileAPIFixture(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

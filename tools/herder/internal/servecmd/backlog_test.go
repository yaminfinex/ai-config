package servecmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/backlogapi"
	"ai-config/tools/herder/internal/filecandidate"
	"ai-config/tools/herder/internal/fileresolver"
)

func TestBacklogEndpointServesPinnedBoardShape(t *testing.T) {
	root := t.TempDir()
	writeFileAPIFixture(t, root, "backlog/config.yml", "statuses: [To Do, In Progress, Done]\nunknown_key: accepted\n")
	writeFileAPIFixture(t, root, "backlog/tasks/task-2.md", "---\nid: TASK-2\ntitle: 'Unicode — title'\nstatus: To Do\nordinal: 2\nlabels: []\nassignee: []\n---\nbody bytes stay in the file viewer\n")
	writeFileAPIFixture(t, root, "backlog/tasks/task-1.md", "---\nid: TASK-1\ntitle: first\nstatus: Done\nordinal: 1\npriority: high\n---\n")
	writeFileAPIFixture(t, root, "backlog/tasks/bad.md", "---\nid: BAD\nordinal: nope\n---\n")
	writeFileAPIFixture(t, root, "backlog/tasks/notes.txt", "not a task")
	fetched := time.Date(2026, 8, 28, 13, 0, 0, 731, time.UTC)
	deps := fileAPIDeps(t, []string{root}, nil)
	deps.now = func() time.Time { return fetched }

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/backlog?root="+url.QueryEscape(root)+"&path=backlog", nil)
	newHandler(deps).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("backlog=%d %s", response.Code, response.Body.String())
	}
	var body backlogapi.Result
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Root != root || body.Path != "backlog" || body.Backlog != nil || body.Statuses == nil || len(*body.Statuses) != 3 || body.Tasks == nil || len(*body.Tasks) != 2 || body.Unparsed == nil || len(*body.Unparsed) != 1 || body.Truncated == nil || *body.Truncated || !body.FetchedAt.Equal(fetched) {
		t.Fatalf("body=%#v", body)
	}
	if (*body.Tasks)[0].ID != "TASK-1" || (*body.Tasks)[1].ID != "TASK-2" || !strings.HasPrefix((*body.Tasks)[0].File, "tasks/") {
		t.Fatalf("ranked tasks=%#v", *body.Tasks)
	}
	if strings.Contains(response.Body.String(), "body bytes stay") {
		t.Fatalf("task body leaked: %s", response.Body.String())
	}
}

func TestBacklogEndpointReturns200UnavailableForReadableNonBoards(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "config-only"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFileAPIFixture(t, root, "config-only/config.yml", "statuses: [To Do]\n")
	deps := fileAPIDeps(t, []string{root}, nil)
	for _, path := range []string{"plain", "config-only"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/backlog?root="+url.QueryEscape(root)+"&path="+url.QueryEscape(path), nil)
		newHandler(deps).ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"backlog":{"status":"unavailable","reason":`) || !strings.Contains(response.Body.String(), `"fetched_at":`) {
			t.Fatalf("%s=%d %s", path, response.Code, response.Body.String())
		}
	}
}

func TestBacklogEndpointPinsMalformedAndContainmentRefusals(t *testing.T) {
	root := newFileAPIGitRepo(t)
	outside := t.TempDir()
	writeFileAPIFixture(t, outside, "config.yml", "statuses: [To Do]\n")
	if err := os.Mkdir(filepath.Join(outside, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	deps := fileAPIDeps(t, []string{root}, nil)
	rootQuery := url.QueryEscape(root)
	tests := []struct {
		path   string
		status int
		error  string
	}{
		{"/api/backlog?path=x", http.StatusBadRequest, "bad request"},
		{"/api/backlog?root=" + rootQuery, http.StatusBadRequest, "bad request"},
		{"/api/backlog?root=" + rootQuery + "&path=a&path=b", http.StatusBadRequest, "bad request"},
		{"/api/backlog?root=" + url.QueryEscape(t.TempDir()) + "&path=x", http.StatusNotFound, "unknown root"},
		{"/api/backlog?root=" + rootQuery + "&path=..", http.StatusConflict, "refused by substrate"},
		{"/api/backlog?root=" + rootQuery + "&path=.git", http.StatusConflict, "refused by substrate"},
		{"/api/backlog?root=" + rootQuery + "&path=escape", http.StatusConflict, "refused by substrate"},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.status || !strings.Contains(response.Body.String(), `"error":"`+test.error+`"`) {
			t.Errorf("%s=%d %s", test.path, response.Code, response.Body.String())
		}
	}
}

func TestResolveEndpointReturnsDirectoryKindsWithoutChangingRawScores(t *testing.T) {
	root := newFileAPIGitRepo(t)
	writeFileAPIFixture(t, root, "backlog/tasks/task.md", "fixture\n")
	fileAPIGit(t, root, "add", ".")
	fileAPIGit(t, root, "commit", "-m", "fixture")
	deps := fileAPIDeps(t, []string{root}, nil)

	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?q=backlog", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("resolve=%d %s", response.Code, response.Body.String())
	}
	var body resolveResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Candidates) < 2 || body.Candidates[0].Path != "backlog" || body.Candidates[0].Kind != filecandidate.KindDir || body.Candidates[0].Tier != fileresolver.TierExact || body.Candidates[0].Score <= 0 {
		t.Fatalf("candidates=%#v", body.Candidates)
	}
}

package servecmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/gitapi"
)

func TestGitEndpointsServePinnedShapes(t *testing.T) {
	root := newFileAPIGitRepo(t)
	writeFileAPIFixture(t, root, "history.txt", "one\r\n")
	fileAPIGit(t, root, "add", ".")
	fileAPIGit(t, root, "commit", "-m", "initial")
	sha := strings.TrimSpace(fileAPIGitOutput(t, root, "rev-parse", "HEAD"))
	writeFileAPIFixture(t, root, "history.txt", "one\r\ntwo\r\n")
	fileAPIGit(t, root, "add", "history.txt")
	writeFileAPIFixture(t, root, "history.txt", "one\r\ntwo\r\nthree\r\n")
	fetched := time.Date(2026, 8, 28, 15, 0, 0, 731, time.UTC)
	deps := fileAPIDeps(t, []string{root}, nil)
	deps.now = func() time.Time { return fetched }
	handler := newHandler(deps)
	rootQuery := url.QueryEscape(root)

	statusResponse := httptest.NewRecorder()
	handler.ServeHTTP(statusResponse, httptest.NewRequest(http.MethodGet, "/api/git/status?root="+rootQuery, nil))
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("status=%d %s", statusResponse.Code, statusResponse.Body.String())
	}
	var status gitapi.StatusResult
	if err := json.Unmarshal(statusResponse.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Root != root || status.Repo == nil || status.Repo.Branch != "main" || status.Entries == nil || len(*status.Entries) != 1 || (*status.Entries)[0].Additions == nil || *(*status.Entries)[0].Additions != 2 || !status.FetchedAt.Equal(fetched) {
		t.Fatalf("status body=%#v", status)
	}

	diffResponse := httptest.NewRecorder()
	handler.ServeHTTP(diffResponse, httptest.NewRequest(http.MethodGet, "/api/git/diff?root="+rootQuery+"&path=history.txt&base=uncommitted", nil))
	if diffResponse.Code != http.StatusOK || !strings.Contains(diffResponse.Body.String(), `"patch":"diff --git`) || !strings.Contains(diffResponse.Body.String(), `"kind":"uncommitted"`) || !strings.Contains(diffResponse.Body.String(), `"fetched_at":`) {
		t.Fatalf("diff=%d %s", diffResponse.Code, diffResponse.Body.String())
	}

	commitDiffResponse := httptest.NewRecorder()
	handler.ServeHTTP(commitDiffResponse, httptest.NewRequest(http.MethodGet, "/api/git/diff?root="+rootQuery+"&path=history.txt&base=commit&sha="+sha, nil))
	if commitDiffResponse.Code != http.StatusOK || commitDiffResponse.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || !strings.Contains(commitDiffResponse.Body.String(), `"kind":"commit"`) || !strings.Contains(commitDiffResponse.Body.String(), `"label":"root commit vs empty tree"`) || strings.Contains(commitDiffResponse.Body.String(), "fetched_at") {
		t.Fatalf("commit diff=%d cache=%q %s", commitDiffResponse.Code, commitDiffResponse.Header().Get("Cache-Control"), commitDiffResponse.Body.String())
	}

	logResponse := httptest.NewRecorder()
	handler.ServeHTTP(logResponse, httptest.NewRequest(http.MethodGet, "/api/git/log?root="+rootQuery+"&path=history.txt", nil))
	if logResponse.Code != http.StatusOK || !strings.Contains(logResponse.Body.String(), `"entries":[{`) || !strings.Contains(logResponse.Body.String(), `"subject":"initial"`) || !strings.Contains(logResponse.Body.String(), `"path_then":"history.txt"`) {
		t.Fatalf("log=%d %s", logResponse.Code, logResponse.Body.String())
	}

	fileResponse := httptest.NewRecorder()
	handler.ServeHTTP(fileResponse, httptest.NewRequest(http.MethodGet, "/api/git/file?root="+rootQuery+"&path=history.txt&sha="+sha, nil))
	if fileResponse.Code != http.StatusOK || fileResponse.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || !strings.Contains(fileResponse.Body.String(), `"content":"one\r\n"`) || strings.Contains(fileResponse.Body.String(), "fetched_at") {
		t.Fatalf("file=%d cache=%q %s", fileResponse.Code, fileResponse.Header().Get("Cache-Control"), fileResponse.Body.String())
	}
}

func TestGitEndpointsPinUnavailableAndRefusalShapes(t *testing.T) {
	repo := newFileAPIGitRepo(t)
	writeFileAPIFixture(t, repo, "tracked.txt", "one\n")
	fileAPIGit(t, repo, "add", ".")
	fileAPIGit(t, repo, "commit", "-m", "initial")
	plain := t.TempDir()
	deps := fileAPIDeps(t, []string{repo, plain}, nil)
	handler := newHandler(deps)

	statusResponse := httptest.NewRecorder()
	handler.ServeHTTP(statusResponse, httptest.NewRequest(http.MethodGet, "/api/git/status?root="+url.QueryEscape(plain), nil))
	if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), `"git":{"status":"unavailable","reason":"not a git repository"}`) {
		t.Fatalf("plain status=%d %s", statusResponse.Code, statusResponse.Body.String())
	}

	unknownRoot := httptest.NewRecorder()
	handler.ServeHTTP(unknownRoot, httptest.NewRequest(http.MethodGet, "/api/git/status?root="+url.QueryEscape(t.TempDir()), nil))
	if unknownRoot.Code != http.StatusNotFound || !strings.Contains(unknownRoot.Body.String(), `"error":"unknown root"`) {
		t.Fatalf("unknown root=%d %s", unknownRoot.Code, unknownRoot.Body.String())
	}

	badBase := httptest.NewRecorder()
	handler.ServeHTTP(badBase, httptest.NewRequest(http.MethodGet, "/api/git/diff?root="+url.QueryEscape(repo)+"&path=tracked.txt&base=guess", nil))
	if badBase.Code != http.StatusBadRequest || !strings.Contains(badBase.Body.String(), `"error":"bad request"`) {
		t.Fatalf("bad base=%d %s", badBase.Code, badBase.Body.String())
	}

	mutableWithSHA := httptest.NewRecorder()
	handler.ServeHTTP(mutableWithSHA, httptest.NewRequest(http.MethodGet, "/api/git/diff?root="+url.QueryEscape(repo)+"&path=tracked.txt&base=uncommitted&sha=0123456789012345678901234567890123456789", nil))
	if mutableWithSHA.Code != http.StatusBadRequest || !strings.Contains(mutableWithSHA.Body.String(), `"error":"bad request"`) {
		t.Fatalf("mutable sha=%d %s", mutableWithSHA.Code, mutableWithSHA.Body.String())
	}

	unknownCommit := httptest.NewRecorder()
	handler.ServeHTTP(unknownCommit, httptest.NewRequest(http.MethodGet, "/api/git/diff?root="+url.QueryEscape(repo)+"&path=tracked.txt&base=commit&sha=0123456789012345678901234567890123456789", nil))
	if unknownCommit.Code != http.StatusNotFound || !strings.Contains(unknownCommit.Body.String(), `"error":"not found"`) {
		t.Fatalf("unknown commit=%d %s", unknownCommit.Code, unknownCommit.Body.String())
	}

	branchUnavailable := httptest.NewRecorder()
	handler.ServeHTTP(branchUnavailable, httptest.NewRequest(http.MethodGet, "/api/git/diff?root="+url.QueryEscape(repo)+"&path=tracked.txt&base=branch", nil))
	if branchUnavailable.Code != http.StatusConflict || !strings.Contains(branchUnavailable.Body.String(), `"error":"base unavailable"`) || !strings.Contains(branchUnavailable.Body.String(), "origin/HEAD") {
		t.Fatalf("branch unavailable=%d %s", branchUnavailable.Code, branchUnavailable.Body.String())
	}

	badSHA := httptest.NewRecorder()
	handler.ServeHTTP(badSHA, httptest.NewRequest(http.MethodGet, "/api/git/file?root="+url.QueryEscape(repo)+"&path=tracked.txt&sha=nope", nil))
	if badSHA.Code != http.StatusNotFound || !strings.Contains(badSHA.Body.String(), `"error":"not found"`) {
		t.Fatalf("bad sha=%d %s", badSHA.Code, badSHA.Body.String())
	}
}

func TestGitStatusEmitsExplicitEmptyEntries(t *testing.T) {
	repo := newFileAPIGitRepo(t)
	writeFileAPIFixture(t, repo, "clean.txt", "clean\n")
	fileAPIGit(t, repo, "add", ".")
	fileAPIGit(t, repo, "commit", "-m", "clean")
	deps := fileAPIDeps(t, []string{repo}, nil)
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/git/status?root="+url.QueryEscape(repo), nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"entries":[]`) {
		t.Fatalf("clean status=%d %s", response.Code, response.Body.String())
	}
}

func fileAPIGitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

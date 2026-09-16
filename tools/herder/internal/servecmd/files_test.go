package servecmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/fileapi"
	"ai-config/tools/herder/internal/filecandidate"
	"ai-config/tools/herder/internal/fileindex"
	"ai-config/tools/herder/internal/fileresolver"
	"ai-config/tools/herder/internal/fileroots"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/repoctx"
)

func TestResolveEndpointKeepsLegacyAgentAndContextFreeRankingBytes(t *testing.T) {
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
	if len(body.Candidates) != 2 || body.Candidates[0].Root != agentRoot || body.Candidates[1].Root != configuredRoot || body.Candidates[0].Kind != filecandidate.KindFile || body.Candidates[1].Kind != filecandidate.KindFile || body.Candidates[0].Tier != fileresolver.TierSuffix {
		t.Fatalf("candidates = %#v", body.Candidates)
	}
	if len(body.Roots) != 2 || body.Roots[0].Status != fileresolver.RootComplete || body.Roots[1].Status != fileresolver.RootComplete {
		t.Fatalf("roots = %#v", body.Roots)
	}
	agentRanking, err := json.Marshal([]string{body.Candidates[0].Root, body.Candidates[1].Root})
	if err != nil {
		t.Fatal(err)
	}
	wantAgentRanking := fmt.Sprintf(`[%q,%q]`, agentRoot, configuredRoot)
	if string(agentRanking) != wantAgentRanking {
		t.Fatalf("agent ranking bytes=%s want %s", agentRanking, wantAgentRanking)
	}

	response = httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?q=README.md", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("context-free resolve = %d %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	contextFreeRanking, err := json.Marshal([]string{body.Candidates[0].Root, body.Candidates[1].Root})
	if err != nil {
		t.Fatal(err)
	}
	wantContextFreeRanking := fmt.Sprintf(`[%q,%q]`, configuredRoot, agentRoot)
	if string(contextFreeRanking) != wantContextFreeRanking {
		t.Fatalf("context-free ranking bytes=%s want %s", contextFreeRanking, wantContextFreeRanking)
	}

	response = httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?q=README.md&agent=missing", nil))
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"error":"unknown agent"`) {
		t.Fatalf("unknown agent = %d %s", response.Code, response.Body.String())
	}
}

func TestResolveEndpointAnchorsToViewedFileDirectoryAndAncestors(t *testing.T) {
	root := newFileAPIGitRepo(t)
	for _, path := range []string{
		"elsewhere/target.md",
		"target.md",
		"docs/target.md",
		"docs/guides/target.md",
		"docs/guides/deep/target.md",
		"docs/guides/deep/topic.md",
	} {
		writeFileAPIFixture(t, root, path, "fixture\n")
	}
	fileAPIGit(t, root, "add", ".")
	fileAPIGit(t, root, "commit", "-m", "anchor fixture")
	deps := fileAPIDeps(t, []string{root}, nil)
	requestURL := "/api/resolve?" + url.Values{
		"q": {"target.md"}, "root": {root}, "path": {"docs/guides/deep/topic.md"},
	}.Encode()
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, requestURL, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("resolve = %d %s", response.Code, response.Body.String())
	}
	var body resolveResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"docs/guides/deep/target.md",
		"docs/guides/target.md",
		"docs/target.md",
		"target.md",
		"elsewhere/target.md",
	}
	got := make([]string, len(body.Candidates))
	for index, candidate := range body.Candidates {
		got[index] = candidate.Path
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths=%#v want %#v", got, want)
	}
}

func TestResolveEndpointKeepsHealthyResultsWhenANonGitRootFailsWithoutWalking(t *testing.T) {
	healthy := newFileAPIGitRepo(t)
	writeFileAPIFixture(t, healthy, "healthy-needle.md", "healthy\n")
	fileAPIGit(t, healthy, "add", ".")
	fileAPIGit(t, healthy, "commit", "-m", "fixture")

	// A non-git root cannot be configured (CanonicalConfigured refuses it),
	// but if one reaches the index it must fail loudly, never be walked.
	nonGit := t.TempDir()
	writeFileAPIFixture(t, nonGit, "walked-needle.md", "must not appear\n")

	deps := fileAPIDeps(t, []string{healthy, nonGit}, nil)
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?q=needle", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("resolve = %d %s", response.Code, response.Body.String())
	}
	var body resolveResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Candidates) != 1 || body.Candidates[0].Root != healthy || body.Candidates[0].Path != "healthy-needle.md" {
		t.Fatalf("candidates = %#v", body.Candidates)
	}
	if len(body.Roots) != 2 || body.Roots[0].Status != fileresolver.RootComplete || body.Roots[0].Detail != "" ||
		body.Roots[1].Root != nonGit || body.Roots[1].Status != fileresolver.RootFailed || !strings.Contains(body.Roots[1].Detail, "not a git repository") {
		t.Fatalf("roots = %#v", body.Roots)
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

func TestRawFileEndpointServesHTMLAsByteIdenticalPlainText(t *testing.T) {
	root := newFileAPIGitRepo(t)
	html := append([]byte("<!doctype html><main>"), bytes.Repeat([]byte("x"), 600*1024)...)
	html = append(html, []byte("<span id=tail></span></main>")...)
	if err := os.WriteFile(filepath.Join(root, "large.html"), html, 0o644); err != nil {
		t.Fatal(err)
	}
	deps := fileAPIDeps(t, []string{root}, nil)
	requestURL := "/api/files/raw?root=" + url.QueryEscape(root) + "&path=large.html"
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, requestURL, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("raw file = %d %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := response.Header().Get("Content-Length"); got != fmt.Sprint(response.Body.Len()) {
		t.Errorf("Content-Length = %q, body length = %d", got, response.Body.Len())
	}
	if !bytes.Equal(response.Body.Bytes(), html) {
		t.Fatal("raw HTML body was sniffed or rewritten")
	}
}

func TestRawFileEndpointPinsFileRefusals(t *testing.T) {
	root := newFileAPIGitRepo(t)
	outside := t.TempDir()
	writeFileAPIFixture(t, outside, "outside.html", "outside")
	if err := os.Symlink(filepath.Join(outside, "outside.html"), filepath.Join(root, "escape.html")); err != nil {
		t.Fatal(err)
	}
	large, err := os.Create(filepath.Join(root, "large.html"))
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
	for _, test := range []struct {
		path   string
		status int
		shape  string
	}{
		{"/api/files/raw?root=" + rootQuery + "&path=missing.html", http.StatusNotFound, `"error":"not found"`},
		{"/api/files/raw?root=" + rootQuery + "&path=.git%2Fconfig", http.StatusConflict, `"error":"refused by substrate"`},
		{"/api/files/raw?root=" + rootQuery + "&path=large.html", http.StatusConflict, `"error":"refused by substrate"`},
		{"/api/files/raw?root=" + rootQuery + "&path=escape.html", http.StatusConflict, `"error":"refused by substrate"`},
		{"/api/files/raw?root=" + url.QueryEscape(t.TempDir()) + "&path=x", http.StatusNotFound, `"error":"not found"`},
		{"/api/files/raw?root=relative%2Froot&path=x", http.StatusNotFound, `"error":"unknown root"`},
		{"/api/files/raw?root=" + rootQuery, http.StatusBadRequest, `"error":"bad request"`},
	} {
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.shape) {
			t.Errorf("%s = %d %s", test.path, response.Code, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/files/raw", nil))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"error":"bad request"`) {
		t.Errorf("POST raw = %d %s", response.Code, response.Body.String())
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
		{"/api/files?root=" + url.QueryEscape(t.TempDir()) + "&path=x", http.StatusNotFound, `"error":"not found"`, nil},
		{"/api/files?root=relative%2Froot&path=x", http.StatusNotFound, `"error":"unknown root"`, nil},
		{"/api/files?root=" + url.QueryEscape(filepath.Join(t.TempDir(), "missing")) + "&path=x", http.StatusNotFound, `"error":"unknown root"`, nil},
		{"/api/files?root=" + url.QueryEscape(filepath.Join(root, "large.md")) + "&path=x", http.StatusNotFound, `"error":"unknown root"`, nil},
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
		"/api/resolve?q=a&root=" + url.QueryEscape(root),
		"/api/resolve?q=a&path=docs%2Freadme.md",
		"/api/resolve?q=a&root=&path=docs%2Freadme.md",
		"/api/resolve?q=a&agent=dore&root=" + url.QueryEscape(root) + "&path=docs%2Freadme.md",
		"/api/resolve?q=a&agent=&root=" + url.QueryEscape(root) + "&path=docs%2Freadme.md",
		"/api/resolve?q=a&root=" + url.QueryEscape(root) + "&root=" + url.QueryEscape(root) + "&path=docs%2Freadme.md",
		"/api/resolve?q=a&root=" + url.QueryEscape(root) + "&path=%2Fabsolute.md",
		"/api/resolve?q=a&root=" + url.QueryEscape(root) + "&path=..%2Fescape.md",
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

func TestResolveEndpointRejectsUnknownFileContextRoot(t *testing.T) {
	root := newFileAPIGitRepo(t)
	deps := fileAPIDeps(t, []string{root}, nil)
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?q=a&root=%2Funknown&path=docs%2Freadme.md", nil))
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"error":"unknown root"`) {
		t.Fatalf("unknown root = %d %s", response.Code, response.Body.String())
	}
}

func TestFileEndpointRootUniverseIsEmptyWithoutGitButDirectOpenRootsStillRead(t *testing.T) {
	root := t.TempDir()
	writeFileAPIFixture(t, root, "readme.md", "fixture\n")
	t.Setenv("PATH", t.TempDir())
	deps := fileAPIDeps(t, nil, []hcomidentity.Row{{Name: "dore", Tool: "codex", Status: "active", Directory: root}})
	rootQuery := url.QueryEscape(root)
	tests := []struct {
		path string
		want string
	}{
		{"/api/resolve?q=readme", `{"candidates":[],"roots":[]}`},
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

func TestResolveEndpointOpensExistingAbsolutePathDirectlyWithoutIndex(t *testing.T) {
	repo := newFileAPIGitRepo(t)
	writeFileAPIFixture(t, repo, "notes/design/spot-first.md", "design\n")
	plain := t.TempDir()
	writeFileAPIFixture(t, plain, "scratch.md", "scratch\n")
	outside := t.TempDir()
	writeFileAPIFixture(t, outside, "secret.md", "secret\n")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(repo, "escape.md")); err != nil {
		t.Fatal(err)
	}
	// Neither repo nor plain is live: the only live root is unrelated.
	deps := fileAPIDeps(t, []string{newFileAPIGitRepo(t)}, nil)
	deps.fileResolver = resolverFunc(func(context.Context, fileresolver.Request) (fileresolver.Resolution, error) {
		t.Fatal("direct open must not consult the resolver or any index")
		return fileresolver.Resolution{}, nil
	})

	tests := []struct {
		name  string
		query string
		root  string
		path  string
		kind  filecandidate.Kind
	}{
		{"file inside repo", filepath.Join(repo, "notes/design/spot-first.md"), repo, "notes/design/spot-first.md", filecandidate.KindFile},
		{"file inside repo with line suffix", filepath.Join(repo, "notes/design/spot-first.md") + ":12", repo, "notes/design/spot-first.md", filecandidate.KindFile},
		{"unclean path inside repo", filepath.Join(repo, "notes", "..", "notes", "design") + "/", repo, "notes/design", filecandidate.KindDir},
		{"directory inside repo", filepath.Join(repo, "notes"), repo, "notes", filecandidate.KindDir},
		{"repo top level itself", repo, repo, "", filecandidate.KindDir},
		{"repo top level with trailing slash", repo + "/", repo, "", filecandidate.KindDir},
		{"file outside any repo", filepath.Join(plain, "scratch.md"), plain, "scratch.md", filecandidate.KindFile},
		{"directory outside any repo", plain, filepath.Dir(plain), filepath.Base(plain), filecandidate.KindDir},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?q="+url.QueryEscape(test.query), nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s: resolve = %d %s", test.name, response.Code, response.Body.String())
		}
		var body resolveResponse
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Candidates) != 1 || body.Candidates[0].Root != test.root || body.Candidates[0].Path != test.path || body.Candidates[0].Kind != test.kind || body.Candidates[0].Tier != fileresolver.TierExact {
			t.Errorf("%s: candidates = %#v", test.name, body.Candidates)
		}
		if len(body.Roots) != 1 || body.Roots[0].Root != test.root || body.Roots[0].Status != fileresolver.RootComplete {
			t.Errorf("%s: roots = %#v", test.name, body.Roots)
		}
	}

	// Refused shapes count as "does not exist": with no live root containing
	// them the answer is an honest empty response and still no index.
	for _, query := range []string{
		filepath.Join(repo, "escape.md"),
		filepath.Join(repo, ".git", "config"),
		filepath.Join(repo, "notes", "absent.md"),
	} {
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?q="+url.QueryEscape(query), nil))
		if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != `{"candidates":[],"roots":[]}` {
			t.Errorf("%s = %d %s", query, response.Code, response.Body.String())
		}
	}
}

func TestResolveEndpointScopesMissingAbsolutePathToMostSpecificLiveRoot(t *testing.T) {
	outer := newFileAPIGitRepo(t)
	writeFileAPIFixture(t, outer, "docs/needle-outer.md", "outer\n")
	fileAPIGit(t, outer, "add", ".")
	fileAPIGit(t, outer, "commit", "-m", "fixture")
	nested := filepath.Join(outer, "vendor", "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	fileAPIGit(t, nested, "init", "-q", "-b", "main")
	writeFileAPIFixture(t, nested, "docs/needle-nested.md", "nested\n")
	deps := fileAPIDeps(t, nil, []hcomidentity.Row{
		{Name: "outer", Tool: "codex", Status: "active", Directory: outer},
		{Name: "nested", Tool: "codex", Status: "active", Directory: nested},
	})

	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?q="+url.QueryEscape(filepath.Join(nested, "docs", "needle-neted.md")), nil))
	if response.Code != http.StatusOK {
		t.Fatalf("resolve = %d %s", response.Code, response.Body.String())
	}
	var body resolveResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Roots) != 1 || body.Roots[0].Root != nested || body.Roots[0].Status != fileresolver.RootComplete {
		t.Fatalf("roots = %#v", body.Roots)
	}
	if len(body.Candidates) == 0 {
		t.Fatalf("candidates = %#v, want fuzzy matches from the nested root", body.Candidates)
	}
	for _, candidate := range body.Candidates {
		if candidate.Root != nested {
			t.Fatalf("candidate from another root: %#v", candidate)
		}
	}

	response = httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?q="+url.QueryEscape(filepath.Join(t.TempDir(), "nowhere.md")), nil))
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != `{"candidates":[],"roots":[]}` {
		t.Fatalf("unrooted missing path = %d %s", response.Code, response.Body.String())
	}
}

func TestResolveEndpointNeverListsNonGitAgentCWD(t *testing.T) {
	repo := newFileAPIGitRepo(t)
	writeFileAPIFixture(t, repo, "README.md", "repo\n")
	fileAPIGit(t, repo, "add", ".")
	fileAPIGit(t, repo, "commit", "-m", "fixture")
	home := t.TempDir()
	writeFileAPIFixture(t, home, "README.md", "home\n")
	deps := fileAPIDeps(t, nil, []hcomidentity.Row{
		{Name: "homebody", Tool: "codex", Status: "active", Directory: home},
		{Name: "coder", Tool: "codex", Status: "active", Directory: repo},
	})
	for _, path := range []string{"/api/resolve?q=README.md", "/api/resolve?q=README.md&agent=homebody", "/api/resolve?q=" + url.QueryEscape(filepath.Join(home, "missing.md"))} {
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), home) {
			t.Fatalf("%s = %d %s", path, response.Code, response.Body.String())
		}
	}
}

func TestDirectOpenChoosesRootFromLexicalPathNotFromFollowedDirectorySymlink(t *testing.T) {
	repo := newFileAPIGitRepo(t)
	writeFileAPIFixture(t, repo, "docs/inside.md", "inside\n")
	outside := t.TempDir()
	writeFileAPIFixture(t, outside, "hello.md", "outside\n")
	if err := os.Symlink(outside, filepath.Join(repo, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repo, ".git"), filepath.Join(repo, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repo, "docs"), filepath.Join(repo, "docs-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repo, "escape"), filepath.Join(repo, "chain")); err != nil {
		t.Fatal(err)
	}
	writeFileAPIFixture(t, outside, "sub/hello.md", "outside sub\n")
	writeFileAPIFixture(t, repo, "docs/sub/deep.md", "deep\n")
	// Exists on disk, so its refusal below is the .git law, not a missing file.
	writeFileAPIFixture(t, repo, ".git/refs/probe", "probe\n")
	plain := t.TempDir()
	writeFileAPIFixture(t, plain, "hello.md", "plain\n")
	if err := os.Symlink(outside, filepath.Join(plain, "link")); err != nil {
		t.Fatal(err)
	}
	deps := fileAPIDeps(t, []string{newFileAPIGitRepo(t)}, nil)

	refused := []string{
		filepath.Join(repo, "escape", "hello.md"),         // parent symlink must not rebase the root onto the escape
		filepath.Join(repo, "escape"),                     // the escaping directory itself
		filepath.Join(repo, "escape", "sub", "hello.md"),  // a real directory BELOW the symlink must not become the anchor
		filepath.Join(repo, "chain", "sub", "hello.md"),   // symlink chain to the escape
		filepath.Join(repo, "alias", "config"),            // alias into .git, refused on the resolved location
		filepath.Join(repo, "alias", "refs", "probe"),     // never advertise a root inside .git through an alias
		filepath.Join(plain, "link", "hello.md"),          // same law outside any repository
		filepath.Join(repo, "docs", "inside.md", "child"), // regular file in the chain
	}
	for _, query := range refused {
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?q="+url.QueryEscape(query), nil))
		if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != `{"candidates":[],"roots":[]}` {
			t.Errorf("%s = %d %s", query, response.Code, response.Body.String())
		}
	}
	// A symlink that stays inside the repository keeps the repository root.
	for _, test := range []struct{ query, root, path string }{
		{filepath.Join(repo, "docs-link", "inside.md"), repo, "docs-link/inside.md"},
		{filepath.Join(repo, "docs-link", "sub", "deep.md"), repo, "docs-link/sub/deep.md"},
		{filepath.Join(plain, "hello.md"), plain, "hello.md"},
	} {
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?q="+url.QueryEscape(test.query), nil))
		var body resolveResponse
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || len(body.Candidates) != 1 || body.Candidates[0].Root != test.root || body.Candidates[0].Path != test.path {
			t.Errorf("%s = %d %s", test.query, response.Code, response.Body.String())
		}
	}
}

func TestFileEndpointsRefuseGitDirectoryAsDirectOpenRoot(t *testing.T) {
	repo := newFileAPIGitRepo(t)
	writeFileAPIFixture(t, repo, "README.md", "repo\n")
	if err := os.Symlink(filepath.Join(repo, ".git"), filepath.Join(repo, "alias")); err != nil {
		t.Fatal(err)
	}
	plain := t.TempDir()
	writeFileAPIFixture(t, plain, "note.md", "plain\n")
	deps := fileAPIDeps(t, nil, nil)
	for _, root := range []string{filepath.Join(repo, ".git"), filepath.Join(repo, "alias"), filepath.Join(repo, ".git", "refs")} {
		for _, path := range []string{
			"/api/files?root=" + url.QueryEscape(root) + "&path=config",
			"/api/files/raw?root=" + url.QueryEscape(root) + "&path=config",
			"/api/files/tree?root=" + url.QueryEscape(root),
			"/api/backlog?root=" + url.QueryEscape(root) + "&path=",
		} {
			response := httptest.NewRecorder()
			newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"error":"unknown root"`) {
				t.Errorf("%s = %d %s", path, response.Code, response.Body.String())
			}
		}
	}
	// Ordinary directories outside any repository stay readable.
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/files?root="+url.QueryEscape(plain)+"&path=note.md", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"content":"plain\n"`) {
		t.Fatalf("plain direct-open root = %d %s", response.Code, response.Body.String())
	}
}

func TestResolveEndpointAnswersAbsoluteMentionFromDirectlyOpenedFileOutsideLiveRoots(t *testing.T) {
	live := newFileAPIGitRepo(t)
	writeFileAPIFixture(t, live, "docs/target.md", "target\n")
	fileAPIGit(t, live, "add", ".")
	fileAPIGit(t, live, "commit", "-m", "fixture")
	tmp := t.TempDir()
	target := filepath.Join(live, "docs", "target.md")
	writeFileAPIFixture(t, tmp, "mentions.md", "see "+target+"\n")
	deps := fileAPIDeps(t, nil, []hcomidentity.Row{{Name: "dore", Tool: "codex", Status: "active", Directory: live}})

	context := "&root=" + url.QueryEscape(tmp) + "&path=mentions.md"
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?q="+url.QueryEscape(target)+context, nil))
	var body resolveResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(body.Candidates) != 1 || body.Candidates[0].Root != live || body.Candidates[0].Path != "docs/target.md" || body.Candidates[0].Tier != fileresolver.TierExact {
		t.Fatalf("absolute mention with direct-open context = %d %s", response.Code, response.Body.String())
	}
	// A missing absolute path under a live repo with that non-live context
	// still scopes to the repo's fuzzy candidates and its single root.
	response = httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?q="+url.QueryEscape(filepath.Join(live, "docs", "targt.md"))+context, nil))
	body = resolveResponse{}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(body.Candidates) != 1 || body.Candidates[0].Root != live || body.Candidates[0].Path != "docs/target.md" || len(body.Roots) != 1 || body.Roots[0].Root != live {
		t.Fatalf("missing absolute with direct-open context = %d %s", response.Code, response.Body.String())
	}
	// A relative mention still needs a live context root.
	response = httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?q=target.md"+context, nil))
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"error":"unknown root"`) {
		t.Fatalf("relative mention with non-live context = %d %s", response.Code, response.Body.String())
	}
}

type resolverFunc func(context.Context, fileresolver.Request) (fileresolver.Resolution, error)

func (f resolverFunc) Resolve(ctx context.Context, request fileresolver.Request) ([]fileresolver.Result, error) {
	resolution, err := f(ctx, request)
	return resolution.Results, err
}

func (f resolverFunc) ResolveDetailed(ctx context.Context, request fileresolver.Request) (fileresolver.Resolution, error) {
	return f(ctx, request)
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

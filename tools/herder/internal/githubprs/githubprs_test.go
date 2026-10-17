package githubprs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// testdata/repository.json is a real GitHub answer to Query([309, 7, 999])
// with the titles, bodies, URLs and branches replaced (two bodies carry lane
// markers, one of them two) and PR 7 taken from a real merged PR.
func fixtureServer(t *testing.T) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	fixture, err := os.ReadFile("testdata/repository.json")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "bearer tok" {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		mu.Lock()
		requests = append(requests, request)
		mu.Unlock()
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func TestParseRef(t *testing.T) {
	for text, want := range map[string]string{
		"acme/app#12":                               "acme/app#12",
		"Acme/App#12":                               "acme/app#12",
		"https://github.com/acme/app/pull/12":       "acme/app#12",
		"https://github.com/acme/app/pull/12/files": "acme/app#12",
		"#12":                                   "",
		"acme/app":                              "",
		"https://gitlab.com/acme/app/pull/12":   "",
		"https://github.com/acme/app/issues/12": "",
	} {
		ref, ok := ParseRef(text)
		if got := map[bool]string{true: ref.String(), false: ""}[ok]; got != want {
			t.Errorf("ParseRef(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestPollLinksMarkersAndTwoLanes(t *testing.T) {
	server, requests := fixtureServer(t)
	var logs []string
	poller := New(HTTPPost(server.Client(), server.URL, "tok"), func(format string, args ...any) { logs = append(logs, format) })
	poller.Want(map[string][]int{"acme/app": {309, 7, 999}})
	poller.Poll(context.Background())

	if len(*requests) != 1 {
		t.Fatalf("requests = %d, want one per repo", len(*requests))
	}
	query := (*requests)[0]["query"].(string)
	for _, alias := range []string{"n309: pullRequest(number: 309)", "n7: pullRequest(number: 7)", "pullRequests(states: OPEN, first: 100"} {
		if !strings.Contains(query, alias) {
			t.Errorf("query lacks %q", alias)
		}
	}
	if vars := (*requests)[0]["variables"].(map[string]any); vars["owner"] != "acme" || vars["name"] != "app" {
		t.Errorf("variables = %v", vars)
	}

	linked, ok := poller.Get(Ref{"acme/app", 309})
	if !ok || !linked.Draft || linked.Checks != "FAILURE" || linked.Review != "REVIEW_REQUIRED" || linked.Mergeable != "MERGEABLE" || linked.Head != "branch-309" {
		t.Errorf("linked 309 = %+v", linked)
	}
	merged, ok := poller.Get(Ref{"acme/app", 7})
	if !ok || merged.State != "MERGED" || merged.Checks != "" {
		t.Errorf("linked 7 = %+v", merged)
	}
	if _, ok := poller.Get(Ref{"acme/app", 999}); ok {
		t.Error("a number that is not a PR has a row")
	}
	numbers := func(prs []PR) []int {
		out := []int{}
		for _, pr := range prs {
			out = append(out, pr.Number)
		}
		return out
	}
	if got := numbers(poller.Marked("lane-a")); len(got) != 2 || got[0] != 312 || got[1] != 314 {
		t.Errorf("lane-a marked = %v, want [312 314]", got)
	}
	if got := numbers(poller.Marked("lane-b")); len(got) != 1 || got[0] != 312 {
		t.Errorf("lane-b marked = %v, want [312]: one PR in two lanes", got)
	}
	if pr, _ := poller.Get(Ref{"acme/app", 314}); pr.Unresolved != 1 {
		t.Errorf("314 unresolved = %d, want 1", pr.Unresolved)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "%d linked, %d PRs") {
		t.Errorf("no request line in the log: %v", logs)
	}

	// A marked PR is kept by number afterwards, so it shows merged rather
	// than vanishing from the open list; a repo no longer wanted is dropped.
	poller.Want(map[string][]int{"acme/app": {309}})
	poller.Poll(context.Background())
	if query := (*requests)[1]["query"].(string); strings.Contains(query, "n7:") || !strings.Contains(query, "n312:") || !strings.Contains(query, "n314:") {
		t.Errorf("second query = %s", query)
	}
	poller.Want(map[string][]int{"acme/other": nil})
	poller.Poll(context.Background())
	if _, ok := poller.Get(Ref{"acme/app", 309}); ok || poller.Marked("lane-a")[0].Repo != "acme/other" {
		t.Error("an unwanted repo kept its rows")
	}
}

func TestNoTokenSaysSoAndNeverAsks(t *testing.T) {
	poller := New(nil, t.Logf)
	if poller.Problem() != NoToken {
		t.Fatalf("Problem = %q", poller.Problem())
	}
	poller.Want(map[string][]int{"acme/app": {1}})
	done := make(chan struct{})
	go func() { poller.Run(context.Background(), time.Millisecond); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run without a token kept running")
	}
}

func TestFailedRequestKeepsTheLastAnswer(t *testing.T) {
	server, _ := fixtureServer(t)
	good := HTTPPost(server.Client(), server.URL, "tok")
	fail := false
	poller := New(func(ctx context.Context, query string, vars map[string]any) ([]byte, error) {
		if fail {
			return HTTPPost(server.Client(), server.URL, "wrong")(ctx, query, vars)
		}
		return good(ctx, query, vars)
	}, t.Logf)
	poller.Want(map[string][]int{"acme/app": {309}})
	poller.Poll(context.Background())
	fail = true
	poller.Poll(context.Background())
	if _, ok := poller.Get(Ref{"acme/app", 309}); !ok {
		t.Error("a failed poll dropped the last answer")
	}
}

package servecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"ai-config/tools/herder/internal/fileresolver"
	"ai-config/tools/herder/internal/fileroots"
	"ai-config/tools/herder/internal/hcomidentity"
)

// A launch joins an agent the cache cannot know of: after Changed, no read
// serves rows observed before it, so the board shows the new agent at once.
func TestChangedRosterIsNeverServedFromRowsObservedBeforeIt(t *testing.T) {
	clock := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	deps := fixtureDeps()
	deps.rosterCache = &rosterCache{now: func() time.Time { return clock }}
	old := hcomidentity.Row{Name: "dore", Tool: "codex", Status: "active"}
	joined := hcomidentity.Row{Name: "vava", Tool: "codex", Status: "listening"}
	calls := 0
	deps.roster = func() ([]hcomidentity.Row, error) {
		calls++
		return []hcomidentity.Row{old, joined}, nil
	}
	deps.rosterCache.set([]hcomidentity.Row{old})
	clock = clock.Add(time.Second)
	deps.fleet().Changed()

	if rows, err := deps.fleet().Roster(); err != nil || calls != 1 || len(rows) != 2 {
		t.Fatalf("after Changed: rows=%#v calls=%d err=%v", rows, calls, err)
	}
	// The live answer began after the change, so it is fresh again.
	if rows, err := deps.fleet().Roster(); err != nil || calls != 1 || len(rows) != 2 {
		t.Fatalf("cached after the live ask: rows=%#v calls=%d err=%v", rows, calls, err)
	}
	// A slow poll that began before the change still lands, but not as fresh.
	clock = clock.Add(time.Second)
	deps.fleet().Changed()
	deps.rosterCache.setObserved([]hcomidentity.Row{old}, clock.Add(-time.Nanosecond))
	if _, ok := deps.fleet().Agents(); ok {
		t.Fatal("rows observed before Changed served fresh")
	}
}

// The request path reads the agent list through the roster cache while it is
// fresh within RosterFreshness, asks hcom live past it, and stores that
// answer for the next request.
func TestRequestRosterServesCacheWithinFreshnessAndGoesLivePastIt(t *testing.T) {
	clock := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	deps := fixtureDeps()
	deps.rosterCache = &rosterCache{now: func() time.Time { return clock }}
	cachedRow := hcomidentity.Row{Name: "dore", Tool: "codex", Status: "active", SessionID: "cached"}
	liveRow := hcomidentity.Row{Name: "dore", Tool: "codex", Status: "active", SessionID: "live"}
	calls := 0
	deps.roster = func() ([]hcomidentity.Row, error) {
		calls++
		return []hcomidentity.Row{liveRow}, nil
	}
	deps.rosterCache.set([]hcomidentity.Row{cachedRow})

	clock = clock.Add(RosterFreshness)
	rows, err := deps.fleet().Roster()
	if err != nil || calls != 0 || len(rows) != 1 || rows[0].SessionID != "cached" {
		t.Fatalf("within freshness: rows=%#v calls=%d err=%v", rows, calls, err)
	}

	clock = clock.Add(time.Nanosecond)
	rows, err = deps.fleet().Roster()
	if err != nil || calls != 1 || len(rows) != 1 || rows[0].SessionID != "live" {
		t.Fatalf("past freshness: rows=%#v calls=%d err=%v", rows, calls, err)
	}
	if rows, _ := deps.fleet().Roster(); calls != 1 || rows[0].SessionID != "live" {
		t.Fatalf("live answer not cached: rows=%#v calls=%d", rows, calls)
	}

	// A fresh cache that lacks the name a handler needs asks hcom live.
	newRow := hcomidentity.Row{Name: "vava", Tool: "claude", Status: "active", SessionID: "new"}
	deps.roster = func() ([]hcomidentity.Row, error) {
		calls++
		return []hcomidentity.Row{liveRow, newRow}, nil
	}
	if rows, err := deps.fleet().RosterHolding("dore"); err != nil || calls != 1 || len(rows) != 1 {
		t.Fatalf("held name: rows=%#v calls=%d err=%v", rows, calls, err)
	}
	if rows, err := deps.fleet().RosterHolding("vava"); err != nil || calls != 2 || len(rows) != 2 {
		t.Fatalf("missing name: rows=%#v calls=%d err=%v", rows, calls, err)
	}
}

func TestRootSetCacheReusesSetForUnchangedRows(t *testing.T) {
	deps := fixtureDeps()
	deps.rootSets = &rootSetCache{}
	builds := 0
	deps.roots = func(_ context.Context, configured []string, rows []hcomidentity.Row) (fileroots.Set, error) {
		builds++
		set := fileroots.Set{AgentRoot: map[string]string{}}
		for _, row := range rows {
			set.Roots = append(set.Roots, row.Directory)
		}
		return set, nil
	}
	rows := []hcomidentity.Row{{Name: "dore", Directory: "/invented/a"}, {Name: "vava", Directory: "/invented/b"}}
	for range 3 {
		if _, err := deps.rootSets.get(context.Background(), deps.configuredRoots, rows, deps.roots); err != nil {
			t.Fatal(err)
		}
	}
	// Row order is not part of the key.
	if _, err := deps.rootSets.get(context.Background(), deps.configuredRoots, []hcomidentity.Row{rows[1], rows[0]}, deps.roots); err != nil || builds != 1 {
		t.Fatalf("unchanged rows: builds=%d err=%v", builds, err)
	}
	moved := []hcomidentity.Row{rows[0], {Name: "vava", Directory: "/invented/c"}}
	set, err := deps.rootSets.get(context.Background(), deps.configuredRoots, moved, deps.roots)
	if err != nil || builds != 2 || !set.Contains("/invented/c") {
		t.Fatalf("moved cwd: set=%#v builds=%d err=%v", set, builds, err)
	}
	deps.rootSets.at = deps.rootSets.at.Add(-RootSetFreshness)
	if _, err := deps.rootSets.get(context.Background(), deps.configuredRoots, moved, deps.roots); err != nil || builds != 3 {
		t.Fatalf("past RootSetFreshness: builds=%d err=%v", builds, err)
	}
}

// An agent that joined after the cached roster was polled is found by the
// live fallback, never refused as unknown.
func TestResolveFallsBackLiveForAgentMissingFromCache(t *testing.T) {
	repo := newFileAPIGitRepo(t)
	writeFileAPIFixture(t, repo, "notes/alpha.md", "a\n")
	deps := fileAPIDeps(t, nil, []hcomidentity.Row{{Name: "vava", Tool: "codex", Status: "active", Directory: repo}})
	deps.rosterCache = &rosterCache{}
	deps.rosterCache.set([]hcomidentity.Row{})
	response := httptest.NewRecorder()
	newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?q=alpha&agent=vava", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("resolve = %d %s", response.Code, response.Body.String())
	}
}

func TestResolveCapsRankedCandidatesAndReportsTotal(t *testing.T) {
	repo := newFileAPIGitRepo(t)
	for k := range 25 {
		writeFileAPIFixture(t, repo, fmt.Sprintf("notes/needle-%02d.md", k), "x\n")
	}
	writeFileAPIFixture(t, repo, "needle", "exact\n")
	deps := fileAPIDeps(t, nil, []hcomidentity.Row{{Name: "dore", Tool: "codex", Status: "active", Directory: repo}})
	resolve := func(query string) (int, resolveResponse, string) {
		t.Helper()
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?"+query, nil))
		var body resolveResponse
		if response.Code == http.StatusOK {
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
		}
		return response.Code, body, response.Body.String()
	}

	_, all, _ := resolve("q=needle&limit=100")
	if all.Total != len(all.Candidates) || all.Total < 26 {
		t.Fatalf("uncapped total=%d candidates=%d", all.Total, len(all.Candidates))
	}
	code, capped, raw := resolve("q=needle")
	if code != http.StatusOK || len(capped.Candidates) != DefaultResolveLimit || capped.Total != all.Total {
		t.Fatalf("default cap = %d candidates=%d total=%d %s", code, len(capped.Candidates), capped.Total, raw)
	}
	// The cap keeps the top of the unchanged ranking.
	for k, candidate := range capped.Candidates {
		if !reflect.DeepEqual(candidate, all.Candidates[k]) {
			t.Fatalf("capped[%d]=%#v, want %#v", k, candidate, all.Candidates[k])
		}
	}
	if capped.Candidates[0].Path != "needle" || capped.Candidates[0].Tier != fileresolver.TierExact {
		t.Fatalf("top = %#v, want the exact match", capped.Candidates[0])
	}
	if _, three, _ := resolve("q=needle&limit=3"); len(three.Candidates) != 3 || three.Total != all.Total {
		t.Fatalf("limit=3 candidates=%d total=%d", len(three.Candidates), three.Total)
	}
	for _, bad := range []string{"0", "101", "-1", "ten", ""} {
		if code, _, raw := resolve("q=needle&limit=" + url.QueryEscape(bad)); code != http.StatusBadRequest {
			t.Fatalf("limit=%q = %d %s", bad, code, raw)
		}
	}
	if code, _, raw := resolve("q=needle&limit=2&limit=3"); code != http.StatusBadRequest {
		t.Fatalf("repeated limit = %d %s", code, raw)
	}
}

// One repository checked out three times shows each file once: the main
// checkout's copy with no agent, the agent's own worktree copy with one, and
// total counts after the fold. Another repository's same path stays apart.
func TestResolveFoldsWorktreeCopiesIntoOneCandidate(t *testing.T) {
	repo := newFileAPIGitRepo(t)
	writeFileAPIFixture(t, repo, "notes/alpha.md", "a\n")
	fileAPIGit(t, repo, "add", ".")
	fileAPIGit(t, repo, "commit", "-q", "-m", "fixture")
	worktrees := t.TempDir()
	one, two := filepath.Join(worktrees, "one"), filepath.Join(worktrees, "two")
	fileAPIGit(t, repo, "worktree", "add", "-q", "-b", "one", one)
	fileAPIGit(t, repo, "worktree", "add", "-q", "-b", "two", two)
	other := newFileAPIGitRepo(t)
	writeFileAPIFixture(t, other, "notes/alpha.md", "b\n")
	fileAPIGit(t, other, "add", ".")
	fileAPIGit(t, other, "commit", "-q", "-m", "fixture")
	// The main checkout joins last, so only the explicit tie-break puts it first.
	deps := fileAPIDeps(t, nil, []hcomidentity.Row{
		{Name: "wone", Tool: "codex", Status: "active", Directory: one},
		{Name: "wtwo", Tool: "codex", Status: "active", Directory: two},
		{Name: "oter", Tool: "codex", Status: "active", Directory: other},
		{Name: "mane", Tool: "codex", Status: "active", Directory: repo},
	})
	resolve := func(query string) resolveResponse {
		t.Helper()
		response := httptest.NewRecorder()
		newHandler(deps).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/resolve?"+query, nil))
		var body resolveResponse
		if response.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", query, response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	real := func(path string) string {
		t.Helper()
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		return resolved
	}
	repo, one, two, other = real(repo), real(one), real(two), real(other)

	plain := resolve("q=notes/alpha.md")
	if plain.Total != 2 || len(plain.Candidates) != 2 {
		t.Fatalf("no agent: %#v", plain)
	}
	if top := plain.Candidates[0]; top.Root != repo || top.Also != 2 || !reflect.DeepEqual(top.AlsoRoots, []string{one, two}) {
		t.Fatalf("no agent top = %#v, want the main checkout with both worktrees folded", top)
	}
	if rest := plain.Candidates[1]; rest.Root != other || rest.Also != 0 {
		t.Fatalf("other repo = %#v", rest)
	}

	agent := resolve("q=notes/alpha.md&agent=wtwo")
	if agent.Total != 2 || agent.Candidates[0].Root != two || agent.Candidates[0].Also != 2 || !reflect.DeepEqual(agent.Candidates[0].AlsoRoots, []string{one, repo}) {
		t.Fatalf("agent wtwo = %#v", agent)
	}
}

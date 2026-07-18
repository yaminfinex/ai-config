package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"
)

// The /api/v1 surface is the wire contract with tools/mc/ui. These tests pin
// the DTO shapes against the same fake-binary fixtures the HTML pages use.

func apiTestWeb(t *testing.T) *Web {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenStore(dir + "/journal.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Open("task-x", "grok A/B", "context", "decide", "moment", "mission-one", "builder-lobo", []string{"human-yamen"}, "owner", "managed"); err != nil {
		t.Fatal(err)
	}
	if err := s.Link("task-x", Msg{BusID: 41, From: "builder-lobo", Text: "which shape wins?", TS: "2026-07-15T05:40:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Open("task-y", "quiet reply thread", "context", "reply", "moment", "mission-one", "worker-suna", nil, "owner", "managed"); err != nil {
		t.Fatal(err)
	}
	if err := s.Open("task-z", "other mission thread", "context", "reply", "moment", "mission-two", "worker-suna", nil, "owner", "managed"); err != nil {
		t.Fatal(err)
	}
	hcom := writeExecutable(t, dir, "hcom", "#!/bin/sh\ncase \"$1\" in list) printf '[]';; *) : ;; esac\n")
	herder := writeExecutable(t, dir, "herder", `#!/bin/sh
printf '%s\n' '{"kind":"session","label":"builder-lobo","guid":"g-1","tool":"claude","role":"builder","state":"seated","cwd":"/work","branch":"task-x","mission":{"slug":"mission-one","source":"marker"}}'
`)
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf(`#!/bin/sh
case "$*" in
  *--all*) cat %q ;;
  *) cat %q ;;
esac
`, mishFixture(t, "status-all.json"), mishFixture(t, "status-mission.json")))
	w := NewWeb(s, &Bus{Hcom: hcom}, nil, "human-yamen", "owner", herder, newMissionResolver(mish, ""))
	w.now = func() time.Time { return time.Date(2026, 7, 15, 6, 0, 0, 0, time.UTC) }
	return w
}

func getJSON(t *testing.T, w *Web, url string, out any) *httptest.ResponseRecorder {
	t.Helper()
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, url, nil))
	if rw.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", url, rw.Code, rw.Body.String())
	}
	if ct := rw.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("GET %s content-type = %q", url, ct)
	}
	if err := json.Unmarshal(rw.Body.Bytes(), out); err != nil {
		t.Fatalf("GET %s: %v: %s", url, err, rw.Body.String())
	}
	return rw
}

func checkProvenance(t *testing.T, p apiProvenanceDTO, source string) {
	t.Helper()
	if p.Source != source {
		t.Fatalf("provenance source = %q, want %q", p.Source, source)
	}
	if p.Version == "" {
		t.Fatalf("provenance for %s has no version token", source)
	}
	if _, err := time.Parse(time.RFC3339, p.ObservedAt); err != nil {
		t.Fatalf("provenance observedAt %q: %v", p.ObservedAt, err)
	}
}

func TestAPIVersionStampsEverySourceFamily(t *testing.T) {
	w := apiTestWeb(t)
	var v apiVersionDTO
	getJSON(t, w, "/api/v1/version", &v)
	checkProvenance(t, v.Journal, sourceJournal)
	checkProvenance(t, v.Missions, sourceMissions)
	checkProvenance(t, v.Roster, sourceRoster)
	cursor, generation := w.store.Version()
	if want := fmt.Sprintf("%d-%d", cursor, generation); v.Journal.Version != want {
		t.Fatalf("journal version = %q, want %q", v.Journal.Version, want)
	}
}

func TestAPIVersionJournalTokenMovesWithStore(t *testing.T) {
	w := apiTestWeb(t)
	var before apiVersionDTO
	getJSON(t, w, "/api/v1/version", &before)
	if err := w.store.Open("task-new", "new thread", "", "reply", "moment", "mission-one", "worker-suna", nil, "owner", "managed"); err != nil {
		t.Fatal(err)
	}
	var after apiVersionDTO
	getJSON(t, w, "/api/v1/version", &after)
	if before.Journal.Version == after.Journal.Version {
		t.Fatal("journal token did not move after a store write")
	}
	if before.Missions.Version != after.Missions.Version {
		t.Fatal("missions token moved without a mission content change")
	}
	if before.Roster.Version != after.Roster.Version {
		t.Fatal("roster token moved without a roster content change")
	}
}

func TestContentTokenTracksContentNotObservation(t *testing.T) {
	a := missionStatus{Slug: "mission-one", OK: true, FetchedAt: time.Unix(100, 0)}
	b := a
	b.FetchedAt = time.Unix(999, 0) // re-observed, identical content
	if contentToken([]missionStatus{a}, "") != contentToken([]missionStatus{b}, "") {
		t.Fatal("token moved on observation time alone")
	}
	c := a
	c.Board.Total = 7
	if contentToken([]missionStatus{a}, "") == contentToken([]missionStatus{c}, "") {
		t.Fatal("token did not move on content change")
	}
	if contentToken([]missionStatus{a}, "") == contentToken([]missionStatus{a}, "degraded") {
		t.Fatal("token did not move on warning change")
	}
}

func TestAPIMissionsListsAllStatuses(t *testing.T) {
	w := apiTestWeb(t)
	var out apiMissionsDTO
	getJSON(t, w, "/api/v1/missions", &out)
	if out.Warning != "" {
		t.Fatalf("unexpected warning %q", out.Warning)
	}
	checkProvenance(t, out.Provenance, sourceMissions)
	if len(out.Missions) != 2 {
		t.Fatalf("missions = %+v", out.Missions)
	}
	byslug := map[string]apiMissionDTO{}
	for _, m := range out.Missions {
		byslug[m.Slug] = m
	}
	healthy, ok := byslug["mission-one"]
	if !ok || !healthy.OK || healthy.Owner != "riley" || !healthy.BoardAvailable {
		t.Fatalf("mission-one = %+v", healthy)
	}
	broken, ok := byslug["mission-broken"]
	if !ok || broken.OK || len(broken.Warnings) == 0 {
		t.Fatalf("mission-broken = %+v", broken)
	}
	var version apiVersionDTO
	getJSON(t, w, "/api/v1/version", &version)
	if version.Missions.Version != out.Provenance.Version {
		t.Fatal("missions list and version endpoint disagree on the missions token")
	}
}

func TestAPIMissionsSurfacesRefusalAsWarning(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir + "/journal.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf("#!/bin/sh\ncat %q\nexit 1\n", mishFixture(t, "status-repo-unset.json")))
	w := NewWeb(s, &Bus{Hcom: "/missing/hcom"}, nil, "human-yamen", "owner", "/missing/herder", newMissionResolver(mish, ""))
	var out apiMissionsDTO
	getJSON(t, w, "/api/v1/missions", &out)
	if len(out.Missions) != 0 || out.Warning == "" {
		t.Fatalf("expected empty list with warning, got %+v", out)
	}
	checkProvenance(t, out.Provenance, sourceMissions)
}

func TestAPIMissionDetailCarriesThreadsAndRoster(t *testing.T) {
	w := apiTestWeb(t)
	var out apiMissionDetailDTO
	getJSON(t, w, "/api/v1/mission/mission-one", &out)
	if out.Mission.Status.Slug != "mission-one" {
		t.Fatalf("mission = %+v", out.Mission.Status)
	}
	checkProvenance(t, out.Mission.Provenance, sourceMissions)
	checkProvenance(t, out.Threads.Provenance, sourceJournal)
	checkProvenance(t, out.Roster.Provenance, sourceRoster)
	if len(out.Threads.Rows) != 2 {
		t.Fatalf("threads = %+v", out.Threads.Rows)
	}
	var withMsg apiThreadDTO
	for _, th := range out.Threads.Rows {
		if th.ID == "task-x" {
			withMsg = th
		}
		if th.ID == "task-z" {
			t.Fatal("task-z belongs to mission-two and must not leak in")
		}
	}
	if withMsg.MessageCount != 1 || withMsg.LastFrom != "builder-lobo" || withMsg.LastText != "which shape wins?" {
		t.Fatalf("task-x = %+v", withMsg)
	}
	if withMsg.Expects != "decide" || withMsg.Status != "open" {
		t.Fatalf("task-x = %+v", withMsg)
	}
	agents := out.Roster.Agents
	if len(agents) != 1 || agents[0].Name != "builder-lobo" || agents[0].Role != "builder" {
		t.Fatalf("agents = %+v", agents)
	}
	if agents[0].MissionSource != "marker" {
		t.Fatalf("agent missionSource = %q, want marker", agents[0].MissionSource)
	}
	if out.Roster.Warning != "" {
		t.Fatalf("roster warning = %q", out.Roster.Warning)
	}
}

func TestAPIMissionDetailUnknownSlugStaysDegradedHonest(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir + "/journal.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf("#!/bin/sh\ncat %q\nexit 1\n", mishFixture(t, "status-mission-not-found.json")))
	w := NewWeb(s, &Bus{Hcom: "/missing/hcom"}, nil, "human-yamen", "owner", "/missing/herder", newMissionResolver(mish, ""))
	var out apiMissionDetailDTO
	getJSON(t, w, "/api/v1/mission/ghost", &out)
	if out.Mission.Status.OK {
		t.Fatalf("mission = %+v", out.Mission.Status)
	}
	if out.Mission.Status.Slug != "ghost" || len(out.Mission.Status.Warnings) == 0 {
		t.Fatalf("mission = %+v", out.Mission.Status)
	}
	checkProvenance(t, out.Mission.Provenance, sourceMissions)
	if out.Roster.Warning == "" {
		t.Fatal("missing roster binaries should surface a roster warning")
	}
	checkProvenance(t, out.Roster.Provenance, sourceRoster)
}

func TestUIHandlerServesSPAWithFallback(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":        {Data: []byte("<html>shell</html>")},
		"assets/app-abc.js": {Data: []byte("console.log(1)")},
	}
	h := uiHandler(fsys)

	for _, tc := range []struct {
		url, want, cache string
	}{
		{"/ui/", "<html>shell</html>", "no-cache"},
		{"/ui/mission/mission-one", "<html>shell</html>", "no-cache"},
		{"/ui/assets/app-abc.js", "console.log(1)", "public, max-age=31536000, immutable"},
	} {
		rw := httptest.NewRecorder()
		h(rw, httptest.NewRequest(http.MethodGet, tc.url, nil))
		if rw.Code != http.StatusOK || rw.Body.String() != tc.want {
			t.Fatalf("GET %s = %d %q", tc.url, rw.Code, rw.Body.String())
		}
		if got := rw.Header().Get("Cache-Control"); got != tc.cache {
			t.Fatalf("GET %s cache-control = %q, want %q", tc.url, got, tc.cache)
		}
	}

	rw := httptest.NewRecorder()
	uiHandler(fstest.MapFS{})(rw, httptest.NewRequest(http.MethodGet, "/ui/", nil))
	if rw.Code != http.StatusNotImplemented {
		t.Fatalf("unbuilt ui = %d, want 501", rw.Code)
	}
}

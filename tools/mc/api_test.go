package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sort"
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

// A change only the per-slug projection can observe (mish's git-staleness
// check runs for --mission, never for --all JSON) must move the key a
// detail-page client polls, while every other family — including the list
// token — holds. This is the reason /version has a mission scope at all.
func TestAPIVersionMissionScopeSeesPerSlugOnlyChanges(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir + "/journal.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	hcom := writeExecutable(t, dir, "hcom", "#!/bin/sh\ncase \"$1\" in list) printf '[]';; *) : ;; esac\n")
	herder := writeExecutable(t, dir, "herder", "#!/bin/sh\nexit 0\n")
	// --all output never changes; --mission output grows the git-staleness
	// warning once the flip file appears (mirrors status.go:552-556).
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf(`#!/bin/sh
case "$*" in
  *--all*)
    printf '%%s\n' '[{"ok":true,"slug":"mission-one","manifest":{"mission":"mission-one","owner":"riley"},"board":{"available":true,"total":1,"counts":[]},"warnings":[]}]'
    ;;
  *"--mission mission-one"*)
    if [ -f %q ]; then
      printf '%%s\n' '{"ok":true,"slug":"mission-one","manifest":{"mission":"mission-one","owner":"riley"},"board":{"available":true,"total":1,"counts":[]},"warnings":["mission subtree has uncommitted or unpushed changes"]}'
    else
      printf '%%s\n' '{"ok":true,"slug":"mission-one","manifest":{"mission":"mission-one","owner":"riley"},"board":{"available":true,"total":1,"counts":[]},"warnings":[]}'
    fi
    ;;
esac
`, dir+"/flip"))
	resolver := newMissionResolver(mish, "")
	resolver.ttl = 0 // observe every poll; the token must still only move on content
	w := NewWeb(s, &Bus{Hcom: hcom}, nil, "human-yamen", "owner", herder, resolver)

	var before apiVersionDTO
	getJSON(t, w, "/api/v1/version?mission=mission-one", &before)
	if before.Mission == nil {
		t.Fatal("mission-scoped poll carries no mission stamp")
	}
	checkProvenance(t, *before.Mission, sourceMissions)
	var detailBefore apiMissionDetailDTO
	getJSON(t, w, "/api/v1/mission/mission-one", &detailBefore)
	if detailBefore.Mission.Provenance.Version != before.Mission.Version {
		t.Fatal("polled mission stamp and detail section token disagree")
	}

	// The same content re-observed: the polled key must hold.
	var steady apiVersionDTO
	getJSON(t, w, "/api/v1/version?mission=mission-one", &steady)
	if steady.Mission == nil || steady.Mission.Version != before.Mission.Version {
		t.Fatal("mission token moved without a content change")
	}

	if err := os.WriteFile(dir+"/flip", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var after apiVersionDTO
	getJSON(t, w, "/api/v1/version?mission=mission-one", &after)
	if after.Mission == nil || after.Mission.Version == before.Mission.Version {
		t.Fatal("per-slug-only change did not move the polled mission token")
	}
	if after.Missions.Version != before.Missions.Version {
		t.Fatal("list token moved on a per-slug-only change")
	}
	if after.Journal.Version != before.Journal.Version || after.Roster.Version != before.Roster.Version {
		t.Fatal("unrelated families moved on a per-slug-only change")
	}
	var detailAfter apiMissionDetailDTO
	getJSON(t, w, "/api/v1/mission/mission-one", &detailAfter)
	if detailAfter.Mission.Provenance.Version != after.Mission.Version {
		t.Fatal("detail section token drifted from the polled mission stamp")
	}
	var bare apiVersionDTO
	getJSON(t, w, "/api/v1/version", &bare)
	if bare.Mission != nil {
		t.Fatal("unscoped poll must not carry a mission stamp")
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

// --- raw wire-shape pinning ---------------------------------------------
//
// The typed tests above decode responses through the same DTO structs that
// produced them, so a json tag rename moves encoder and decoder in lockstep
// and slips through. These tests pin the external JSON keys as literals:
// they are the frontend entity layer's contract, and they break on any
// wire-shape change regardless of what the Go structs think.

func rawGET(t *testing.T, w *Web, url string) map[string]any {
	t.Helper()
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, url, nil))
	if rw.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", url, rw.Code, rw.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rw.Body.Bytes(), &out); err != nil {
		t.Fatalf("GET %s: %v: %s", url, err, rw.Body.String())
	}
	return out
}

// wantKeys asserts v is a JSON object holding exactly the wanted keys and
// returns it for descent.
func wantKeys(t *testing.T, label string, v any, want ...string) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s: not a JSON object: %T", label, v)
	}
	got := make([]string, 0, len(m))
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)
	sorted := append([]string{}, want...)
	sort.Strings(sorted)
	if !slices.Equal(got, sorted) {
		t.Fatalf("%s keys = %v, want %v", label, got, sorted)
	}
	return m
}

// wantArray asserts v is a JSON array — a nil Go slice encodes as null, and
// null is a contract break: list-shaped fields stay arrays even when empty.
func wantArray(t *testing.T, label string, v any) []any {
	t.Helper()
	arr, ok := v.([]any)
	if !ok {
		t.Fatalf("%s: not a JSON array (nil slice encoded as null?): %T", label, v)
	}
	return arr
}

var provenanceKeys = []string{"source", "observedAt", "version"}

func TestAPIWireShapeRawKeys(t *testing.T) {
	w := apiTestWeb(t)

	version := rawGET(t, w, "/api/v1/version?mission=mission-one")
	wantKeys(t, "version", version, "journal", "missions", "roster", "mission")
	for _, family := range []string{"journal", "missions", "roster", "mission"} {
		wantKeys(t, "version."+family, version[family], provenanceKeys...)
	}
	// The mission stamp is scope-gated: a bare poll must omit the key.
	bare := rawGET(t, w, "/api/v1/version")
	wantKeys(t, "version (bare)", bare, "journal", "missions", "roster")

	missionKeys := []string{
		"slug", "ok", "name", "owner", "authority", "status", "created",
		"boardAvailable", "taskTotal", "taskCounts", "warnings",
	}
	missions := rawGET(t, w, "/api/v1/missions")
	wantKeys(t, "missions", missions, "missions", "warning", "provenance")
	wantKeys(t, "missions.provenance", missions["provenance"], provenanceKeys...)
	rows := wantArray(t, "missions.missions", missions["missions"])
	if len(rows) != 2 {
		t.Fatalf("missions rows = %d, want 2", len(rows))
	}
	for i, row := range rows {
		m := wantKeys(t, fmt.Sprintf("missions[%d]", i), row, missionKeys...)
		wantArray(t, fmt.Sprintf("missions[%d].warnings", i), m["warnings"])
		counts := wantArray(t, fmt.Sprintf("missions[%d].taskCounts", i), m["taskCounts"])
		for j, count := range counts {
			wantKeys(t, fmt.Sprintf("missions[%d].taskCounts[%d]", i, j), count, "status", "count")
		}
	}

	detail := rawGET(t, w, "/api/v1/mission/mission-one")
	wantKeys(t, "detail", detail, "mission", "threads", "roster")
	mission := wantKeys(t, "detail.mission", detail["mission"], "status", "provenance")
	wantKeys(t, "detail.mission.status", mission["status"], missionKeys...)
	wantKeys(t, "detail.mission.provenance", mission["provenance"], provenanceKeys...)
	threads := wantKeys(t, "detail.threads", detail["threads"], "rows", "provenance")
	wantKeys(t, "detail.threads.provenance", threads["provenance"], provenanceKeys...)
	threadRows := wantArray(t, "detail.threads.rows", threads["rows"])
	if len(threadRows) == 0 {
		t.Fatal("expected thread rows for mission-one")
	}
	for i, row := range threadRows {
		m := wantKeys(t, fmt.Sprintf("threads.rows[%d]", i), row,
			"id", "title", "status", "grade", "expects", "openedBy", "with",
			"turn", "updated", "messageCount", "lastFrom", "lastText")
		wantArray(t, fmt.Sprintf("threads.rows[%d].with", i), m["with"])
	}
	roster := wantKeys(t, "detail.roster", detail["roster"], "agents", "warning", "provenance")
	wantKeys(t, "detail.roster.provenance", roster["provenance"], provenanceKeys...)
	agents := wantArray(t, "detail.roster.agents", roster["agents"])
	if len(agents) == 0 {
		t.Fatal("expected roster agents for mission-one")
	}
	for i, agent := range agents {
		wantKeys(t, fmt.Sprintf("roster.agents[%d]", i), agent,
			"name", "address", "tool", "status", "detail", "unread", "role",
			"branch", "missionSource", "unmanaged")
	}
}

// Empty upstreams must produce empty ARRAYS, not null: the entity layer
// consumes rows/agents/missions unconditionally.
func TestAPIEmptyListsStayArrays(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir + "/journal.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	hcom := writeExecutable(t, dir, "hcom", "#!/bin/sh\ncase \"$1\" in list) printf '[]';; *) : ;; esac\n")
	herder := writeExecutable(t, dir, "herder", "#!/bin/sh\nexit 0\n")
	mish := writeExecutable(t, dir, "mish", `#!/bin/sh
case "$*" in
  *--all*) printf '[]' ;;
  *) printf '%s\n' '{"ok":true,"slug":"mission-one","manifest":{"mission":"mission-one","owner":"riley"},"board":{"available":true,"total":0,"counts":[]},"warnings":[]}' ;;
esac
`)
	w := NewWeb(s, &Bus{Hcom: hcom}, nil, "human-yamen", "owner", herder, newMissionResolver(mish, ""))

	missions := rawGET(t, w, "/api/v1/missions")
	if rows := wantArray(t, "missions.missions", missions["missions"]); len(rows) != 0 {
		t.Fatalf("missions = %v, want empty array", rows)
	}

	detail := rawGET(t, w, "/api/v1/mission/mission-one")
	threads := wantKeys(t, "detail.threads", detail["threads"], "rows", "provenance")
	if rows := wantArray(t, "detail.threads.rows", threads["rows"]); len(rows) != 0 {
		t.Fatalf("threads.rows = %v, want empty array", rows)
	}
	roster := wantKeys(t, "detail.roster", detail["roster"], "agents", "warning", "provenance")
	if agents := wantArray(t, "detail.roster.agents", roster["agents"]); len(agents) != 0 {
		t.Fatalf("roster.agents = %v, want empty array", agents)
	}
	status := wantKeys(t, "detail.mission", detail["mission"], "status", "provenance")["status"].(map[string]any)
	wantArray(t, "detail.mission.status.taskCounts", status["taskCounts"])
	wantArray(t, "detail.mission.status.warnings", status["warnings"])
}

// Both upstream families degraded at once: the response stays one honest
// HTTP 200 carrying BOTH warnings and a provenance stamp per section —
// degradation of one source never blanks or hides the other's.
func TestAPIMissionDetailDualDegradationRawShape(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir + "/journal.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf("#!/bin/sh\ncat %q\nexit 1\n", mishFixture(t, "status-mission-not-found.json")))
	w := NewWeb(s, &Bus{Hcom: "/missing/hcom"}, nil, "human-yamen", "owner", "/missing/herder", newMissionResolver(mish, ""))

	detail := rawGET(t, w, "/api/v1/mission/ghost")
	mission := wantKeys(t, "detail.mission", detail["mission"], "status", "provenance")
	status := wantKeys(t, "detail.mission.status", mission["status"],
		"slug", "ok", "name", "owner", "authority", "status", "created",
		"boardAvailable", "taskTotal", "taskCounts", "warnings")
	if ok, _ := status["ok"].(bool); ok {
		t.Fatal("degraded mission renders ok=true")
	}
	if warnings := wantArray(t, "mission warnings", status["warnings"]); len(warnings) == 0 {
		t.Fatal("degraded mission carries no warning")
	}
	roster := wantKeys(t, "detail.roster", detail["roster"], "agents", "warning", "provenance")
	if warning, _ := roster["warning"].(string); warning == "" {
		t.Fatal("degraded roster carries no warning")
	}
	for _, section := range []string{"mission", "threads", "roster"} {
		sec := detail[section].(map[string]any)
		wantKeys(t, section+".provenance", sec["provenance"], provenanceKeys...)
	}
}

// Each source family's polled token must move when THAT family's upstream
// content changes — observed through the live endpoints, one family at a
// time, while the others hold. (The journal family has its own test above.)
func TestAPIVersionTokensTrackSourceChanges(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir + "/journal.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	hcom := writeExecutable(t, dir, "hcom", "#!/bin/sh\ncase \"$1\" in list) printf '[]';; *) : ;; esac\n")
	session := `{"kind":"session","label":"builder-lobo","guid":"g-1","tool":"claude","role":"builder","state":"seated","cwd":"/work","branch":"task-x","mission":{"slug":"mission-one","source":"marker"}}`
	extra := `{"kind":"session","label":"builder-mona","guid":"g-2","tool":"claude","role":"builder","state":"seated","cwd":"/work","branch":"task-y","mission":{"slug":"mission-one","source":"marker"}}`
	herder := writeExecutable(t, dir, "herder", fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '%s'
if [ -f %q ]; then printf '%%s\n' '%s'; fi
`, session, dir+"/roster-flip", extra))
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf(`#!/bin/sh
case "$*" in
  *--all*)
    if [ -f %q ]; then
      printf '%%s\n' '[{"ok":true,"slug":"mission-one","manifest":{"mission":"mission-one","owner":"riley"},"board":{"available":true,"total":2,"counts":[]},"warnings":[]}]'
    else
      printf '%%s\n' '[{"ok":true,"slug":"mission-one","manifest":{"mission":"mission-one","owner":"riley"},"board":{"available":true,"total":1,"counts":[]},"warnings":[]}]'
    fi
    ;;
  *) printf '%%s\n' '{"ok":true,"slug":"mission-one","manifest":{"mission":"mission-one","owner":"riley"},"board":{"available":true,"total":1,"counts":[]},"warnings":[]}' ;;
esac
`, dir+"/mish-flip"))
	resolver := newMissionResolver(mish, "")
	resolver.ttl = 0 // observe every poll: token movement must come from content
	w := NewWeb(s, &Bus{Hcom: hcom}, nil, "human-yamen", "owner", herder, resolver)

	var before apiVersionDTO
	getJSON(t, w, "/api/v1/version", &before)

	if err := os.WriteFile(dir+"/roster-flip", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var afterRoster apiVersionDTO
	getJSON(t, w, "/api/v1/version", &afterRoster)
	if afterRoster.Roster.Version == before.Roster.Version {
		t.Fatal("roster token did not move on a roster content change")
	}
	if afterRoster.Missions.Version != before.Missions.Version || afterRoster.Journal.Version != before.Journal.Version {
		t.Fatal("unrelated family tokens moved on a roster change")
	}

	if err := os.WriteFile(dir+"/mish-flip", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var afterMish apiVersionDTO
	getJSON(t, w, "/api/v1/version", &afterMish)
	if afterMish.Missions.Version == afterRoster.Missions.Version {
		t.Fatal("missions token did not move on a mish content change")
	}
	if afterMish.Roster.Version != afterRoster.Roster.Version || afterMish.Journal.Version != afterRoster.Journal.Version {
		t.Fatal("unrelated family tokens moved on a mish change")
	}
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
		// Route-like paths fall back to the shell even when they LOOK like
		// files: artifact identities are file paths, so an extension must
		// never be grounds for a 404 before the router sees the URL.
		{"/ui/mission/mission-one/README.md", "<html>shell</html>", "no-cache"},
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

	// Misses in the reserved built-asset namespaces must 404, never
	// masquerade as the SPA shell: a stale hashed URL served as 200 HTML
	// would look like a working script. The namespace is assets/ (including
	// the bare directory path) plus the finite root-file set — never a
	// generic extension test (see the README.md route case above).
	for _, url := range []string{
		"/ui/assets/app-stale.js",
		"/ui/assets/missing",
		"/ui/assets",
		"/ui/assets/",
		"/ui/favicon.ico",
		"/ui/robots.txt",
	} {
		rw := httptest.NewRecorder()
		h(rw, httptest.NewRequest(http.MethodGet, url, nil))
		if rw.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", url, rw.Code)
		}
	}

	rw := httptest.NewRecorder()
	uiHandler(fstest.MapFS{})(rw, httptest.NewRequest(http.MethodGet, "/ui/", nil))
	if rw.Code != http.StatusNotImplemented {
		t.Fatalf("unbuilt ui = %d, want 501", rw.Code)
	}
}

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDerivedTitleUsesFirstSentenceAndCapsAt80Runes(t *testing.T) {
	if got := derivedTitle("Ship the simpler compose. More detail follows."); got != "Ship the simpler compose" {
		t.Fatalf("derived title = %q", got)
	}
	long := strings.Repeat("ø", 90)
	if got := derivedTitle(long); len([]rune(got)) != 80 {
		t.Fatalf("derived title has %d runes, want 80", len([]rune(got)))
	}
}

func TestTalkFormCarriesTargetInURLAndKeepsTitleVisible(t *testing.T) {
	_, s := testIngestor(t)
	w := NewWeb(s, &Bus{}, nil, "human-yamen", "owner", "", nil)
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/talk?agent=builder-dunu", nil))
	body := rw.Body.String()
	for _, want := range []string{`action="/talk?agent=builder-dunu"`, `name="message"`, `title (optional)`} {
		if !strings.Contains(body, want) {
			t.Errorf("talk form missing %q", want)
		}
	}
	for _, unwanted := range []string{`name="expects"`, `name="weight"`, `>Advanced<`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("talk form exposes agent-shaped field %q", unwanted)
		}
	}
	if strings.Contains(body, "Open a thread") || strings.Contains(body, "cold-open") {
		t.Fatal("talk form still exposes agent-grade open-thread language")
	}
}

func TestInboxShowsIngestStall(t *testing.T) {
	hcom, _ := fakePagedHcom(t, 1)
	s, err := OpenStore(filepath.Join(t.TempDir(), "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.f.Close(); err != nil {
		t.Fatal(err)
	}
	in := NewIngestor(s, &Bus{Hcom: hcom}, "human-yamen", "owner")
	if err := in.Tick(); err == nil {
		t.Fatal("Tick succeeded with a closed journal")
	}

	w := NewWeb(s, &Bus{}, in, "human-yamen", "owner", "", nil)
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rw.Body.String()
	if !strings.Contains(body, "ingest stalled at #1 since ") || !strings.Contains(body, "file already closed") {
		t.Fatalf("root page missing ingest stall warning: %s", body)
	}
}

func TestHumanTalkDerivesMetadataAndSendsMessageVerbatim(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(dir, "journal.jsonl")
	s, err := OpenStore(journal)
	if err != nil {
		t.Fatal(err)
	}
	agentDir := filepath.Join(dir, "agent")
	if err := os.Mkdir(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(dir, "send.txt")
	hcom := writeExecutable(t, dir, "hcom", fmt.Sprintf(`#!/bin/sh
if [ "$1" = list ]; then
  printf '%%s\n' '[{"name":"builder-dunu","status":"active","directory":%q}]'
  exit 0
fi
printf '%%s\n' "$@" > %q
`, agentDir, capture))
	herder := writeExecutable(t, dir, "herder", "#!/bin/sh\nexit 0\n")
	mish := writeExecutable(t, dir, "mish", "#!/bin/sh\nprintf '%s\\n' '{\"ok\":true,\"slug\":\"mission-one\"}'\n")
	bus := &Bus{Hcom: hcom}
	ing := NewIngestor(s, bus, "human-yamen", "owner")
	w := NewWeb(s, bus, ing, "human-yamen", "owner", herder, newMissionResolver(mish, ""))
	targets, home, err := w.resolveTalkTarget("mission", "mission-one")
	if err != nil || len(targets) != 1 || targets[0] != "builder-dunu" || home != "mission-one" {
		t.Fatalf("mission target = %v home %q err %v", targets, home, err)
	}

	message := "Please review this. Keep the response concise."
	form := url.Values{"message": {message}}
	req := httptest.NewRequest(http.MethodPost, "/talk?agent=builder-dunu", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, req)
	if rw.Code != http.StatusSeeOther {
		t.Fatalf("POST /talk = %d: %s", rw.Code, rw.Body.String())
	}
	id := strings.TrimPrefix(rw.Header().Get("Location"), "/thread/")
	th := s.Get(id)
	if th == nil {
		t.Fatal("talk did not create managed thread")
	}
	if th.Title != "Please review this" || th.Context != message || th.Expects != "reply" || th.Weight != "moment" {
		t.Fatalf("derived metadata = title %q context %q expects %q weight %q", th.Title, th.Context, th.Expects, th.Weight)
	}
	if th.Home != "mission-one" {
		t.Fatalf("home = %q, want target agent mission", th.Home)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if got := lines[len(lines)-1]; got != message {
		t.Fatalf("sent body = %q, want message verbatim", got)
	}
}

func TestHumanCloseDefaultsResolutionButAgentCloseStillRequiresOne(t *testing.T) {
	ing, s := testIngestor(t)
	if err := s.Open("managed-close", "close me", "ctx", "reply", "moment", "", "human-yamen", nil, "owner", "managed"); err != nil {
		t.Fatal(err)
	}

	postEmptyClose := func(user string) *httptest.ResponseRecorder {
		t.Helper()
		w := NewWeb(s, &Bus{}, ing, user, "owner", "", nil)
		rw := httptest.NewRecorder()
		w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodPost, "/thread/managed-close/close", nil))
		return rw
	}

	agent := postEmptyClose("builder-dunu")
	if got := agent.Header().Get("Location"); agent.Code != http.StatusSeeOther || !strings.Contains(got, "no+close+without+a+resolution") {
		t.Fatalf("agent close = %d Location %q, want missing-resolution refusal", agent.Code, got)
	}
	if got := s.Get("managed-close").Status; got != "open" {
		t.Fatalf("agent close changed status to %q, want open", got)
	}

	human := postEmptyClose("human-yamen")
	if got := human.Header().Get("Location"); human.Code != http.StatusSeeOther || got != "/" {
		t.Fatalf("human close = %d Location %q, want successful inbox redirect", human.Code, got)
	}
	thread := s.Get("managed-close")
	if thread.Status != "closed" || thread.Resolution != "closed by human-yamen" {
		t.Fatalf("human close = status %q resolution %q", thread.Status, thread.Resolution)
	}
}

func TestCloseNoticesOnlyLiveParticipantsAndSurfacesOrphans(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "sends.txt")
	hcom := writeExecutable(t, dir, "hcom", fmt.Sprintf(`#!/bin/sh
if [ "$1" = list ]; then
  printf '%%s\n' '[{"name":"builder-live","status":"listening"},{"name":"builder-dead","status":"inactive"}]'
  exit 0
fi
printf '%%s\n' "$*" >> %q
`, capture))
	ing, s := testIngestor(t)
	for _, id := range []string{"partly-live", "orphaned"} {
		with := []string{"builder-dead"}
		if id == "partly-live" {
			with = append(with, "builder-live")
		}
		if err := s.Open(id, id, "ctx", "reply", "moment", "", "human-yamen", with, strings.Join(with, ","), "managed"); err != nil {
			t.Fatal(err)
		}
	}
	w := NewWeb(s, &Bus{Hcom: hcom}, ing, "human-yamen", "owner", "", nil)

	for path, want := range map[string]string{
		"/thread/partly-live": "1 of 2 agent participants live",
		"/thread/orphaned":    "all agent participants gone",
	} {
		rw := httptest.NewRecorder()
		w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, path, nil))
		if !strings.Contains(rw.Body.String(), want) {
			t.Errorf("GET %s missing %q: %s", path, want, rw.Body.String())
		}
	}

	closeThread := func(id string) *httptest.ResponseRecorder {
		t.Helper()
		form := url.Values{"resolution": {"done"}}
		req := httptest.NewRequest(http.MethodPost, "/thread/"+id+"/close", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rw := httptest.NewRecorder()
		w.Routes().ServeHTTP(rw, req)
		return rw
	}
	if got := closeThread("partly-live").Header().Get("Location"); got != "/" {
		t.Fatalf("partly-live close Location = %q", got)
	}
	if got := closeThread("orphaned").Header().Get("Location"); got != "/" {
		t.Fatalf("orphaned close Location = %q", got)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	sends := string(raw)
	if !strings.Contains(sends, "@builder-live") || strings.Contains(sends, "@builder-dead") || strings.Contains(sends, "orphaned") {
		t.Fatalf("filtered close sends = %q", sends)
	}
}

func TestCloseReportsFailureForRecipientConfirmedLive(t *testing.T) {
	dir := t.TempDir()
	hcom := writeExecutable(t, dir, "hcom", `#!/bin/sh
if [ "$1" = list ]; then
  printf '%s\n' '[{"name":"builder-live","status":"active"}]'
  exit 0
fi
printf '%s\n' 'live delivery broke' >&2
exit 1
`)
	ing, s := testIngestor(t)
	if err := s.Open("live-failure", "live failure", "ctx", "reply", "moment", "", "human-yamen", []string{"builder-live"}, "builder-live", "managed"); err != nil {
		t.Fatal(err)
	}
	w := NewWeb(s, &Bus{Hcom: hcom}, ing, "human-yamen", "owner", "", nil)
	form := url.Values{"resolution": {"done"}}
	req := httptest.NewRequest(http.MethodPost, "/thread/live-failure/close", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, req)
	if got := rw.Header().Get("Location"); !strings.Contains(got, "closed%2C+but+the+bus+notice+failed") {
		t.Fatalf("live delivery failure Location = %q", got)
	}
}

func TestRosterHygieneAndMissionlessRepoBranchGrouping(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "parent-repo")
	worktree := filepath.Join(dir, "feature-worktree")
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=MC Test", "GIT_AUTHOR_EMAIL=mc@example.test", "GIT_COMMITTER_NAME=MC Test", "GIT_COMMITTER_EMAIL=mc@example.test")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	runGit("init", "-b", "main", repo)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("-C", repo, "add", "README.md")
	runGit("-C", repo, "commit", "-m", "fixture")
	runGit("-C", repo, "worktree", "add", "-b", "feature", worktree)

	hcom := writeExecutable(t, dir, "hcom", fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '[{"name":"owner","base_name":"owner","status":"listening","directory":%q},{"name":"builder-main","status":"active","session_id":"sid-main","directory":%q},{"name":"builder-feature","status":"listening","session_id":"sid-feature","directory":%q},{"name":"unmanaged-husk","status":"active","directory":%q}]'
`, repo, repo, worktree, repo))
	herder := writeExecutable(t, dir, "herder", fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '{"kind":"session","label":"builder-main","guid":"g-main","tool":"codex","role":"builder","state":"seated","provenance":{"cwd":%q,"branch":"main","tool_session_id":"sid-main"}}'
printf '%%s\n' '{"kind":"session","label":"builder-feature","guid":"g-feature","tool":"codex","role":"builder","state":"seated","provenance":{"cwd":%q,"branch":"feature","tool_session_id":"sid-feature"}}'
`, repo, worktree))
	mish := writeExecutable(t, dir, "mish", "#!/bin/sh\nprintf '%s\\n' '{\"ok\":false}'\nexit 1\n")
	_, s := testIngestor(t)
	w := NewWeb(s, &Bus{Hcom: hcom}, nil, "human-yamen", "owner", herder, newMissionResolver(mish, ""))

	groups, errText := w.rosterGroups(false)
	if errText != "" {
		t.Fatalf("roster error = %q", errText)
	}
	if len(groups) != 1 || groups[0].Dir != "no mission" || len(groups[0].Repos) != 1 {
		t.Fatalf("missionless groups = %#v", groups)
	}
	rg := groups[0].Repos[0]
	if rg.Repo != "parent-repo" || len(rg.Branches) != 2 || rg.Branches[0].Branch != "feature" || rg.Branches[1].Branch != "main" {
		t.Fatalf("repo/branch grouping = %#v", rg)
	}
	var unmanagedFound, ownerFound bool
	for _, branch := range rg.Branches {
		for _, a := range branch.Agents {
			ownerFound = ownerFound || a.Name == "owner"
			unmanagedFound = unmanagedFound || (a.Name == "unmanaged-husk" && a.Unmanaged)
		}
	}
	if ownerFound || !unmanagedFound {
		t.Fatalf("ownerFound=%v unmanagedFound=%v", ownerFound, unmanagedFound)
	}

	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/roster", nil))
	body := rw.Body.String()
	for _, want := range []string{"<h2>no mission</h2>", "repo: parent-repo", "branch: feature", "branch: main", "unmanaged-husk", "unmanaged"} {
		if !strings.Contains(body, want) {
			t.Errorf("roster missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "<strong>owner</strong>") {
		t.Fatal("roster rendered mc's own seat")
	}
}

func TestRetitleIsManagedOnlyAndReplays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Open("managed", "placeholder", "ctx", "reply", "moment", "", "human-yamen", []string{"vile"}, "vile", "managed"); err != nil {
		t.Fatal(err)
	}
	if err := s.Open("observed", "hands off", "", "read", "moment", "", "vile", nil, "vile", "observed"); err != nil {
		t.Fatal(err)
	}
	if err := s.Retitle("managed", "Durable title"); err != nil {
		t.Fatal(err)
	}
	if err := s.Retitle("observed", "bypass"); err == nil {
		t.Fatal("observed retitle was accepted")
	}

	s2, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Get("managed").Title; got != "Durable title" {
		t.Fatalf("replayed title = %q", got)
	}
	w := NewWeb(s2, &Bus{}, nil, "human-yamen", "owner", "", nil)
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/thread/observed", nil))
	if strings.Contains(rw.Body.String(), "/retitle") {
		t.Fatal("observed thread page exposes retitle")
	}
}

func TestMissionPageRendersStatusWarningsThreadsAndRoster(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(dir, "journal.jsonl")
	s, err := OpenStore(journal)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, grade string }{{"managed-thread", "managed"}, {"observed-thread", "observed"}} {
		if err := s.Open(tc.id, tc.id+" title", "ctx", "reply", "moment", "mission-one", "vile", nil, "vile", tc.grade); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Open("other", "other title", "ctx", "reply", "moment", "mission-two", "vile", nil, "vile", "managed"); err != nil {
		t.Fatal(err)
	}
	agentDir := filepath.Join(dir, "agent")
	if err := os.Mkdir(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hcom := writeExecutable(t, dir, "hcom", fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '[{"name":"builder-dunu","status":"active","directory":%q,"tool":"codex"}]'
`, agentDir))
	herder := writeExecutable(t, dir, "herder", "#!/bin/sh\nexit 0\n")
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf(`#!/bin/sh
if [ "$1" = "resolve" ]; then
  printf '%%s\n' '{"ok":true,"slug":"mission-one"}'
else
  cat %q
fi
`, mishFixture(t, "status-mission.json")))
	w := NewWeb(s, &Bus{Hcom: hcom}, nil, "human-yamen", "owner", herder, newMissionResolver(mish, ""))
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/mission/mission-one", nil))
	if rw.Code != http.StatusOK {
		t.Fatalf("GET mission = %d: %s", rw.Code, rw.Body.String())
	}
	body := rw.Body.String()
	for _, want := range []string{
		"Goal / manifest", "authority hera", "owner riley", "/missions-repo/missions/mission-one",
		"Board", "To Do 1", "TASK-1", "First task", "artifacts missing: artifacts/", "ui", "high",
		"managed-thread title", "observed-thread title", "managed", "observed",
		"Seated agents", "builder-dunu", "/talk?mission=mission-one",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("mission page missing %q", want)
		}
	}
	if strings.Contains(body, "other title") {
		t.Fatal("mission page included a thread homed on another mission")
	}
}

func TestMissionPageRendersTypedRefusalAsWarning(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf(`#!/bin/sh
cat %q
exit 1
`, mishFixture(t, "status-mission-not-found.json")))
	w := NewWeb(s, &Bus{Hcom: "/missing/hcom"}, nil, "human-yamen", "owner", "/missing/herder", newMissionResolver(mish, ""))
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/mission/ghost", nil))
	if rw.Code != http.StatusOK {
		t.Fatalf("typed refusal status = %d, want 200", rw.Code)
	}
	for _, want := range []string{"Mission data unavailable", "mission_not_found", "mission ghost not found", "check the slug or create the mission", "board unavailable"} {
		if !strings.Contains(rw.Body.String(), want) {
			t.Errorf("refusal page missing %q", want)
		}
	}
}

func TestMissionEntryPointsLinkToMissionPage(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	agentDir := filepath.Join(dir, "agent")
	if err := os.Mkdir(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hcom := writeExecutable(t, dir, "hcom", fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '[{\"name\":\"builder-dunu\",\"status\":\"active\",\"directory\":%q}]'\n", agentDir))
	herder := writeExecutable(t, dir, "herder", "#!/bin/sh\nexit 0\n")
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf(`#!/bin/sh
if [ "$1" = "resolve" ]; then
  printf '%%s\n' '{"ok":true,"slug":"mission-one"}'
elif [ "$3" = "--all" ] || [ "$2" = "--all" ]; then
  cat %q
else
  exit 1
fi
`, mishFixture(t, "status-all.json")))
	w := NewWeb(s, &Bus{Hcom: hcom}, nil, "human-yamen", "owner", herder, newMissionResolver(mish, ""))
	for _, path := range []string{"/", "/roster"} {
		rw := httptest.NewRecorder()
		w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, path, nil))
		if !strings.Contains(rw.Body.String(), `href="/mission/mission-one"`) {
			t.Errorf("%s missing mission-page entry point: %s", path, rw.Body.String())
		}
		if path == "/" {
			for _, want := range []string{`href="/mission/mission-broken"`, "degraded", "artifacts missing: artifacts/", `href="/mission/mission-one"`} {
				if !strings.Contains(rw.Body.String(), want) {
					t.Errorf("root missing isolated degraded card %q", want)
				}
			}
			if strings.Contains(rw.Body.String(), "board missing: backlog/config.yml") {
				t.Error("degraded card rendered more than its first warning")
			}
		}
	}
}

func TestMissionListRendersTypedObjectRefusal(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	mish := writeExecutable(t, dir, "mish", fmt.Sprintf("#!/bin/sh\ncat %q\nexit 1\n", mishFixture(t, "status-repo-unset.json")))
	w := NewWeb(s, &Bus{}, nil, "human-yamen", "owner", "", newMissionResolver(mish, ""))
	rw := httptest.NewRecorder()
	w.Routes().ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/", nil))
	if rw.Code != http.StatusOK {
		t.Fatalf("GET / = %d: %s", rw.Code, rw.Body.String())
	}
	for _, want := range []string{"missions_repo_unset", "$MISSIONS_REPO is not set", "set MISSIONS_REPO to the shared missions repo"} {
		if !strings.Contains(rw.Body.String(), want) {
			t.Errorf("mission-list refusal missing %q: %s", want, rw.Body.String())
		}
	}
}

func mishFixture(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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
	for _, want := range []string{`action="/talk?agent=builder-dunu"`, `name="message"`, `title (optional)`, `>Advanced<`} {
		if !strings.Contains(body, want) {
			t.Errorf("talk form missing %q", want)
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

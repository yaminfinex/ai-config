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

func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

package showcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdersock"
	"ai-config/tools/herder/internal/sessionvitals"
)

func seed(t *testing.T) string {
	t.Helper()
	state := t.TempDir()
	t.Setenv("HERDER_STATE_DIR", state)
	s := agentstore.Open(state, nil)
	at := time.Date(2026, 9, 9, 5, 0, 0, 0, time.UTC)
	for _, e := range []agentstore.Event{
		{ID: agentstore.NewID(at), At: at, Kind: agentstore.KindLaunchReady, By: "ziru", ByKind: "agent", Name: "impl-gime", Tool: "codex", Pane: "w80:p1", Session: "claimed-S"},
		{ID: agentstore.NewID(at), At: at.Add(time.Second), Kind: agentstore.KindAssign, By: "ziru", Name: "impl-gime", Group: "fleet-refit"},
		{ID: agentstore.NewID(at), At: at.Add(2 * time.Second), Kind: agentstore.KindAssign, By: "bigboss", Name: "impl-gime", Manager: "vara"},
	} {
		if _, err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	return state
}

func seedStaleSnapshot(t *testing.T) (string, string, []byte, time.Time) {
	t.Helper()
	state := t.TempDir()
	t.Setenv("HERDER_STATE_DIR", state)
	s := agentstore.Open(state, nil)
	at := time.Date(2026, 9, 9, 5, 0, 0, 0, time.UTC)
	if _, err := s.Append(agentstore.Event{ID: agentstore.NewID(at), At: at, Kind: agentstore.KindLaunchReady, By: "ziru", Name: "mavu"}); err != nil {
		t.Fatal(err)
	}
	if proj, err := s.Load(); err != nil || proj.SnapshotErr != nil {
		t.Fatalf("seed snapshot: proj=%+v err=%v", proj, err)
	}
	path := s.SnapshotPath()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(agentstore.Event{ID: agentstore.NewID(at.Add(time.Second)), At: at.Add(time.Second), Kind: agentstore.KindAssign, By: "ziru", Name: "mavu", Manager: "vara"}); err != nil {
		t.Fatal(err)
	}
	return state, path, before, info.ModTime()
}

func TestShowFoldsSnapshotTailWithoutRewritingSnapshot(t *testing.T) {
	_, snapshotPath, before, beforeModTime := seedStaleSnapshot(t)
	deps := dependencies{roster: func() ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{{Name: "mavu", Tool: "claude"}}, nil
	}}
	var out, errBuf bytes.Buffer
	if code := run([]string{"mavu"}, &out, &errBuf, deps); code != 0 || errBuf.Len() != 0 || !strings.Contains(out.String(), "manager          vara") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errBuf.String())
	}
	after, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) || !info.ModTime().Equal(beforeModTime) {
		t.Fatalf("show rewrote snapshot: content_equal=%t mtime_before=%s mtime_after=%s", bytes.Equal(after, before), beforeModTime, info.ModTime())
	}
}

func TestShowFallsBackToUniqueBaseName(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HERDER_STATE_DIR", state)
	at := time.Date(2026, 9, 10, 5, 0, 0, 0, time.UTC)
	store := agentstore.Open(state, nil)
	if _, err := store.Append(agentstore.Event{ID: agentstore.NewID(at), At: at, Kind: agentstore.KindMirrorReady, By: "hamo", ByKind: "mirror", Name: "kele"}); err != nil {
		t.Fatal(err)
	}
	rows := []hcomidentity.Row{{Name: "sesh-kele", BaseName: "kele", Tool: "claude", CreatedAt: at.Add(-time.Second)}}
	deps := dependencies{roster: func() ([]hcomidentity.Row, error) { return rows, nil }}
	var out, errBuf bytes.Buffer
	if code := run([]string{"sesh-kele"}, &out, &errBuf, deps); code != 0 || errBuf.Len() != 0 || !strings.Contains(out.String(), "manager          hamo") || !strings.Contains(out.String(), "name             sesh-kele") {
		t.Fatalf("unique fallback: code=%d out=%q err=%q", code, out.String(), errBuf.String())
	}

	rows = append(rows, hcomidentity.Row{Name: "other-kele", BaseName: "kele", CreatedAt: at.Add(-time.Second)})
	out.Reset()
	errBuf.Reset()
	if code := run([]string{"sesh-kele"}, &out, &errBuf, deps); code != 0 || !strings.Contains(out.String(), "manager          -") {
		t.Fatalf("ambiguous fallback: code=%d out=%q err=%q", code, out.String(), errBuf.String())
	}
}

func TestShowOverlaysFullNameAnnotationOnUniqueBase(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HERDER_STATE_DIR", state)
	at := time.Date(2026, 9, 10, 5, 0, 0, 0, time.UTC)
	store := agentstore.Open(state, nil)
	for _, event := range []agentstore.Event{
		{ID: agentstore.NewID(at), At: at, Kind: agentstore.KindMirrorReady, By: "hamo", ByKind: "mirror", Name: "mesa"},
		{ID: agentstore.NewID(at.Add(time.Second)), At: at.Add(time.Second), Kind: agentstore.KindAnnotate, By: "web-owner", ByKind: "web", Name: "sesh-mesa", Title: "sesh-measurement"},
	} {
		if _, err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	rows := []hcomidentity.Row{{Name: "sesh-mesa", BaseName: "mesa", Tool: "claude", CreatedAt: at.Add(-time.Second)}}
	deps := dependencies{roster: func() ([]hcomidentity.Row, error) { return rows, nil }}
	var out, errBuf bytes.Buffer
	if code := run([]string{"sesh-mesa"}, &out, &errBuf, deps); code != 0 || errBuf.Len() != 0 || !strings.Contains(out.String(), "launcher         mirrored: hamo") || !strings.Contains(out.String(), "manager          hamo") || !strings.Contains(out.String(), "title            sesh-measurement") {
		t.Fatalf("overlay: code=%d out=%q err=%q", code, out.String(), errBuf.String())
	}
}

func TestShowReadsCorrectProjectionWhenStateDirectoryIsReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	state, _, _, _ := seedStaleSnapshot(t)
	agentsDir := filepath.Join(state, "agents")
	if err := os.Chmod(state, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(agentsDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(state, 0o700)
		_ = os.Chmod(agentsDir, 0o700)
	})
	deps := dependencies{roster: func() ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{{Name: "mavu", Tool: "claude"}}, nil
	}}
	var out, errBuf bytes.Buffer
	if code := run([]string{"mavu"}, &out, &errBuf, deps); code != 0 || errBuf.Len() != 0 || !strings.Contains(out.String(), "manager          vara") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errBuf.String())
	}
}

func TestShowTextAndJSONFoldRosterConflict(t *testing.T) {
	seed(t)
	deps := dependencies{
		roster: func() ([]hcomidentity.Row, error) {
			return []hcomidentity.Row{{Name: "impl-gime", BaseName: "gime", Tool: "codex", SessionID: "roster-S-prime", CreatedAt: time.Date(2026, 9, 9, 4, 59, 0, 0, time.UTC)}}, nil
		},
		vitals: func(hcomidentity.Row) (sessionvitals.Result, error) {
			window := int64(258400)
			percent := 31.733746
			return sessionvitals.Result{Vitals: claudesession.Vitals{Model: "gpt-5.6-sol", ContextUsage: &claudesession.ContextUsage{UsedTokens: 82000, InputTokens: 82000, WindowTokens: &window, UsedPercent: &percent}}, Path: "/tmp/invented-session.jsonl", ObservedAt: time.Date(2026, 9, 10, 12, 34, 56, 0, time.UTC), Source: "direct"}, nil
		},
	}
	var out, errBuf bytes.Buffer
	if code := run([]string{"impl-gime"}, &out, &errBuf, deps); code != 0 || errBuf.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, errBuf.String())
	}
	for _, want := range []string{"launcher         ziru", "manager          vara", "group            fleet-refit", "binding          conflict {claimed: claimed-S, roster: roster-S-prime}", "session          roster-S-prime", "vitals:", "model            gpt-5.6-sol", "context_used     82k tokens", "context_window   258k tokens", "context_percent  32% used", "observed_at      2026-09-10T12:34:56Z", "session_file     /tmp/invented-session.jsonl", "incarnation      2026-09-09T04:59:00Z", "assign", "events (last 3 of 3)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("text lacks %q:\n%s", want, out.String())
		}
	}
	out.Reset()
	if code := run([]string{"--json", "impl-gime"}, &out, &errBuf, deps); code != 0 {
		t.Fatalf("json code=%d", code)
	}
	if strings.Contains(out.String(), "assignment_at") || strings.Contains(out.String(), `"assignment":null`) {
		t.Fatalf("json carries orphan assignment state: %s", out.String())
	}
	var view agentstore.AgentView
	if err := json.Unmarshal(out.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Binding == nil || view.Binding.State != "conflict" || view.Manager != "vara" || view.Provenance.Launcher != "ziru" || len(view.Events) != 3 || view.Session.SessionID != "roster-S-prime" {
		t.Fatalf("json view = %+v", view)
	}
	var raw struct {
		Vitals struct {
			Model        string                      `json:"model"`
			ContextUsage *claudesession.ContextUsage `json:"context_usage"`
			ObservedAt   time.Time                   `json:"observed_at"`
			SessionFile  string                      `json:"session_file"`
		} `json:"vitals"`
		VitalsError string `json:"vitals_error"`
	}
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil || raw.Vitals.Model != "gpt-5.6-sol" || raw.Vitals.ContextUsage == nil || raw.Vitals.ContextUsage.UsedTokens != 82000 || raw.Vitals.SessionFile == "" || raw.VitalsError != "" {
		t.Fatalf("json vitals = %+v, err=%v", raw, err)
	}
}

func TestShowSessionResolutionAndVitalsFailures(t *testing.T) {
	seed(t)
	rows := []hcomidentity.Row{{Name: "impl-gime", Tool: "codex", SessionID: "exact-session"}}
	deps := dependencies{
		roster: func() ([]hcomidentity.Row, error) { return rows, nil },
		vitals: func(hcomidentity.Row) (sessionvitals.Result, error) {
			return sessionvitals.Result{}, errors.New("invented read failure")
		},
	}
	var out, errBuf bytes.Buffer
	if code := run([]string{"--session", "exact-session", "--json"}, &out, &errBuf, deps); code != 0 || !strings.Contains(out.String(), `"vitals_error": "invented read failure"`) {
		t.Fatalf("session show: code=%d out=%q err=%q", code, out.String(), errBuf.String())
	}
	for _, args := range [][]string{{"--session", "exact"}, {"--session", "missing"}} {
		out.Reset()
		errBuf.Reset()
		if code := run(args, &out, &errBuf, deps); code != 1 || strings.Count(errBuf.String(), "\n") != 1 || !strings.Contains(errBuf.String(), "not found") {
			t.Fatalf("unknown session: args=%v code=%d err=%q", args, code, errBuf.String())
		}
	}
	for _, args := range [][]string{{"impl-gime", "--session", "exact-session"}, {"--session"}} {
		out.Reset()
		errBuf.Reset()
		if code := run(args, &out, &errBuf, deps); code != 2 {
			t.Fatalf("usage: args=%v code=%d err=%q", args, code, errBuf.String())
		}
	}
}

func TestShowMissingVitalsPrintsDashes(t *testing.T) {
	seed(t)
	deps := dependencies{
		roster: func() ([]hcomidentity.Row, error) { return []hcomidentity.Row{{Name: "impl-gime", Tool: "codex"}}, nil },
		vitals: func(hcomidentity.Row) (sessionvitals.Result, error) {
			return sessionvitals.Result{}, nil
		},
	}
	var out, errBuf bytes.Buffer
	if code := run([]string{"impl-gime"}, &out, &errBuf, deps); code != 0 {
		t.Fatalf("code=%d err=%q", code, errBuf.String())
	}
	for _, field := range []string{"model", "context_used", "context_window", "context_percent", "observed_at", "session_file"} {
		if !strings.Contains(out.String(), field+strings.Repeat(" ", 17-len(field))+"-") {
			t.Errorf("missing dash for %s:\n%s", field, out.String())
		}
	}
}

func TestShowUnregisteredAndStoreOnly(t *testing.T) {
	seed(t)
	deps := dependencies{roster: func() ([]hcomidentity.Row, error) { return nil, errors.New("hcom down") }}
	var out, errBuf bytes.Buffer
	if code := run([]string{"nobody"}, &out, &errBuf, deps); code != 0 || !strings.Contains(out.String(), "provenance       unregistered") || !strings.Contains(errBuf.String(), "store-only view") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errBuf.String())
	}
	out.Reset()
	if code := run(nil, &out, &errBuf, deps); code != 2 {
		t.Fatalf("no name: code=%d", code)
	}
	if code := run([]string{"--bogus"}, &out, &errBuf, deps); code != 2 {
		t.Fatalf("bad flag: code=%d", code)
	}
}

func TestShowReadsEvenWhenStoreImportCannotWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	state := t.TempDir()
	os.WriteFile(filepath.Join(state, "launch-edges.jsonl"), []byte(`{"name":"x","launcher":"w","time":"2026-09-08T22:21:01Z"}`+"\n"), 0o600)
	os.Chmod(state, 0o500)
	t.Cleanup(func() { os.Chmod(state, 0o700) })
	t.Setenv("HERDER_STATE_DIR", state)
	deps := dependencies{roster: func() ([]hcomidentity.Row, error) { return []hcomidentity.Row{{Name: "x", Tool: "claude"}}, nil }}
	var out, errBuf bytes.Buffer
	if code := run([]string{"x"}, &out, &errBuf, deps); code != 0 || !strings.Contains(out.String(), "unregistered") || strings.Count(errBuf.String(), "\n") != 1 {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errBuf.String())
	}
}

func TestShowReadsEvenWhenJournalIsUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	state := seed(t)
	journal := filepath.Join(state, "agents", "events.jsonl")
	if err := os.Chmod(journal, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(journal, 0o600) })
	deps := dependencies{roster: func() ([]hcomidentity.Row, error) {
		return []hcomidentity.Row{{Name: "impl-gime", Tool: "codex", SessionID: "roster-S"}}, nil
	}}
	var out, errBuf bytes.Buffer
	code := run([]string{"impl-gime"}, &out, &errBuf, deps)
	if code != 0 || !strings.Contains(out.String(), "provenance       unregistered") || !strings.Contains(out.String(), "session          roster-S") || strings.Count(errBuf.String(), "\n") != 1 || !strings.Contains(errBuf.String(), "cannot read agent store") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errBuf.String())
	}
}

// Reddens: source missing or mislabelled; observed_at taken from the file
// instead of the observer; the hermetic state dir reaching a real socket.
func TestShowReportsCacheSourceFromSocketAndDirectWithout(t *testing.T) {
	state := seedShortState(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	claudeID := "73100000-0000-4000-8000-000000000731"
	path := filepath.Join(home, ".claude", "projects", "-invented-violet", claudeID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	record := `{"type":"assistant","isSidechain":false,"message":{"model":"invented-direct","usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":1}}}` + "\n"
	if err := os.WriteFile(path, []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 9, 10, 13, 0, 0, 0, time.UTC)
	server, err := herdersock.Listen(state, func(r herdersock.Request) herdersock.Response {
		return herdersock.Response{Vitals: claudesession.Vitals{Model: "invented-cached", ContextUsage: &claudesession.ContextUsage{UsedTokens: 4242, InputTokens: 4242}}, Path: path, ObservedAt: stamp}
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	deps := dependencies{
		roster: func() ([]hcomidentity.Row, error) {
			return []hcomidentity.Row{{Name: "impl-gime", BaseName: "gime", Tool: "claude", Directory: "/invented/violet", SessionID: claudeID}}, nil
		},
		vitals: sessionvitals.Read,
	}
	var out, errBuf bytes.Buffer
	if code := run([]string{"impl-gime", "--json"}, &out, &errBuf, deps); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errBuf.String())
	}
	var cached struct {
		Vitals struct {
			Model      string    `json:"model"`
			Source     string    `json:"source"`
			ObservedAt time.Time `json:"observed_at"`
		} `json:"vitals"`
	}
	if err := json.Unmarshal(out.Bytes(), &cached); err != nil || cached.Vitals.Source != "cache" || cached.Vitals.Model != "invented-cached" || !cached.Vitals.ObservedAt.Equal(stamp) {
		t.Fatalf("cached show = %+v err=%v\n%s", cached.Vitals, err, out.String())
	}
	server.Close()
	out.Reset()
	if code := run([]string{"impl-gime", "--json"}, &out, &errBuf, deps); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errBuf.String())
	}
	if err := json.Unmarshal(out.Bytes(), &cached); err != nil || cached.Vitals.Source != "direct" || cached.Vitals.Model != "invented-direct" {
		t.Fatalf("direct show = %+v err=%v", cached.Vitals, err)
	}
	out.Reset()
	if code := run([]string{"impl-gime"}, &out, &errBuf, deps); code != 0 || strings.Contains(out.String(), "source") {
		t.Fatalf("text output must not carry source: %s", out.String())
	}
}

// seedShortState is seed(t) under a short /tmp path (unix socket path limit).
func seedShortState(t *testing.T) string {
	t.Helper()
	state, err := os.MkdirTemp("/tmp", "hshow")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(state) })
	t.Setenv("HERDER_STATE_DIR", state)
	return state
}

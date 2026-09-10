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
)

func seed(t *testing.T) string {
	t.Helper()
	state := t.TempDir()
	t.Setenv("HERDER_STATE_DIR", state)
	s := agentstore.Open(state, nil)
	at := time.Date(2026, 9, 9, 5, 0, 0, 0, time.UTC)
	for _, e := range []agentstore.Event{
		{ID: agentstore.NewID(at), At: at, Kind: agentstore.KindLaunchReady, By: "ziru", ByKind: "agent", Name: "impl-gime", Tool: "codex", Pane: "w80:p1", Session: "claimed-S"},
		{ID: agentstore.NewID(at), At: at.Add(time.Second), Kind: agentstore.KindAssign, By: "ziru", Name: "impl-gime", Mission: "fleet-refit", Thread: "agent-store"},
		{ID: agentstore.NewID(at), At: at.Add(2 * time.Second), Kind: agentstore.KindReparent, By: "bigboss", Name: "impl-gime", Manager: "vara"},
	} {
		if _, err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	return state
}

func TestShowTextAndJSONFoldRosterConflict(t *testing.T) {
	seed(t)
	deps := dependencies{
		roster: func() ([]hcomidentity.Row, error) {
			return []hcomidentity.Row{{Name: "impl-gime", BaseName: "gime", Tool: "codex", SessionID: "roster-S-prime", CreatedAt: time.Date(2026, 9, 9, 4, 59, 0, 0, time.UTC)}}, nil
		},
		vitals: func(hcomidentity.Row) (claudesession.Vitals, string, time.Time, error) {
			window := int64(258400)
			percent := 31.733746
			return claudesession.Vitals{Model: "gpt-5.6-sol", ContextUsage: &claudesession.ContextUsage{UsedTokens: 82000, InputTokens: 82000, WindowTokens: &window, UsedPercent: &percent}}, "/tmp/invented-session.jsonl", time.Date(2026, 9, 10, 12, 34, 56, 0, time.UTC), nil
		},
	}
	var out, errBuf bytes.Buffer
	if code := run([]string{"impl-gime"}, &out, &errBuf, deps); code != 0 || errBuf.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, errBuf.String())
	}
	for _, want := range []string{"launcher         ziru", "manager          vara", "mission          fleet-refit", "binding          conflict {claimed: claimed-S, roster: roster-S-prime}", "session          roster-S-prime", "vitals:", "model            gpt-5.6-sol", "context_used     82k tokens", "context_window   258k tokens", "context_percent  32% used", "observed_at      2026-09-10T12:34:56Z", "session_file     /tmp/invented-session.jsonl", "incarnation      2026-09-09T04:59:00Z", "reparent", "events (last 3 of 3)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("text lacks %q:\n%s", want, out.String())
		}
	}
	out.Reset()
	if code := run([]string{"--json", "impl-gime"}, &out, &errBuf, deps); code != 0 {
		t.Fatalf("json code=%d", code)
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
		vitals: func(hcomidentity.Row) (claudesession.Vitals, string, time.Time, error) {
			return claudesession.Vitals{}, "", time.Time{}, errors.New("invented read failure")
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
		vitals: func(hcomidentity.Row) (claudesession.Vitals, string, time.Time, error) {
			return claudesession.Vitals{}, "", time.Time{}, nil
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

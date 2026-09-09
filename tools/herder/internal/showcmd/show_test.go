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
		now: time.Now,
	}
	var out, errBuf bytes.Buffer
	if code := run([]string{"impl-gime"}, &out, &errBuf, deps); code != 0 || errBuf.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, errBuf.String())
	}
	for _, want := range []string{"launcher         ziru", "manager          vara", "mission          fleet-refit", "binding          conflict {claimed: claimed-S, roster: roster-S-prime}", "session          roster-S-prime", "incarnation      2026-09-09T04:59:00Z", "reparent", "events (last 3 of 3)"} {
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
}

func TestShowUnregisteredAndStoreOnly(t *testing.T) {
	seed(t)
	deps := dependencies{roster: func() ([]hcomidentity.Row, error) { return nil, errors.New("hcom down") }, now: time.Now}
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
	deps := dependencies{roster: func() ([]hcomidentity.Row, error) { return []hcomidentity.Row{{Name: "x", Tool: "claude"}}, nil }, now: time.Now}
	var out, errBuf bytes.Buffer
	if code := run([]string{"x"}, &out, &errBuf, deps); code != 0 || !strings.Contains(out.String(), "unregistered") || strings.Count(errBuf.String(), "\n") != 1 {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errBuf.String())
	}
}

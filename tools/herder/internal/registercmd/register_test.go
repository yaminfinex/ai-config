package registercmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/agentstore"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code := Run(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestRegisterAppendsWithDefaultsAndEchoesJSON(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HERDER_STATE_DIR", state)
	t.Setenv("HCOM_NAME", "impl-lima")
	code, stdout, stderr := run(t, "launch-requested", "--tool", "codex", "--tag", "impl", "--workspace", "w80", "--model", "gpt-6", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	var e agentstore.Event
	if err := json.Unmarshal([]byte(stdout), &e); err != nil {
		t.Fatal(err)
	}
	if e.Kind != "launch-requested" || e.By != "impl-lima" || e.ByKind != "agent" || e.Placement == nil || e.Placement.Workspace != "w80" || !agentstore.ValidID(e.ID) || e.At.IsZero() {
		t.Fatalf("event = %+v", e)
	}
	t.Setenv("HCOM_NAME", "")
	t.Setenv("HCOM_INSTANCE_NAME", "")
	t.Setenv("HCOM_TAG", "")
	t.Setenv("USER", "yamen")
	code, stdout, _ = run(t, "launch-ready", "--name", "impl-gime", "--request", e.ID, "--pane", "w80:p1", "--cwd", "/x", "--at", "2026-09-09T05:00:00Z")
	if code != 0 || !strings.HasPrefix(stdout, "id=") {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	proj, err := agentstore.Open(state, nil).Replay()
	if err != nil {
		t.Fatal(err)
	}
	v := proj.Latest("impl-gime")
	if v == nil || v.Provenance.Launcher != "impl-lima" || v.Provenance.ModelRequested != "gpt-6" || v.Provenance.Pane != "w80:p1" || v.Events[0].By != "yamen" || v.Events[0].ByKind != "user" {
		t.Fatalf("view = %+v", v)
	}
	if n := bytes.Count(mustRead(t, filepath.Join(state, "agents", "events.jsonl")), []byte("\n")); n != 2 {
		t.Fatalf("lines = %d", n)
	}
}

func TestDefaultByFromHcomEnv(t *testing.T) {
	t.Setenv("HCOM_NAME", "")
	t.Setenv("HCOM_TAG", "impl")
	t.Setenv("HCOM_INSTANCE_NAME", "nife")
	t.Setenv("USER", "yamen")
	if by, kind := defaultBy(); by != "impl-nife" || kind != "agent" {
		t.Fatalf("tagged seat = %q/%q", by, kind)
	}
	t.Setenv("HCOM_TAG", "")
	if by, kind := defaultBy(); by != "nife" || kind != "agent" {
		t.Fatalf("untagged seat = %q/%q", by, kind)
	}
	t.Setenv("HCOM_NAME", "legacy-full")
	if by, kind := defaultBy(); by != "legacy-full" || kind != "agent" {
		t.Fatalf("HCOM_NAME winner = %q/%q", by, kind)
	}
	t.Setenv("HCOM_NAME", "")
	t.Setenv("HCOM_INSTANCE_NAME", "")
	if by, kind := defaultBy(); by != "yamen" || kind != "user" {
		t.Fatalf("USER fallback = %q/%q", by, kind)
	}
}

func TestRegisterUsageErrorsExit2WithoutWriting(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HERDER_STATE_DIR", state)
	for _, args := range [][]string{
		{},
		{"bogus", "--name", "a"},
		{"culled", "--name", "a", "--pane", "p"},
		{"culled", "--name", "a", "--pane", "p", "--close", "other"},
		{"launch-requested", "--tool", "codex", "--tag", "t"},
		{"launch-requested", "--tool", "codex", "--tag", "t", "--workspace", "w", "--pane", "p"},
		{"annotate", "--name", "a"},
		{"reparent", "--name", "a"},
		{"assign", "--name", "a", "--mission", "m", "--id", "not-a-uuid"},
		{"assign", "--name", "a", "--mission", "m", "--at", "yesterday"},
		{"assign", "--name", "a", "--mission", "m", "extra"},
	} {
		code, _, stderr := run(t, args...)
		if code != 2 || stderr == "" && len(args) > 0 {
			t.Errorf("args %v: code=%d stderr=%q", args, code, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(state, "agents", "events.jsonl")); err == nil {
		t.Fatal("usage error wrote to the store")
	}
}

func TestRegisterStoreUnavailableExits3(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	state := t.TempDir()
	os.Chmod(state, 0o500)
	t.Cleanup(func() { os.Chmod(state, 0o700) })
	t.Setenv("HERDER_STATE_DIR", state)
	code, _, stderr := run(t, "assign", "--name", "a", "--mission", "m")
	if code != 3 || !strings.Contains(stderr, "store unavailable") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	// The first-open import failing is also exit 3 for register (only register).
	state2 := t.TempDir()
	os.WriteFile(filepath.Join(state2, "launch-edges.jsonl"), []byte(`{"name":"x","launcher":"w","time":"2026-09-08T22:21:01Z"}`+"\n"), 0o600)
	os.Chmod(state2, 0o500)
	t.Cleanup(func() { os.Chmod(state2, 0o700) })
	t.Setenv("HERDER_STATE_DIR", state2)
	code, _, stderr = run(t, "assign", "--name", "a", "--mission", "m")
	if code != 3 || !strings.Contains(stderr, "store unavailable") {
		t.Fatalf("import failure: code=%d stderr=%q", code, stderr)
	}
}

func TestRegisterHelpCoversEveryKind(t *testing.T) {
	code, stdout, _ := run(t, "--help")
	if code != 0 || !strings.Contains(stdout, "herder register") {
		t.Fatalf("code=%d", code)
	}
	for _, kind := range agentstore.Kinds {
		if !strings.Contains(stdout, "  "+kind+" ") {
			t.Errorf("help lacks kind %s", kind)
		}
		if _, ok := agentstore.SpecFor(kind); !ok {
			t.Errorf("no spec for %s", kind)
		}
	}
	if code, stdout, _ = run(t, "fork", "--help"); code != 0 || !strings.Contains(stdout, "--from V") {
		t.Fatalf("fork help: %d %q", code, stdout)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

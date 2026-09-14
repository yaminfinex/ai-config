package eventcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/agentstore"
)

func TestParseSharedEventFlags(t *testing.T) {
	at := "2026-09-10T12:34:56.123Z"
	id := "12345678-1234-4123-8123-123456789abc"
	e, asJSON, err := Parse(agentstore.KindAssign, []string{"--name", " impl-geni ", "--manager", " ziru ", "--by", " owner ", "--by-kind", " user ", "--at", at, "--id", id, "--json"})
	if err != nil {
		t.Fatal(err)
	}
	wantAt, _ := time.Parse(time.RFC3339Nano, at)
	if !asJSON || e.Name != "impl-geni" || e.Manager != "ziru" || e.By != "owner" || e.ByKind != "user" || e.ID != id || !e.At.Equal(wantAt) {
		t.Fatalf("event=%+v json=%v", e, asJSON)
	}
}

func TestDefaultBy(t *testing.T) {
	t.Run("live self name", func(t *testing.T) {
		calls := installFakeHcom(t, "printf '{\"name\":\"ziru\"}\\n'")
		t.Setenv("HCOM_NAME", "")
		t.Setenv("HCOM_TAG", "")
		t.Setenv("HCOM_PROCESS_ID", "process")
		t.Setenv("HCOM_INSTANCE_NAME", "fimu")
		if by, kind := DefaultBy(); by != "ziru" || kind != "agent" {
			t.Fatalf("got %q/%q", by, kind)
		}
		assertHcomCalls(t, calls, "list self --json\n")
	})

	t.Run("failed self falls back to environment", func(t *testing.T) {
		installFakeHcom(t, "exit 1")
		t.Setenv("HCOM_NAME", "")
		t.Setenv("HCOM_TAG", "")
		t.Setenv("HCOM_PROCESS_ID", "process")
		t.Setenv("HCOM_INSTANCE_NAME", "fimu")
		if by, kind := DefaultBy(); by != "fimu" || kind != "agent" {
			t.Fatalf("got %q/%q", by, kind)
		}
	})

	t.Run("hung self times out and falls back", func(t *testing.T) {
		installFakeHcom(t, "sleep 30")
		t.Setenv("HCOM_NAME", "")
		t.Setenv("HCOM_TAG", "")
		t.Setenv("HCOM_PROCESS_ID", "process")
		t.Setenv("HCOM_INSTANCE_NAME", "fimu")
		started := time.Now()
		if by, kind := DefaultBy(); by != "fimu" || kind != "agent" {
			t.Fatalf("got %q/%q", by, kind)
		}
		if elapsed := time.Since(started); elapsed > 4*time.Second {
			t.Fatalf("DefaultBy took %s, want about 2s", elapsed)
		}
	})

	t.Run("no process id does not call hcom", func(t *testing.T) {
		calls := installFakeHcom(t, "printf '{\"name\":\"ziru\"}\\n'")
		t.Setenv("HCOM_NAME", "")
		t.Setenv("HCOM_TAG", "")
		t.Setenv("HCOM_PROCESS_ID", "")
		t.Setenv("HCOM_INSTANCE_NAME", "fimu")
		if by, kind := DefaultBy(); by != "fimu" || kind != "agent" {
			t.Fatalf("got %q/%q", by, kind)
		}
		assertHcomCalls(t, calls, "")
	})

	t.Run("user fallback", func(t *testing.T) {
		for _, key := range []string{"HCOM_PROCESS_ID", "HCOM_NAME", "HCOM_TAG", "HCOM_INSTANCE_NAME"} {
			t.Setenv(key, "")
		}
		t.Setenv("USER", "alice")
		if by, kind := DefaultBy(); by != "alice" || kind != "user" {
			t.Fatalf("got %q/%q", by, kind)
		}
	})
}

func installFakeHcom(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	stub := filepath.Join(dir, "hcom")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >>\"$HCOM_CALLS\"\n" + body + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("HCOM_CALLS", calls)
	return calls
}

func assertHcomCalls(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if got := strings.ReplaceAll(string(raw), "\r\n", "\n"); got != want {
		t.Fatalf("hcom calls = %q, want %q", got, want)
	}
}

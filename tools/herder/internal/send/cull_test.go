package send

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeliverCullRequestBoundsHungBusProbe(t *testing.T) {
	bin := t.TempDir()
	stub := "#!/usr/bin/env bash\nsleep 5\n"
	if err := os.WriteFile(filepath.Join(bin, "hcom"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HCOM_DIR", t.TempDir())
	start := time.Now()
	got := DeliverCullRequest(CullRequest{
		Sender: "caller-seat", Target: "peer-seat", BusDir: os.Getenv("HCOM_DIR"),
		Thread: "herder-cull-deadline", Message: "release external resources, then acknowledge", Deadline: time.Now().Add(40 * time.Millisecond),
	})
	if got.Verdict != "not_joined" {
		t.Fatalf("verdict=%q, want not_joined after bounded probe", got.Verdict)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("hung bus probe escaped deadline: %s", elapsed)
	}
}

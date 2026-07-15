package send

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
		Sender: "worker-caller-seat", SenderBase: "caller-seat", Target: "worker-peer-seat", TargetBase: "peer-seat", BusDir: os.Getenv("HCOM_DIR"),
		Thread: "herder-cull-deadline", Message: "release external resources, then acknowledge", Deadline: time.Now().Add(40 * time.Millisecond),
	})
	if got.Verdict != "roster_timeout" {
		t.Fatalf("verdict=%q, want roster_timeout after bounded probe", got.Verdict)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("hung bus probe escaped deadline: %s", elapsed)
	}
}

func TestDeliverCullRequestBoundsChildHoldingEventStdout(t *testing.T) {
	bin := t.TempDir()
	pidLog := filepath.Join(t.TempDir(), "child-pid")
	stub := `#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  list)
    exit 0
    ;;
  events)
    sleep 5 &
    printf '%s\n' "$!" >"$CULL_CHILD_PID_LOG"
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "hcom"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		data, err := os.ReadFile(pidLog)
		if err != nil {
			return
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			return
		}
		if process, err := os.FindProcess(pid); err == nil {
			_ = process.Kill()
		}
	})
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HCOM_DIR", t.TempDir())
	t.Setenv("CULL_CHILD_PID_LOG", pidLog)
	start := time.Now()
	_ = DeliverCullRequest(CullRequest{
		Sender: "worker-caller-seat", SenderBase: "caller-seat", Target: "worker-peer-seat", TargetBase: "peer-seat", BusDir: os.Getenv("HCOM_DIR"),
		Thread: "herder-cull-fd-bound", Message: "release external resources, then acknowledge", Deadline: time.Now().Add(100 * time.Millisecond),
	})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("child-held event stdout escaped deadline: %s", elapsed)
	}
}

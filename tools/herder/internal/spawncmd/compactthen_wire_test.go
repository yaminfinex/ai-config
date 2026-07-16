package spawncmd

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/send"
)

func TestDerivedContinuationSenderDeliversWithExactReceipt(t *testing.T) {
	bin := realHcomForCompactThenTest(t)
	home := t.TempDir()
	busDir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(home, "runtime"))
	t.Setenv("HCOM_DIR", busDir)
	t.Setenv("HCOM_SESSION_ID", "")
	t.Setenv("HCOM_PROCESS_ID", "")
	t.Setenv("HCOM_INSTANCE_NAME", "")
	t.Setenv("HERDER_LABEL", "")
	t.Setenv("CLAUDECODE", "")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "")
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))

	const processID = "fixture-recipient"
	startPayload := `{"session_id":"fixture-session","transcript_path":"/tmp/fixture.jsonl","cwd":"/tmp","hook_event_name":"SessionStart","source":"startup"}`
	start := exec.Command(bin, "sessionstart")
	start.Env = compactThenHcomEnv(busDir, home, processID)
	start.Stdin = strings.NewReader(startPayload)
	startOut, err := start.CombinedOutput()
	if err != nil {
		t.Fatalf("hcom sessionstart: %v: %s", err, startOut)
	}
	match := regexp.MustCompile(`\[hcom:([A-Za-z0-9-]+)\]`).FindSubmatch(startOut)
	if len(match) != 2 {
		t.Fatalf("hcom sessionstart returned no recipient identity: %s", startOut)
	}
	recipient := string(match[1])
	sender, err := compactThenSenderName(recipient)
	if err != nil {
		t.Fatal(err)
	}
	if sender == recipient {
		t.Fatalf("derived sender %q equals recipient", sender)
	}

	stopPayload := `{"session_id":"fixture-session","transcript_path":"/tmp/fixture.jsonl","cwd":"/tmp","hook_event_name":"Stop","stop_hook_active":false}`
	var pollOut bytes.Buffer
	poll := exec.Command(bin, "poll")
	poll.Env = compactThenHcomEnv(busDir, home, processID)
	poll.Stdin = strings.NewReader(stopPayload)
	poll.Stdout = &pollOut
	if err := poll.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if poll.ProcessState == nil {
			_ = poll.Process.Kill()
			_, _ = poll.Process.Wait()
		}
	})

	if verdict := send.DeliverBus(sender, recipient, busDir, "continue after compaction", 3000); verdict != "delivered" {
		t.Fatalf("DeliverBus verdict = %q, want delivered; hook output: %s", verdict, pollOut.String())
	}
	done := make(chan error, 1)
	go func() { done <- poll.Wait() }()
	select {
	case waitErr := <-done:
		var exitErr *exec.ExitError
		if waitErr != nil && (!errors.As(waitErr, &exitErr) || exitErr.ExitCode() != 2) {
			t.Fatalf("hcom poll: %v: %s", waitErr, pollOut.String())
		}
	case <-time.After(3 * time.Second):
		_ = poll.Process.Kill()
		t.Fatal("hcom poll did not return after delivery")
	}
	if !strings.Contains(pollOut.String(), "continue after compaction") {
		t.Fatalf("recipient hook did not inject the continuation: %s", pollOut.String())
	}

	receipt := exec.Command(bin, "events", "--last", "10", "--agent", recipient, "--context", "deliver:"+sender)
	receipt.Env = compactThenHcomEnv(busDir, home, "")
	receiptOut, err := receipt.CombinedOutput()
	if err != nil {
		t.Fatalf("query exact sender-keyed receipt: %v: %s", err, receiptOut)
	}
	if !strings.Contains(string(receiptOut), `"context":"deliver:`+sender+`"`) {
		t.Fatalf("exact sender-keyed receipt missing: %s", receiptOut)
	}
}

func realHcomForCompactThenTest(t *testing.T) string {
	t.Helper()
	if path := os.Getenv("HERDER_TEST_HCOM_BIN"); path != "" {
		return path
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if strings.Contains(dir, "tools/herder/shims") {
			continue
		}
		path := filepath.Join(dir, "hcom")
		if info, err := os.Stat(path); err == nil && info.Mode()&0o111 != 0 {
			return path
		}
	}
	t.Fatal("real hcom binary unavailable")
	return ""
}

func compactThenHcomEnv(busDir, home, processID string) []string {
	env := make([]string, 0, len(os.Environ())+8)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if key == "HCOM" || strings.HasPrefix(key, "HCOM_") || strings.HasPrefix(key, "CLAUDE") || key == "CODEX_THREAD_ID" {
			continue
		}
		env = append(env, item)
	}
	env = append(env,
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, "config"),
		"XDG_DATA_HOME="+filepath.Join(home, "data"),
		"XDG_STATE_HOME="+filepath.Join(home, "state"),
		"XDG_RUNTIME_DIR="+filepath.Join(home, "runtime"),
		"HCOM_DIR="+busDir,
		"CLAUDECODE=1",
		"CLAUDE_CODE_ENTRYPOINT=cli",
	)
	if processID != "" {
		env = append(env, "HCOM_PROCESS_ID="+processID)
	}
	return env
}

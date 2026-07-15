package send

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestPromptReplyRoutesOnlyToLiveSender(t *testing.T) {
	bin := realHcomForTest(t)
	home := t.TempDir()
	busDir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HCOM_DIR", busDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(home, "runtime"))
	t.Setenv("HCOM_SESSION_ID", "")
	t.Setenv("HCOM_PROCESS_ID", "")
	t.Setenv("HCOM_INSTANCE_NAME", "")
	t.Setenv("HERDER_LABEL", "")
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))

	dispatcher := startIsolatedHcomIdentity(t, bin, busDir, "process-dispatcher")
	worker := startIsolatedHcomIdentity(t, bin, busDir, "process-worker")
	ownerSidePeer := startIsolatedHcomIdentity(t, bin, busDir, "process-owner-side-peer")

	verdict := DeliverBus(dispatcher, worker, busDir, "prompt", 1)
	if verdict != "delivered" && verdict != "queued" {
		t.Fatalf("DeliverBus verdict = %q, want submitted", verdict)
	}
	prompt := latestMessageFromBus(t, bin, busDir)
	reply := exec.Command(bin, "send", "@"+prompt.From, "--name", worker, "--intent", "ack", "--reply-to", prompt.ID, "--", "ack")
	reply.Env = isolatedHcomEnv(busDir, "")
	if out, err := reply.CombinedOutput(); err != nil {
		t.Fatalf("reply to prompt sender %q failed: %v: %s", prompt.From, err, out)
	}

	ack := latestMessageFromBus(t, bin, busDir)
	if len(ack.DeliveredTo) != 1 || ack.DeliveredTo[0] != dispatcher {
		t.Fatalf("reply delivered_to = %v, want only live sender %q (owner-side peer %q must not receive it)", ack.DeliveredTo, dispatcher, ownerSidePeer)
	}
}

type testBusMessage struct {
	ID          string
	From        string
	DeliveredTo []string
}

func realHcomForTest(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("HERDER_TEST_HCOM_BIN"); p != "" {
		return p
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if strings.Contains(dir, "tools/herder/shims") {
			continue
		}
		p := filepath.Join(dir, "hcom")
		if st, err := os.Stat(p); err == nil && st.Mode()&0o111 != 0 {
			return p
		}
	}
	t.Fatal("real hcom binary unavailable")
	return ""
}

func startIsolatedHcomIdentity(t *testing.T, bin, busDir, processID string) string {
	t.Helper()
	cmd := exec.Command(bin, "start")
	cmd.Env = isolatedHcomEnv(busDir, processID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hcom start: %v: %s", err, out)
	}
	match := regexp.MustCompile(`(?m)^\[hcom:([A-Za-z0-9-]+)\]`).FindSubmatch(out)
	if len(match) != 2 {
		t.Fatalf("hcom start returned no identity: %s", out)
	}
	return string(match[1])
}

func latestMessageFromBus(t *testing.T, _, busDir string) testBusMessage {
	t.Helper()
	const query = `
import json, sqlite3, sys
db = sqlite3.connect(sys.argv[1])
event_id, data = db.execute("select id, data from events where type='message' order by id desc limit 1").fetchone()
value = json.loads(data)
print(json.dumps({"id": event_id, "data": {"from": value.get("from", ""), "delivered_to": value.get("delivered_to", [])}}))
`
	cmd := exec.Command("python3", "-c", query, filepath.Join(busDir, "hcom.db"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("read isolated hcom event: %v: %s", err, out)
	}
	var event struct {
		ID   json.Number `json:"id"`
		Data struct {
			From        string   `json:"from"`
			DeliveredTo []string `json:"delivered_to"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &event); err != nil {
		t.Fatalf("decode hcom event: %v: %s", err, out)
	}
	return testBusMessage{ID: event.ID.String(), From: event.Data.From, DeliveredTo: event.Data.DeliveredTo}
}

func isolatedHcomEnv(busDir, processID string) []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if key == "HCOM" || strings.HasPrefix(key, "HCOM_") || strings.HasPrefix(key, "CLAUDE") || key == "CODEX_THREAD_ID" {
			continue
		}
		env = append(env, item)
	}
	env = append(env, "HCOM_DIR="+busDir)
	if processID != "" {
		env = append(env, "HCOM_PROCESS_ID="+processID)
	}
	return env
}

// TestConcurrentSendsAreSerialized pins codex review P2-CONCURRENT: two
// concurrent sends sharing sender identity and target must not share a
// receipt. The stub bus keeps a stale receipt (id 41) visible on every
// events call, acks id 42 once ANY send has landed, and never acks the
// second message. Without the send-window lock both senders snapshot
// preMax=41 before either receipt exists, so the first wake's id 42
// satisfies BOTH waiters — two delivered verdicts, one of them false. With
// the lock the window serializes: the winner snapshots 41 and sees 42
// (delivered); the loser snapshots 42 and nothing newer ever appears
// (queued). The stub's events call sleeps to widen the race window, so a
// regression to unlocked behavior fails this test reliably.
func TestConcurrentSendsAreSerialized(t *testing.T) {
	stubDir := t.TempDir()
	stateDir := t.TempDir()
	stub := `#!/usr/bin/env bash
STATE="$STUB_STATE"
case "$1" in
  list) exit 0;;
  send) echo x >>"$STATE/sends"; exit 0;;
  events)
    sleep 0.15
    printf '{"id":41,"data":{"context":"deliver:orchestrator"},"type":"status"}\n'
    if [[ -s "$STATE/sends" ]]; then
      printf '{"id":42,"data":{"context":"deliver:orchestrator"},"type":"status"}\n'
    fi
    exit 0;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(stubDir, "hcom"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("STUB_STATE", stateDir)
	busDir := t.TempDir()

	var wg sync.WaitGroup
	verdicts := make([]string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			verdicts[i] = DeliverBus("sender-rive", "peer-rive", busDir, "hello", 1500)
		}(i)
	}
	wg.Wait()

	counts := map[string]int{}
	for _, v := range verdicts {
		counts[v]++
	}
	if counts["delivered"] != 1 || counts["queued"] != 1 {
		t.Fatalf("verdicts = %v, want exactly one delivered and one queued", verdicts)
	}
}

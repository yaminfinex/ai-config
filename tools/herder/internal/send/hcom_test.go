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
	"time"

	"ai-config/tools/herder/internal/pendingprompt"
)

func TestPromptSenderStampIsAddressableLiveIdentity(t *testing.T) {
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

	verdict := DeliverBus(dispatcher, worker, busDir, "prompt", 1)
	if verdict != "delivered" && verdict != "queued" {
		t.Fatalf("DeliverBus verdict = %q, want submitted", verdict)
	}
	prompt := latestMessageFromBus(t, bin, busDir)
	if prompt.From != dispatcher {
		t.Fatalf("prompt sender stamp = %q, want addressable dispatcher identity %q", prompt.From, dispatcher)
	}
	reply := exec.Command(bin, "send", "@"+prompt.From, "--name", worker, "--intent", "ack", "--reply-to", prompt.ID, "--", "ack")
	reply.Env = isolatedHcomEnv(busDir, "")
	if out, err := reply.CombinedOutput(); err != nil {
		t.Fatalf("reply to prompt sender %q failed: %v: %s", prompt.From, err, out)
	}

	ack := latestMessageFromBus(t, bin, busDir)
	if len(ack.DeliveredTo) != 1 || ack.DeliveredTo[0] != dispatcher {
		t.Fatalf("explicit reply to prompt sender delivered_to = %v, want dispatcher %q", ack.DeliveredTo, dispatcher)
	}
}

func TestTaggedPromptSenderPreservesFullAddressableIdentity(t *testing.T) {
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

	dispatcherBase := startIsolatedHcomIdentity(t, bin, busDir, "process-tagged-dispatcher")
	worker := startIsolatedHcomIdentity(t, bin, busDir, "process-tagged-worker")
	const tag = "dispatcher"
	tagIsolatedHcomIdentity(t, busDir, dispatcherBase, tag)
	dispatcher := tag + "-" + dispatcherBase
	if dispatcher == dispatcherBase {
		t.Fatal("fixture must produce a full tagged name different from base_name")
	}
	assertHcomIdentityShape(t, bin, busDir, dispatcher, dispatcherBase)

	wrapperDir := t.TempDir()
	argvLog := filepath.Join(t.TempDir(), "hcom-argv.log")
	wrapper := `#!/bin/sh
printf '%s\n' "$*" >>"$HCOM_ARGV_LOG"
exec "$HCOM_REAL_BIN" "$@"
`
	if err := os.WriteFile(filepath.Join(wrapperDir, "hcom"), []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HCOM_REAL_BIN", bin)
	t.Setenv("HCOM_ARGV_LOG", argvLog)
	t.Setenv("PATH", wrapperDir+string(os.PathListSeparator)+filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))

	verdict := DeliverBus(dispatcher, worker, busDir, "tagged prompt", 1)
	if verdict != "delivered" && verdict != "queued" {
		t.Fatalf("DeliverBus verdict = %q, want submitted", verdict)
	}
	prompt := latestMessageFromBus(t, bin, busDir)
	if prompt.From != dispatcher {
		t.Fatalf("tagged prompt sender = %q, want full identity %q (base %q)", prompt.From, dispatcher, dispatcherBase)
	}
	logged, err := os.ReadFile(argvLog)
	if err != nil {
		t.Fatal(err)
	}
	argv := string(logged)
	if !strings.Contains(argv, "send --from "+dispatcher+" @"+worker) {
		t.Fatalf("send argv does not preserve full sender %q:\n%s", dispatcher, argv)
	}
	if !strings.Contains(argv, "--context deliver:"+dispatcher) {
		t.Fatalf("receipt query does not use full sender %q:\n%s", dispatcher, argv)
	}

	reply := exec.Command(bin, "send", "@"+dispatcher, "--name", worker, "--intent", "ack", "--reply-to", prompt.ID, "--", "tagged ack")
	reply.Env = isolatedHcomEnv(busDir, "")
	if out, err := reply.CombinedOutput(); err != nil {
		t.Fatalf("reply to full tagged sender %q failed: %v: %s", dispatcher, err, out)
	}
	ack := latestMessageFromBus(t, bin, busDir)
	if len(ack.DeliveredTo) != 1 || ack.DeliveredTo[0] != dispatcherBase {
		t.Fatalf("explicit reply to @%s delivered_to = %v, want only underlying dispatcher identity %q", dispatcher, ack.DeliveredTo, dispatcherBase)
	}
}

func TestDeliverBusRejectsEmptySenderBeforeHcomSend(t *testing.T) {
	stubDir := t.TempDir()
	argvLog := filepath.Join(t.TempDir(), "argv.log")
	stub := `#!/bin/sh
printf '%s\n' "$*" >>"$HCOM_ARGV_LOG"
exit 0
`
	if err := os.WriteFile(filepath.Join(stubDir, "hcom"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HCOM_ARGV_LOG", argvLog)

	if got := DeliverBus("", "peer-rive", t.TempDir(), "hello", 1); got != "sender_unverified" {
		t.Fatalf("DeliverBus empty sender = %q, want sender_unverified", got)
	}
	if data, err := os.ReadFile(argvLog); err == nil && strings.Contains(string(data), "send") {
		t.Fatalf("empty sender invoked hcom send:\n%s", data)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestManualSendClaimsPendingPromptBeforeSidecar(t *testing.T) {
	stubDir := t.TempDir()
	stateDir := t.TempDir()
	stub := `#!/bin/sh
case "$1" in
  list) exit 0 ;;
  send) printf 'sent\n' >>"$STUB_STATE/sends"; exit 0 ;;
  events) exit 0 ;;
esac
exit 1
`
	if err := os.WriteFile(filepath.Join(stubDir, "hcom"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STUB_STATE", stateDir)
	registryPath := filepath.Join(stateDir, "registry.jsonl")
	now := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	message := "initial prompt"
	if err := pendingprompt.Store(registryPath, pendingprompt.Record{
		GUID: "child-guid", Sender: "dispatcher", BusDir: stateDir, Message: message, VerifyMS: 1,
	}, now); err != nil {
		t.Fatal(err)
	}

	clock := now
	sender := &busSender{
		Bin:   filepath.Join(stubDir, "hcom"),
		Sleep: func(time.Duration) {},
		Now: func() time.Time {
			clock = clock.Add(2 * time.Second)
			return clock
		},
	}
	var stdout, stderr strings.Builder
	if code := sender.sendPending(registryPath, "child-guid", "dispatcher", "child", "worker", stateDir, message, 1, false, &stdout, &stderr); code != 0 {
		t.Fatalf("manual pending send code=%d stderr=%q", code, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "sends"))
	if err != nil || strings.Count(string(data), "sent") != 1 {
		t.Fatalf("manual hcom sends = %q err=%v", data, err)
	}

	sidecarCalls := 0
	result, err := pendingprompt.Attempt(registryPath, "child-guid", "", pendingprompt.ActorSidecar, clock, func(pendingprompt.Record) string {
		sidecarCalls++
		return "delivered"
	})
	if err != nil || !result.Suppressed || sidecarCalls != 0 {
		t.Fatalf("sidecar replay = %+v calls=%d err=%v", result, sidecarCalls, err)
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

func tagIsolatedHcomIdentity(t *testing.T, busDir, baseName, tag string) {
	t.Helper()
	const update = `
import sqlite3, sys
db = sqlite3.connect(sys.argv[1])
db.execute("update instances set tag=? where name=?", (sys.argv[2], sys.argv[3]))
db.commit()
`
	cmd := exec.Command("python3", "-c", update, filepath.Join(busDir, "hcom.db"), tag, baseName)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tag isolated hcom identity: %v: %s", err, out)
	}
}

func assertHcomIdentityShape(t *testing.T, bin, busDir, fullName, baseName string) {
	t.Helper()
	cmd := exec.Command(bin, "list", "--json")
	cmd.Env = isolatedHcomEnv(busDir, "")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("hcom list --json: %v: %s", err, out)
	}
	var rows []struct {
		Name     string `json:"name"`
		BaseName string `json:"base_name"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("decode hcom roster: %v: %s", err, out)
	}
	for _, row := range rows {
		if row.Name == fullName {
			if row.BaseName != baseName {
				t.Fatalf("tagged identity base_name = %q, want %q", row.BaseName, baseName)
			}
			return
		}
	}
	t.Fatalf("tagged identity %q not present in roster: %s", fullName, out)
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

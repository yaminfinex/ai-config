package spawncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/grokbridge"
	"ai-config/tools/herder/internal/launchcmd"
)

func serveGrokStatus(t *testing.T, stateDir, seat, sessionID, bus string) <-chan error {
	t.Helper()
	socket := grokbridge.SocketPath(stateDir, seat)
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	done := make(chan error, 1)
	go func() {
		defer close(done)
		for i := 0; i < 2; i++ {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				done <- acceptErr
				return
			}
			var req grokbridge.Request
			decodeErr := json.NewDecoder(conn).Decode(&req)
			if decodeErr != nil {
				conn.Close()
				done <- decodeErr
				return
			}
			if req.SessionID != sessionID {
				conn.Close()
				done <- fmt.Errorf("session id = %q, want owning capability", req.SessionID)
				return
			}
			resp := grokbridge.Response{OK: true, Generation: 17}
			switch i {
			case 0:
				if req.Op != "handshake" {
					conn.Close()
					done <- fmt.Errorf("first op = %q, want handshake", req.Op)
					return
				}
			case 1:
				if req.Op != "status" || req.Generation != 17 {
					conn.Close()
					done <- fmt.Errorf("status request = %+v, want generation-fenced status", req)
					return
				}
				resp.Status = &grokbridge.BridgeStatus{PID: 1234, Bus: bus, Wake: "armed"}
			}
			encodeErr := json.NewEncoder(conn).Encode(resp)
			conn.Close()
			if encodeErr != nil {
				done <- encodeErr
				return
			}
		}
	}()
	return done
}

func TestT19GrokPassthroughRefusals(t *testing.T) {
	cases := []string{
		"--session-id", "--session-id=value", "-s", "--resume", "-r", "--continue", "-c", "--continue=1", "--fork-session",
		"--rules", "--permission-mode", "--always-approve", "--bypassPermissions",
		"--no-auto-update", "--auto-update", "--disable-auto-update", "--agents", "--agent", "--subagents",
		"--no-subagents", "--no-no-subagents", "HOME=/tmp/elsewhere", "GROK_HOME=/tmp/elsewhere",
	}
	for _, arg := range cases {
		t.Run(strings.TrimLeft(strings.ReplaceAll(arg, "=", "_"), "-"), func(t *testing.T) {
			if err := launchcmd.ValidateGrokExtraArgs([]string{arg}, false); err == nil || !strings.Contains(err.Error(), "remove") {
				t.Fatalf("validateGrokExtraArgs(%q) = %v, want targeted refusal with remedy", arg, err)
			}
		})
	}
}

func TestT19GrokModelPassthroughOnlyConflictsWithFirstClassModel(t *testing.T) {
	if err := launchcmd.ValidateGrokExtraArgs([]string{"--model", "grok-4.5"}, false); err != nil {
		t.Fatalf("passthrough-only model refused: %v", err)
	}
	if err := launchcmd.ValidateGrokExtraArgs([]string{"--model=grok-4.5"}, true); err == nil || !strings.Contains(err.Error(), "--model conflicts") {
		t.Fatalf("first-class model collision = %v", err)
	}
}

func TestGrokNormalAndSafePermissionMapping(t *testing.T) {
	if got := defaultPermFlag("grok"); got != "--always-approve" {
		t.Fatalf("normal Grok permission flag = %q", got)
	}
	if !hasExplicitPermFlag([]string{"--always-approve"}) {
		t.Fatal("Grok permission flag not recognized as explicit")
	}
}

func TestGrokAbsentKeyDefersToFreshPanePreflight(t *testing.T) {
	t.Setenv("XAI_API_KEY", "")
	var stdout, stderr bytes.Buffer
	_, rc := parseArgs([]string{"--role", "neutral", "--agent", "grok"}, &stdout, &stderr)
	if rc != 0 || stderr.Len() != 0 {
		t.Fatalf("spawn-side auth check rc=%d stderr=%q; fresh pane must own the preflight", rc, stderr.String())
	}
}

func TestGrokBoundBusComesFromGenerationFencedBridgeStatus(t *testing.T) {
	stateDir := t.TempDir()
	done := serveGrokStatus(t, stateDir, "seat", "owning-session", "bridge-bus")
	if got := grokBoundBusOnce(stateDir, "seat", "owning-session"); got != "bridge-bus" {
		t.Fatalf("grokBoundBusOnce() = %q, want bridge status bus", got)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestGrokAwaitBindNeverConsultsHcomRoster(t *testing.T) {
	stateDir := t.TempDir()
	done := serveGrokStatus(t, stateDir, "seat", "owning-session", "bridge-bus")
	marker := filepath.Join(t.TempDir(), "hcom-called")
	stubDir := t.TempDir()
	stub := "#!/bin/sh\nprintf called > \"$HCOM_MARKER\"\necho '[]'\n"
	if err := os.WriteFile(filepath.Join(stubDir, "hcom"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HCOM_MARKER", marker)
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	r := &runner{opts: options{Agent: "grok", BindTimeoutMS: 500}, herdr: &cleanupHerdr{}}
	paneID := "pane"
	name, reason, blocked, _ := r.awaitBind(&paneID, filepath.Join(stateDir, "registry.jsonl"), "seat", t.TempDir(), "launch-pane", "owning-session")
	if name != "bridge-bus" || reason != "bound" || blocked {
		t.Fatalf("awaitBind() = name %q reason %q blocked %v", name, reason, blocked)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("Grok bind consulted hcom roster: %v", err)
	}
}

func TestNonGrokAwaitBindKeepsRosterContract(t *testing.T) {
	stubDir := t.TempDir()
	stub := "#!/bin/sh\necho '[{\"name\":\"matched-bus\",\"launch_context\":{\"pane_id\":\"launch-pane\"}}]'\n"
	if err := os.WriteFile(filepath.Join(stubDir, "hcom"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	r := &runner{opts: options{Agent: "codex", BindTimeoutMS: 500}, herdr: &cleanupHerdr{}}
	paneID := "pane"
	name, reason, _, _ := r.awaitBind(&paneID, filepath.Join(t.TempDir(), "registry.jsonl"), "seat", t.TempDir(), "launch-pane", "")
	if name != "matched-bus" || reason != "bound" {
		t.Fatalf("non-Grok awaitBind() = name %q reason %q", name, reason)
	}
}

func TestGrokBindTimeoutHardFailsWithConfirmedCleanup(t *testing.T) {
	client := &cleanupHerdr{}
	var stderr strings.Builder
	r := &runner{opts: options{Agent: "grok", BindTimeoutMS: 1}, herdr: client, stderr: &stderr}
	paneID := "p_new"
	name, reason, _, _ := r.awaitBind(&paneID, filepath.Join(t.TempDir(), "registry.jsonl"), "seat", t.TempDir(), "launch-pane", "owning-session")
	if name != "" || !strings.HasPrefix(reason, "bind-timeout") {
		t.Fatalf("unbound awaitBind() = name %q reason %q", name, reason)
	}
	if code := r.failUnboundGrok(name, reason, paneID, "term_new"); code != 1 {
		t.Fatalf("failUnboundGrok() = %d, want 1", code)
	}
	if !client.closed {
		t.Fatal("unbound Grok pane was not closed")
	}
	got := stderr.String()
	for _, want := range []string{"did not report a live bound bus", "inspect the seat bridge log", "correct the bridge or hcom configuration", "retry the spawn", "cleanup confirmed"} {
		if !strings.Contains(got, want) {
			t.Fatalf("stderr = %q, missing %q", got, want)
		}
	}
}

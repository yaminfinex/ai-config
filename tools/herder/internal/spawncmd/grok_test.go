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
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

func serveGrokStatus(t *testing.T, stateDir, seat, sessionID, bus string) <-chan error {
	return serveGrokStatusCalls(t, stateDir, seat, sessionID, bus, 1)
}

func serveGrokStatusCalls(t *testing.T, stateDir, seat, sessionID, bus string, statusCalls int) <-chan error {
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
		for call := 0; call < statusCalls; call++ {
			for phase := 0; phase < 2; phase++ {
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
				switch phase {
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
		}
	}()
	return done
}

type grokSpawnFlowHerdr struct {
	cleanupHerdr
	onStart func([]string)
}

func (f *grokSpawnFlowHerdr) Combined(args ...string) ([]byte, int, error) {
	if len(args) >= 2 && args[0] == "agent" && args[1] == "start" {
		if f.onStart != nil {
			f.onStart(args)
		}
		return []byte(`{"result":{"agent":{"pane_id":"p_new","workspace_id":"w_new","tab_id":"t_new","terminal_id":"term_new","cwd":"/tmp"}}}`), 0, nil
	}
	return f.cleanupHerdr.Combined(args...)
}

func flowArg(args []string, prefix string) string {
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, prefix); ok {
			return value
		}
	}
	return ""
}

func grokFlowRunner(t *testing.T, client herdrClient, state string) (*runner, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	stubDir := t.TempDir()
	if err = os.WriteFile(filepath.Join(stubDir, "hcom"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_CONFIG_ROOT", root)
	t.Setenv("HERDER_STATE_DIR", state)
	t.Setenv("HERDR_PANE_ID", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	r := &runner{
		opts: options{
			Role: "neutral", Agent: "grok", Split: "right", FocusFlag: "--no-focus",
			BindTimeoutMS: 1, JSONOutput: true,
		},
		stdout: stdout,
		stderr: stderr,
		herdr:  client,
	}
	return r, stdout, stderr
}

func shortGrokFlowState(t *testing.T) string {
	t.Helper()
	state, err := os.MkdirTemp("/tmp", "gsf-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(state) })
	return state
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

func TestGrokSpawnFlowHardFailsNoBindWithConfirmedCleanup(t *testing.T) {
	state := shortGrokFlowState(t)
	client := &grokSpawnFlowHerdr{}
	r, _, stderr := grokFlowRunner(t, client, state)
	if code := r.run(); code == 0 {
		t.Fatal("no-bind Grok spawn flow returned success")
	}
	if !client.closed {
		t.Fatal("no-bind Grok spawn flow did not close the launched pane")
	}
	for _, want := range []string{"did not report a live bound bus", "cleanup confirmed"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr=%q, missing %q", stderr.String(), want)
		}
	}
	if _, err := os.Stat(filepath.Join(state, "registry.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("no-bind Grok spawn registered a session: %v", err)
	}
}

func TestGrokSpawnFlowNeverEnrichesNameFromRegistryCapture(t *testing.T) {
	state := shortGrokFlowState(t)
	client := &grokSpawnFlowHerdr{}
	var guid string
	var bridgeDone <-chan error
	client.onStart = func(args []string) {
		guid = flowArg(args, "HERDER_GUID=")
		sessionID := flowArg(args, "HERDER_GROK_SESSION_ID=")
		if guid == "" || sessionID == "" {
			t.Fatalf("spawn argv missing Grok identity: %v", args)
		}
		bridgeDone = serveGrokStatusCalls(t, state, guid, sessionID, "bridge-bus", 2)
	}
	r, _, stderr := grokFlowRunner(t, client, state)
	r.opts.ReadyMatch = "never-visible"
	injected := false
	r.updateRegistry = func(path string, fn registry.LockedUpdateFunc) ([]registry.WriteOutcome, error) {
		outcomes, err := registry.UpdateLocked(path, fn)
		if err != nil || injected {
			return outcomes, err
		}
		injected = true
		injectedOutcomes, err := registry.UpdateLocked(path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
			current := registry.V2ByGUID(tx.Projection, guid)
			if current == nil {
				return nil, fmt.Errorf("registered Grok row %s missing", guid)
			}
			next := *current
			seat := v2.Seat{Kind: "herdr"}
			if current.Seat != nil {
				seat = *current.Seat
			}
			seat.HcomName = "registry-poison"
			next.Seat = &seat
			next.Event = "recognised"
			next.RecordedAt = "2026-07-13T00:00:01Z"
			return []v2.SessionRecord{next}, nil
		})
		if err != nil {
			return outcomes, err
		}
		if len(injectedOutcomes) != 1 || injectedOutcomes[0].Status != registry.WriteApplied {
			return outcomes, fmt.Errorf("registry poison write outcomes=%+v, want one applied row", injectedOutcomes)
		}
		if err := injectedOutcomes[0].Err(); err != nil {
			return outcomes, err
		}
		return outcomes, nil
	}
	if code := r.run(); code != 0 {
		t.Fatalf("Grok spawn flow rc=%d stderr=%q", code, stderr.String())
	}
	recs, err := registry.Load(filepath.Join(state, "registry.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	rec := registry.Resolve(recs, guid)
	if rec == nil || rec.HcomName != "bridge-bus" {
		name := "<missing>"
		if rec != nil {
			name = rec.HcomName
		}
		t.Fatalf("Grok registry name=%q, want live bridge status bus and never registry-poison", name)
	}
	if err := <-bridgeDone; err != nil {
		t.Fatal(err)
	}
}

package cullcmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/grokbridge"
	"ai-config/tools/herder/internal/hcomidentity"
	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/launchcmd"
	"ai-config/tools/herder/internal/liveness"
	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

type cullResponse struct {
	want string
	out  []byte
	rc   int
	err  error
}

func TestObserverDownCLIUsesSharedPredicateToUnseatDeadProcess(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	_, err := registry.UpdateLocked(path, func(tx registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{
			GUID: "fixture-cli-death", Event: "seated", RecordedAt: "2026-07-17T08:00:00Z", State: v2.StateSeated,
			Seat: &v2.Seat{Kind: "process", Node: tx.NodeID, PID: pid, HcomName: "fixture-bus"},
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	recs, err := registry.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	rec := registry.Resolve(recs, "fixture-cli-death")
	if rec == nil {
		t.Fatal("missing process fixture")
	}
	verdict := liveness.Evaluate(cullLivenessInputFromRows(*rec, map[string]herdrcli.Agent{}, []hcomidentity.Row{{
		Name: "fixture-bus", Status: "listening", StatusAge: 301,
	}}, nil))
	if verdict.Class != liveness.VerdictPositiveDeath || verdict.Cause != liveness.CauseDeadPIDStaleBusRow {
		t.Fatalf("verdict = %+v", verdict)
	}
	stamp := "2026-07-17T14:00:00Z"
	closed, appended, err := applyObservedDeath(path, *rec, verdict, stamp, "cull")
	if err != nil || !appended {
		t.Fatalf("apply = appended=%t closed=%+v err=%v", appended, closed, err)
	}
	got := latestSession(t, path, "fixture-cli-death")
	if got.State != v2.StateUnseated || got.RecordedAt != stamp || !strings.Contains(got.CloseReason, "dead_pid_stale_bus_row") || !strings.Contains(got.ObservedVia, "cull") {
		t.Fatalf("unseat evidence = %+v", got)
	}
}

type scriptedCullClient struct {
	responses []cullResponse
	calls     []string
}

func (c *scriptedCullClient) Combined(args ...string) ([]byte, int, error) {
	call := strings.Join(args, " ")
	c.calls = append(c.calls, call)
	if len(c.responses) == 0 {
		return nil, 64, errors.New("unexpected call")
	}
	r := c.responses[0]
	c.responses = c.responses[1:]
	if call != r.want {
		return nil, 64, fmt.Errorf("call = %q, want %q", call, r.want)
	}
	return r.out, r.rc, r.err
}

func assertCullScriptConsumed(t *testing.T, client *scriptedCullClient) {
	t.Helper()
	if len(client.responses) != 0 {
		t.Fatalf("%d scripted response(s) were not consumed; calls = %v", len(client.responses), client.calls)
	}
}

func TestProcessTargetPreservesFocusWhenClosingPane(t *testing.T) {
	registryPath := seedSeatedCullRow(t, "worker", "p_target", "term_target")
	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	rec := registry.Resolve(recs, "guid-ghost")
	client := &scriptedCullClient{responses: []cullResponse{
		{want: "pane list", out: []byte(`{"result":{"panes":[{"pane_id":"p_owner","focused":true}]}}`)},
		{want: "pane close p_target", out: []byte(`{"result":{"type":"ok"}}`)},
		{want: "pane list", out: []byte(`{"result":{"panes":[{"pane_id":"p_owner","focused":true}]}}`)},
	}}
	var stdout, stderr strings.Builder
	if ok := processTargetWithClient(registryPath, *rec, nil, options{force: true}, "2026-07-15T00:00:00Z", &stdout, &stderr, client); !ok {
		t.Fatalf("processTargetWithClient failed\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	assertCullScriptConsumed(t, client)
}

func TestGrokCullRetiresBridgeAndPendingMessages(t *testing.T) {
	state, err := os.MkdirTemp("/tmp", "grok-cull-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(state) })
	guid, err := registry.NewGUID()
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := launchcmd.NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	seatDir := grokbridge.SeatDir(state, guid)
	j, err := grokbridge.OpenJournal(filepath.Join(seatDir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	event := json.RawMessage(`{"id":41,"type":"message","data":{"from":"sender","text":"pending","intent":"request","delivered_to":["grok-cull-bus"]}}`)
	if _, added, queueErr := j.Queue(event); queueErr != nil || !added {
		t.Fatalf("queue added=%v err=%v", added, queueErr)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	hcom := filepath.Join(state, "hcom")
	if err := os.WriteFile(hcom, []byte("#!/bin/sh\nif [ \"$1\" = start ]; then printf '%s\\n' '[hcom:grok-cull-bus]'; exit 0; fi\nif [ \"$1\" = list ]; then printf '%s\\n' '{\"name\":\"grok-cull-bus\"}'; exit 0; fi\ncase \" $* \" in *' --wait '*) exec sleep 60;; esac\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := grokbridge.OpenBinder(grokbridge.BinderConfig{Seat: guid, StateDir: state, HcomBin: hcom, SessionID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = b.Close()
		}
	})
	serveDone := make(chan error, 1)
	go func() { serveDone <- b.Serve(context.Background()) }()
	waitForCullBridge(t, grokbridge.SocketPath(state, guid))

	registryPath := filepath.Join(state, "registry.jsonl")
	outcomes, err := registry.UpdateLocked(registryPath, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{
			Kind: v2.KindSession, GUID: guid, Event: "registered", RecordedAt: "2026-07-13T00:00:00Z", State: v2.StateSeated,
			Label: "grok-cull", Role: "worker", Tool: "grok",
			SIDs: []v2.SID{{SID: sessionID, Source: "launch"}}, Provenance: v2.Provenance{ToolSessionID: sessionID},
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertWriteOutcomes(t, outcomes)
	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	rec := registry.Resolve(recs, guid)
	if rec == nil {
		t.Fatal("missing seeded Grok row")
	}
	if rec.Capabilities != nil {
		t.Fatalf("spawn-shaped Grok row unexpectedly has capabilities: %+v", rec.Capabilities)
	}
	var stdout, stderr strings.Builder
	if ok := processTarget(registryPath, *rec, nil, options{}, "2026-07-13T00:01:00Z", &stdout, &stderr); !ok {
		t.Fatalf("processTarget failed\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	select {
	case <-serveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Grok binder did not stop after cull retirement")
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true
	latest := latestSession(t, registryPath, guid)
	if latest.State != v2.StateUnseated || latest.Capabilities == nil || latest.Capabilities.Bus != "" || latest.Capabilities.Wake != "down" || latest.Capabilities.Pending != 0 || latest.Capabilities.BinderPID != 0 || latest.Capabilities.Undeliverable != 1 {
		t.Fatalf("latest row = %+v, want unseated/down with one undeliverable", latest)
	}
	journal, err := os.ReadFile(filepath.Join(seatDir, "journal.jsonl"))
	if err != nil || !strings.Contains(string(journal), `"kind":"undeliverable","at":`) {
		t.Fatalf("journal err=%v contents=%s, want durable undeliverable record", err, journal)
	}
	if !strings.Contains(stdout.String(), "grok bridge: retired 1 pending message(s) as undeliverable") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestGrokCullDeadBridgeConvergesOfflineAndDropsRosterEntry(t *testing.T) {
	state := shortCullStateDir(t)
	guid, err := registry.NewGUID()
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := launchcmd.NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	journal, err := grokbridge.OpenJournal(filepath.Join(grokbridge.SeatDir(state, guid), "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = journal.AdvanceGeneration(); err != nil {
		t.Fatal(err)
	}
	if _, added, queueErr := journal.Queue(json.RawMessage(`{"id":51,"type":"message","data":{"from":"sender","text":"pending","intent":"request","delivered_to":["dead-bus"]}}`)); queueErr != nil || !added {
		t.Fatalf("queue added=%v err=%v", added, queueErr)
	}
	if err = journal.Close(); err != nil {
		t.Fatal(err)
	}
	registryPath := seedSpawnShapedGrokCullRow(t, state, guid, sessionID, "dead-bus")
	mockDir, stopProbe, rowState := installHcomGrokTeardownMock(t, "dead-bus", true)
	t.Setenv("PATH", mockDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	rec := registry.Resolve(recs, guid)
	if rec == nil || rec.Capabilities != nil {
		t.Fatalf("spawn-shaped row=%+v", rec)
	}
	var stdout, stderr strings.Builder
	if ok := processTarget(registryPath, *rec, nil, options{}, "2026-07-13T00:01:00Z", &stdout, &stderr); !ok {
		t.Fatalf("dead bridge cull failed\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	latest := latestSession(t, registryPath, guid)
	if latest.State != v2.StateUnseated || latest.Capabilities == nil || latest.Capabilities.Wake != "down" || latest.Capabilities.Pending != 0 || latest.Capabilities.Undeliverable != 1 {
		t.Fatalf("latest=%+v, want converged down row", latest)
	}
	data := mustReadFile(t, filepath.Join(grokbridge.SeatDir(state, guid), "journal.jsonl"))
	if !strings.Contains(string(data), `"kind":"undeliverable"`) {
		t.Fatalf("journal=%s, want undeliverable", data)
	}
	if got := strings.TrimSpace(string(mustReadFile(t, stopProbe))); got != "dead-bus" {
		t.Fatalf("hcom stop probe=%q", got)
	}
	if _, err := os.Stat(rowState); !os.IsNotExist(err) {
		t.Fatalf("Grok bus row residue remains after cull: %v", err)
	}
	if !strings.Contains(stdout.String(), "bus: stopped @dead-bus (row absence confirmed)") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestGrokCullHeldLockReportsAliveButUnservedBridge(t *testing.T) {
	state := shortCullStateDir(t)
	guid, err := registry.NewGUID()
	if err != nil {
		t.Fatal(err)
	}
	sessionID, err := launchcmd.NewGrokSessionID()
	if err != nil {
		t.Fatal(err)
	}
	hcom := filepath.Join(state, "hcom")
	if err = os.WriteFile(hcom, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	binder, err := grokbridge.OpenBinder(grokbridge.BinderConfig{Seat: guid, StateDir: state, HcomBin: hcom, SessionID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	defer binder.Close()
	registryPath := seedSpawnShapedGrokCullRow(t, state, guid, sessionID, "wedged-bus")
	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	rec := registry.Resolve(recs, guid)
	var stdout, stderr strings.Builder
	if ok := processTarget(registryPath, *rec, nil, options{}, "2026-07-13T00:01:00Z", &stdout, &stderr); ok {
		t.Fatalf("wedged bridge cull unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	if got := stderr.String(); !strings.Contains(got, "binder is alive but not serving") || !strings.Contains(got, "inspect the seat bridge log") || !strings.Contains(got, "retry the cull") {
		t.Fatalf("stderr=%q, want honest cause and remedy", got)
	}
	if latest := latestSession(t, registryPath, guid); latest.State != v2.StateUnseated {
		t.Fatalf("latest=%+v, want cull fact recorded before retirement error", latest)
	}
}

func shortCullStateDir(t *testing.T) string {
	t.Helper()
	state, err := os.MkdirTemp("/tmp", "gc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(state) })
	return state
}

func seedSpawnShapedGrokCullRow(t *testing.T, state, guid, sessionID, busName string) string {
	t.Helper()
	registryPath := filepath.Join(state, "registry.jsonl")
	verified := true
	outcomes, err := registry.UpdateLocked(registryPath, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{
			Kind: v2.KindSession, GUID: guid, Event: "registered", RecordedAt: "2026-07-13T00:00:00Z", State: v2.StateSeated,
			Label: "grok-cull", Role: "worker", Tool: "grok",
			Seat: &v2.Seat{Kind: "herdr", HcomName: busName, HcomVerified: &verified},
			SIDs: []v2.SID{{SID: sessionID, Source: "launch"}}, Provenance: v2.Provenance{ToolSessionID: sessionID},
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertWriteOutcomes(t, outcomes)
	return registryPath
}

func installHcomKillMock(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	probe := filepath.Join(dir, "hcom-kill")
	script := "#!/bin/sh\nif [ \"$1\" = kill ]; then printf '%s\\n' \"$2\" >>\"" + probe + "\"; exit 0; fi\nexit 64\n"
	if err := os.WriteFile(filepath.Join(dir, "hcom"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir, probe
}

func installHcomGrokTeardownMock(t *testing.T, busName string, removeOnStop bool) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	stopProbe := filepath.Join(dir, "hcom-stop")
	rowState := filepath.Join(dir, "live-row")
	if err := os.WriteFile(rowState, []byte(busName+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	remove := ""
	if removeOnStop {
		remove = "[ \"$(tr -d '\\n' <\"" + rowState + "\")\" = \"$2\" ] && rm -f \"" + rowState + "\"; "
	}
	script := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"  stop) printf '%s\\n' \"$2\" >>\"" + stopProbe + "\"; " + remove + "exit 0;;\n" +
		"  list) if [ -f \"" + rowState + "\" ]; then printf '[{\"name\":\"%s\"}]\\n' \"$(tr -d '\\n' <\"" + rowState + "\")\"; else printf '[]\\n'; fi; exit 0;;\n" +
		"  kill) exit 65;;\n" +
		"esac\n" +
		"exit 64\n"
	if err := os.WriteFile(filepath.Join(dir, "hcom"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir, stopProbe, rowState
}

func TestGrokBusTeardownRejectsSuccessfulStopWithRowResidue(t *testing.T) {
	mockDir, stopProbe, rowState := installHcomGrokTeardownMock(t, "sticky-bus", false)
	t.Setenv("PATH", mockDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var stdout strings.Builder
	err := stopGrokBusEntry(registry.Record{Agent: "grok", HcomName: "sticky-bus"}, &stdout)
	if err == nil || !strings.Contains(err.Error(), "row is still present") {
		t.Fatalf("stopGrokBusEntry() error = %v, want row-residue refusal", err)
	}
	if got := strings.TrimSpace(string(mustReadFile(t, stopProbe))); got != "sticky-bus" {
		t.Fatalf("hcom stop probe=%q", got)
	}
	if _, statErr := os.Stat(rowState); statErr != nil {
		t.Fatalf("sticky row was unexpectedly removed: %v", statErr)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q, must not claim confirmed absence", stdout.String())
	}
}

func TestNonGrokBusTeardownKeepsKillPath(t *testing.T) {
	mockDir, killProbe := installHcomKillMock(t)
	t.Setenv("PATH", mockDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var stdout strings.Builder
	rec := registry.Record{Agent: "codex", HcomName: "other-bus"}
	if err := teardownBusEntry(rec, &stdout); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(mustReadFile(t, killProbe))); got != "other-bus" {
		t.Fatalf("non-Grok hcom kill probe=%q", got)
	}
	if !strings.Contains(stdout.String(), "bus: dropped @other-bus") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func waitForCullBridge(t *testing.T, socket string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if st, err := os.Stat(socket); err == nil && st.Mode()&os.ModeSocket != 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Grok bridge socket %s did not become ready", socket)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRunClosesSeatedPaneLessRowWithoutForce(t *testing.T) {
	registryPath := seedSeatedCullRow(t, "ghost", "", "")
	mockDir, closeProbe := installCullTestMocks(t)
	t.Setenv("PATH", mockDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "p_culler")
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(registryPath))

	var stdout, stderr strings.Builder
	if rc := Run([]string{"--label", "ghost"}, &stdout, &stderr); rc != 0 {
		t.Fatalf("Run rc = %d, want 0\nstdout:\n%s\nstderr:\n%s", rc, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(closeProbe); !os.IsNotExist(err) {
		t.Fatalf("pane close probe exists = %v, want no pane close call for pane-less row", err)
	}
	if got := latestSession(t, registryPath, "guid-ghost"); got.Event != "unseated" || got.State != v2.StateUnseated || got.CloseResult != "requested" || !strings.Contains(got.CloseReason, "operator-cull") || got.Seat != nil {
		t.Fatalf("latest row = %+v, want explicit requested cull without a fabricated death verdict", got)
	}
	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := registry.Resolve(recs, "guid-ghost"); got == nil || got.State != v2.StateUnseated {
		t.Fatalf("latest = %+v, want unseated dormant row", got)
	}
	if !strings.Contains(stdout.String(), "recorded unseated ghost (guid-ghost) pane= → requested") {
		t.Fatalf("stdout = %q, want recorded-unseated line", stdout.String())
	}
}

func TestRunPaneLessUnannotatedCullAppendsOneVerifiedAnnotation(t *testing.T) {
	registryPath := seedUnseatedCullRow(t, "ghost")
	mockDir, _ := installCullTestMocks(t)
	t.Setenv("PATH", mockDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "p_culler")
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(registryPath))

	before := closeRecordCount(t, registryPath, "guid-ghost")
	var stdout, stderr strings.Builder
	if rc := Run([]string{"--label", "ghost"}, &stdout, &stderr); rc != 0 {
		t.Fatalf("Run rc = %d, want 0\nstdout:\n%s\nstderr:\n%s", rc, stdout.String(), stderr.String())
	}
	if after := closeRecordCount(t, registryPath, "guid-ghost"); after != before+1 {
		t.Fatalf("unseated rows = %d, want %d", after, before+1)
	}
	if got := latestSession(t, registryPath, "guid-ghost"); got.State != v2.StateUnseated || got.CloseResult != "requested" || !strings.Contains(got.CloseReason, "source=operator-cull") || got.RecordedAt == "2026-07-08T00:00:00Z" {
		t.Fatalf("latest row = %+v, want fresh operator-request annotation", got)
	}
	if !strings.Contains(stdout.String(), "recorded unseated ghost (guid-ghost) pane= → requested") {
		t.Fatalf("stdout = %q, want recorded-unseated line", stdout.String())
	}

	beforeBytes := mustReadFile(t, registryPath)
	stdout.Reset()
	stderr.Reset()
	if rc := Run([]string{"--label", "ghost"}, &stdout, &stderr); rc != 0 {
		t.Fatalf("second Run rc = %d, want 0\nstdout:\n%s\nstderr:\n%s", rc, stdout.String(), stderr.String())
	}
	if afterBytes := mustReadFile(t, registryPath); string(afterBytes) != string(beforeBytes) {
		t.Fatalf("registry changed after repeat cull\nbefore:\n%s\nafter:\n%s", beforeBytes, afterBytes)
	}
	if !strings.Contains(stdout.String(), "already unseated ghost (guid-ghost) at ") || !strings.Contains(stdout.String(), "close_result=requested") {
		t.Fatalf("second stdout = %q, want recorded fact line", stdout.String())
	}
}

func TestRunPaneLessDifferingRepeatCullPreservesRecordedCloseResult(t *testing.T) {
	registryPath := seedAnnotatedUnseatedCullRow(t, "ghost", "closed", "first observer")
	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	before := mustReadFile(t, registryPath)
	closed, appended, err := appendClosed(registryPath, *registry.Resolve(recs, "ghost"), "2026-07-08T12:00:00Z", "error", "second observer")
	if err != nil {
		t.Fatal(err)
	}
	if appended {
		t.Fatalf("appendClosed appended on already-unseated row")
	}
	if closed.CloseResult != "closed" || closed.CloseReason != "first observer" {
		t.Fatalf("unseated fact = %+v, want original close annotation", closed)
	}
	if got := latestSession(t, registryPath, "guid-ghost"); got.CloseResult != "closed" || got.CloseReason != "first observer" {
		t.Fatalf("latest row = %+v, want original close annotation", got)
	}
	if after := mustReadFile(t, registryPath); string(after) != string(before) {
		t.Fatalf("registry changed after differing repeat cull\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestRunUnannotatedStaleCoordinateCorpseDoesNotStampBlindly(t *testing.T) {
	registryPath := seedUnseatedCullRows(t, v2.SessionRecord{
		GUID:  "guid-ghost",
		Label: "ghost",
		Seat: &v2.Seat{
			Kind:       "herdr",
			PaneID:     "p_stale",
			TerminalID: "term_stale",
		},
	})
	mockDir, _ := installCullTestMocks(t)
	t.Setenv("PATH", mockDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "p_culler")
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(registryPath))

	before := mustReadFile(t, registryPath)
	var stdout, stderr strings.Builder
	if rc := Run([]string{"--label", "ghost"}, &stdout, &stderr); rc != 0 {
		t.Fatalf("Run rc = %d, want 0\nstdout:\n%s\nstderr:\n%s", rc, stdout.String(), stderr.String())
	}
	if after := mustReadFile(t, registryPath); string(after) != string(before) {
		t.Fatalf("registry changed after unverifiable cull\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if !strings.Contains(stdout.String(), "never close-annotated (migrated corpse); gone-ness unverifiable from here") {
		t.Fatalf("stdout = %q, want unverifiable migrated corpse render", stdout.String())
	}
	if strings.Contains(stdout.String(), "close_result=") {
		t.Fatalf("stdout = %q, must not render blank close_result as recorded fact", stdout.String())
	}
}

func TestRunGoneSweepSkipsUnseatedCorpsesByteIdentically(t *testing.T) {
	registryPath := seedUnseatedCullRows(t,
		v2.SessionRecord{GUID: "guid-ghost-a", Label: "ghost-a", CloseResult: "already_gone", CloseReason: "terminal_id not in live agent list"},
		v2.SessionRecord{GUID: "guid-ghost-b", Label: "ghost-b"},
	)
	mockDir, _ := installCullTestMocks(t)
	t.Setenv("PATH", mockDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "p_culler")
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(registryPath))

	before := mustReadFile(t, registryPath)
	for i := 0; i < 2; i++ {
		var stdout, stderr strings.Builder
		if rc := Run([]string{"--gone"}, &stdout, &stderr); rc != 0 {
			t.Fatalf("run %d rc = %d, want 0\nstdout:\n%s\nstderr:\n%s", i+1, rc, stdout.String(), stderr.String())
		}
		if !strings.Contains(stdout.String(), "no gone records to cull") {
			t.Fatalf("run %d stdout = %q, want no-op sweep line", i+1, stdout.String())
		}
		if after := mustReadFile(t, registryPath); string(after) != string(before) {
			t.Fatalf("registry changed after gone sweep %d\nbefore:\n%s\nafter:\n%s", i+1, before, after)
		}
	}
}

func TestRunPaneLessCloseWriteFailureExitsNonzero(t *testing.T) {
	registryPath := seedPaneLessCullRow(t, "ghost")
	mockDir, _ := installCullTestMocks(t)
	t.Setenv("PATH", mockDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "p_culler")
	t.Setenv("HERDER_STATE_DIR", filepath.Dir(registryPath))
	t.Setenv("HERDER_TEST_FLOCK_REFUSE", "1")

	var stdout, stderr strings.Builder
	if rc := Run([]string{"--label", "ghost"}, &stdout, &stderr); rc == 0 {
		t.Fatalf("Run rc = 0, want nonzero\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "recorded unseated") || strings.Contains(stdout.String(), "recorded closed") || strings.Contains(stdout.String(), "marked closed") {
		t.Fatalf("stdout claims closure despite write failure: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "registry lock unavailable") {
		t.Fatalf("stderr = %q, want lock failure", stderr.String())
	}
	if got := closeRecordCount(t, registryPath, "guid-ghost"); got != 0 {
		t.Fatalf("unseated records = %d, want none after write refusal", got)
	}
}

func TestAppendClosedRecordsPaneNotFoundAsUnseated(t *testing.T) {
	registryPath := seedSeatedCullRow(t, "ghost", "p_missing", "")
	recs, err := registry.Load(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	closed, appended, err := appendClosed(registryPath, *registry.Resolve(recs, "ghost"), "2026-07-08T12:00:00Z", "error", "pane_not_found")
	if err != nil {
		t.Fatal(err)
	}
	if !appended {
		t.Fatalf("appendClosed appended = false, want true")
	}
	if closed.State != v2.StateUnseated || closed.CloseResult != "error" || closed.CloseReason != "pane_not_found" {
		t.Fatalf("unseated fact row = %+v", closed)
	}
	if got := latestSession(t, registryPath, "guid-ghost"); got.Event != "unseated" || got.State != v2.StateUnseated || got.CloseResult != "error" || got.CloseReason != "pane_not_found" || got.Seat != nil {
		t.Fatalf("latest row = %+v, want unseated pane_not_found without seat", got)
	}
}

func seedPaneLessCullRow(t *testing.T, label string) string {
	t.Helper()
	return seedUnseatedCullRow(t, label)
}

func seedUnseatedCullRow(t *testing.T, label string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	if err := os.WriteFile(path, []byte(`{"kind":"session","guid":"guid-ghost","event":"migrated_v1","recorded_at":"2026-07-08T00:00:00Z","state":"unseated","label":"`+label+`","role":"worker","tool":"codex"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func seedAnnotatedUnseatedCullRow(t *testing.T, label, closeResult, closeReason string) string {
	t.Helper()
	return seedUnseatedCullRows(t, v2.SessionRecord{GUID: "guid-ghost", Label: label, CloseResult: closeResult, CloseReason: closeReason})
}

func seedUnseatedCullRows(t *testing.T, rows ...v2.SessionRecord) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		out := make([]v2.SessionRecord, 0, len(rows))
		for _, row := range rows {
			row.Kind = v2.KindSession
			row.Event = "unseated"
			row.RecordedAt = "2026-07-08T00:00:00Z"
			row.State = v2.StateUnseated
			row.Role = "worker"
			row.Tool = "codex"
			out = append(out, row)
		}
		return out, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertWriteOutcomes(t, outcomes)
	return path
}

func seedSeatedCullRow(t *testing.T, label, paneID, terminalID string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{
			Kind:       v2.KindSession,
			GUID:       "guid-ghost",
			Event:      "registered",
			RecordedAt: "2026-07-08T00:00:00Z",
			State:      v2.StateSeated,
			Label:      label,
			Role:       "worker",
			Tool:       "codex",
			Seat: &v2.Seat{
				Kind:       "herdr",
				TerminalID: terminalID,
				PaneID:     paneID,
			},
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertWriteOutcomes(t, outcomes)
	return path
}

func assertWriteOutcomes(t *testing.T, outcomes []registry.WriteOutcome) {
	t.Helper()
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			t.Fatal(err)
		}
	}
}

func installCullTestMocks(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	closeProbe := filepath.Join(dir, "pane_close_called")
	herdr := `#!/usr/bin/env bash
set -euo pipefail
case "${1:-} ${2:-}" in
  "agent list")
    printf '{"result":{"agents":[]}}\n'
    ;;
  "pane close")
    printf '%s\n' "${3:-}" >>"` + closeProbe + `"
    printf '{"error":{"code":"pane_not_found"}}\n'
    ;;
  *)
    printf 'mock herdr: unhandled %s\n' "$*" >&2
    exit 64
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "herdr"), []byte(herdr), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "jq"), []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, closeProbe
}

func latestSession(t *testing.T, path, guid string) v2.SessionRecord {
	t.Helper()
	proj, err := v2.LoadFile(path, v2.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := registry.V2ByGUID(proj, guid)
	if got == nil {
		t.Fatalf("missing guid %s", guid)
	}
	return *got
}

func closeRecordCount(t *testing.T, path, guid string) int {
	t.Helper()
	data := mustReadFile(t, path)
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.Contains(line, `"guid":"`+guid+`"`) && strings.Contains(line, `"event":"unseated"`) && strings.Contains(line, `"close_result":"`) {
			count++
		}
	}
	return count
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

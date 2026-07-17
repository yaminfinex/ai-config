package cullcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"ai-config/tools/herder/internal/registry"
	v2 "ai-config/tools/herder/internal/registry/v2"
)

type gracefulHarness struct {
	stateDir  string
	busDir    string
	sendLog   string
	eventsLog string
	closeLog  string
}

func TestGracefulCullAcknowledgedRequest(t *testing.T) {
	h := installGracefulHarness(t, "ack", true)
	stdout, stderr, rc := h.run(t, "--label", "peer", "--grace-timeout-ms", "250")
	if rc != 0 {
		t.Fatalf("cull rc=%d\nstdout:\n%s\nstderr:\n%s", rc, stdout, stderr)
	}
	h.assertClosed(t)
	if !strings.Contains(stdout, "release notice: acknowledged") {
		t.Fatalf("stdout=%q, want acknowledgement outcome", stdout)
	}
	sent := string(mustReadGraceFile(t, h.sendLog))
	for _, want := range []string{"--name worker-caller-seat", "@worker-peer-seat", "--intent request", "--thread", "release external resources", "then acknowledge"} {
		if !strings.Contains(sent, want) {
			t.Errorf("send argv=%q, missing %q", sent, want)
		}
	}
	events := string(mustReadGraceFile(t, h.eventsLog))
	for _, want := range []string{"--context deliver:worker-caller-seat", "--from caller-seat --thread", "--from peer-seat --intent ack"} {
		if !strings.Contains(events, want) {
			t.Errorf("events argv=%q, missing tagged-wire correlate %q", events, want)
		}
	}
	for _, forbidden := range []string{"browser", "chrome", "container", "tunnel"} {
		if strings.Contains(strings.ToLower(sent), forbidden) {
			t.Errorf("generic release notice names resource type %q: %s", forbidden, sent)
		}
	}
}

func TestGracefulCullTimeoutIsBoundedAndStillCloses(t *testing.T) {
	h := installGracefulHarness(t, "timeout", true)
	start := time.Now()
	stdout, stderr, rc := h.run(t, "--label", "peer", "--grace-timeout-ms", "250")
	elapsed := time.Since(start)
	if rc != 0 {
		t.Fatalf("cull rc=%d\nstdout:\n%s\nstderr:\n%s", rc, stdout, stderr)
	}
	h.assertClosed(t)
	if elapsed > time.Second {
		t.Fatalf("bounded cull took %s, want under 1s", elapsed)
	}
	if !strings.Contains(stdout, "release notice: grace window expired") {
		t.Fatalf("stdout=%q, want bounded-timeout outcome", stdout)
	}
}

func TestGracefulCullCallerRosterChildHoldingStdoutIsBounded(t *testing.T) {
	h := installGracefulHarness(t, "caller_fd_leak", true)
	start := time.Now()
	stdout, stderr, rc := h.run(t, "--label", "peer", "--grace-timeout-ms", "100")
	if rc != 0 {
		t.Fatalf("cull rc=%d\nstdout:\n%s\nstderr:\n%s", rc, stdout, stderr)
	}
	h.assertClosed(t)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("caller roster child-held stdout escaped bound: %s", elapsed)
	}
	if !strings.Contains(stdout, "caller bus identity unverified") {
		t.Fatalf("stdout=%q, want caller roster failure outcome", stdout)
	}
}

func TestGracefulCullStatusProbeChildHoldingStdoutIsBounded(t *testing.T) {
	h := installGracefulHarness(t, "status_fd_leak", true)
	start := time.Now()
	stdout, stderr, rc := h.run(t, "--label", "peer", "--grace-timeout-ms", "100")
	if rc != 0 {
		t.Fatalf("cull rc=%d\nstdout:\n%s\nstderr:\n%s", rc, stdout, stderr)
	}
	h.assertClosed(t)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("status probe child-held stdout escaped bound: %s", elapsed)
	}
}

func TestGracefulCullRosterTimeoutReportsHonestReason(t *testing.T) {
	h := installGracefulHarness(t, "roster_timeout", true)
	start := time.Now()
	stdout, stderr, rc := h.run(t, "--label", "peer", "--grace-timeout-ms", "40")
	if rc != 0 {
		t.Fatalf("cull rc=%d\nstdout:\n%s\nstderr:\n%s", rc, stdout, stderr)
	}
	h.assertClosed(t)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("roster timeout escaped grace bound: %s", elapsed)
	}
	if !strings.Contains(stdout, "target roster probe exceeded grace deadline") {
		t.Fatalf("stdout=%q, want honest roster-timeout reason", stdout)
	}
	if strings.Contains(stdout, "target is not joined") {
		t.Fatalf("stdout=%q, deadline must not be reported as a genuine roster miss", stdout)
	}
}

func TestGracefulCullObservedWorkingThenIdle(t *testing.T) {
	h := installGracefulHarness(t, "idle", true)
	stdout, stderr, rc := h.run(t, "--label", "peer", "--grace-timeout-ms", "700")
	if rc != 0 {
		t.Fatalf("cull rc=%d\nstdout:\n%s\nstderr:\n%s", rc, stdout, stderr)
	}
	h.assertClosed(t)
	if !strings.Contains(stdout, "release notice: observed working->idle") {
		t.Fatalf("stdout=%q, want post-notice status transition", stdout)
	}
}

func TestGracefulCullNowBypassesNotice(t *testing.T) {
	h := installGracefulHarness(t, "timeout", true)
	stdout, stderr, rc := h.run(t, "--label", "peer", "--now")
	if rc != 0 {
		t.Fatalf("cull rc=%d\nstdout:\n%s\nstderr:\n%s", rc, stdout, stderr)
	}
	h.assertClosed(t)
	if _, err := os.Stat(h.sendLog); !os.IsNotExist(err) {
		t.Fatalf("--now send log stat=%v, want no release notice", err)
	}
	if strings.Contains(stdout+stderr, "release notice:") {
		t.Fatalf("--now unexpectedly ran graceful protocol: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestGracefulCullUnboundTargetProceedsImmediately(t *testing.T) {
	h := installGracefulHarness(t, "timeout", false)
	start := time.Now()
	stdout, stderr, rc := h.run(t, "--label", "peer", "--grace-timeout-ms", "250")
	if rc != 0 {
		t.Fatalf("cull rc=%d\nstdout:\n%s\nstderr:\n%s", rc, stdout, stderr)
	}
	h.assertClosed(t)
	if time.Since(start) > 150*time.Millisecond {
		t.Fatalf("unbound target waited instead of proceeding immediately")
	}
	if _, err := os.Stat(h.sendLog); !os.IsNotExist(err) {
		t.Fatalf("unbound send log stat=%v, want no release notice", err)
	}
}

func TestGracefulCullQueuedNoticeCanAckLater(t *testing.T) {
	h := installGracefulHarness(t, "queued_ack", true)
	stdout, stderr, rc := h.run(t, "--label", "peer", "--grace-timeout-ms", "300")
	if rc != 0 {
		t.Fatalf("cull rc=%d\nstdout:\n%s\nstderr:\n%s", rc, stdout, stderr)
	}
	h.assertClosed(t)
	if !strings.Contains(stdout, "release notice: verify=queued") || !strings.Contains(stdout, "release notice: acknowledged") {
		t.Fatalf("stdout=%q, want queued delivery followed by acknowledgement", stdout)
	}
}

func TestGracefulCullAcceptsReplyToWithoutThread(t *testing.T) {
	h := installGracefulHarness(t, "reply_ack", true)
	stdout, stderr, rc := h.run(t, "--label", "peer", "--grace-timeout-ms", "250")
	if rc != 0 {
		t.Fatalf("cull rc=%d\nstdout:\n%s\nstderr:\n%s", rc, stdout, stderr)
	}
	h.assertClosed(t)
	if !strings.Contains(stdout, "release notice: acknowledged") {
		t.Fatalf("stdout=%q, want reply_to acknowledgement", stdout)
	}
}

func TestGracefulCullAlreadyIdleDoesNotSatisfyTransition(t *testing.T) {
	h := installGracefulHarness(t, "idle_static", true)
	start := time.Now()
	stdout, stderr, rc := h.run(t, "--label", "peer", "--grace-timeout-ms", "150")
	if rc != 0 {
		t.Fatalf("cull rc=%d\nstdout:\n%s\nstderr:\n%s", rc, stdout, stderr)
	}
	h.assertClosed(t)
	if !strings.Contains(stdout, "release notice: grace window expired") {
		t.Fatalf("stdout=%q, already-idle snapshot must not satisfy transition", stdout)
	}
	if time.Since(start) < 120*time.Millisecond {
		t.Fatalf("already-idle target bypassed the bounded wait")
	}
}

func TestGracefulCullRejectsUnrelatedAcknowledgements(t *testing.T) {
	for _, mode := range []string{"foreign_ack", "inform_ack", "pre_notice_ack", "wrong_thread_ack"} {
		t.Run(mode, func(t *testing.T) {
			h := installGracefulHarness(t, mode, true)
			stdout, stderr, rc := h.run(t, "--label", "peer", "--grace-timeout-ms", "100")
			if rc != 0 {
				t.Fatalf("cull rc=%d\nstdout:\n%s\nstderr:\n%s", rc, stdout, stderr)
			}
			h.assertClosed(t)
			if !strings.Contains(stdout, "release notice: grace window expired") {
				t.Fatalf("stdout=%q, %s must not satisfy acknowledgement", stdout, mode)
			}
		})
	}
}

func TestGracefulCullUnverifiedCallerSkipsNotice(t *testing.T) {
	h := installGracefulHarness(t, "timeout", true)
	t.Setenv("HCOM_SESSION_ID", "not-the-live-caller")
	t.Setenv("HERDR_PANE_ID", "pane-not-in-roster")
	stdout, stderr, rc := h.run(t, "--label", "peer", "--grace-timeout-ms", "100")
	if rc != 0 {
		t.Fatalf("cull rc=%d\nstdout:\n%s\nstderr:\n%s", rc, stdout, stderr)
	}
	h.assertClosed(t)
	if _, err := os.Stat(h.sendLog); !os.IsNotExist(err) {
		t.Fatalf("unverified caller send log stat=%v, want no release notice", err)
	}
	if !strings.Contains(stdout, "caller bus identity unverified") {
		t.Fatalf("stdout=%q, want verified-caller failure reason", stdout)
	}
}

func TestGracefulCullHelpDocumentsBoundsAndBypass(t *testing.T) {
	var stdout, stderr strings.Builder
	if rc := Run([]string{"--help"}, &stdout, &stderr); rc != 0 {
		t.Fatalf("help rc=%d stderr=%q", rc, stderr.String())
	}
	for _, want := range []string{"--now", "--grace-timeout-ms MS", "default 120000"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestGracefulCullRejectsInvalidGraceTimeouts(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "zero", args: []string{"--grace-timeout-ms", "0"}},
		{name: "negative", args: []string{"--grace-timeout-ms", "-1"}},
		{name: "non_integer", args: []string{"--grace-timeout-ms", "soon"}},
		{name: "missing", args: []string{"--grace-timeout-ms"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			_, rc := parseArgs(tc.args, &stdout, &stderr)
			if rc == 0 {
				t.Fatalf("parseArgs(%q) rc=0, want rejection", tc.args)
			}
			if !strings.Contains(stderr.String(), "requires a positive integer") {
				t.Fatalf("stderr=%q, want positive-integer explanation", stderr.String())
			}
		})
	}
}

func TestGracefulCullRevalidatesTerminalAfterNotice(t *testing.T) {
	h := installGracefulHarness(t, "reassigned", true)
	stdout, stderr, rc := h.run(t, "--label", "peer", "--grace-timeout-ms", "250")
	if rc != 0 {
		t.Fatalf("cull rc=%d\nstdout:\n%s\nstderr:\n%s", rc, stdout, stderr)
	}
	if _, err := os.Stat(h.closeLog); !os.IsNotExist(err) {
		t.Fatalf("reassigned pane close log stat=%v, want no pane close", err)
	}
	got := latestSession(t, filepath.Join(h.stateDir, "registry.jsonl"), "guid-peer")
	if got.State != v2.StateUnseated || got.CloseResult != "requested" || !strings.Contains(got.CloseReason, "operator-cull-post-grace") {
		t.Fatalf("latest=%+v, want post-grace operator-request fact without a death verdict", got)
	}
	if !strings.Contains(stderr, "no longer belongs to terminal") {
		t.Fatalf("stderr=%q, want post-grace identity explanation", stderr)
	}
}

func (h gracefulHarness) run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr strings.Builder
	rc := Run(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), rc
}

func (h gracefulHarness) assertClosed(t *testing.T) {
	t.Helper()
	if got := strings.TrimSpace(string(mustReadGraceFile(t, h.closeLog))); got != "pane-peer" {
		t.Fatalf("closed pane=%q, want pane-peer", got)
	}
	got := latestSession(t, filepath.Join(h.stateDir, "registry.jsonl"), "guid-peer")
	if got.State != v2.StateUnseated {
		t.Fatalf("latest state=%q, want unseated", got.State)
	}
}

func installGracefulHarness(t *testing.T, mode string, busBound bool) gracefulHarness {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	state := filepath.Join(root, "state")
	bus := filepath.Join(root, "bus")
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{home, state, bus, bin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h := gracefulHarness{
		stateDir:  state,
		busDir:    bus,
		sendLog:   filepath.Join(root, "send.log"),
		eventsLog: filepath.Join(root, "events.log"),
		closeLog:  filepath.Join(root, "close.log"),
	}
	seedGracefulTarget(t, filepath.Join(state, "registry.jsonl"), bus, busBound)

	herdr := `#!/usr/bin/env bash
set -euo pipefail
case "${1:-} ${2:-}" in
  "agent list")
    n=0
    [[ -f "$GRACE_AGENT_COUNT" ]] && n=$(<"$GRACE_AGENT_COUNT")
    n=$((n+1)); printf '%s' "$n" >"$GRACE_AGENT_COUNT"
    if [[ "$GRACE_MODE" == reassigned && "$n" -ge 2 ]]; then
      printf '{"result":{"agents":[{"terminal_id":"term-other","pane_id":"pane-peer","agent":"codex","agent_status":"idle"}]}}\n'
      exit 0
    fi
    status=working
    if [[ "$GRACE_MODE" == idle && "$n" -ge 3 ]]; then status=idle; fi
    if [[ "$GRACE_MODE" == idle_static ]]; then status=idle; fi
    printf '{"result":{"agents":[{"terminal_id":"term-peer","pane_id":"pane-peer","agent":"codex","agent_status":"%s"}]}}\n' "$status"
    if [[ "$GRACE_MODE" == status_fd_leak && "$n" -ge 2 ]]; then
      sleep 5 &
      printf '%s\n' "$!" >>"$GRACE_CHILD_PID_LOG"
    fi
    ;;
  "pane get")
    n=0
    [[ -f "$GRACE_PANE_GET_COUNT" ]] && n=$(<"$GRACE_PANE_GET_COUNT")
    n=$((n+1)); printf '%s' "$n" >"$GRACE_PANE_GET_COUNT"
    if [[ "$GRACE_MODE" == reassigned && "$n" -ge 2 ]]; then
      printf '{"result":{"pane":{"pane_id":"pane-peer","terminal_id":"term-other"}}}\n'
      exit 0
    fi
    printf '{"result":{"pane":{"pane_id":"pane-peer","terminal_id":"term-peer"}}}\n'
    ;;
  "pane list")
    printf '{"result":{"panes":[]}}\n'
    ;;
  "pane close")
    printf '%s\n' "${3:-}" >>"$GRACE_CLOSE_LOG"
    printf '{"result":{"type":"closed"}}\n'
    ;;
  *)
    printf 'fake herdr: unhandled %s\n' "$*" >&2
    exit 64
    ;;
esac
`
	hcom := `#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  list)
    if [[ "${2:-}" == --json ]]; then
      printf '[{"name":"worker-caller-seat","base_name":"caller-seat","status":"active","session_id":"caller-session","launch_context":{"pane_id":"pane-caller"}},{"name":"worker-peer-seat","base_name":"peer-seat","status":"active","session_id":"peer-session","launch_context":{"pane_id":"pane-peer"}}]\n'
      if [[ "$GRACE_MODE" == caller_fd_leak ]]; then
        sleep 5 &
        printf '%s\n' "$!" >>"$GRACE_CHILD_PID_LOG"
      fi
      exit 0
    fi
    if [[ "$GRACE_MODE" == roster_timeout ]]; then
      sleep 5
    fi
    [[ "${2:-}" == worker-peer-seat ]]
    ;;
  send)
    printf '%s\n' "$*" >"$GRACE_SEND_LOG"
    thread=""
    prev=""
    for arg in "$@"; do
      if [[ "$prev" == --thread ]]; then thread="$arg"; fi
      prev="$arg"
    done
    printf '%s' "$thread" >"$GRACE_THREAD_FILE"
    : >"$GRACE_SENT_FILE"
    ;;
  events)
    printf '%s\n' "$*" >>"$GRACE_EVENTS_LOG"
    [[ -f "$GRACE_SENT_FILE" ]] || exit 0
    n=0
    [[ -f "$GRACE_EVENT_COUNT" ]] && n=$(<"$GRACE_EVENT_COUNT")
    n=$((n+1)); printf '%s' "$n" >"$GRACE_EVENT_COUNT"
    thread=$(<"$GRACE_THREAD_FILE")
    if [[ "$*" == *"--context deliver:worker-caller-seat"* ]]; then
      if [[ "$GRACE_MODE" != queued_ack ]]; then
        printf '{"id":42,"type":"status","data":{"context":"deliver:worker-caller-seat"}}\n'
      fi
      exit 0
    fi
    printf '{"id":41,"type":"message","data":{"from":"caller-seat","text":"release","intent":"request","thread":"%s","mentions":["worker-peer-seat"],"delivered_to":["worker-peer-seat"]}}\n' "$thread"
    if [[ "$GRACE_MODE" == ack || "$GRACE_MODE" == reassigned || ( "$GRACE_MODE" == queued_ack && "$n" -ge 5 ) ]]; then
      printf '{"id":43,"type":"message","data":{"from":"peer-seat","text":"released","intent":"ack","thread":"%s","mentions":["worker-caller-seat"]}}\n' "$thread"
    fi
    if [[ "$GRACE_MODE" == foreign_ack ]]; then
      printf '{"id":43,"type":"message","data":{"from":"other-seat","text":"released","intent":"ack","thread":"%s","mentions":["worker-caller-seat"]}}\n' "$thread"
    fi
    if [[ "$GRACE_MODE" == inform_ack ]]; then
      printf '{"id":43,"type":"message","data":{"from":"peer-seat","text":"released","intent":"inform","thread":"%s","mentions":["worker-caller-seat"]}}\n' "$thread"
    fi
    if [[ "$GRACE_MODE" == pre_notice_ack ]]; then
      printf '{"id":40,"type":"message","data":{"from":"peer-seat","text":"released","intent":"ack","thread":"%s","mentions":["worker-caller-seat"]}}\n' "$thread"
    fi
    if [[ "$GRACE_MODE" == wrong_thread_ack ]]; then
      printf '{"id":43,"type":"message","data":{"from":"peer-seat","text":"released","intent":"ack","thread":"unrelated-thread","mentions":["worker-caller-seat"]}}\n'
    fi
    if [[ "$GRACE_MODE" == reply_ack ]]; then
      printf '{"id":44,"type":"message","data":{"from":"peer-seat","text":"released","intent":"ack","reply_to":"41","reply_to_local":41,"mentions":["worker-caller-seat"]}}\n'
    fi
    ;;
  kill)
    exit 0
    ;;
  *)
    printf 'fake hcom: unhandled %s\n' "$*" >&2
    exit 64
    ;;
esac
`
	writeGraceExecutable(t, filepath.Join(bin, "herdr"), herdr)
	writeGraceExecutable(t, filepath.Join(bin, "hcom"), hcom)
	writeGraceExecutable(t, filepath.Join(bin, "jq"), "#!/usr/bin/env bash\nexit 0\n")

	t.Setenv("HOME", home)
	t.Setenv("HCOM_DIR", bus)
	t.Setenv("HERDER_STATE_DIR", state)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "pane-caller")
	t.Setenv("HERDER_GUID", "guid-caller")
	t.Setenv("HERDER_LABEL", "caller-label")
	t.Setenv("HCOM_SESSION_ID", "caller-session")
	t.Setenv("HCOM_PROCESS_ID", "")
	t.Setenv("GRACE_MODE", mode)
	t.Setenv("GRACE_AGENT_COUNT", filepath.Join(root, "agent-count"))
	t.Setenv("GRACE_EVENT_COUNT", filepath.Join(root, "event-count"))
	t.Setenv("GRACE_PANE_GET_COUNT", filepath.Join(root, "pane-get-count"))
	t.Setenv("GRACE_CLOSE_LOG", h.closeLog)
	t.Setenv("GRACE_SEND_LOG", h.sendLog)
	t.Setenv("GRACE_EVENTS_LOG", h.eventsLog)
	t.Setenv("GRACE_THREAD_FILE", filepath.Join(root, "thread"))
	t.Setenv("GRACE_SENT_FILE", filepath.Join(root, "sent"))
	childPIDLog := filepath.Join(root, "child-pids")
	t.Setenv("GRACE_CHILD_PID_LOG", childPIDLog)
	t.Cleanup(func() { killGraceChildren(childPIDLog) })
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return h
}

func killGraceChildren(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, field := range strings.Fields(string(data)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			continue
		}
		if process, err := os.FindProcess(pid); err == nil {
			_ = process.Kill()
		}
	}
}

func seedGracefulTarget(t *testing.T, path, busDir string, busBound bool) {
	t.Helper()
	verified := true
	seat := &v2.Seat{Kind: "herdr", PaneID: "pane-peer", TerminalID: "term-peer"}
	if busBound {
		seat.HcomName = "worker-peer-seat"
		seat.Namespace = busDir
		seat.HcomVerified = &verified
	}
	outcomes, err := registry.UpdateLocked(path, func(registry.LockedUpdate) ([]v2.SessionRecord, error) {
		return []v2.SessionRecord{{
			Kind: v2.KindSession, GUID: "guid-peer", Event: "registered", RecordedAt: "2026-01-01T00:00:00Z", State: v2.StateSeated,
			Label: "peer", Role: "worker", Tool: "codex", Seat: seat,
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range outcomes {
		if err := outcome.Err(); err != nil {
			t.Fatal(err)
		}
	}
}

func writeGraceExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustReadGraceFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(fmt.Errorf("read %s: %w", path, err))
	}
	return data
}

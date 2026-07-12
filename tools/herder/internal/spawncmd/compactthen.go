package spawncmd

// herder compact-then — the detached continuation sender behind `herder compact
// --then` (TASK-034). It is an INTERNAL subcommand: `herder compact --then`
// forks it, Setsid-detached, once the /compact line's paste is verified, and it
// outlives the parent so it can deliver AFTER the caller's turn ends.
//
// Two live experiments fixed this shape (task-034 comments):
//   #1  A plain queued TUI line JUMPS the /compact queue: claude injects plain
//       messages into the RUNNING turn at the next tool boundary (landing
//       PRE-compact), while slash commands hold until turn end. So --then is a
//       BUS send, not a paste — and it must wait for the caller's turn to END
//       before sending, or the same mid-turn injection happens via the bus.
//   #2  Pane-id re-resolution misresolved to a stale registry row. So the
//       continuation targets the caller's OWN verified bus name, captured at
//       compact time from the caller's own registry row (never re-resolved from
//       a pane id here).
//
// This file therefore never touches the paste engine or a pane: it polls both
// `hcom list <name> --json` and hcom's event history for independent proof that
// the caller's turn ended, then delivers over the bus through send.DeliverBus —
// the same receipt-verified engine `herder send` uses. Claude-only: codex
// compaction semantics differ (stated in `herder compact --help`).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ai-config/tools/herder/internal/continuationstate"
	"ai-config/tools/herder/internal/herderpaths"
	"ai-config/tools/herder/internal/registry"
	"ai-config/tools/herder/internal/send"
	"ai-config/tools/herder/internal/shellquote"
)

// thenConfig is the fully-resolved plan for one detached continuation: WHERE
// (the caller's own verified bus coordinate), WHAT (the continuation text), and
// the timing bounds. Every field is captured by the parent at compact time so
// the child re-resolves nothing.
type thenConfig struct {
	BusName        string
	BusDir         string
	Message        string
	PollMS         int
	TimeoutMS      int
	RetryBackoffMS int // settling delay between transient send retries
	DeliverdMS     int // per-send bus receipt window
	RecordID       string
	LogPath        string
}

// busProbe is the seam runThenLoop is tested against:
//   - listStatus reports the caller's CURRENT session status
//     ("active"|"listening"|"blocked"|"" unknown).
//   - maxEventID snapshots the newest hcom event id for the caller at arm time,
//     the watermark that makes a later turn-end event provably POST-arm. It
//     returns ok=false when the snapshot could not be ESTABLISHED (hcom errored
//     or returned unparseable output) — distinct from a genuinely empty history
//     (ok=true, id 0). A bare 0 would conflate the two and let proof (b) accept
//     a PRE-arm listening record under an hcom transient (codex review P1
//     residual: fail-open); the ok flag gates proof (b) on a trusted watermark.
//   - turnEndedSince reports whether hcom's event history shows a working→idle
//     (status "listening") transition for the caller with an id STRICTLY newer
//     than the arm-time watermark — proof the turn ended after we armed, even
//     when we never sampled the live "active" ourselves (armed-late).
//   - deliver hands the continuation to the bus and returns the transport
//     verdict ("delivered"|"queued"|"not_joined"|"send_failed").
type busProbe interface {
	listStatus(busName, busDir string) string
	maxEventID(busName, busDir string) (int64, bool)
	turnEndedSince(busName, busDir string, watermark int64) bool
	deliver(busName, busDir, message string, timeoutMS int) string
}

// RunCompactThen is the detached child entry point (`herder compact-then …`).
// It is not in the visible command table — only `herder compact --then` spawns
// it. Its stdout/stderr are the diagnostics log the parent wired up; every exit
// path writes one summary line there so a timeout or a failed send is never a
// silent zombie.
func RunCompactThen(args []string, stdout, stderr io.Writer) int {
	cfg, code := parseThenArgs(args, stderr)
	if code != 0 {
		return code
	}
	return runThenLoop(&hcomProbe{}, cfg, stderr, time.Now, time.Sleep)
}

// runThenLoop waits for the caller's turn to end, then delivers the
// continuation. Turn-end must be PROVEN, never assumed from a fixed delay (codex
// review P1): a naked sampled "listening" never suffices, because a stale or
// lagged status read would let the continuation inject MID-TURN — experiment
// #1's failure, now over the bus. Two proofs, in preference order:
//
//	(a) we observed the live active→listening transition ourselves (saw the
//	    caller "active" on an earlier poll, "listening" now); or
//	(b) armed-late — the caller's turn ended before our first poll could catch
//	    "active", so we consult hcom's EVENT history: a status "listening" event
//	    for the caller with an id newer than the arm-time watermark proves the
//	    working→idle transition already happened AFTER we armed.
//
// The event-history proof is independent of the live status sample: a trusted
// post-arm listening event remains sufficient when live status is unavailable.
// If neither proof materializes within --then-timeout it FAILS CLOSED with a
// loud line and delivers nothing: a dropped continuation is visible and
// recoverable (the user just re-sends), a mid-turn injection silently corrupts
// the compaction it was meant to follow.
func runThenLoop(p busProbe, cfg thenConfig, log io.Writer, now func() time.Time, sleep func(time.Duration)) int {
	recordThenLifecycle(cfg, "armed", "", log, now())
	watermark, snapOK := establishWatermark(p, cfg, now, sleep)
	if snapOK {
		fmt.Fprintf(log, "herder compact-then: armed for @%s (bus %s) — waiting for a PROVEN turn end before delivering %d chars (poll %dms, timeout %dms, arm event #%d)\n",
			cfg.BusName, busDirLabel(cfg.BusDir), runeLen(cfg.Message), cfg.PollMS, cfg.TimeoutMS, watermark)
	} else {
		// No trusted arm-time watermark: proof (b) is DISABLED (an untrusted
		// watermark could accept a pre-arm listening record — fail-open). Only a
		// live observed active→listening transition (proof (a)) can deliver; if
		// that never comes, fail closed at timeout.
		fmt.Fprintf(log, "herder compact-then: armed for @%s (bus %s) — arm-time event snapshot NOT established (hcom unavailable); event-history proof DISABLED, only a live observed working→listening transition will deliver (poll %dms, timeout %dms)\n",
			cfg.BusName, busDirLabel(cfg.BusDir), cfg.PollMS, cfg.TimeoutMS)
	}

	start := now()
	deadline := start.Add(time.Duration(cfg.TimeoutMS) * time.Millisecond)
	sawActive := false

	for {
		status := p.listStatus(cfg.BusName, cfg.BusDir)
		if status == "active" {
			sawActive = true
		}
		turnEndEventFound := false
		if snapOK {
			// Event history is an independent proof path. In particular, a live
			// status probe may be unavailable even though hcom retained a strict
			// post-arm listening event for this identity.
			turnEndEventFound = p.turnEndedSince(cfg.BusName, cfg.BusDir, watermark)
		}
		proof := ""
		if status == "listening" && sawActive {
			proof = "observed working→listening transition"
		} else if turnEndEventFound {
			proof = "hcom events show a post-arm working→listening transition (armed after the turn began)"
		}
		if proof != "" {
			fmt.Fprintf(log, "herder compact-then: turn end PROVEN (%s) — delivering continuation to @%s\n", proof, cfg.BusName)
			return thenDeliver(p, cfg, log, now, sleep, deadline)
		}
		if !now().Before(deadline) {
			reason := fmt.Sprintf("turn end never proven within %dms (last status=%s)", cfg.TimeoutMS, statusLabel(status))
			fmt.Fprintf(log, "herder compact-then: TIMEOUT after %dms — turn end never PROVEN (last status=%q, saw_active=%t, snapshot_established=%t, turn_end_event_found=%t); FAILING CLOSED, continuation NOT delivered (a dropped continuation beats a mid-turn injection). Deliver it manually once the session is idle:\n  herder send %s -- %s\n",
				cfg.TimeoutMS, statusLabel(status), sawActive, snapOK, turnEndEventFound, cfg.BusName, shellPreview(cfg.Message))
			recordThenLifecycle(cfg, "failed", reason, log, now())
			return 1
		}
		sleep(time.Duration(cfg.PollMS) * time.Millisecond)
	}
}

// establishWatermark takes the arm-time event-id snapshot, retrying a bounded
// few times so a single hcom transient at arm does not permanently disable
// proof (b). ok=false only after every retry failed to establish a trusted
// watermark — the caller then restricts itself to proof (a).
func establishWatermark(p busProbe, cfg thenConfig, now func() time.Time, sleep func(time.Duration)) (int64, bool) {
	tries := envInt("HERDER_COMPACT_THEN_ARM_TRIES", 3)
	if tries < 1 {
		tries = 1
	}
	for i := 1; ; i++ {
		if wm, ok := p.maxEventID(cfg.BusName, cfg.BusDir); ok {
			return wm, true
		}
		if i >= tries {
			return 0, false
		}
		sleep(time.Duration(cfg.PollMS) * time.Millisecond)
	}
}

// thenDeliver hands the continuation to the bus once the turn end is proven.
// "delivered" and "queued" are BOTH success — queued means the target was busy
// and the bus will inject at its next turn; resending would double-deliver.
// A transient not_joined / send_failed (e.g. the instant compaction is still
// running) is retried with a settling backoff, spending the REMAINING
// --then-timeout budget rather than a fixed handful of no-delay attempts that
// burn out in milliseconds (codex review P2). Every attempt is logged so the
// diagnostics file tells the whole story.
func thenDeliver(p busProbe, cfg thenConfig, log io.Writer, now func() time.Time, sleep func(time.Duration), deadline time.Time) int {
	backoff := time.Duration(cfg.RetryBackoffMS) * time.Millisecond
	for attempt := 1; ; attempt++ {
		// Cap the receipt window to the budget actually left (viro P2 nit): a
		// 3s receipt wait must not overshoot a near-exhausted --then-timeout.
		perSend := capMS(cfg.DeliverdMS, deadline, now)
		verdict := p.deliver(cfg.BusName, cfg.BusDir, cfg.Message, perSend)
		switch verdict {
		case "delivered":
			fmt.Fprintf(log, "herder compact-then: delivered on attempt %d — continuation is in @%s's queue post-compaction.\n", attempt, cfg.BusName)
			recordThenLifecycle(cfg, "delivered", "", log, now())
			return 0
		case "queued":
			fmt.Fprintf(log, "herder compact-then: queued on attempt %d — @%s was busy; the bus will inject the continuation at its next turn. NOT resending.\n", attempt, cfg.BusName)
			recordThenLifecycle(cfg, "queued", "", log, now())
			return 0
		}
		if !now().Before(deadline) {
			reason := fmt.Sprintf("delivery budget exhausted after %d attempt(s) (last verdict: %s)", attempt, verdict)
			fmt.Fprintf(log, "herder compact-then: FAILED to deliver within the --then-timeout budget after %d attempt(s) (last: %s); continuation NOT delivered. Deliver it manually:\n  herder send %s -- %s\n",
				attempt, verdict, cfg.BusName, shellPreview(cfg.Message))
			recordThenLifecycle(cfg, "failed", reason, log, now())
			return 1
		}
		// Cap the backoff to the remaining budget so the last sleep lands exactly
		// on the deadline rather than past it.
		wait := backoff
		if rem := deadline.Sub(now()); rem < wait {
			wait = rem
		}
		fmt.Fprintf(log, "herder compact-then: send attempt %d -> %s; retrying in %dms (session may still be compacting)\n", attempt, verdict, int(wait/time.Millisecond))
		sleep(wait)
	}
}

// capMS clamps a millisecond budget to the time actually left before deadline,
// with a 1ms floor — never 0, since send.DeliverBus treats a 0 timeout as its
// 3s default (which would defeat the cap).
func capMS(want int, deadline time.Time, now func() time.Time) int {
	remMS := int(deadline.Sub(now()) / time.Millisecond)
	if remMS < want {
		want = remMS
	}
	if want < 1 {
		want = 1
	}
	return want
}

// hcomProbe is the production busProbe: `hcom list <name> --json` for status,
// send.DeliverBus for delivery (the receipt-verified bus engine `herder send`
// uses in-process, TASK-032).
type hcomProbe struct{}

// hcomRow is the subset of an `hcom list --json` entry this loop reads. Live
// hcom (0.7.23) reports a scoped query (`hcom list <name> --json`) as a SINGLE
// object whose "name" is the agent's BASE name (e.g. "reko" for bus name
// "smoke034-reko"), while an unscoped query is a JSON array whose entries carry
// both "base_name" and the full "name". listStatus handles both.
type hcomRow struct {
	Name     string `json:"name"`
	BaseName string `json:"base_name"`
	Status   string `json:"status"`
}

func (hcomProbe) listStatus(busName, busDir string) string {
	out, err := runHcomListRaw(busName, busDir)
	if err != nil {
		return ""
	}
	return normalizeStatus(pickStatus(out, busName))
}

func runHcomListRaw(busName, busDir string) ([]byte, error) {
	cmd := exec.Command("hcom", "list", busName, "--json")
	cmd.Env = os.Environ()
	if busDir != "" && busDir != "null" {
		cmd.Env = append(cmd.Env, "HCOM_DIR="+busDir)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// pickStatus extracts the queried agent's status from `hcom list <name> --json`
// output, accepting either a single object (the scoped-query shape) or an array
// (defensive — the unscoped shape). `hcom list <name>` already narrows to the
// one agent, so a single object / sole array element is taken directly; only a
// multi-row array is disambiguated by matching the full name or base name.
func pickStatus(raw []byte, busName string) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return ""
	}
	if trimmed[0] == '{' {
		var row hcomRow
		if json.Unmarshal(trimmed, &row) != nil {
			return ""
		}
		return row.Status
	}
	var rows []hcomRow
	if json.Unmarshal(trimmed, &rows) != nil || len(rows) == 0 {
		return ""
	}
	if len(rows) == 1 {
		return rows[0].Status
	}
	for _, r := range rows {
		if r.Name == busName || r.BaseName == busName {
			return r.Status
		}
	}
	return ""
}

// hcomAgentKnown checks whether a successful scoped list query actually
// returned an agent row. This lets an empty events response mean trusted empty
// history only when hcom can also identify the queried agent; an unknown agent
// that happens to produce empty event output leaves the snapshot unestablished.
func hcomAgentKnown(raw []byte, busName string) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	if trimmed[0] == '{' {
		var row hcomRow
		return json.Unmarshal(trimmed, &row) == nil && (row.Name != "" || row.BaseName != "")
	}
	var rows []hcomRow
	if json.Unmarshal(trimmed, &rows) != nil || len(rows) == 0 {
		return false
	}
	if len(rows) == 1 {
		return rows[0].Name != "" || rows[0].BaseName != ""
	}
	for _, row := range rows {
		if row.Name == busName || row.BaseName == busName {
			return true
		}
	}
	return false
}

func (hcomProbe) deliver(busName, busDir, message string, timeoutMS int) string {
	return send.DeliverBus(busName, busDir, message, timeoutMS)
}

// hcomEvent is the subset of an `hcom events` record turn-end detection reads:
// the monotone id and, for status events, the reported status.
type hcomEvent struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
	Data struct {
		Status string `json:"status"`
	} `json:"data"`
}

// maxEventID snapshots the caller's newest event id, distinguishing a trusted
// empty history for a known agent (ok=true, 0) from an UNESTABLISHED snapshot
// (ok=false): an hcom error, an unknown agent with empty output, or non-empty
// output that parses to zero events (garbage we must not trust). Only a trusted
// watermark may gate proof (b).
func (hcomProbe) maxEventID(busName, busDir string) (int64, bool) {
	out, err := runHcomEventsRaw(busName, busDir)
	if err != nil {
		return 0, false
	}
	if len(bytes.TrimSpace(out)) == 0 {
		listed, listErr := runHcomListRaw(busName, busDir)
		return 0, listErr == nil && hcomAgentKnown(listed, busName)
	}
	events := parseHcomEvents(out)
	if len(events) == 0 {
		return 0, false // output present but unparseable — distrust it
	}
	var max int64
	for _, e := range events {
		if e.ID > max {
			max = e.ID
		}
	}
	return max, true
}

func (hcomProbe) turnEndedSince(busName, busDir string, watermark int64) bool {
	out, err := runHcomEventsRaw(busName, busDir)
	if err != nil {
		return false
	}
	for _, e := range parseHcomEvents(out) {
		if e.ID > watermark && e.Type == "status" && e.Data.Status == "listening" {
			return true
		}
	}
	return false
}

// runHcomEventsRaw fetches recent events for busName, scoped to busDir, and
// returns the raw bytes + any exec error. Live hcom emits JSONL (one event per
// line, monotone id); a JSON array is also accepted by parseHcomEvents.
func runHcomEventsRaw(busName, busDir string) ([]byte, error) {
	cmd := exec.Command("hcom", "events", "--agent", busName, "--last", "50")
	cmd.Env = os.Environ()
	if busDir != "" && busDir != "null" {
		cmd.Env = append(cmd.Env, "HCOM_DIR="+busDir)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func parseHcomEvents(raw []byte) []hcomEvent {
	var events []hcomEvent
	if json.Unmarshal(bytes.TrimSpace(raw), &events) == nil {
		return events
	}
	events = nil
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var e hcomEvent
		if json.Unmarshal(line, &e) == nil {
			events = append(events, e)
		}
	}
	return events
}

// normalizeStatus maps hcom's status vocabulary onto the three states this loop
// reasons about, mirroring the sidecar's mapStatus: active=working (turn
// running), listening=idle (turn ended), blocked=modal. Anything else is
// unknown ("").
func normalizeStatus(status string) string {
	switch status {
	case "active", "listening", "blocked":
		return status
	default:
		return ""
	}
}

func statusLabel(status string) string {
	if status == "" {
		return "unknown"
	}
	return status
}

func busDirLabel(busDir string) string {
	if busDir == "" || busDir == "null" {
		return "default"
	}
	return busDir
}

func runeLen(s string) int {
	return len([]rune(s))
}

// shellPreview renders the continuation for the manual-remedy hint, truncated
// so a huge continuation does not flood the log line.
func shellPreview(message string) string {
	const max = 80
	single := strings.ReplaceAll(message, "\n", " ")
	runes := []rune(single)
	if len(runes) > max {
		return "'" + string(runes[:max]) + "…'"
	}
	return "'" + single + "'"
}

// recordThenLifecycle wraps the delivery loop without participating in its
// proof or transport decisions. Durable-state trouble is diagnostic only:
// delivery remains primary and proceeds exactly as it would without the store.
func recordThenLifecycle(cfg thenConfig, status, reason string, log io.Writer, at time.Time) {
	if cfg.RecordID == "" {
		return
	}
	rec := continuationstate.Record{
		ID:              cfg.RecordID,
		Status:          status,
		Target:          cfg.BusName,
		UpdatedAt:       at.UTC().Format(time.RFC3339),
		Reason:          reason,
		LogPath:         cfg.LogPath,
		RecoveryCommand: "herder send " + shellquote.Quote(cfg.BusName) + " -- " + shellquote.Quote(cfg.Message),
	}
	if err := continuationstate.Advance("", rec); err != nil {
		fmt.Fprintf(log, "herder compact-then: WARNING — durable continuation status %q could not be recorded: %v; delivery is continuing and diagnostics remain in %s\n", status, err, cfg.LogPath)
	}
}

// armCompactThen launches the detached continuation sender AFTER the parent has
// verified the /compact paste landed (AC#2 ordering floor). It never blocks the
// compact verdict: any launch trouble WARNS and returns (the compact itself
// already succeeded; the continuation is the best-effort side channel, TASK-017
// warn-never-block precedent). In HERDER_COMPACT_THEN_DRYRUN mode it describes
// the armed sender deterministically and forks nothing (hermetic goldens).
func armCompactThen(stderr io.Writer, shortGUID, busName, busDir, message string, timeoutMS int) {
	logDir := compactThenLogDir()
	if os.Getenv("HERDER_COMPACT_THEN_DRYRUN") == "1" {
		fmt.Fprintf(stderr, "herder compact: --then armed (dry-run) — after this turn ends, the continuation (%d chars) delivers to @%s on bus %s (timeout %dms); diagnostics under %s/\n",
			runeLen(message), busName, busDirLabel(busDir), timeoutMS, logDir)
		return
	}

	bin := herderBinPath()
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "herder compact: WARNING — --then NOT armed: cannot create diagnostics dir %s: %v. The /compact still fires; deliver the continuation manually: herder send %s -- %s\n", logDir, err, busName, shellPreview(message))
		return
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("compact-then-%s-%d-%d.log", firstNonEmpty(shortGUID, "self"), os.Getpid(), time.Now().UnixNano()))
	recordID := strings.TrimSuffix(filepath.Base(logPath), filepath.Ext(logPath))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(stderr, "herder compact: WARNING — --then NOT armed: cannot open diagnostics log %s: %v. Deliver the continuation manually: herder send %s -- %s\n", logPath, err, busName, shellPreview(message))
		return
	}
	defer logFile.Close()

	child := exec.Command(bin, "compact-then",
		"--name", busName,
		"--dir", busDir,
		"--message", message,
		"--timeout-ms", strconv.Itoa(timeoutMS),
		"--record-id", recordID,
		"--log-path", logPath,
	)
	child.Stdout = logFile
	child.Stderr = logFile
	child.Env = os.Environ()
	// A NEW session detaches the child from the caller's controlling terminal and
	// process group, so it survives the compact tool call's teardown and the
	// caller's own turn ending (the very moment it is waiting for).
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		fmt.Fprintf(stderr, "herder compact: WARNING — --then NOT armed: could not start detached sender: %v. Deliver the continuation manually: herder send %s -- %s\n", err, busName, shellPreview(message))
		return
	}
	pid := child.Process.Pid
	_ = child.Process.Release()
	fmt.Fprintf(stderr, "herder compact: --then armed — the continuation will deliver to @%s over the bus after this turn ends (detached sender pid %d; diagnostics: %s)\n", busName, pid, logPath)
}

// compactThenLogDir is where detached senders write their diagnostics: a
// compact-then/ subdir of the herder state dir (same root as the registry), so
// operators find them next to the rest of herder's state.
func compactThenLogDir() string {
	return filepath.Join(filepath.Dir(registry.DefaultPath()), "compact-then")
}

// herderBinPath resolves the herder binary to re-invoke for the detached child,
// pinned to this checkout the same way `herder launch` resolves it; "herder"
// (PATH lookup) is the last resort.
func herderBinPath() string {
	if paths, err := herderpaths.Resolve(); err == nil && paths.BinHerder != "" {
		return paths.BinHerder
	}
	return "herder"
}

func parseThenArgs(args []string, stderr io.Writer) (thenConfig, int) {
	cfg := thenConfig{
		PollMS:         envInt("HERDER_COMPACT_THEN_POLL_MS", 1000),
		TimeoutMS:      envInt("HERDER_COMPACT_THEN_TIMEOUT_MS", 15*60*1000),
		RetryBackoffMS: envInt("HERDER_COMPACT_THEN_RETRY_MS", 2000),
		DeliverdMS:     envInt("HERDER_COMPACT_THEN_DELIVER_MS", 3000),
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name":
			cfg.BusName, i = nextValue(args, i)
		case "--dir":
			cfg.BusDir, i = nextValue(args, i)
		case "--message":
			cfg.Message, i = nextValue(args, i)
		case "--timeout-ms":
			var v string
			v, i = nextValue(args, i)
			cfg.TimeoutMS = atoiOrDefault(v, cfg.TimeoutMS)
		case "--poll-ms":
			var v string
			v, i = nextValue(args, i)
			cfg.PollMS = atoiOrDefault(v, cfg.PollMS)
		case "--record-id":
			cfg.RecordID, i = nextValue(args, i)
		case "--log-path":
			cfg.LogPath, i = nextValue(args, i)
		default:
			fmt.Fprintf(stderr, "herder compact-then: unknown arg: %s\n", args[i])
			return cfg, 64
		}
	}
	if cfg.BusName == "" || cfg.Message == "" {
		fmt.Fprintf(stderr, "herder compact-then: --name and --message are required (internal subcommand; use `herder compact --then`)\n")
		return cfg, 64
	}
	if cfg.PollMS <= 0 {
		cfg.PollMS = 1
	}
	return cfg, 0
}

func nextValue(args []string, i int) (string, int) {
	if i+1 < len(args) {
		return args[i+1], i + 1
	}
	return "", i
}

func atoiOrDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

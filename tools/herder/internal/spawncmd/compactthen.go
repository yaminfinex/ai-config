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
// This file therefore never touches the paste engine or a pane: it polls
// `hcom list <name> --json` for the caller's session status (active→listening =
// turn ended) and then delivers over the bus through send.DeliverBus — the same
// receipt-verified engine `herder send` uses. Claude-only: codex compaction
// semantics differ (stated in `herder compact --help`).

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

	"ai-config/tools/herder/internal/herderpaths"
	"ai-config/tools/herder/internal/registry"
	"ai-config/tools/herder/internal/send"
)

// thenConfig is the fully-resolved plan for one detached continuation: WHERE
// (the caller's own verified bus coordinate), WHAT (the continuation text), and
// the timing bounds. Every field is captured by the parent at compact time so
// the child re-resolves nothing.
type thenConfig struct {
	BusName    string
	BusDir     string
	Message    string
	PollMS     int
	TimeoutMS  int
	GraceMS    int
	DeliverdMS int // per-send bus receipt window
}

// busProbe is the seam runThenLoop is tested against: listStatus reports the
// caller's current session status ("active"|"listening"|"blocked"|"" unknown),
// deliver hands the continuation to the bus and returns the transport verdict
// ("delivered"|"queued"|"not_joined"|"send_failed").
type busProbe interface {
	listStatus(busName, busDir string) string
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
// continuation. Turn-end is a status transition, never a fixed sleep: it fires
// on the first "listening" (idle) sample that follows either an observed
// "active" (a proven fresh turn boundary) or a grace window (covers a turn that
// ended faster than the first poll could catch "active"). It NEVER delivers
// while "active" — that is experiment #1's mid-turn injection, now via the bus.
func runThenLoop(p busProbe, cfg thenConfig, log io.Writer, now func() time.Time, sleep func(time.Duration)) int {
	fmt.Fprintf(log, "herder compact-then: armed for @%s (bus %s) — waiting for this turn to end before delivering %d chars (poll %dms, timeout %dms)\n",
		cfg.BusName, busDirLabel(cfg.BusDir), runeLen(cfg.Message), cfg.PollMS, cfg.TimeoutMS)

	start := now()
	deadline := start.Add(time.Duration(cfg.TimeoutMS) * time.Millisecond)
	grace := start.Add(time.Duration(cfg.GraceMS) * time.Millisecond)
	sawActive := false

	for {
		status := p.listStatus(cfg.BusName, cfg.BusDir)
		if status == "active" {
			sawActive = true
		}
		turnEnded := status == "listening" && (sawActive || !now().Before(grace))
		if turnEnded {
			fmt.Fprintf(log, "herder compact-then: turn ended (status=listening%s) — delivering continuation to @%s\n",
				map[bool]string{true: " after working", false: " via grace window"}[sawActive], cfg.BusName)
			return thenDeliver(p, cfg, log)
		}
		if !now().Before(deadline) {
			fmt.Fprintf(log, "herder compact-then: TIMEOUT after %dms waiting for turn end (last status=%q); continuation NOT delivered. Deliver it manually once the session is idle:\n  herder send %s -- %s\n",
				cfg.TimeoutMS, statusLabel(status), cfg.BusName, shellPreview(cfg.Message))
			return 1
		}
		sleep(time.Duration(cfg.PollMS) * time.Millisecond)
	}
}

// thenDeliver hands the continuation to the bus. queue-until-deliverable
// (hcom) makes post-turn-end timing forgiving, so a transient not_joined /
// send_failed (e.g. the instant compaction is still running) is retried a few
// times before giving up loudly. "delivered" and "queued" are BOTH success —
// queued means the target was busy and the bus will inject at its next turn;
// resending would double-deliver.
func thenDeliver(p busProbe, cfg thenConfig, log io.Writer) int {
	verdict := ""
	for attempt := 1; attempt <= 5; attempt++ {
		verdict = p.deliver(cfg.BusName, cfg.BusDir, cfg.Message, cfg.DeliverdMS)
		switch verdict {
		case "delivered":
			fmt.Fprintf(log, "herder compact-then: delivered — continuation is in @%s's queue post-compaction.\n", cfg.BusName)
			return 0
		case "queued":
			fmt.Fprintf(log, "herder compact-then: queued — @%s was busy; the bus will inject the continuation at its next turn. NOT resending.\n", cfg.BusName)
			return 0
		}
		fmt.Fprintf(log, "herder compact-then: send attempt %d/5 -> %s (retrying; the session may still be compacting)\n", attempt, verdict)
	}
	fmt.Fprintf(log, "herder compact-then: FAILED to deliver after 5 attempts (last: %s); continuation NOT delivered. Deliver it manually:\n  herder send %s -- %s\n",
		verdict, cfg.BusName, shellPreview(cfg.Message))
	return 1
}

// hcomProbe is the production busProbe: `hcom list <name> --json` for status,
// send.DeliverBus for delivery (the receipt-verified bus engine `herder send`
// uses in-process, TASK-032).
type hcomProbe struct{}

func (hcomProbe) listStatus(busName, busDir string) string {
	cmd := exec.Command("hcom", "list", busName, "--json")
	cmd.Env = os.Environ()
	if busDir != "" && busDir != "null" {
		cmd.Env = append(cmd.Env, "HCOM_DIR="+busDir)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	var rows []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if json.Unmarshal(out.Bytes(), &rows) != nil {
		return ""
	}
	// Prefer the row whose name matches exactly; fall back to the sole row when
	// `hcom list <name>` already narrowed to one.
	status := ""
	for _, r := range rows {
		if r.Name == busName {
			return normalizeStatus(r.Status)
		}
	}
	if len(rows) == 1 {
		status = rows[0].Status
	}
	return normalizeStatus(status)
}

func (hcomProbe) deliver(busName, busDir, message string, timeoutMS int) string {
	return send.DeliverBus(busName, busDir, message, timeoutMS)
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
	logPath := filepath.Join(logDir, fmt.Sprintf("compact-then-%s-%d.log", firstNonEmpty(shortGUID, "self"), os.Getpid()))
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
		PollMS:     envInt("HERDER_COMPACT_THEN_POLL_MS", 1000),
		TimeoutMS:  envInt("HERDER_COMPACT_THEN_TIMEOUT_MS", 15*60*1000),
		GraceMS:    envInt("HERDER_COMPACT_THEN_GRACE_MS", 4000),
		DeliverdMS: envInt("HERDER_COMPACT_THEN_DELIVER_MS", 3000),
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

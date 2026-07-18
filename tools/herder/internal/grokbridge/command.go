package grokbridge

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printHelp(stdout)
		return 0
	}
	switch args[0] {
	case "tap":
		return runTap(args[1:], stdout, stderr)
	case "mcp":
		return runMCP(args[1:], stdout, stderr)
	case "bridge":
		return runBridge(args[1:], stderr)
	case "stop-bridge":
		return runStopBridge(args[1:], stdout, stderr)
	case "retire-offline":
		return runRetireOffline(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "herder grok: unknown subcommand %q — use check, tap, mcp, bridge, stop-bridge, or retire-offline\n", args[0])
		return 2
	}
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, "herder grok — health and transport for first-class Grok seats.\n\nUsage:\n  herder grok check\n  herder grok tap --seat <guid>\n  herder grok mcp --seat <guid>\n  herder grok bridge --seat <guid> --hcom-bin <path> [--hcom-dir <path>] [--supervise]\n  herder grok stop-bridge --seat <guid> [--state-dir <herder-state>]\n  herder grok retire-offline --seat <guid> [--state-dir <herder-state>]\n")
}

func runStopBridge(args []string, stdout, stderr io.Writer) int {
	fs, seat, state := commonFS("herder grok stop-bridge", stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *seat == "" || fs.NArg() != 0 {
		fmt.Fprintln(stderr, "herder grok stop-bridge: --seat is required and no positional arguments are accepted")
		return 2
	}
	result, err := StopSeatSupervisors(*state, *seat, DefaultSupervisorStopTimeout)
	if err != nil {
		fmt.Fprintf(stderr, "herder grok stop-bridge: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "stopped seat=%s supervisors=%d term=%d kill=%d children=%d child-term=%d child-kill=%d\n", *seat, result.Matched, result.Termed, result.Killed, result.ChildrenMatched, result.ChildrenTermed, result.ChildrenKilled)
	return 0
}
func stateDefault() string {
	if v := os.Getenv("HERDER_STATE_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "herder")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "state", "herder")
}
func seatDefault() string {
	return processCapability("HERDER_GROK_SEAT")
}
func commonFS(name string, stderr io.Writer) (*flag.FlagSet, *string, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	seat := fs.String("seat", seatDefault(), "seat guid")
	state := fs.String("state-dir", stateDefault(), "herder state directory")
	return fs, seat, state
}
func runTap(args []string, stdout, stderr io.Writer) int {
	fs, seat, state := commonFS("herder grok tap", stderr)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *seat == "" {
		return 2
	}
	if err := Tap(SocketPath(*state, *seat), stdout); err != nil {
		_ = appendDiagnostic(filepath.Join(SeatDir(*state, *seat), "tap.log"), err)
		return 1
	}
	return 0
}

func runRetireOffline(args []string, stdout, stderr io.Writer) int {
	fs, seat, state := commonFS("herder grok retire-offline", stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *seat == "" || fs.NArg() != 0 {
		fmt.Fprintln(stderr, "herder grok retire-offline: --seat is required and no positional arguments are accepted")
		return 2
	}
	retired, err := RetireOffline(*state, *seat)
	if err != nil {
		fmt.Fprintf(stderr, "herder grok retire-offline: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "retired seat=%s undeliverable=%d\n", *seat, retired)
	return 0
}

func appendDiagnostic(path string, err error) error {
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o700); mkErr != nil {
		return mkErr
	}
	f, openErr := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if openErr != nil {
		return openErr
	}
	defer f.Close()
	_, writeErr := fmt.Fprintf(f, "%s %v\n", time.Now().UTC().Format(time.RFC3339), err)
	return writeErr
}
func runMCP(args []string, stdout, stderr io.Writer) int {
	fs, seat, state := commonFS("herder grok mcp", stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *seat == "" {
		fmt.Fprintln(stderr, "herder grok mcp: seat is required; pass --seat or set HERDER_GROK_SEAT")
		return 2
	}
	if err := ServeMCP(SocketPath(*state, *seat), os.Stdin, stdout); err != nil {
		fmt.Fprintf(stderr, "herder grok mcp: %v\n", err)
		return 1
	}
	return 0
}

func runBridge(args []string, stderr io.Writer) int {
	fs, seat, state := commonFS("herder grok bridge", stderr)
	hbin := fs.String("hcom-bin", os.Getenv("HERDER_REAL_HCOM"), "real hcom binary")
	hdir := fs.String("hcom-dir", os.Getenv("HCOM_DIR"), "hcom state directory")
	name := fs.String("name", "", "existing bus name")
	sessionID := fs.String("session-id", processCapability("HERDER_GROK_SESSION_ID"), "owning Grok session id used for request fencing")
	lifecycleMode := fs.String("lifecycle-mode", "", "internal managed lifecycle mode")
	forkedFromGUID := fs.String("forked-from-guid", "", "internal fork parent registry coordinate")
	completeSeat := fs.Bool("complete-seat", false, "internal bridge-owned canonical seat completion")
	supervise := fs.Bool("supervise", false, "restart the bridge with capped backoff")
	retireOnStop := fs.Bool("retire-on-stop", false, "retire the journal when a supervised manual bridge is stopped")
	child := fs.Bool("child", false, "internal supervised child")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *seat == "" || *hbin == "" {
		fmt.Fprintln(stderr, "herder grok bridge: --seat and --hcom-bin are required; provide the seat and the resolved real hcom binary")
		return 2
	}
	if *supervise && !*child {
		_ = lifecycleMode
		_ = forkedFromGUID
		_ = completeSeat
		return superviseBridge(args, *state, *seat, *retireOnStop)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	b, err := OpenBinder(BinderConfig{Seat: *seat, StateDir: *state, HcomBin: *hbin, HcomDir: *hdir, BusName: *name, SessionID: *sessionID})
	if err != nil {
		fmt.Fprintf(stderr, "herder grok bridge: %v\n", err)
		return 1
	}
	defer b.Close()
	if err = b.Serve(ctx); err != nil {
		if errors.Is(err, errSeatRetired) {
			return 24
		}
		fmt.Fprintf(stderr, "herder grok bridge: %v\n", err)
		return 1
	}
	return 0
}

func superviseBridge(args []string, state, seat string, retireOnStop bool) int {
	dir := SeatDir(state, seat)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 1
	}
	log, err := os.OpenFile(filepath.Join(dir, "bridge.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 1
	}
	defer log.Close()
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(log, err)
		return 1
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	return superviseBridgeContext(ctx, args, state, seat, retireOnStop, exe, log)
}

func superviseBridgeContext(ctx context.Context, args []string, state, seat string, retireOnStop bool, exe string, log io.Writer) int {
	lease, err := acquireSupervisorLease(state, seat)
	if err != nil {
		fmt.Fprintf(log, "%s supervisor refused: %v\n", time.Now().UTC().Format(time.RFC3339), err)
		return 23
	}
	defer lease.Close()
	completionCtx, cancelCompletion := context.WithCancel(ctx)
	defer cancelCompletion()
	if cfg, ok := managedCompletionFromArgs(args, state, seat); ok {
		go superviseManagedCompletion(completionCtx, cfg, func(format string, values ...any) {
			fmt.Fprintf(log, "%s "+format+"\n", append([]any{time.Now().UTC().Format(time.RFC3339)}, values...)...)
		})
	}
	childArgs := []string{"grok", "bridge"}
	for _, a := range args {
		if !strings.HasPrefix(a, "--supervise") && !strings.HasPrefix(a, "--child") {
			childArgs = append(childArgs, a)
		}
	}
	childArgs = append(childArgs, "--child")
	backoff := 100 * time.Millisecond
	for {
		cmd := exec.CommandContext(ctx, exe, childArgs...)
		cmd.Stdout = log
		cmd.Stderr = log
		err := cmd.Run()
		if ctx.Err() != nil {
			return retireStoppedBridge(state, seat, retireOnStop, log)
		}
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 24 {
			return 0
		}
		fmt.Fprintf(log, "%s bridge exited: %v\n", time.Now().UTC().Format(time.RFC3339), err)
		select {
		case <-ctx.Done():
			return retireStoppedBridge(state, seat, retireOnStop, log)
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 5*time.Second {
			backoff = 5 * time.Second
		}
	}
}

func managedCompletionFromArgs(args []string, state, seat string) (managedCompletionConfig, bool) {
	cfg := managedCompletionConfig{Seat: seat, StateDir: state, PaneID: os.Getenv("HERDR_PANE_ID")}
	enabled := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--complete-seat":
			enabled = true
		case "--hcom-dir":
			if i+1 < len(args) {
				cfg.HcomDir = args[i+1]
				i++
			}
		case "--session-id":
			if i+1 < len(args) {
				cfg.SessionID = args[i+1]
				i++
			}
		case "--lifecycle-mode":
			if i+1 < len(args) {
				cfg.LifecycleMode = args[i+1]
				i++
			}
		case "--forked-from-guid":
			if i+1 < len(args) {
				cfg.ForkedFromGUID = args[i+1]
				i++
			}
		}
	}
	return cfg, enabled
}

func retireStoppedBridge(state, seat string, retireOnStop bool, log io.Writer) int {
	if !retireOnStop {
		return 0
	}
	if _, err := RetireOffline(state, seat); err != nil {
		fmt.Fprintf(log, "%s retire-on-stop failed: %v\n", time.Now().UTC().Format(time.RFC3339), err)
		return 1
	}
	return 0
}

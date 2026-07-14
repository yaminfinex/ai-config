package launchcmd

import (
	"bytes"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ai-config/tools/herder/internal/grokbridge"
	"ai-config/tools/herder/internal/hcombin"
	"ai-config/tools/herder/internal/registry"
)

const (
	grokPreassignedEnv = "HERDER_GROK_PREASSIGNED"
	grokDefaultModel   = "grok-4.5"
	grokBootPrompt     = "Start your monitor per your rules, then list pending messages and proceed."
)

// --no-auto-update is intentionally absent from 0.2.93's help text, so its
// capability probe is the successful version invocation that already carries
// the flag. The visible contract flags remain pinned from --help.
var grokRequiredFlags = []string{"--no-subagents", "--session-id", "--rules"}

var grokMCPExecutable = os.Executable

func GrokAuthError() string {
	return "XAI_API_KEY is absent or empty in the fresh Grok pane environment; export it from a login-shell profile such as $HOME/.profile, then spawn a fresh pane"
}

// RunGrokCheck runs the same isolated binary version/capability gate used by
// launch, without activating a seat, starting a bridge, seeding GROK_HOME, or
// requiring credentials. It exists for ai-doctor; callers must not probe the
// vendor binary directly because even --version has mutated vendor state.
func RunGrokCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("herder grok check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateDir := fs.String("state-dir", "", "throwaway root for the isolated capability probe")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "herder grok check: unexpected arguments; pass only --state-dir <throwaway-root>")
		return 2
	}
	probeRoot := *stateDir
	if probeRoot == "" {
		var err error
		probeRoot, err = os.MkdirTemp("", "herder-grok-check-")
		if err != nil {
			fmt.Fprintf(stderr, "herder grok check: create throwaway probe root: %v\n", err)
			return 1
		}
		defer os.RemoveAll(probeRoot)
	}
	path, version, err := gateGrokBinary(probeRoot)
	if err != nil {
		fmt.Fprintf(stderr, "herder grok check: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "path=%s\nversion=%s\n", path, version)
	return 0
}

// NewGrokSessionID returns the UUIDv7 shape required by Grok's --session-id.
func NewGrokSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

type grokLaunchPlan struct {
	Binary    string
	Version   string
	Mode      string
	StateDir  string
	GrokHome  string
	Seat      string
	SessionID string
	ParentSID string
	HcomBin   string
	HcomDir   string
	Argv      []string
	Env       []string
}

type GrokLifecyclePlan struct {
	Mode      string
	SessionID string
	ParentSID string
}

func BuildGrokLifecyclePlan(mode, target, preassigned string) (GrokLifecyclePlan, error) {
	switch mode {
	case "launch":
		if preassigned == "" {
			return GrokLifecyclePlan{}, errors.New("fresh Grok launch is missing its preassigned session id; retry through herder spawn")
		}
		if !isUUIDv7(preassigned) {
			return GrokLifecyclePlan{}, errors.New("fresh Grok launch session id is not a UUIDv7; retry through herder spawn")
		}
		return GrokLifecyclePlan{Mode: mode, SessionID: preassigned}, nil
	case "resume":
		if target == "" {
			return GrokLifecyclePlan{}, errors.New("Grok resume is missing its recorded session id; choose a session with confirmed continuity")
		}
		if preassigned != "" && preassigned != target {
			return GrokLifecyclePlan{}, errors.New("Grok resume session evidence disagrees with the requested session; retry from the recorded session identity")
		}
		if !isUUIDv7(target) {
			return GrokLifecyclePlan{}, errors.New("recorded Grok resume session id is not a UUIDv7; choose a session with confirmed Grok identity")
		}
		return GrokLifecyclePlan{Mode: mode, SessionID: target}, nil
	case "fork":
		if target == "" {
			return GrokLifecyclePlan{}, errors.New("Grok fork is missing its parent session id; choose a parent with confirmed continuity")
		}
		if preassigned == "" {
			return GrokLifecyclePlan{}, errors.New("Grok fork is missing its fresh preassigned session id; retry through herder fork")
		}
		if preassigned == target {
			return GrokLifecyclePlan{}, errors.New("Grok fork session id matches its parent; retry so herder can mint a fresh identity")
		}
		if !isUUIDv7(target) || !isUUIDv7(preassigned) {
			return GrokLifecyclePlan{}, errors.New("Grok fork requires UUIDv7 parent and child session ids; choose a confirmed Grok parent and retry")
		}
		return GrokLifecyclePlan{Mode: mode, SessionID: preassigned, ParentSID: target}, nil
	default:
		return GrokLifecyclePlan{}, fmt.Errorf("unsupported Grok lifecycle mode %q; use launch, resume, or fork", mode)
	}
}

func runGrokLaunch(mode, target string, rest []string, stderr io.Writer) int {
	manual := false
	if mode == "launch" {
		var err error
		manual, err = ensureManualGrokIdentity()
		if err != nil {
			die(stderr, err.Error())
			return 1
		}
	}
	lifecycle, err := BuildGrokLifecyclePlan(mode, target, os.Getenv("HERDER_GROK_SESSION_ID"))
	if err != nil {
		recordGrokLaunchFailure(err)
		die(stderr, err.Error())
		return 1
	}
	plan, err := prepareGrokLifecycleLaunch(rest, lifecycle)
	if err != nil {
		recordGrokLaunchFailure(err)
		die(stderr, err.Error())
		return 1
	}
	clearGrokLaunchFailure(plan.StateDir, plan.Seat)
	busName, err := ensureGrokBridge(plan, manual)
	if err != nil {
		recordGrokLaunchFailure(err)
		die(stderr, err.Error())
		return 1
	}
	doctrine := grokDoctrine(busName, plan.Seat, plan.SessionID)
	plan.Argv = appendGrokLifecycleArgs(plan, doctrine)
	if manual {
		return runManualGrokProcess(plan, stderr)
	}
	if err := syscall.Exec(plan.Binary, plan.Argv, plan.Env); err != nil {
		recordGrokLaunchFailure(fmt.Errorf("exec Grok: %w", err))
		die(stderr, "exec Grok: "+err.Error())
		return 1
	}
	return 0
}

// ensureManualGrokIdentity distinguishes herder's two-phase managed spawn from
// a hand-run launch. Managed spawn explicitly preassigns identities before its
// registry row can exist. Every other ambient GUID must resolve to a Grok row;
// a foreign-family row is refused and an unregistered value is replaced rather
// than silently adopted. The bool reports whether this is a bounded manual
// guest whose foreground wrapper must retire its bridge on exit.
func ensureManualGrokIdentity() (bool, error) {
	if os.Getenv(grokPreassignedEnv) == "1" {
		if !validGrokSeat(os.Getenv("HERDER_GUID")) || !isUUIDv7(os.Getenv("HERDER_GROK_SESSION_ID")) {
			return false, errors.New("managed Grok launch has invalid preassigned identity; retry through herder spawn")
		}
		return false, nil
	}

	ambient := os.Getenv("HERDER_GUID")
	if ambient != "" {
		recs, err := registry.Load(registry.DefaultPath())
		if err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("corroborate ambient HERDER_GUID against the registry: %w", err)
		}
		if row := registry.Resolve(recs, ambient); row != nil {
			if row.Agent != "grok" {
				return false, fmt.Errorf("refused inherited HERDER_GUID %s: registry row belongs to tool %q, not grok; unset HERDER_GUID and HERDER_GROK_SESSION_ID before a manual Grok launch, or use `herder spawn --agent grok`", ambient, row.Agent)
			}
			if sid := os.Getenv("HCOM_SESSION_ID"); sid != "" {
				if sidRow := registry.ResolveByToolSessionID(recs, sid); sidRow != nil && !sameRegistryGUID(row, sidRow) {
					return false, fmt.Errorf("refused inherited Grok identity: HERDER_GUID %s and HCOM_SESSION_ID %s resolve to different registry rows; clear inherited identity or launch through `herder spawn --agent grok`", ambient, sid)
				}
			}
			return false, nil
		}
	}

	seat, err := registry.NewGUID()
	if err != nil {
		return false, fmt.Errorf("mint manual Grok seat identity: %w", err)
	}
	sessionID, err := NewGrokSessionID()
	if err != nil {
		return false, fmt.Errorf("mint manual Grok session identity: %w", err)
	}
	if err := os.Setenv("HERDER_GUID", seat); err != nil {
		return false, fmt.Errorf("set manual Grok seat identity: %w", err)
	}
	if err := os.Setenv("HERDER_GROK_SESSION_ID", sessionID); err != nil {
		return false, fmt.Errorf("set manual Grok session identity: %w", err)
	}
	return true, nil
}

func sameRegistryGUID(a, b *registry.Record) bool {
	return a != nil && b != nil && a.GUID != nil && b.GUID != nil && *a.GUID == *b.GUID
}

// runManualGrokProcess keeps the launch wrapper alive as the owner of a
// registry-less manual guest. Normal exit and catchable termination signals
// both converge through the same generation-fenced retire operation, so its
// detached supervisor cannot become an unlistable phantom bridge.
func runManualGrokProcess(plan grokLaunchPlan, stderr io.Writer) int {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	return runManualGrokProcessWithSignals(plan, stderr, signals, retireManualGrokSeat)
}

func runManualGrokProcessWithSignals(plan grokLaunchPlan, stderr io.Writer, signals <-chan os.Signal, retire func(grokLaunchPlan) error) int {
	cmd := exec.Command(plan.Binary)
	cmd.Args = append([]string(nil), plan.Argv...)
	cmd.Env = plan.Env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		_ = retire(plan)
		die(stderr, "exec Grok: "+err.Error())
		return 1
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case sig := <-signals:
		_ = cmd.Process.Signal(sig)
		waitErr = <-done
	}
	if err := retire(plan); err != nil {
		fmt.Fprintf(stderr, "herder launch: manual Grok bridge retirement failed: %v; retry `herder grok retire-offline --seat %s --state-dir %s` once the bridge is stopped\n", err, plan.Seat, plan.StateDir)
		return 1
	}
	if waitErr == nil {
		return 0
	}
	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	fmt.Fprintf(stderr, "herder launch: wait for Grok: %v\n", waitErr)
	return 1
}

func retireManualGrokSeat(plan grokLaunchPlan) error {
	client, err := grokbridge.DialClientForSession(grokbridge.SocketPath(plan.StateDir, plan.Seat), plan.SessionID)
	if err == nil {
		_, err = client.Call(grokbridge.Request{Op: "retire"})
	}
	if err == nil {
		return nil
	}
	if _, offlineErr := grokbridge.RetireOffline(plan.StateDir, plan.Seat); offlineErr == nil {
		return nil
	} else {
		return fmt.Errorf("generation-fenced retire failed: %v; offline recovery failed: %w", err, offlineErr)
	}
}

func appendGrokLifecycleArgs(plan grokLaunchPlan, doctrine string) []string {
	argv := append([]string(nil), plan.Argv...)
	switch plan.Mode {
	case "launch":
		argv = append(argv, "--session-id", plan.SessionID)
	case "resume":
		argv = append(argv, "--resume", plan.SessionID)
	case "fork":
		argv = append(argv, "--resume", plan.ParentSID, "--fork-session", "--session-id", plan.SessionID)
	}
	return append(argv, "--rules", doctrine, grokBootPrompt)
}

func prepareGrokLaunch(rest []string) (grokLaunchPlan, error) {
	sessionID := os.Getenv("HERDER_GROK_SESSION_ID")
	if sessionID == "" {
		var err error
		sessionID, err = NewGrokSessionID()
		if err != nil {
			return grokLaunchPlan{}, fmt.Errorf("preassign Grok session id: %w", err)
		}
	}
	lifecycle, err := BuildGrokLifecyclePlan("launch", "", sessionID)
	if err != nil {
		return grokLaunchPlan{}, err
	}
	return prepareGrokLifecycleLaunch(rest, lifecycle)
}

func prepareGrokLifecycleLaunch(rest []string, lifecycle GrokLifecyclePlan) (grokLaunchPlan, error) {
	if err := validatePreparedGrokArgs(rest, os.Getenv("HERDER_GROK_SAFE") == "1"); err != nil {
		return grokLaunchPlan{}, err
	}
	// Check auth inside the fresh pane, after its login shell has loaded the
	// owner's profile. The seat-scoped failure marker lets spawn surface this
	// precondition immediately instead of timing out. The value stays in the
	// pane environment and is never copied into argv, config, registry, or logs.
	if os.Getenv("XAI_API_KEY") == "" {
		return grokLaunchPlan{}, errors.New(GrokAuthError())
	}
	stateDir := grokStateDir()
	binary, version, err := gateGrokBinary(stateDir)
	if err != nil {
		return grokLaunchPlan{}, err
	}
	hcomBin := resolveRealHcom()
	if hcomBin == "" {
		return grokLaunchPlan{}, errors.New("real hcom 0.7.23 binary was not found; install the pinned hcom or set HERDER_REAL_HCOM to its executable path")
	}
	seat := os.Getenv("HERDER_GUID")
	if !validGrokSeat(seat) {
		return grokLaunchPlan{}, errors.New("HERDER_GUID is missing or unsafe; launch Grok through `herder spawn` so the seat identity is preassigned")
	}
	sessionID := lifecycle.SessionID
	if !isUUIDv7(sessionID) {
		return grokLaunchPlan{}, errors.New("preassigned Grok session id is not a UUIDv7; launch through `herder spawn` or provide a valid HERDER_GROK_SESSION_ID")
	}
	grokHome := filepath.Join(stateDir, "grok-home")
	if err := seedGrokHome(grokHome); err != nil {
		return grokLaunchPlan{}, err
	}
	matches, _ := filepath.Glob(filepath.Join(grokHome, "sessions", "*", sessionID))
	if lifecycle.Mode == "resume" {
		if len(matches) == 0 {
			return grokLaunchPlan{}, fmt.Errorf("recorded Grok session %s is absent from the controlled home; restore that session under GROK_HOME or fork a session that still exists", sessionID)
		}
	} else if len(matches) != 0 {
		if lifecycle.Mode == "launch" {
			return grokLaunchPlan{}, fmt.Errorf("preassigned Grok session %s already exists; retry the spawn so herder can mint a fresh session id", sessionID)
		}
		return grokLaunchPlan{}, fmt.Errorf("preassigned Grok session %s already exists; retry the fork so herder can mint a fresh session id", sessionID)
	}
	if lifecycle.Mode == "fork" {
		parents, _ := filepath.Glob(filepath.Join(grokHome, "sessions", "*", lifecycle.ParentSID))
		if len(parents) == 0 {
			return grokLaunchPlan{}, fmt.Errorf("parent Grok session %s is absent from the controlled home; restore that session before forking it", lifecycle.ParentSID)
		}
	}
	hcomDir := os.Getenv("HCOM_DIR")
	args := append([]string(nil), rest...)
	if !hasArg(args, "--no-auto-update") {
		args = append(args, "--no-auto-update")
	}
	if !hasArg(args, "--no-subagents") {
		args = append(args, "--no-subagents")
	}
	if os.Getenv("HERDER_GROK_SAFE") != "1" && !hasArg(args, "--always-approve") {
		args = append(args, "--always-approve")
	}
	if !hasArg(args, "--model") && !hasPrefixArg(args, "--model=") && !hasArg(args, "-m") {
		args = append(args, "--model", grokDefaultModel)
	}
	env := replaceLaunchEnv(os.Environ(), map[string]string{
		"GROK_HOME":                 grokHome,
		"GROK_CLAUDE_HOOKS_ENABLED": "0",
		"HERDER_STATE_DIR":          stateDir,
		"HERDER_GROK_SEAT":          seat,
		"HERDER_GROK_SESSION_ID":    sessionID,
		"HERDER_REAL_HCOM":          hcomBin,
	})
	if childHome := os.Getenv("HERDER_GROK_CHILD_HOME"); childHome != "" {
		env = replaceLaunchEnv(env, map[string]string{"HOME": childHome})
	}
	return grokLaunchPlan{Binary: binary, Version: version, Mode: lifecycle.Mode, StateDir: stateDir, GrokHome: grokHome, Seat: seat, SessionID: sessionID, ParentSID: lifecycle.ParentSID, HcomBin: hcomBin, HcomDir: hcomDir, Argv: append([]string{"grok"}, args...), Env: env}, nil
}

func ensureGrokBridge(plan grokLaunchPlan, manual bool) (string, error) {
	if plan.Mode == "resume" {
		client, err := grokbridge.DialClient(grokbridge.SocketPath(plan.StateDir, plan.Seat))
		if err == nil {
			if status, statusErr := client.Call(grokbridge.Request{Op: "status"}); statusErr == nil && status.Status != nil && status.Status.Bus == "bound" {
				if data, readErr := os.ReadFile(filepath.Join(plan.StateDir, "grok", plan.Seat, "bus-name")); readErr == nil && strings.TrimSpace(string(data)) != "" {
					return strings.TrimSpace(string(data)), nil
				}
			}
		}
	}
	return startGrokBridge(plan, manual)
}

// ReadGrokLaunchFailure returns a launch-side refusal for the preassigned seat.
// It never contains credential values; launch writes only cause+remedy text.
func ReadGrokLaunchFailure(stateDir, seat string) string {
	if !validGrokSeat(seat) {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "grok", seat, "launch-error"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func recordGrokLaunchFailure(err error) {
	if err == nil {
		return
	}
	stateDir, seat := grokStateDir(), os.Getenv("HERDER_GUID")
	if !validGrokSeat(seat) {
		return
	}
	dir := filepath.Join(stateDir, "grok", seat)
	if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
		return
	}
	tmp, createErr := os.CreateTemp(dir, ".launch-error-")
	if createErr != nil {
		return
	}
	name := tmp.Name()
	defer os.Remove(name)
	_ = tmp.Chmod(0o600)
	if _, writeErr := io.WriteString(tmp, err.Error()+"\n"); writeErr != nil {
		tmp.Close()
		return
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		tmp.Close()
		return
	}
	if closeErr := tmp.Close(); closeErr != nil {
		return
	}
	_ = os.Rename(name, filepath.Join(dir, "launch-error"))
}

func clearGrokLaunchFailure(stateDir, seat string) {
	if validGrokSeat(seat) {
		_ = os.Remove(filepath.Join(stateDir, "grok", seat, "launch-error"))
	}
}

func startGrokBridge(plan grokLaunchPlan, manual bool) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve herder executable for Grok bridge: %w", err)
	}
	args := []string{"grok", "bridge", "--seat", plan.Seat, "--state-dir", plan.StateDir, "--hcom-bin", plan.HcomBin, "--session-id", plan.SessionID, "--supervise"}
	if manual {
		args = append(args, "--retire-on-stop")
	}
	if plan.HcomDir != "" {
		args = append(args, "--hcom-dir", plan.HcomDir)
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = plan.Env
	cmd.SysProcAttr = grokBridgeProcessAttributes(manual)
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return "", err
	}
	defer devnull.Close()
	cmd.Stdout, cmd.Stderr = devnull, devnull
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start Grok bridge: %w", err)
	}
	namePath := filepath.Join(plan.StateDir, "grok", plan.Seat, "bus-name")
	socketPath := filepath.Join(plan.StateDir, "grok", plan.Seat, "bridge.sock")
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if data, readErr := os.ReadFile(namePath); readErr == nil && strings.TrimSpace(string(data)) != "" {
			if st, statErr := os.Stat(socketPath); statErr == nil && st.Mode()&os.ModeSocket != 0 {
				return strings.TrimSpace(string(data)), nil
			}
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	return "", errors.New("Grok bridge did not become ready within 8s; inspect the seat bridge log, correct the hcom/state configuration, and retry")
}

func grokDoctrine(busName, seat, sessionID string) string {
	return fmt.Sprintf(`HERDER GROK SEAT RULES
Your hcom bus name is %s; your seat is %s and owning session is %s.
Immediately start `+"`herder grok tap --seat %s`"+` as a persistent monitor and restart it whenever it is missing.
On HCOM id=... call the hcom MCP fetch_message tool for that id; process the full message; only then call ack_message. On HCOM_RECOVER call list_pending. Also list pending at session start and after any error or compaction.
Do not spawn or use subagents. Never print, persist, or place credential material in commands. Do not send speculative bus chatter.
For the hcom MCP send_message tool, only the first output line is the trusted send result; any later lines are unrelated pending-delivery noise and must be ignored.`, busName, seat, sessionID, seat)
}

func grokStateDir() string {
	if v := os.Getenv("HERDER_STATE_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "herder")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "herder")
}

func seedGrokHome(home string) error {
	herderBin, err := grokMCPExecutable()
	if err != nil {
		return fmt.Errorf("resolve herder executable for Grok MCP config: %w", err)
	}
	herderBin, err = filepath.Abs(herderBin)
	if err != nil {
		return fmt.Errorf("resolve absolute herder executable for Grok MCP config: %w", err)
	}
	return seedGrokHomeForExecutable(home, herderBin)
}

func seedGrokHomeForExecutable(home, herderBin string) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("create dedicated GROK_HOME: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(home, ".seed.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	config := filepath.Join(home, "config.toml")
	controlled := `[cli]
auto_update = false

[compat.claude]
hooks = false

[mcp_servers.hcom]
command = ` + strconv.Quote(herderBin) + `
args = ["grok", "mcp"]
enabled = true
`
	tmp, err := os.CreateTemp(home, ".config.toml-")
	if err != nil {
		return fmt.Errorf("create controlled Grok config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("set controlled Grok config permissions: %w", err)
	}
	if _, err = io.WriteString(tmp, controlled); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return fmt.Errorf("write controlled Grok config: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close controlled Grok config: %w", closeErr)
	}
	if err := os.Rename(tmpName, config); err != nil {
		return fmt.Errorf("install controlled Grok config: %w", err)
	}
	return nil
}

func gateGrokBinary(stateDir string) (string, string, error) {
	path := os.Getenv("HERDER_GROK_BIN")
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".grok", "downloads", "grok-linux-x86_64")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("resolve Grok binary: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = resolved
	}
	if st, statErr := os.Stat(abs); statErr != nil || st.IsDir() || st.Mode()&0o111 == 0 {
		return "", "", fmt.Errorf("Grok binary %s is not executable; set HERDER_GROK_BIN to the characterized 0.2.93 executable", abs)
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", "", fmt.Errorf("create Grok capability state root: %w", err)
	}
	probeHome, err := os.MkdirTemp(stateDir, ".grok-capability-")
	if err != nil {
		return "", "", fmt.Errorf("create Grok capability probe root: %w", err)
	}
	defer os.RemoveAll(probeHome)
	env := grokProbeEnv(os.Environ(), probeHome)
	versionOut, err := commandOutput(abs, env, "--no-auto-update", "--version")
	if err != nil {
		return "", "", fmt.Errorf("read Grok version from %s: %w", abs, err)
	}
	version := parseGrokVersion(versionOut)
	supported := supportedGrokVersions()
	if !containsString(supported, version) {
		return "", "", fmt.Errorf("Grok binary %s reports version %s, supported versions are %s; set HERDER_GROK_BIN to a characterized supported executable", abs, version, strings.Join(supported, ", "))
	}
	help, err := commandOutput(abs, env, "--no-auto-update", "--help")
	if err != nil {
		return "", "", fmt.Errorf("read Grok capabilities from %s: %w", abs, err)
	}
	for _, flag := range grokRequiredFlags {
		if !strings.Contains(help, flag) {
			return "", "", fmt.Errorf("Grok binary %s version %s lacks required capability %s; use a characterized supported executable", abs, version, flag)
		}
	}
	return abs, version, nil
}

func supportedGrokVersions() []string {
	raw := os.Getenv("HERDER_GROK_SUPPORTED_VERSIONS")
	if raw == "" {
		return []string{"0.2.93"}
	}
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return []string{"0.2.93"}
	}
	return out
}

func commandOutput(path string, env []string, args ...string) (string, error) {
	cmd := exec.Command(path, args...)
	cmd.Env = env
	var stdout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, io.Discard
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return stdout.String(), fmt.Errorf("probe %s exited with code %d (child stderr suppressed); run it by hand to see why, then set HERDER_GROK_BIN to a characterized supported executable", path, exitErr.ExitCode())
		}
		return stdout.String(), fmt.Errorf("run probe %s: %w; run it by hand to see why, then set HERDER_GROK_BIN to a characterized supported executable", path, err)
	}
	return stdout.String(), nil
}

var grokVersionRE = regexp.MustCompile(`(?m)\bgrok\s+([0-9]+\.[0-9]+\.[0-9]+)\b`)

func parseGrokVersion(out string) string {
	match := grokVersionRE.FindStringSubmatch(out)
	if len(match) == 2 {
		return match[1]
	}
	return "unknown"
}

var uuidV7RE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-7[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func isUUIDv7(value string) bool { return uuidV7RE.MatchString(value) }

func resolveRealHcom() string {
	for _, key := range []string{"HERDER_REAL_HCOM", "HERDER_HOOK_HCOM"} {
		if path, _ := hcomCandidate(os.Getenv(key)); path != "" {
			return path
		}
	}
	var argv0Fallback string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		path, argv0Dispatch := hcomCandidate(filepath.Join(dir, "hcom"))
		if path == "" {
			continue
		}
		if argv0Dispatch {
			if argv0Fallback == "" {
				argv0Fallback = path
			}
			continue
		}
		return path
	}
	return argv0Fallback
}

// hcomCandidate resolves ordinary hcom symlinks but does not turn an argv0-
// dispatch shim into its dispatcher. The bool lets PATH discovery prefer a
// later real binary while retaining the invoked shim as a safe last resort.
func hcomCandidate(path string) (string, bool) {
	if !executableFile(path) || herderShim(path) {
		return "", false
	}
	resolved, argv0Dispatch, err := hcombin.ResolveExecPath(path)
	if err != nil {
		return "", false
	}
	return resolved, argv0Dispatch
}

func executableFile(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

func herderShim(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	return bytes.Contains(buf[:n], []byte("herder-path-shim"))
}

func replaceLaunchEnv(env []string, values map[string]string) []string {
	out := make([]string, 0, len(env)+len(values))
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		if _, replace := values[key]; !replace {
			out = append(out, item)
		}
	}
	for key, value := range values {
		out = append(out, key+"="+value)
	}
	return out
}

func grokProbeEnv(env []string, probeHome string) []string {
	out := make([]string, 0, len(env))
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		switch {
		case key == "PATH", key == "HOME", key == "GROK_HOME", key == "LANG", key == "TERM", key == "TMPDIR", strings.HasPrefix(key, "LC_"):
			out = append(out, item)
		}
	}
	return replaceLaunchEnv(out, map[string]string{
		"HOME":      probeHome,
		"GROK_HOME": filepath.Join(probeHome, "grok-home"),
	})
}

func validGrokSeat(seat string) bool {
	return seat != "" && !strings.ContainsAny(seat, "/\\\x00")
}

// ValidateGrokExtraArgs rejects passthrough that collides with the launch
// contract before spawn creates a pane.
func ValidateGrokExtraArgs(args []string, firstClassModel bool) error {
	return validateGrokArgs(args, firstClassModel, false)
}

func validatePreparedGrokArgs(args []string, safe bool) error {
	return validateGrokArgs(args, false, !safe)
}

func validateGrokArgs(args []string, firstClassModel, allowMappedPermission bool) error {
	for _, arg := range args {
		name := arg
		if before, _, ok := strings.Cut(arg, "="); ok {
			name = before
		}
		switch name {
		case "--session-id", "-s":
			return fmt.Errorf("Grok passthrough %s conflicts with the preassigned session identity; remove it and let herder mint the session id", name)
		case "--resume", "-r", "--continue", "-c", "--fork-session":
			return fmt.Errorf("Grok passthrough %s conflicts with the fresh-seat launch contract; remove it and use the matching herder lifecycle command after launch", name)
		case "--rules":
			return errors.New("Grok passthrough --rules conflicts with the seat doctrine; remove it so herder can install the monitor and receipt rules")
		case "--permission-mode", "--bypassPermissions":
			return fmt.Errorf("Grok passthrough %s conflicts with herder's permission mapping; remove it, then use normal launch for --always-approve or pass --safe for ask mode", name)
		case "--always-approve":
			if !allowMappedPermission {
				return errors.New("Grok passthrough --always-approve conflicts with herder's permission mapping; remove it, then use normal launch for --always-approve or pass --safe for ask mode")
			}
		case "--no-auto-update", "--auto-update", "--disable-auto-update":
			return fmt.Errorf("Grok passthrough %s conflicts with mandatory update suppression; remove it because herder supplies both update controls", name)
		case "--agents", "--agent", "--subagents", "--no-subagents", "--no-no-subagents":
			return fmt.Errorf("Grok passthrough %s conflicts with the enforced subagent boundary; remove it because first-class seats always disable subagents", name)
		case "--model", "-m":
			if firstClassModel {
				return errors.New("--model conflicts with a model pin in --extra-arg; use the first-class --model flag or the passthrough form, not both")
			}
		}
		upper := strings.ToUpper(arg)
		if strings.HasPrefix(upper, "HOME=") || strings.HasPrefix(upper, "GROK_HOME=") || name == "--home" || name == "--grok-home" {
			return fmt.Errorf("Grok passthrough %s attempts to re-point an owned home; remove it because herder pins GROK_HOME to the isolated seat state", name)
		}
		if strings.Contains(strings.ToLower(name), "subagent") {
			return fmt.Errorf("Grok passthrough %s could change the enforced subagent boundary; remove it because first-class seats always disable subagents", name)
		}
		if strings.Contains(strings.ToLower(name), "auto-update") {
			return fmt.Errorf("Grok passthrough %s could change mandatory update suppression; remove it because herder supplies both update controls", name)
		}
	}
	return nil
}

func hasArg(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
}

func hasPrefixArg(args []string, prefix string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

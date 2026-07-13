package launchcmd

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	grokActivationEnv = "HERDER_GROK_ACTIVATED"
	grokDefaultModel  = "grok-4.5"
	grokBootPrompt    = "Start your monitor per your rules, then list pending messages and proceed."
)

// --no-auto-update is intentionally absent from 0.2.93's help text, so its
// capability probe is the successful version invocation that already carries
// the flag. The visible contract flags remain pinned from --help.
var grokRequiredFlags = []string{"--no-subagents", "--session-id", "--rules"}

// GrokActivated is deliberately narrow: U2 ships the launch contract for
// isolated validation, while ordinary Grok spawns remain blocked until the
// lifecycle and observer contracts land.
func GrokActivated() bool { return os.Getenv(grokActivationEnv) == "1" }

func GrokActivationError() string {
	return "Grok family is not activated; set HERDER_GROK_ACTIVATED=1 only for an isolated experimental launch after providing throwaway HOME, HCOM_DIR, and HERDER_STATE_DIR"
}

func GrokAuthError() string {
	return "XAI_API_KEY not present in the herder spawn environment; export it in the environment that launches the herdr server, then respawn"
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
	StateDir  string
	GrokHome  string
	Seat      string
	SessionID string
	HcomBin   string
	HcomDir   string
	Argv      []string
	Env       []string
}

func runGrokLaunch(_ string, rest []string, stderr io.Writer) int {
	plan, err := prepareGrokLaunch(rest)
	if err != nil {
		recordGrokLaunchFailure(err)
		die(stderr, err.Error())
		return 1
	}
	clearGrokLaunchFailure(plan.StateDir, plan.Seat)
	busName, err := startGrokBridge(plan)
	if err != nil {
		recordGrokLaunchFailure(err)
		die(stderr, err.Error())
		return 1
	}
	doctrine := grokDoctrine(busName, plan.Seat, plan.SessionID)
	plan.Argv = append(plan.Argv, "--session-id", plan.SessionID, "--rules", doctrine, grokBootPrompt)
	if err := syscall.Exec(plan.Binary, plan.Argv, plan.Env); err != nil {
		recordGrokLaunchFailure(fmt.Errorf("exec Grok: %w", err))
		die(stderr, "exec Grok: "+err.Error())
		return 1
	}
	return 0
}

func prepareGrokLaunch(rest []string) (grokLaunchPlan, error) {
	if !GrokActivated() {
		return grokLaunchPlan{}, errors.New(GrokActivationError())
	}
	if err := validatePreparedGrokArgs(rest, os.Getenv("HERDER_GROK_SAFE") == "1"); err != nil {
		return grokLaunchPlan{}, err
	}
	// Herdr creates panes from its long-lived server, so process-only auth must
	// be present in the environment that launched that server; a CLI caller's
	// later environment cannot be handed across via argv or files without
	// violating the credential contract. The seat-scoped failure marker below
	// lets spawn surface this precondition immediately instead of timing out.
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
	sessionID := os.Getenv("HERDER_GROK_SESSION_ID")
	if sessionID == "" {
		sessionID, err = NewGrokSessionID()
		if err != nil {
			return grokLaunchPlan{}, fmt.Errorf("preassign Grok session id: %w", err)
		}
	}
	if !isUUIDv7(sessionID) {
		return grokLaunchPlan{}, errors.New("preassigned Grok session id is not a UUIDv7; launch through `herder spawn` or provide a valid HERDER_GROK_SESSION_ID")
	}
	grokHome := filepath.Join(stateDir, "grok-home")
	if err := seedGrokHome(grokHome); err != nil {
		return grokLaunchPlan{}, err
	}
	if matches, _ := filepath.Glob(filepath.Join(grokHome, "sessions", "*", sessionID)); len(matches) != 0 {
		return grokLaunchPlan{}, fmt.Errorf("preassigned Grok session %s already exists; retry the spawn so herder can mint a fresh session id", sessionID)
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
		"GROK_HOME":              grokHome,
		"HERDER_STATE_DIR":       stateDir,
		"HERDER_GROK_SEAT":       seat,
		"HERDER_GROK_SESSION_ID": sessionID,
		"HERDER_REAL_HCOM":       hcomBin,
	})
	if childHome := os.Getenv("HERDER_GROK_CHILD_HOME"); childHome != "" {
		env = replaceLaunchEnv(env, map[string]string{"HOME": childHome})
	}
	return grokLaunchPlan{Binary: binary, Version: version, StateDir: stateDir, GrokHome: grokHome, Seat: seat, SessionID: sessionID, HcomBin: hcomBin, HcomDir: hcomDir, Argv: append([]string{"grok"}, args...), Env: env}, nil
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
	if !GrokActivated() || err == nil {
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

func startGrokBridge(plan grokLaunchPlan) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve herder executable for Grok bridge: %w", err)
	}
	args := []string{"grok", "bridge", "--seat", plan.Seat, "--state-dir", plan.StateDir, "--hcom-bin", plan.HcomBin, "--session-id", plan.SessionID, "--supervise"}
	if plan.HcomDir != "" {
		args = append(args, "--hcom-dir", plan.HcomDir)
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = plan.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
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
	herderBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve herder executable for Grok MCP config: %w", err)
	}
	herderBin, err = filepath.Abs(herderBin)
	if err != nil {
		return fmt.Errorf("resolve absolute herder executable for Grok MCP config: %w", err)
	}
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
	if data, readErr := os.ReadFile(config); readErr == nil {
		if string(data) != controlled {
			return fmt.Errorf("dedicated GROK_HOME config %s is not the herder-controlled launch config; point HERDER_STATE_DIR at a fresh isolated state root and retry", config)
		}
		return nil
	} else if !os.IsNotExist(readErr) {
		return readErr
	}
	f, err := os.OpenFile(config, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = io.WriteString(f, controlled); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
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
	env := replaceLaunchEnv(withoutCredentialEnv(os.Environ()), map[string]string{"HOME": probeHome, "GROK_HOME": filepath.Join(probeHome, "grok-home")})
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
			return stdout.String(), fmt.Errorf("probe %s exited with code %d (child stderr suppressed)", path, exitErr.ExitCode())
		}
		return stdout.String(), fmt.Errorf("run probe %s: %w", path, err)
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
		if path := os.Getenv(key); executableFile(path) && !herderShim(path) {
			return canonicalFile(path)
		}
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		path := filepath.Join(dir, "hcom")
		if executableFile(path) && !herderShim(path) {
			return canonicalFile(path)
		}
	}
	return ""
}

func executableFile(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

func canonicalFile(path string) string {
	abs, _ := filepath.Abs(path)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
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

func withoutCredentialEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		upper := strings.ToUpper(key)
		if strings.HasSuffix(upper, "_API_KEY") ||
			strings.HasSuffix(upper, "_ACCESS_KEY") ||
			strings.HasSuffix(upper, "_PRIVATE_KEY") ||
			strings.HasSuffix(upper, "_TOKEN") ||
			strings.HasSuffix(upper, "_SECRET") ||
			strings.HasSuffix(upper, "_PASSWORD") ||
			strings.Contains(upper, "CREDENTIAL") {
			continue
		}
		out = append(out, item)
	}
	return out
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
		case "--resume", "-r", "--fork-session":
			return fmt.Errorf("Grok passthrough %s conflicts with the fresh-seat launch contract; remove it and use the lifecycle command after that contract is activated", name)
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

package launchcmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"ai-config/tools/herder/internal/hookcmd"
	"ai-config/tools/herder/internal/registry"
)

// IsHcomCapable is the single source of truth for agents that herder spawn
// routes through hcom. Adding a tool here must also add its config pin in
// PinConfigDir when hcom local mode would otherwise redirect it.
func IsHcomCapable(agent string) bool {
	switch agent {
	case "claude", "codex", "gemini":
		return true
	default:
		return false
	}
}

// PinConfigDir preserves each supported tool's real config dir when HCOM_DIR
// points at an isolated bus. This is the Go home for the retired hcom-tools.sh
// pin table.
func PinConfigDir(tool string) {
	home := os.Getenv("HOME")
	hcomDir := os.Getenv("HCOM_DIR")
	if hcomDir == "" || hcomDir == filepath.Join(home, ".hcom") {
		return
	}
	switch tool {
	case "claude":
		dir := filepath.Join(home, ".claude")
		if setEnvDefault("CLAUDE_CONFIG_DIR", dir) {
			seedClaudeConfig(home, dir)
		}
	case "codex":
		setEnvDefault("CODEX_HOME", filepath.Join(home, ".codex"))
	case "gemini":
		setEnvDefault("GEMINI_CLI_HOME", filepath.Join(home, ".gemini"))
	}
}

// seedClaudeConfig copies ~/.claude.json into the pinned config dir when the
// pin re-roots claude's top-level config to a path that does not exist yet.
// CLAUDE_CONFIG_DIR moves that file from ~/.claude.json to $dir/.claude.json,
// so without a seed the first pinned launch is a fresh install to claude:
// one-time onboarding in the pane, plus alarming "configuration file not
// found / backup file exists" stderr in headless launch logs (claude itself
// is the emitter — TASK-011). An existing target is never touched, and any
// failure degrades silently to that fresh-state behavior.
func seedClaudeConfig(home, dir string) {
	dst := filepath.Join(dir, ".claude.json")
	if _, err := os.Stat(dst); err == nil {
		return
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	_ = os.WriteFile(dst, data, 0o600)
}

func setEnvDefault(key, value string) bool {
	if os.Getenv(key) == "" {
		_ = os.Setenv(key, value)
		return true
	}
	return false
}

// Run executes the herder launch contract: parse hcom-owned flags, pin real
// config dirs when needed, optionally fork the status sidecar, then exec hcom.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		printHelp(stdout)
		return 0
	}
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		if len(args) == 0 || args[0] != "--resume" && args[0] != "--fork" {
			die(stderr, "usage: herder launch <tool> [--tag TAG] [tool-args...]")
			return 1
		}
	}
	mode := "launch"
	tool := args[0]
	target := ""
	parentSessionID := ""
	args = args[1:]
	if tool == "--resume" || tool == "--fork" {
		if len(args) < 2 || args[0] == "" || args[1] == "" {
			die(stderr, "usage: herder launch --resume <tool> <target> [--tag TAG] [tool-args...]")
			return 1
		}
		if tool == "--fork" {
			mode = "fork"
		} else {
			mode = "resume"
		}
		tool = args[0]
		target = args[1]
		if mode == "fork" {
			parentSessionID = target
		}
		args = args[2:]
	}

	tag := ""
	var rest []string
	for i := 0; i < len(args); {
		arg := args[i]
		switch {
		case arg == "--tag":
			if i+1 >= len(args) {
				die(stderr, "--tag needs a value")
				return 1
			}
			tag = args[i+1]
			i += 2
		case arg == "--parent-session":
			if i+1 >= len(args) {
				die(stderr, "--parent-session needs a value")
				return 1
			}
			parentSessionID = args[i+1]
			i += 2
		case len(arg) >= len("--tag=") && arg[:len("--tag=")] == "--tag=":
			tag = arg[len("--tag="):]
			i++
		case len(arg) >= len("--parent-session=") && arg[:len("--parent-session=")] == "--parent-session=":
			parentSessionID = arg[len("--parent-session="):]
			i++
		case arg == "--":
			rest = append(rest, args[i+1:]...)
			i = len(args)
		default:
			rest = append(rest, arg)
			i++
		}
	}

	// One-shot print runs skip the bus by design (TASK-010, option c): hcom
	// hard-codes -p/--print as its background switch — stdin nulled, stdout to
	// hcom logs, Stop hook polling the bus for up to a day — so a hand-run
	// one-shot routed through hcom never returns its answer. Exec the
	// PATH-resolved tool instead; that usually re-enters the shim, whose
	// HCOM_LAUNCH_INFLIGHT recursion guard resolves the real binary. This sits
	// before the hcom check: a print one-shot works without hcom installed.
	if mode == "launch" && isPrintInvocation(tool, rest) {
		return execPrintBypass(tool, rest, stderr)
	}

	hcomPath, err := exec.LookPath("hcom")
	if err != nil {
		die(stderr, "hcom not on PATH. herder launches agents through hcom and never falls back to a raw '"+tool+"'. Run ai-setup (installs hcom via mise), or check `mise doctor` / your PATH.")
		return 1
	}

	// Codex gets its herder bootstrap here, not via the sessionstart rewrite:
	// hcom bakes the codex bootstrap into launch args (developer_instructions),
	// never into hook output, so launch time is the only herder-owned seam.
	// Fresh launches only — on resume/fork hcom strips ALL user
	// developer_instructions (they embed the previous instance's identity) and
	// re-adds just its own bootstrap, so threading the block there is dead
	// weight. That covers both herder resume/fork (mode != "launch"), where
	// lifecyclecmd re-delivers the addendum post-boot over the bus (TASK-017),
	// and the codex-native fork fallback, where spawn relaunches with a
	// `fork <session>` subcommand in the tool args and hcom's strip predicate
	// fires on it (no post-boot path there — TASK-027).
	if tool == "codex" && mode == "launch" && !codexStripsDevInstructions(rest) {
		rest = threadCodexBootstrapBlock(rest)
	}

	hcomArgs := []string{tool, "--run-here"}
	if mode == "resume" {
		hcomArgs = []string{"r", target, "--run-here"}
	} else if mode == "fork" {
		hcomArgs = []string{"f", target, "--run-here"}
	}
	if tag != "" {
		hcomArgs = append(hcomArgs, "--tag", tag)
	}
	hcomArgs = append(hcomArgs, rest...)

	PinConfigDir(tool)
	_ = os.Setenv("HCOM_LAUNCH_INFLIGHT", "1")
	if mode == "fork" || mode == "resume" {
		_ = os.Setenv("HERDER_LIFECYCLE_MODE", mode)
		_ = os.Setenv("HERDER_PARENT_SESSION_ID", parentSessionID)
	}
	startSidecar(tool)

	argv := append([]string{"hcom"}, hcomArgs...)
	if err := syscall.Exec(hcomPath, argv, os.Environ()); err != nil {
		die(stderr, "exec hcom: "+err.Error())
		return 1
	}
	return 0
}

// isPrintInvocation mirrors hcom's print-mode switch for claude: any tool-arg
// token exactly "-p" or "--print" (hcom scans raw argv the same way, and the
// flag is boolean — no value forms to handle). Only claude has a flag-based
// print mode today; codex one-shots use the `exec` subcommand and stay on the
// hcom path, where `-p` means something else entirely (--profile).
func isPrintInvocation(tool string, args []string) bool {
	if tool != "claude" {
		return false
	}
	for _, a := range args {
		if a == "-p" || a == "--print" {
			return true
		}
	}
	return false
}

// execPrintBypass replaces the hcom exec for one-shot print runs: resolve the
// tool through PATH (typically the shim), set the shim's recursion guard so it
// execs the real binary, and hand stdio over untouched so the answer returns
// to the caller. --tag is dropped — there is no bus session to name. Config
// pinning is skipped too: only hcom's local mode redirects config dirs, and
// hcom is out of the picture here.
func execPrintBypass(tool string, args []string, stderr io.Writer) int {
	toolPath, err := exec.LookPath(tool)
	if err != nil {
		die(stderr, "print bypass: no '"+tool+"' on PATH: "+err.Error())
		return 1
	}
	_ = os.Setenv("HCOM_LAUNCH_INFLIGHT", "1")
	argv := append([]string{tool}, args...)
	if err := syscall.Exec(toolPath, argv, os.Environ()); err != nil {
		die(stderr, "exec "+tool+": "+err.Error())
		return 1
	}
	return 0
}

// codexStripsDevInstructions mirrors hcom's strip predicate
// (preprocess_codex_args: any codex arg exactly "resume" or "fork"). When it
// fires, hcom discards every user developer_instructions flag before
// re-injecting its own fresh bootstrap, so anything we thread is dead weight
// on the exec'd argv — skip instead. Resumed/forked codex sessions get the
// doctrine post-boot over the bus instead (lifecyclecmd, TASK-017); the full
// seam story is documented on hookcmd.CodexBootstrapBlock.
func codexStripsDevInstructions(args []string) bool {
	for _, a := range args {
		if a == "resume" || a == "fork" {
			return true
		}
	}
	return false
}

// threadCodexBootstrapBlock delivers hookcmd.CodexBootstrapBlock to a fresh
// codex session as a user-level developer_instructions config. hcom's codex
// preprocessing (add_codex_developer_instructions) treats that as a system
// prompt and merges it AFTER its own bootstrap (bootstrap + "\n---\n" + ours) —
// a supported hcom surface, so nothing here parses or rewrites hcom output.
//
// hcom keeps only the LAST developer_instructions flag it sees and silently
// drops earlier ones, so when the caller already passed one we append the block
// inside that value instead of adding a second flag that would clobber theirs.
func threadCodexBootstrapBlock(args []string) []string {
	out := append([]string(nil), args...)
	for i := len(out) - 1; i >= 0; i-- {
		tok := out[i]
		if strings.HasPrefix(tok, "-c=developer_instructions=") ||
			strings.HasPrefix(tok, "--config=developer_instructions=") {
			out[i] = tok + "\n---\n" + hookcmd.CodexBootstrapBlock
			return out
		}
		if (tok == "-c" || tok == "--config") && i+1 < len(out) &&
			strings.HasPrefix(out[i+1], "developer_instructions=") {
			out[i+1] = out[i+1] + "\n---\n" + hookcmd.CodexBootstrapBlock
			return out
		}
	}
	return append(out, "-c", "developer_instructions="+hookcmd.CodexBootstrapBlock)
}

func startSidecar(tool string) {
	if os.Getenv("HERDR_ENV") != "1" || os.Getenv("HERDR_SOCKET_PATH") == "" || os.Getenv("HERDR_PANE_ID") == "" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "sidecar", "--tool", tool)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	logFile, err := sidecarLogFile()
	if err == nil {
		defer logFile.Close()
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	_ = cmd.Start()
}

func sidecarLogFile() (*os.File, error) {
	logDir := filepath.Join(filepath.Dir(registry.DefaultPath()), "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, err
	}
	pane := os.Getenv("HERDR_PANE_ID")
	if pane == "" {
		pane = "unknown"
	}
	return os.OpenFile(filepath.Join(logDir, "sidecar-"+pane+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

func printHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `herder launch — exec an hcom-bound tool in the CURRENT pane (used by herder spawn).

Replaces the raw tool invocation so the agent binds to the hcom message bus from
birth. herder spawn runs this inside the new pane; you rarely invoke it by hand.

Usage:
  herder launch <tool> [--tag TAG] [tool-args...]
  herder launch --resume <tool> <target> [--tag TAG] [tool-args...]
  herder launch --fork   <tool> <target> [--tag TAG] [tool-args...]

Options:
  --tag TAG    hcom tag; names the instance <tag>-<random> so @<tag>- fan-out works
  tool-args    everything after the tool is passed through to it

hcom is a HARD dependency — launch execs 'hcom <tool> --run-here' and never falls
back to a raw tool. HCOM_DIR (the team bus) is inherited from the environment, and
each tool's real config dir is pinned so auth survives an isolated team bus. The
claude pin also seeds ~/.claude/.claude.json from ~/.claude.json when missing, so
pinned launches keep onboarding/identity state instead of bootstrapping fresh
(and don't print claude's "configuration file not found / backup exists" stderr
into headless launch logs).

Exception — print one-shots: 'claude -p/--print ...' skips the bus entirely and
execs the PATH-resolved claude (hcom would background the run and the answer
would never return). --tag is ignored there; hcom is not required.

Codex bootstrap addendum: a fresh 'herder launch codex' threads a
[HERDER SESSION ADDENDUM] (the shared herder AGENTS doctrine + a codex-shaped
SUBAGENTS block) as user-level '-c developer_instructions='; hcom merges it
AFTER its own bootstrap, which stands but is superseded by instruction. On
--resume/--fork — and any codex invocation carrying a 'resume'/'fork' token,
including the fork-session fallback spawn — hcom strips ALL user
developer_instructions and re-adds only its own fresh bootstrap, so nothing is
threaded on those paths. 'herder resume'/'herder fork' close that gap
POST-BOOT instead: once the new session binds a bus name in the registry they
send the addendum as a verified bus message (see their --help).
`)
}

func die(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder launch: %s\n", msg)
}

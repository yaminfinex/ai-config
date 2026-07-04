package launchcmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"ai-config/tools/herder/internal/registry"
)

// IsHcomCapable is the single source of truth for agents that herder-spawn
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
		setEnvDefault("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	case "codex":
		setEnvDefault("CODEX_HOME", filepath.Join(home, ".codex"))
	case "gemini":
		setEnvDefault("GEMINI_CLI_HOME", filepath.Join(home, ".gemini"))
	}
}

func setEnvDefault(key, value string) {
	if os.Getenv(key) == "" {
		_ = os.Setenv(key, value)
	}
}

// Run executes the herder launch contract: parse hcom-owned flags, pin real
// config dirs when needed, optionally fork the status sidecar, then exec hcom.
func Run(args []string, stdout, stderr io.Writer) int {
	_ = stdout
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

	hcomPath, err := exec.LookPath("hcom")
	if err != nil {
		die(stderr, "hcom not on PATH — hcom is a hard dependency (plan 002 R4); install hcom or fix PATH. Not launching '"+tool+"' raw.")
		return 1
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

func die(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder launch: %s\n", msg)
}

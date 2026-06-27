package claude

import (
	"fmt"
	"os"
)

// Pane modes accepted by BuildLaunch. The empty string means interactive
// (current terminal); "right" and "below" launch into a herdr split.
const (
	PaneNone  = ""
	PaneRight = "right"
	PaneBelow = "below"
)

// defaultRole is the herder-spawn --role used when LaunchRequest.Role is empty.
const defaultRole = "bottle"

// LaunchRequest describes how to re-enter a materialized seed.
type LaunchRequest struct {
	SessionID string // the materialized seed's session id
	Cwd       string // resolved run cwd (caller's current dir or --cwd override)
	Pane      string // PaneNone (interactive), PaneRight, or PaneBelow
	Prompt    string // optional initial prompt
	Yolo      bool   // skip permission prompts (--dangerously-skip-permissions)
	Role      string // herder-spawn role label; defaults to "bottle"

	// PermissionMode restores the source session's permission mode on the
	// decant (--permission-mode <mode>). Resume does NOT carry the mode forward
	// — it is launch state — so decant reads the recorded mode and re-imposes
	// it. Empty means "no recorded mode": fall back to the launch default.
	// Yolo wins over it: full bypass regardless of the restored mode.
	PermissionMode string
}

// LaunchPlan is the command the cli layer should run to re-enter the seed.
type LaunchPlan struct {
	Argv   []string // program + args; Argv[0] is the program to exec
	RunCwd string   // mandatory working directory; the interactive path chdirs here
	Pane   bool     // true when launching into a herdr pane via herder-spawn
}

// BuildLaunch builds the launch command for a decant. It never spawns anything
// — it returns the argv and the run cwd for the cli layer to exec.
//
// The run cwd is mandatory and is validated to still exist (resume is
// cwd-scoped, so a dead cwd would fail with an opaque harness error
// otherwise).
//
// Permission semantics are kept identical across both paths, in precedence
// order Yolo > restored mode > launch default:
//   - Yolo: full bypass (--dangerously-skip-permissions), ignoring any
//     recorded mode.
//   - PermissionMode set: restore it verbatim (--permission-mode <mode>). On
//     the pane path this rides through as an --extra-arg, which herder-spawn
//     recognises and so suppresses its own autonomous skip-permissions default
//     — no --safe needed.
//   - neither: interactive falls back to plain claude --resume (ask-mode);
//     the pane path passes --safe to opt out of herder's autonomous default.
func BuildLaunch(req LaunchRequest) (LaunchPlan, error) {
	if req.SessionID == "" {
		return LaunchPlan{}, fmt.Errorf("launch: empty session id")
	}
	if req.Cwd == "" {
		return LaunchPlan{}, fmt.Errorf("launch: empty run cwd")
	}
	if fi, err := os.Stat(req.Cwd); err != nil || !fi.IsDir() {
		return LaunchPlan{}, fmt.Errorf("run cwd %s does not exist", req.Cwd)
	}

	switch req.Pane {
	case PaneNone:
		return interactiveLaunch(req), nil
	case PaneRight, PaneBelow:
		return paneLaunch(req), nil
	default:
		return LaunchPlan{}, fmt.Errorf("launch: unknown pane mode %q (use %q or %q)", req.Pane, PaneRight, PaneBelow)
	}
}

func interactiveLaunch(req LaunchRequest) LaunchPlan {
	argv := []string{"claude", "--resume", req.SessionID}
	argv = append(argv, permissionFlags(req)...)
	if req.Prompt != "" {
		argv = append(argv, req.Prompt)
	}
	return LaunchPlan{Argv: argv, RunCwd: req.Cwd, Pane: false}
}

// permissionFlags returns the claude permission flags for a launch, in
// precedence order Yolo > restored mode > none. Returned as a slice so the
// pane path can fan each token out through --extra-arg.
//
// A recorded mode of "default" (or none) is left implicit: "default" IS the
// launch default, so the existing safe path already restores it faithfully —
// emitting --permission-mode default would only buy a redundant flag and, on
// the pane path, needlessly drop the protective --safe. Only a non-default
// recorded mode is worth re-imposing.
func permissionFlags(req LaunchRequest) []string {
	switch {
	case req.Yolo:
		return []string{"--dangerously-skip-permissions"}
	case req.PermissionMode != "" && req.PermissionMode != "default":
		return []string{"--permission-mode", req.PermissionMode}
	default:
		return nil
	}
}

func paneLaunch(req LaunchRequest) LaunchPlan {
	split := "right"
	if req.Pane == PaneBelow {
		split = "down"
	}
	role := req.Role
	if role == "" {
		role = defaultRole
	}

	perm := permissionFlags(req)

	argv := []string{
		"herder-spawn",
		"--role", role,
		"--agent", "claude",
		"--split", split,
		"--cwd", req.Cwd,
	}
	// With no permission flag to impose, --safe opts out of herder-spawn's
	// autonomous skip-permissions default. When we do pass one (Yolo or a
	// restored mode), it rides through as --extra-arg below — herder-spawn
	// recognises it and suppresses its default, so --safe would be redundant.
	if len(perm) == 0 {
		argv = append(argv, "--safe")
	}
	if req.Prompt != "" {
		argv = append(argv, "--prompt", req.Prompt)
	}
	// claude's own flags ride through herder-spawn via --extra-arg.
	argv = append(argv, "--extra-arg", "--resume", "--extra-arg", req.SessionID)
	for _, f := range perm {
		argv = append(argv, "--extra-arg", f)
	}
	return LaunchPlan{Argv: argv, RunCwd: req.Cwd, Pane: true}
}

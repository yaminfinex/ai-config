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
	Cwd       string // resolved run cwd (bottle's recorded cwd or --cwd override)
	Pane      string // PaneNone (interactive), PaneRight, or PaneBelow
	Prompt    string // optional initial prompt
	Yolo      bool   // skip permission prompts (--dangerously-skip-permissions)
	Role      string // herder-spawn role label; defaults to "bottle"
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
// The run cwd is mandatory and is validated to still exist: a bottle whose
// recorded cwd is gone is refused with the path named and a --cwd suggestion
// (resume is cwd-scoped, so a stale cwd would fail with an opaque harness
// error otherwise).
//
// Permission semantics are kept identical to interactive across both paths:
//   - interactive: plain claude --resume (safe) unless Yolo adds
//     --dangerously-skip-permissions.
//   - pane: herder-spawn defaults claude to skip-permissions, so --safe is
//     passed to opt back into the interactive default; under Yolo, --safe is
//     dropped and --dangerously-skip-permissions is passed through explicitly.
func BuildLaunch(req LaunchRequest) (LaunchPlan, error) {
	if req.SessionID == "" {
		return LaunchPlan{}, fmt.Errorf("launch: empty session id")
	}
	if req.Cwd == "" {
		return LaunchPlan{}, fmt.Errorf("launch: empty run cwd")
	}
	if fi, err := os.Stat(req.Cwd); err != nil || !fi.IsDir() {
		return LaunchPlan{}, fmt.Errorf("recorded cwd %s no longer exists; pass --cwd to decant elsewhere", req.Cwd)
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
	if req.Yolo {
		argv = append(argv, "--dangerously-skip-permissions")
	}
	if req.Prompt != "" {
		argv = append(argv, req.Prompt)
	}
	return LaunchPlan{Argv: argv, RunCwd: req.Cwd, Pane: false}
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

	argv := []string{
		"herder-spawn",
		"--role", role,
		"--agent", "claude",
		"--split", split,
		"--cwd", req.Cwd,
	}
	if !req.Yolo {
		argv = append(argv, "--safe")
	}
	if req.Prompt != "" {
		argv = append(argv, "--prompt", req.Prompt)
	}
	// claude's own flags ride through herder-spawn via --extra-arg.
	argv = append(argv, "--extra-arg", "--resume", "--extra-arg", req.SessionID)
	if req.Yolo {
		argv = append(argv, "--extra-arg", "--dangerously-skip-permissions")
	}
	return LaunchPlan{Argv: argv, RunCwd: req.Cwd, Pane: true}
}

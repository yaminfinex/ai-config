// Package cli wires the herder command surface: one subcommand per original
// herder operation (spawn, send, list, wait, cull). Each subcommand's
// stderr/stdout/exit-code contract is pinned by the hermetic suites in
// tools/herder/tests — a port lands only when its suite is green with zero
// golden edits, so the dispatch layer stays out of the way: it routes on
// args[0] and passes everything else (including -h/--help) through to the
// subcommand, which owns its own flag parsing exactly like the bash script.
package cli

import (
	"fmt"
	"io"
	"strings"

	"ai-config/tools/herder/internal/cullcmd"
	"ai-config/tools/herder/internal/enrollcmd"
	"ai-config/tools/herder/internal/hookcmd"
	"ai-config/tools/herder/internal/launchcmd"
	"ai-config/tools/herder/internal/lifecyclecmd"
	"ai-config/tools/herder/internal/listcmd"
	"ai-config/tools/herder/internal/nodecmd"
	"ai-config/tools/herder/internal/reconcilecmd"
	"ai-config/tools/herder/internal/renamecmd"
	"ai-config/tools/herder/internal/retirecmd"
	"ai-config/tools/herder/internal/send"
	"ai-config/tools/herder/internal/sidecarcmd"
	"ai-config/tools/herder/internal/spawncmd"
	"ai-config/tools/herder/internal/waitcmd"
)

// command is one herder subcommand. summary is the one-line description in
// the root usage table; run receives the argv after the subcommand name.
type command struct {
	name    string
	summary string
	run     func(args []string, stdout, stderr io.Writer) int
}

// commands is the single registry the root usage table is generated from.
var commands = []command{
	{"spawn", "Spawn a named, GUID-tagged agent in a herdr pane", spawncmd.Run},
	{"send", "Deliver a message to a spawned agent over the hcom bus (bus-only)", send.Run},
	{"list", "Show spawned agents, reconciled with live herdr state", listcmd.Run},
	{"reconcile", "Audit or repair registry coordinates after herdr handoff", reconcilecmd.Run},
	{"wait", "Block until an agent reaches a status, optionally read its screen", waitcmd.Run},
	{"cull", "Close spawned agents and mark them closed in the registry", cullcmd.Run},
	{"enroll", "Register the current herdr pane in the registry", enrollcmd.Run},
	{"rename", "Rename an enrolled agent label and sync herdr best-effort", renamecmd.Run},
	{"retire", "Retire an unseated session and release its label", retirecmd.RunRetire},
	{"reopen", "Reopen a retired session as unseated and unlabelled", retirecmd.RunReopen},
	{"fork", "Branch an enrolled agent session into a new guid", lifecyclecmd.RunFork},
	{"resume", "Reopen an enrolled agent session with the same guid", lifecyclecmd.RunResume},
	{"compact", "Queue a steered /compact into the caller's own pane (self only)", spawncmd.RunCompact},
	{"node", "Manage the local herder node id", nodecmd.Run},
	{"launch", "Launch an hcom-bound tool in the current pane", launchcmd.Run},
	{"hook", "(internal) Shim hcom hook calls; rewrite the spawn bootstrap", hookcmd.Run},
	{"sidecar", "(internal) Bridge hcom status to herdr pane status", sidecarcmd.Run},
}

// notPorted builds the stub for a subcommand whose port has not landed yet.
func notPorted(name, phase string) func(args []string, stdout, stderr io.Writer) int {
	return func(args []string, stdout, stderr io.Writer) int {
		fmt.Fprintf(stderr, "herder %s: not ported yet (%s)\n", name, phase)
		return 1
	}
}

// rootUsage renders the no-arg / help output: what the binary is and the
// subcommand table.
func rootUsage() string {
	var b strings.Builder
	b.WriteString("herder — spawn, message, and cull agent sessions in herdr panes.\n")
	b.WriteString("\n")
	b.WriteString("Single interface for running agent sessions: spawns named, GUID-tagged\n")
	b.WriteString("agents into herdr panes bound to the hcom message bus, delivers verified\n")
	b.WriteString("messages, and culls them via a durable registry. Use herder instead of raw\n")
	b.WriteString("`herdr agent start/send` (which writes text without submitting it) and\n")
	b.WriteString("instead of `hcom <n> claude` / `hcom kill` (which bypass the registry).\n")
	b.WriteString("\n")
	b.WriteString("Commands:\n")
	for _, cmd := range commands {
		fmt.Fprintf(&b, "  %-8s %s\n", cmd.name, cmd.summary)
	}
	b.WriteString("\n")
	b.WriteString("Run `herder <command> --help` for that command's usage.\n")
	return b.String()
}

// Run executes the herder CLI and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, rootUsage())
		return 0
	}

	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, rootUsage())
		return 0
	case "compact-then":
		// Internal: the detached continuation sender `herder compact --then`
		// forks. Not in the command table (never invoked by hand), so it stays
		// out of the root usage and help-contract surfaces.
		return spawncmd.RunCompactThen(args[1:], stdout, stderr)
	}

	for _, cmd := range commands {
		if cmd.name == args[0] {
			return cmd.run(args[1:], stdout, stderr)
		}
	}

	fmt.Fprintf(stderr, "herder: unknown command %q — run `herder` for the command list\n", args[0])
	return 2
}

// Package cli wires the herder command surface: one subcommand per bash
// script it replaces (spawn, send, list, wait, cull). Each subcommand's
// stderr/stdout/exit-code contract is pinned by the hermetic suites in
// skills/herder/tests — a port lands only when its suite is green with zero
// golden edits, so the dispatch layer stays out of the way: it routes on
// args[0] and passes everything else (including -h/--help) through to the
// subcommand, which owns its own flag parsing exactly like the bash script.
package cli

import (
	"fmt"
	"io"
	"strings"
)

// command is one herder subcommand. summary is the one-line description in
// the root usage table; run receives the argv after the subcommand name.
type command struct {
	name    string
	summary string
	run     func(args []string, stdout, stderr io.Writer) int
}

// commands is the single registry the root usage table is generated from.
// Ports land here phase by phase; until then a stub points at the bash
// implementation so the binary never silently half-works.
var commands = []command{
	{"spawn", "Spawn a named, GUID-tagged agent in a herdr pane", notPorted("spawn", "P5")},
	{"send", "Deliver a message to a spawned agent (herdr or hcom bus)", notPorted("send", "P2")},
	{"list", "Show spawned agents, reconciled with live herdr state", notPorted("list", "P4")},
	{"wait", "Block until an agent reaches a status, optionally read its screen", notPorted("wait", "P4")},
	{"cull", "Close spawned agents and mark them closed in the registry", notPorted("cull", "P4")},
}

// notPorted builds the stub for a subcommand whose port has not landed yet:
// it names the bash script that is still authoritative and fails loudly, so
// a premature shim flip cannot silently no-op.
func notPorted(name, phase string) func(args []string, stdout, stderr io.Writer) int {
	return func(args []string, stdout, stderr io.Writer) int {
		fmt.Fprintf(stderr, "herder %s: not ported yet (%s) — use skills/herder/scripts/herder-%s\n",
			name, phase, name)
		return 1
	}
}

// rootUsage renders the no-arg / help output: what the binary is, the
// subcommand table, and where the still-bash implementations live.
func rootUsage() string {
	var b strings.Builder
	b.WriteString("herder — spawn, message, and cull herdr-pane agents\n")
	b.WriteString("\n")
	b.WriteString("Go port of the herder skill's bash substrate. Each subcommand mirrors the\n")
	b.WriteString("matching skills/herder/scripts/herder-<name> script byte-for-byte; the\n")
	b.WriteString("hermetic suites in skills/herder/tests are the contract.\n")
	b.WriteString("\n")
	b.WriteString("Commands:\n")
	for _, cmd := range commands {
		fmt.Fprintf(&b, "  %-7s %s\n", cmd.name, cmd.summary)
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
	}

	for _, cmd := range commands {
		if cmd.name == args[0] {
			return cmd.run(args[1:], stdout, stderr)
		}
	}

	fmt.Fprintf(stderr, "herder: unknown command %q — run `herder` for the command list\n", args[0])
	return 2
}

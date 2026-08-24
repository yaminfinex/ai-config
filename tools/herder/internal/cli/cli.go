// Package cli wires the surviving read-only list surface and the observer that
// refreshes the human-facing ledger cache.
package cli

import (
	"fmt"
	"io"
	"strings"

	"ai-config/tools/herder/internal/listcmd"
	"ai-config/tools/herder/internal/observercmd"
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
	{"list", "Show the human-facing fleet ledger cache with live annotations", listcmd.Run},
	{"observer", "Observe seated sessions and surface observer advice", observercmd.Run},
}

// rootUsage renders the no-arg / help output: what the binary is and the
// subcommand table.
func rootUsage() string {
	var b strings.Builder
	b.WriteString("herder — observe and display the human-facing fleet ledger cache.\n")
	b.WriteString("\n")
	b.WriteString("Lifecycle actions compose through tools/fleet, hcom, and herdr. Herder's\n")
	b.WriteString("ledger is display cache only and is never authority for lifecycle actions.\n")
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
	}

	for _, cmd := range commands {
		if cmd.name == args[0] {
			return cmd.run(args[1:], stdout, stderr)
		}
	}

	fmt.Fprintf(stderr, "herder: unknown command %q — run `herder` for the command list\n", args[0])
	return 2
}

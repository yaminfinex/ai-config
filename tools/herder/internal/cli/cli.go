// Package cli wires herder's single read-only live-list surface.
package cli

import (
	"fmt"
	"io"
	"strings"

	"ai-config/tools/herder/internal/listcmd"
	"ai-config/tools/herder/internal/servecmd"
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
	{"list", "Join live herdr placement with the hcom roster", listcmd.Run},
	{"serve", "Serve the live fleet API on loopback and tailscale", servecmd.Run},
}

// rootUsage renders the no-arg / help output: what the binary is and the
// subcommand table.
func rootUsage() string {
	var b strings.Builder
	b.WriteString("herder — display and serve the live fleet view.\n")
	b.WriteString("\n")
	b.WriteString("Lifecycle actions compose through tools/fleet, hcom, and herdr. Herder is\n")
	b.WriteString("a read-only live join and is never lifecycle authority.\n")
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

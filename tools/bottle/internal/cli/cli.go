// Package cli wires the bottle command surface: a root command whose no-arg
// output is the agent-skill help, plus one subcommand per specced verb.
package cli

import (
	"fmt"
	"io"
	"strings"
)

// command is one bottle subcommand. usage is the one-line synopsis shown in
// `bottle <name> --help`; summary is the short description used in the root
// help's command table.
type command struct {
	name    string
	usage   string
	summary string
	run     func(args []string, stdout, stderr io.Writer) int
}

// commands is the single registry the root help table is generated from, so
// help can never drift from what is wired. Order matches the spec's CLI
// surface table.
var commands = []command{
	live("create", "bottle create <name> [--session ID | --last] [--at [N]] [--note ...] [--attach PATH...] [--force]",
		"Snapshot a session into a new bottle", cmdCreate),
	live("decant", "bottle decant <name>[@v] [--pane right|below] [--prompt ...] [--yolo] [--cwd PATH]",
		"Materialize a fresh session from a bottle and resume it", cmdDecant),
	live("rebottle", "bottle rebottle [<name>] [--session ID] [--note ...]",
		"Re-bottle a decanted session, bumping the version", cmdRebottle),
	live("list", "bottle list",
		"Table of bottles: name, latest version, count, age, note", cmdList),
	live("log", "bottle log <name>",
		"Version chain with provenance", cmdLog),
	live("show", "bottle show <name>[@v] [--turns N]",
		"Full metadata plus a transcript preview", cmdShow),
	live("rename", "bottle rename <old> <new>",
		"Move all versions to a new name", cmdRename),
	live("note", "bottle note <name>[@v] <text>",
		"Set or replace the free-text note", cmdNote),
	live("prune", "bottle prune",
		"Drop decant entries whose session files are gone", cmdPrune),
	live("rm", "bottle rm <name>[@v]",
		"Delete one version, or the whole name", cmdRm),
	live("artifacts", "bottle artifacts <name>[@v] [--extract DIR]",
		"List or extract attached artifacts", cmdArtifacts),
}

// RootHelp is the single source of truth for the no-arg `bottle` output. The
// placeholder text here becomes the agent-tuned skill in U8; golden tests will
// pin it then. The command table is generated from the registry.
func RootHelp() string {
	var b strings.Builder
	b.WriteString("bottle — pin, name, and re-enter agent contexts\n")
	b.WriteString("\n")
	b.WriteString("[placeholder — the ~60-line agent-tuned skill help lands in U8]\n")
	b.WriteString("\n")
	b.WriteString("Commands:\n")
	for _, cmd := range commands {
		fmt.Fprintf(&b, "  %-10s %s\n", cmd.name, cmd.summary)
	}
	b.WriteString("\n")
	b.WriteString("Run `bottle <command> --help` for details.\n")
	return b.String()
}

// Run executes the bottle CLI and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, RootHelp())
		return 0
	}

	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, RootHelp())
		return 0
	}

	for _, cmd := range commands {
		if cmd.name == args[0] {
			return cmd.run(args[1:], stdout, stderr)
		}
	}

	fmt.Fprintf(stderr, "bottle: unknown command %q — run `bottle` for the command list\n", args[0])
	return 2
}

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
// help's command table; help is the examples-and-pitfalls body printed after
// the usage/summary on `bottle <name> --help`.
type command struct {
	name    string
	usage   string
	summary string
	help    string
	run     func(args []string, stdout, stderr io.Writer) int
}

// commands is the single registry the root help table is generated from, so
// help can never drift from what is wired. Order matches the spec's CLI
// surface table.
var commands = []command{
	live("create", "bottle create <name> [--session ID | --last] [--at [N]] [--note ...] [--attach PATH...] [--force]",
		"Snapshot a session into a new bottle", createHelp, cmdCreate),
	live("decant", "bottle decant <name>[@v] [--pane right|below] [--prompt ...] [--yolo] [--cwd PATH]",
		"Materialize a fresh session from a bottle and resume it", decantHelp, cmdDecant),
	live("rebottle", "bottle rebottle [<name>] [--session ID] [--note ...]",
		"Re-bottle a decanted session, bumping the version", rebottleHelp, cmdRebottle),
	live("list", "bottle list",
		"Table of bottles: name, latest version, count, age, note", listHelp, cmdList),
	live("log", "bottle log <name>",
		"Version chain with provenance", logHelp, cmdLog),
	live("show", "bottle show <name>[@v] [--turns N]",
		"Full metadata plus a transcript preview", showHelp, cmdShow),
	live("rename", "bottle rename <old> <new>",
		"Move all versions to a new name", renameHelp, cmdRename),
	live("note", "bottle note <name>[@v] <text>",
		"Set or replace the free-text note", noteHelp, cmdNote),
	live("prune", "bottle prune",
		"Drop decant entries whose session files are gone", pruneHelp, cmdPrune),
	live("rm", "bottle rm <name>[@v]",
		"Delete one version, or the whole name", rmHelp, cmdRm),
	live("artifacts", "bottle artifacts <name>[@v] [--extract DIR]",
		"List or extract attached artifacts", artifactsHelp, cmdArtifacts),
}

// RootHelp is the single source of truth for the no-arg `bottle` output: a
// lightweight, agent-tuned skill — concept model in a few lines, the command
// table generated from the registry, and a pointer to per-command help. Kept
// well under 60 lines (a golden test asserts the budget). Written agents-first.
func RootHelp() string {
	var b strings.Builder
	b.WriteString("bottle — pin, name, and re-enter agent contexts\n")
	b.WriteString("\n")
	b.WriteString("A bottle is an immutable snapshot of an agent conversation at a chosen\n")
	b.WriteString("point: a frozen transcript plus its provenance. The workflow is three verbs:\n")
	b.WriteString("\n")
	b.WriteString("  create    freeze the current (or a named) session into a new bottle\n")
	b.WriteString("  decant    materialize a fresh, resumable session seeded from a bottle\n")
	b.WriteString("  rebottle  freeze a decanted session back into a bottle, bumping its version\n")
	b.WriteString("\n")
	b.WriteString("Names carry versions: `auth-expert` is the latest, `auth-expert@2` a fixed\n")
	b.WriteString("snapshot. Rebottling records lineage (the parent bottle and the decant\n")
	b.WriteString("session between them), so `log` shows the full provenance chain. Decanting\n")
	b.WriteString("never mutates a bottle — each decant spawns a fresh conversation and runs\n")
	b.WriteString("against the repo as it is today (branch and sha are recorded, not restored).\n")
	b.WriteString("\n")
	b.WriteString("Self-bottling trims the in-flight turn: a bottle of your own live session is\n")
	b.WriteString("cut at the last completed turn, dropping the running `bottle create` call.\n")
	b.WriteString("\n")
	b.WriteString("v1 is Claude-only. Any agent can inspect bottles and decant into a pane, but\n")
	b.WriteString("self-bottling needs a Claude session id ($CLAUDE_CODE_SESSION_ID); from\n")
	b.WriteString("another harness pass `--session ID` or `--last`.\n")
	b.WriteString("\n")
	b.WriteString("Commands:\n")
	for _, cmd := range commands {
		fmt.Fprintf(&b, "  %-10s %s\n", cmd.name, cmd.summary)
	}
	b.WriteString("\n")
	b.WriteString("Run `bottle <command> --help` for usage, examples, and pitfalls.\n")
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

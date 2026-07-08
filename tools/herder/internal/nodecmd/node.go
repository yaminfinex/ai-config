package nodecmd

import (
	"flag"
	"fmt"
	"io"

	"ai-config/tools/herder/internal/registry"
)

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, usage())
		return 0
	}
	switch args[0] {
	case "init":
		return runInit(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "herder node: unknown command %q — run `herder node --help`\n", args[0])
		return 2
	}
}

func runInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("herder node init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	newNode := fs.Bool("new", false, "mint a fresh node id for a cloned state dir")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "herder node init: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	res, err := registry.InitNode(registry.DefaultPath(), *newNode)
	if err != nil {
		fmt.Fprintf(stderr, "herder node init: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "herder node init: %s: %s\n", res.Message, res.NodeID)
	return 0
}

func usage() string {
	return `herder node — manage the local herder node id.

Usage:
  herder node init [--new]

The first registry write normally mints the node id lazily. Use init to repair
a half-copied state dir explicitly. Use --new on a cloned state dir to keep
prior rows intact and attribute future writes to a fresh local node.
`
}

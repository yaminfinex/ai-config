package waitcmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"ai-config/tools/herder/internal/herdrcli"
	"ai-config/tools/herder/internal/registry"
)

type options struct {
	help      bool
	status    string
	timeoutMS string
	read      bool
	lines     string
	source    string
	target    string
}

func Run(args []string, stdout, stderr io.Writer) int {
	if os.Getenv("HERDR_ENV") != "1" {
		die(stderr, "not running inside a herdr pane (HERDR_ENV != 1)")
		return 1
	}
	if _, err := exec.LookPath("herdr"); err != nil {
		die(stderr, "herdr not on PATH")
		return 1
	}
	if _, err := exec.LookPath("jq"); err != nil {
		die(stderr, "jq not on PATH")
		return 1
	}

	opts, code := parseArgs(args, stdout, stderr)
	if code != 0 {
		return code
	}
	if opts.help {
		return 0
	}

	client := &herdrcli.Client{}
	paneOut, _ := client.Output("pane", "list")
	paneID, ok := resolvePane(opts.target, paneOut, stderr)
	if !ok {
		return 1
	}
	if paneID == "" {
		paneID = opts.target
	}

	waitRC, err := client.Run("wait", "agent-status", paneID, "--status", opts.status, "--timeout", opts.timeoutMS)
	if err != nil {
		waitRC = 1
	}
	if waitRC != 0 {
		fmt.Fprintf(stderr, "herder-wait: timeout waiting for %s to reach status=%s\n", paneID, opts.status)
	}

	if opts.read {
		runPaneRead(paneID, opts.source, opts.lines, stdout, stderr)
	}
	return waitRC
}

func parseArgs(args []string, stdout, stderr io.Writer) (options, int) {
	opts := options{status: "idle", timeoutMS: "60000", lines: "30", source: "recent-unwrapped"}
	for i := 0; i < len(args); {
		arg := args[i]
		switch arg {
		case "--status":
			if i+1 < len(args) {
				opts.status = args[i+1]
			}
			i += 2
		case "--timeout":
			if i+1 < len(args) {
				opts.timeoutMS = args[i+1]
			}
			i += 2
		case "--read":
			opts.read = true
			i++
		case "--lines":
			if i+1 < len(args) {
				opts.lines = args[i+1]
			}
			i += 2
		case "--source":
			if i+1 < len(args) {
				opts.source = args[i+1]
			}
			i += 2
		case "-h", "--help":
			printHelp(stdout)
			opts.help = true
			return opts, 0
		default:
			if len(arg) >= 2 && arg[:2] == "--" {
				die(stderr, "unknown flag: "+arg)
				return opts, 1
			}
			if opts.target != "" {
				die(stderr, "extra positional arg: "+arg)
				return opts, 1
			}
			opts.target = arg
			i++
		}
	}
	if opts.target == "" {
		die(stderr, "target required (guid, label, or pane id)")
		return opts, 1
	}
	return opts, 0
}

func printHelp(stdout io.Writer) {
	fmt.Fprint(stdout, `#!/usr/bin/env bash
# herder-wait — block until a spawned agent reaches a status, optionally read its screen.
#
# Usage:
#   herder-wait <target> [--status idle|working|blocked|done|unknown] [--timeout MS]
#                        [--read] [--lines N] [--source visible|recent|recent-unwrapped]
`)
}

func resolvePane(target string, paneOut []byte, stderr io.Writer) (string, bool) {
	recs, err := registry.Load(registry.DefaultPath())
	var rec *registry.Record
	if err == nil {
		rec = registry.Resolve(recs, target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	if rec == nil {
		return target, true
	}
	term := rec.TerminalID
	stored := rec.PaneID
	if term == "" {
		return stored, true
	}
	panes, paneErr := herdrcli.ParsePaneList(paneOut)
	for _, pane := range panes {
		if pane.TerminalID == term {
			return pane.PaneID, true
		}
	}
	if paneErr != nil || len(panes) == 0 {
		fmt.Fprintf(stderr, "herder-wait: could not read live pane list; cannot resolve %s\n", target)
	} else {
		fmt.Fprintf(stderr, "herder-wait: %s (terminal %s) is not live anywhere — agent gone or culled\n", displayName(rec, target), term)
	}
	return "", false
}

func runPaneRead(paneID, source, lines string, stdout, stderr io.Writer) {
	cmd := exec.Command("herdr", "pane", "read", paneID, "--source", source, "--lines", lines)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	_ = cmd.Run()
}

func displayName(rec *registry.Record, fallback string) string {
	if rec != nil && rec.Label != nil && *rec.Label != "" {
		return *rec.Label
	}
	return fallback
}

func die(stderr io.Writer, msg string) {
	fmt.Fprintf(stderr, "herder-wait: %s\n", msg)
}

// Package assigncmd implements `herder assign`: one assignment event append.
package assigncmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"ai-config/tools/herder/internal/agentstore"
	"ai-config/tools/herder/internal/eventcmd"
	"ai-config/tools/herder/internal/herderstate"
)

const exitUnavailable = 3

// Run assigns an agent's manager, group, or both.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage())
		return 2
	}
	if args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, usage())
		return 0
	}
	if strings.HasPrefix(args[0], "-") {
		fmt.Fprintf(stderr, "herder assign: <agent> must come first\n%s", usage())
		return 2
	}
	for _, arg := range args[1:] {
		if arg == "-name" || arg == "--name" || strings.HasPrefix(arg, "-name=") || strings.HasPrefix(arg, "--name=") {
			fmt.Fprintf(stderr, "herder assign: --name is set by the <agent> argument\n%s", usage())
			return 2
		}
	}
	parsed := append([]string{"--name", args[0]}, args[1:]...)
	event, asJSON, err := eventcmd.Parse(agentstore.KindAssign, parsed)
	if err != nil {
		fmt.Fprintf(stderr, "herder assign: %v\n%s", err, usage())
		return 2
	}
	stateDir, err := herderstate.Dir()
	if err != nil {
		fmt.Fprintf(stderr, "herder assign: store unavailable: %v\n", err)
		return exitUnavailable
	}
	store := agentstore.Open(stateDir, stderr)
	if store.ImportErr != nil {
		fmt.Fprintf(stderr, "herder assign: store unavailable: %v\n", store.ImportErr)
		return exitUnavailable
	}
	receipt, err := store.Append(event)
	if err != nil {
		if errors.Is(err, agentstore.ErrUnavailable) {
			fmt.Fprintf(stderr, "herder assign: %v\n", err)
			return exitUnavailable
		}
		fmt.Fprintf(stderr, "herder assign: %v\n", err)
		return 2
	}
	if asJSON {
		encoded, _ := json.Marshal(struct {
			agentstore.Event
			Replayed bool `json:"replayed"`
		}{receipt.Event, receipt.Replayed})
		fmt.Fprintf(stdout, "%s\n", encoded)
		return 0
	}
	fmt.Fprintf(stdout, "id=%s\n", receipt.Event.ID)
	if receipt.Replayed {
		fmt.Fprintln(stdout, "replayed=true")
	}
	return 0
}

func usage() string {
	return `herder assign — assign an agent's manager and/or group with one event.

Usage:
  herder assign <agent> [--manager NAME|human] [--group NAME] [--clear-group] [--by WHO] [--by-kind KIND] [--at RFC3339] [--id UUID] [--json]

At least one of --manager, --group, or --clear-group is required. --manager human
adopts the seat to the top level. --group replaces the group; --clear-group
removes it. The low-level equivalent is herder register assign --name AGENT ….
Later assignment events win by event time. Exit 0 appended or replayed, 2 usage,
3 store unavailable.

Examples:
  herder assign impl-geni --manager ziru
  herder assign impl-geni --manager human
  herder assign impl-geni --group fleet-refit
  herder assign impl-geni --clear-group
`
}

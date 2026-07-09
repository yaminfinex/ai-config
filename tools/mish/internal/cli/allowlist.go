package cli

import (
	"fmt"
	"strings"
)

type backlogAllowlistEntry struct {
	name string
	note string
}

var backlogAllowlist = []backlogAllowlistEntry{
	{name: "task", note: "Task CRUD, notes, status, and references"},
	{name: "tasks", note: "Alias for task"},
	{name: "draft", note: "Draft task workflow"},
	{name: "board", note: "Kanban render"},
	{name: "search", note: "Read-only index search"},
	{name: "overview", note: "Read-only project stats"},
	{name: "sequence", note: "Read-only dependency sequences"},
	{name: "doc", note: "Board-internal docs inside the mission dir"},
	{name: "decision", note: "Board-internal decisions inside the mission dir"},
	{name: "milestone", note: "Board-internal grouping"},
	{name: "milestones", note: "Alias for milestone"},
	{name: "cleanup", note: "Ages Done tasks into completed; authority etiquette applies"},
}

var backlogAllowed = func() map[string]bool {
	allowed := make(map[string]bool, len(backlogAllowlist))
	for _, entry := range backlogAllowlist {
		allowed[entry.name] = true
	}
	return allowed
}()

func isBacklogAllowed(subcommand string) bool {
	return backlogAllowed[subcommand]
}

func backlogAllowlistNames() []string {
	names := make([]string, 0, len(backlogAllowlist))
	for _, entry := range backlogAllowlist {
		names = append(names, entry.name)
	}
	return names
}

func backlogAllowlistSummary() string {
	names := backlogAllowlistNames()
	return strings.Join(names, ", ")
}

func backlogHelp() string {
	var b strings.Builder
	fmt.Fprintln(&b, "mish backlog runs allowlisted Backlog.md commands inside the resolved mission.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Usage:")
	fmt.Fprintln(&b, "  mish backlog [--mission <slug>] <subcommand> [args...]")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Allowed subcommands:")
	for _, entry := range backlogAllowlist {
		fmt.Fprintf(&b, "  %-10s %s\n", entry.name, entry.note)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Excluded:")
	// init would reinitialize a live mission and damage the scaffold.
	// config is closed because pinned keys are invariant; deliberate tuning is a file edit.
	// agents writes instruction files at the board root, littering the shared repo.
	// browser's settings endpoint rewrites config.yml, pins included.
	// completion, instructions, and mcp have no mission use case yet.
	fmt.Fprintln(&b, "  init, config, agents, browser, completion, instructions, mcp")
	fmt.Fprintln(&b, "Anything outside the allowlist refuses until deliberately added.")
	return b.String()
}

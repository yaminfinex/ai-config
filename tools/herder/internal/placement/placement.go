// Package placement resolves the shared pane-placement policy for lifecycle
// commands before any herdr mutation occurs.
package placement

import "fmt"

// Flags are the placement switches accepted by a pane-creating command.
type Flags struct {
	Split         string
	SplitExplicit bool
	NewTab        bool
	ExistingTab   string
	Worktree      bool
}

// Decision is the normalized placement passed to the launch path.
type Decision struct {
	Split  string
	NewTab bool
}

// Resolve makes a fresh tab the safe default. An explicit split or existing
// tab opts into same-tab placement. Worktree launches keep their native
// workspace/tab placement.
func Resolve(flags Flags) (Decision, error) {
	split := flags.Split
	if split == "" {
		split = "right"
	}
	if split != "right" && split != "down" {
		return Decision{}, fmt.Errorf("--split must be right or down: %s", split)
	}
	if flags.NewTab && flags.SplitExplicit {
		return Decision{}, fmt.Errorf("use --new-tab or --split, not both")
	}
	if flags.NewTab && flags.ExistingTab != "" {
		return Decision{}, fmt.Errorf("use --new-tab or --tab, not both")
	}
	if flags.Worktree {
		return Decision{Split: split}, nil
	}
	if flags.SplitExplicit || flags.ExistingTab != "" {
		return Decision{Split: split}, nil
	}
	return Decision{Split: split, NewTab: true}, nil
}

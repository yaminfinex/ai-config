package cli

import (
	"bufio"
	"flag"
	"fmt"
	"strings"

	"ai-config/tools/bottle/internal/refs"
)

// gitRetentionWarning is printed on every rm: removing a bottle drops it from
// the registry and live store but its transcript survives in the store's git
// history (and would ride along on any future push). Stated plainly per spec.
const gitRetentionWarning = "Warning: rm removes the bottle from the registry and live store, but its " +
	"transcript remains in the store's git history. To expunge it permanently, " +
	"rewrite history under ~/.bottles (e.g. `git -C ~/.bottles` with git filter-repo)."

// cmdRm deletes one version (`name@v`) or the whole name (`name`). It confirms
// destructive intent: a y/N prompt when stdin is a TTY, or --force for agents
// and scripts. A non-TTY stdin without --force is refused with a hint that
// agents must pass --force. The git-history retention warning always prints.
func cmdRm(d *deps, args []string) int {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	force := fs.Bool("force", false, "skip the confirmation prompt (required for non-interactive use)")
	pos, err := parseFlexible(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(d.stderr, "Usage: bottle rm <name>[@v] [--force]")
		return 2
	}
	ref, err := refs.Parse(pos[0])
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle rm: %v\n", err)
		return 1
	}

	target := ref.String()
	scope := "every version of " + target
	if ref.Pinned() {
		scope = "version " + target
	}

	fmt.Fprintln(d.stderr, gitRetentionWarning)

	if !*force {
		if !d.isTTY {
			fmt.Fprintf(d.stderr, "bottle rm: refusing to remove %s on a non-interactive stdin without --force (agents and scripts must pass --force)\n", target)
			return 1
		}
		fmt.Fprintf(d.stdout, "Remove %s? This cannot be undone. [y/N] ", scope)
		reader := bufio.NewReader(d.stdin)
		line, _ := reader.ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
		default:
			fmt.Fprintln(d.stdout, "Aborted.")
			return 1
		}
	}

	if err := d.store.Remove(ref); err != nil {
		fmt.Fprintf(d.stderr, "bottle rm: %v\n", err)
		return 1
	}
	fmt.Fprintf(d.stdout, "Removed %s.\n", scope)
	return 0
}

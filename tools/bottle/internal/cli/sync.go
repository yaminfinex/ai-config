package cli

import (
	"flag"
	"fmt"
)

// cmdSync is a thin wrapper over Store.Sync: parse --remote, run the sync,
// render the report. First-run setup (--remote given, or origin replaced)
// echoes the remote and a privacy reminder — bottles can carry keys and other
// sensitive context, so the remote must be private. There is nothing to
// confirm: sync is additive and abort-safe, so no prompts and no --force.
func cmdSync(d *deps, args []string) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	remote := fs.String("remote", "", "configure (or replace) the store's git remote, then sync")
	pos, err := parseFlexible(fs, args)
	if err != nil || len(pos) != 0 {
		fmt.Fprintln(d.stderr, "Usage: bottle sync [--remote <url>]")
		return 2
	}

	rep, err := d.store.Sync(*remote)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle sync: %v\n", err)
		return 1
	}

	if rep.RemoteConfigured {
		fmt.Fprintf(d.stdout, "Remote configured: %s\n", rep.RemoteURL)
		fmt.Fprintln(d.stdout, "Bottles can carry keys, transcripts, and other sensitive context — keep this remote private.")
	}
	if rep.Received == 0 && rep.Sent == 0 && len(rep.Renames) == 0 {
		fmt.Fprintln(d.stdout, "already in sync")
		return 0
	}
	fmt.Fprintf(d.stdout, "synced: %d received, %d sent\n", rep.Received, rep.Sent)
	for _, rn := range rep.Renames {
		fmt.Fprintf(d.stdout, "renamed: %s → %s (name collision)\n", rn.OldName, rn.NewName)
	}
	return 0
}

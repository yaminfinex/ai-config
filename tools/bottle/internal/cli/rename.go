package cli

import "fmt"

// cmdRename moves every version of <old> to <new> — a thin wrapper over the
// store's registry-only rename (bottle dirs do not move; lineage survives).
func cmdRename(d *deps, args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(d.stderr, "Usage: bottle rename <old> <new>")
		return 2
	}
	old, newName := args[0], args[1]
	if err := d.store.Rename(old, newName); err != nil {
		fmt.Fprintf(d.stderr, "bottle rename: %v\n", err)
		return 1
	}
	fmt.Fprintf(d.stdout, "Renamed %s to %s.\n", old, newName)
	return 0
}

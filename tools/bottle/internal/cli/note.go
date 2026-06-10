package cli

import (
	"fmt"
	"strings"

	"ai-config/tools/bottle/internal/refs"
)

// cmdNote sets or replaces a bottle's free-text note — the one sanctioned
// mutation of bottle metadata. An unpinned ref edits the latest version. The
// note text is the remaining arguments joined with spaces (so an unquoted
// multi-word note still works).
func cmdNote(d *deps, args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(d.stderr, "Usage: bottle note <name>[@v] <text>")
		return 2
	}
	ref, err := refs.Parse(args[0])
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle note: %v\n", err)
		return 1
	}
	note := strings.Join(args[1:], " ")
	if err := d.store.SetNote(ref, note); err != nil {
		fmt.Fprintf(d.stderr, "bottle note: %v\n", err)
		return 1
	}
	fmt.Fprintf(d.stdout, "Updated note for %s.\n", ref.String())
	return 0
}

package cli

import (
	"fmt"
	"text/tabwriter"
)

// cmdList prints the bottle table: name, latest version, version count, age,
// and note. An empty store reports politely and exits 0 — not an error.
func cmdList(d *deps, args []string) int {
	infos, err := d.store.List()
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle list: %v\n", err)
		return 1
	}
	if len(infos) == 0 {
		fmt.Fprintln(d.stdout, "No bottles yet — create one with `bottle create <name>`.")
		return 0
	}
	now := d.now()
	tw := tabwriter.NewWriter(d.stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tLATEST\tVERSIONS\tAGE\tNOTE")
	for _, in := range infos {
		fmt.Fprintf(tw, "%s\t@%d\t%d\t%s\t%s\n",
			in.Name, in.Latest, in.Versions, humanizeAge(now.Sub(in.Created)), in.Note)
	}
	tw.Flush()
	return 0
}

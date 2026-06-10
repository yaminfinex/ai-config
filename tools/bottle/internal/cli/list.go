package cli

import (
	"fmt"
	"strings"
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
	if hint := syncHint(d.store.SyncStatus()); hint != "" {
		fmt.Fprintln(d.stdout, hint)
	}
	return 0
}

// syncHint renders the one quiet unsynced line list appends when a remote is
// configured and the local refs say there is something to sync; "" otherwise
// (no remote, fully synced, or status unreadable — list never gets noisier).
// Local-only commits read as "unsynced"; remote-only commits already seen by
// a fetch read as "to pull".
func syncHint(ahead, behind int, ok bool) string {
	if !ok || ahead+behind == 0 {
		return ""
	}
	var parts []string
	if ahead > 0 {
		parts = append(parts, fmt.Sprintf("%s unsynced", pluralCommits(ahead)))
	}
	if behind > 0 {
		parts = append(parts, fmt.Sprintf("%s to pull", pluralCommits(behind)))
	}
	return "· " + strings.Join(parts, ", ") + " — bottle sync"
}

func pluralCommits(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}

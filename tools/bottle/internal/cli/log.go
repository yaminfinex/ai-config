package cli

import (
	"fmt"
	"strings"

	"ai-config/tools/bottle/internal/refs"
	"ai-config/tools/bottle/internal/store"
)

// cmdLog prints a name's version chain, newest first, with provenance:
// `@3 ← decant of beta@2 (session 9f2c…), 2026-06-10`, plus compaction/rewind
// flags and `(deleted)` for parents that have since been removed.
func cmdLog(d *deps, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(d.stderr, "Usage: bottle log <name>")
		return 2
	}
	ref, err := refs.Parse(args[0])
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle log: %v\n", err)
		return 1
	}
	entries, err := d.store.Log(ref.Name)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle log: %v\n", err)
		return 1
	}
	for _, e := range entries {
		var b strings.Builder
		fmt.Fprintf(&b, "@%d", e.Version)
		if e.Parent != nil {
			fmt.Fprintf(&b, " ← decant of %s", e.Parent.Display())
			if sid := e.Parent.DecantSessionID; sid != "" {
				fmt.Fprintf(&b, " (session %s)", shortSession(sid))
			}
		}
		fmt.Fprintf(&b, ", %s", e.Created.Format("2006-01-02"))
		for _, tag := range logTags(e) {
			fmt.Fprintf(&b, " [%s]", tag)
		}
		if e.Note != "" {
			fmt.Fprintf(&b, " — %s", e.Note)
		}
		fmt.Fprintln(d.stdout, b.String())
	}
	return 0
}

// logTags derives the human-facing annotation tags from a log entry's
// compaction/rewind booleans (computed by create-time and persisted in meta).
func logTags(e store.LogEntry) []string {
	var tags []string
	if e.Compacted {
		tags = append(tags, "compacted")
	}
	if e.CompactionReachesInherited {
		tags = append(tags, "compaction-reaches-inherited")
	}
	if e.RewoundIntoParent {
		tags = append(tags, "rewound-into-parent")
	}
	return tags
}

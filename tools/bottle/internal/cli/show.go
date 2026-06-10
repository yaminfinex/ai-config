package cli

import (
	"bytes"
	"flag"
	"fmt"
	"time"

	"ai-config/tools/bottle/internal/refs"
	"ai-config/tools/bottle/internal/store"
	"ai-config/tools/bottle/internal/transcript"
)

// cmdShow prints a bottle's full metadata plus a preview of its last N
// transcript turns (default 5, adjustable with --turns).
func cmdShow(d *deps, args []string) int {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	turns := fs.Int("turns", 5, "number of recent turns to preview")
	pos, err := parseFlexible(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) < 1 {
		fmt.Fprintln(d.stderr, "Usage: bottle show <name>[@v] [--turns N]")
		return 2
	}
	ref, err := refs.Parse(pos[0])
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle show: %v\n", err)
		return 1
	}
	b, err := d.store.Resolve(ref)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle show: %v\n", err)
		return 1
	}
	m := b.Meta
	out := d.stdout

	fmt.Fprintf(out, "%s@%d\n", m.Name, m.Version)
	fmt.Fprintf(out, "created:  %s\n", m.Created.Format(time.RFC3339))
	if m.Note != "" {
		fmt.Fprintf(out, "note:     %s\n", m.Note)
	}
	fmt.Fprintf(out, "session:  %s (harness %s)\n", shortSession(m.Source.SessionID), m.Source.Harness)
	fmt.Fprintf(out, "cwd:      %s\n", m.Source.CWD)
	if m.Source.GitBranch != "" {
		line := m.Source.GitBranch
		if m.Source.GitSHA != "" {
			line += " @ " + m.Source.GitSHA
		}
		fmt.Fprintf(out, "git:      %s\n", line)
	}
	fmt.Fprintf(out, "turns:    cut at %d of %d\n", m.Source.CutTurn, m.Source.TotalTurns)
	if m.Parent != nil {
		parent := "(deleted)"
		if pb, err := d.store.Get(m.Parent.BottleID); err == nil {
			parent = fmt.Sprintf("%s@%d", pb.Meta.Name, pb.Meta.Version)
		}
		fmt.Fprintf(out, "parent:   %s (decant session %s)\n", parent, shortSession(m.Parent.DecantSessionID))
	}
	for _, tag := range showTags(m) {
		fmt.Fprintf(out, "flag:     %s\n", tag)
	}

	data, err := d.store.ReadTranscript(b.ID)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle show: %v\n", err)
		return 1
	}
	info, err := transcript.Index(bytes.NewReader(data))
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle show: reading transcript: %v\n", err)
		return 1
	}
	all := info.Turns()
	n := *turns
	if n < 0 {
		n = 0
	}
	start := len(all) - n
	if start < 0 {
		start = 0
	}
	preview := all[start:]
	fmt.Fprintf(out, "\nlast %d turn(s) of %d:\n", len(preview), len(all))
	for _, t := range preview {
		fmt.Fprintf(out, "  %d. %s\n", t.N, oneLine(t.Text, 80))
	}
	return 0
}

// showTags renders the compaction/rewind annotation tags for show's metadata
// block, from the same meta booleans log surfaces.
func showTags(m store.Meta) []string {
	var tags []string
	if m.Compacted {
		tags = append(tags, "compacted")
	}
	if m.CompactionReachesInherited {
		tags = append(tags, "compaction-reaches-inherited")
	}
	if m.RewoundIntoParent {
		tags = append(tags, "rewound-into-parent")
	}
	return tags
}

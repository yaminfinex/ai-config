package cli

import (
	"bytes"
	"flag"
	"fmt"

	"ai-config/tools/bottle/internal/refs"
	"ai-config/tools/bottle/internal/store"
	"ai-config/tools/bottle/internal/transcript"
)

// cmdRebottle re-bottles a decanted session, setting its parent automatically.
// It resolves the session (explicit --session, else the live session) in the
// decants map to find the parent bottle, then freezes the current session under
// the parent's name (bumping the version) or a new name (starting a new lineage
// with the parent still recorded). A session that isn't a known decant is
// refused with a pointer to plain `bottle create`.
func cmdRebottle(d *deps, args []string) int {
	fs := flag.NewFlagSet("rebottle", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	session := fs.String("session", "", "decant session id to rebottle (defaults to the live session)")
	note := fs.String("note", "", "free-text note to attach")
	pos, err := parseFlexible(fs, args)
	if err != nil {
		return 2
	}

	sessionID := *session
	if sessionID == "" {
		sessionID = d.selfSession
	}
	if sessionID == "" {
		fmt.Fprintln(d.stderr, "bottle rebottle: not inside a Claude session — pass --session ID")
		return 2
	}

	dec, ok, err := d.store.LookupDecant(sessionID)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle rebottle: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(d.stderr, "bottle rebottle: session %s is not a known decant — there is no parent to inherit from.\n", shortSession(sessionID))
		fmt.Fprintln(d.stderr, "Use `bottle create <name>` to make a fresh (root) bottle instead.")
		return 1
	}
	parent, err := d.store.Get(dec.BottleID)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle rebottle: parent bottle: %v\n", err)
		return 1
	}

	name := parent.Meta.Name
	if len(pos) >= 1 {
		name = pos[0]
	}
	if err := refs.ValidateName(name); err != nil {
		fmt.Fprintf(d.stderr, "bottle rebottle: %v\n", err)
		return 1
	}

	sourcePath, err := d.sessionSourcePath(sessionID)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle rebottle: %v\n", err)
		return 1
	}
	info, err := transcript.IndexFile(sourcePath)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle rebottle: %v\n", err)
		return 1
	}

	// Rebottling the live session trims the in-flight turn, like a self-bottle;
	// rebottling another recorded decant freezes it whole.
	mode := cutWhole
	if sessionID == d.selfSession && d.selfSession != "" {
		mode = cutSelf
	}
	final, cutTurn, totalTurns, err := cutTranscript(sourcePath, info, mode, 0)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle rebottle: %v\n", err)
		return 1
	}

	finalInfo, err := transcript.Index(bytes.NewReader(final))
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle rebottle: reading cut transcript: %v\n", err)
		return 1
	}

	parentTranscript, err := d.store.ReadTranscript(dec.BottleID)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle rebottle: %v\n", err)
		return 1
	}
	inherited := bytes.Count(parentTranscript, []byte{'\n'})

	if n := finalInfo.CompactBoundaries(); n > 0 {
		fmt.Fprintf(d.stderr, "warning: source contains %d compaction boundary(ies); freezing the compacted state (an honest snapshot)\n", n)
	}

	b, err := d.freezeSession(freezeRequest{
		name:            name,
		sessionID:       sessionID,
		cwd:             d.cwd,
		finalTranscript: final,
		finalInfo:       finalInfo,
		cutTurn:         cutTurn,
		totalTurns:      totalTurns,
		parent:          &store.Parent{BottleID: dec.BottleID, DecantSessionID: sessionID},
		inheritedLines:  inherited,
		note:            *note,
	})
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle rebottle: %v\n", err)
		return 1
	}
	fmt.Fprintf(d.stdout, "Rebottled %s@%d (%s) ← decant of %s@%d (session %s).\n",
		b.Meta.Name, b.Meta.Version, b.ID, parent.Meta.Name, parent.Meta.Version, shortSession(sessionID))
	return 0
}

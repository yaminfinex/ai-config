package cli

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"ai-config/tools/bottle/internal/harness/claude"
	"ai-config/tools/bottle/internal/refs"
	"ai-config/tools/bottle/internal/store"
	"ai-config/tools/bottle/internal/transcript"
)

// cmdCreate snapshots a session into a new immutable bottle. The session is
// resolved through the default chain (explicit --session → the live session
// from $CLAUDE_CODE_SESSION_ID → --last, the most recent session for the cwd),
// then cut: a self-bottle trims to the last *completed* turn (the in-flight
// turn that holds the running `bottle create` call is dropped), --at rewinds to
// an earlier turn (interactive picker, or non-interactive --at N), and any
// other source is frozen whole. Compaction is warned about; --attach copies
// files into the bottle, refusing sensitive-looking names without --force.
func cmdCreate(d *deps, args []string) int {
	atMode, atTurn, rest, err := extractAt(args)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle create: %v\n", err)
		return 2
	}

	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	session := fs.String("session", "", "session id to bottle")
	last := fs.Bool("last", false, "bottle the most recent session for this cwd")
	note := fs.String("note", "", "free-text note to attach")
	force := fs.Bool("force", false, "override the sensitive-attachment refusal")
	var attach stringSlice
	fs.Var(&attach, "attach", "attach a file to the bottle (repeatable)")
	pos, err := parseFlexible(fs, rest)
	if err != nil {
		return 2
	}
	if len(pos) < 1 {
		fmt.Fprintln(d.stderr, "Usage: bottle create <name> [--session ID | --last] [--at [N]] [--note ...] [--attach PATH]...")
		return 2
	}
	if len(pos) > 1 {
		fmt.Fprintf(d.stderr, "bottle create: unexpected argument %q — --attach takes one path per flag (write --attach a.md --attach b.md)\n", pos[1])
		return 2
	}
	name := pos[0]
	if err := refs.ValidateName(name); err != nil {
		fmt.Fprintf(d.stderr, "bottle create: %v\n", err)
		return 1
	}
	if *session != "" && *last {
		fmt.Fprintln(d.stderr, "bottle create: choose either --session or --last, not both")
		return 2
	}

	// Validate attachments up front so a refusal writes no bottle.
	attachments, err := validateAttachments(d.cwd, attach, *force)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle create: %v\n", err)
		return 1
	}

	sessionID, sourcePath, isSelf, err := d.resolveCreateSource(*session, *last)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle create: %v\n", err)
		return 1
	}

	// Validate + index the source before anything is written.
	info, err := transcript.IndexFile(sourcePath)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle create: %v\n", err)
		return 1
	}

	mode := cutWhole
	switch {
	case atMode == atPicker:
		n, err := d.runPicker(info.Turns())
		if err != nil {
			fmt.Fprintf(d.stderr, "bottle create: %v\n", err)
			return 1
		}
		atTurn, mode = n, cutAt
	case atMode == atNumber:
		mode = cutAt
	case isSelf:
		mode = cutSelf
	}

	final, cutTurn, totalTurns, err := cutTranscript(sourcePath, info, mode, atTurn)
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle create: %v\n", err)
		return 1
	}

	finalInfo, err := transcript.Index(bytes.NewReader(final))
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle create: reading cut transcript: %v\n", err)
		return 1
	}
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
		note:            *note,
		attachments:     attachments,
	})
	if err != nil {
		fmt.Fprintf(d.stderr, "bottle create: %v\n", err)
		return 1
	}
	fmt.Fprintf(d.stdout, "Created %s@%d (%s) — cut at turn %d of %d.\n",
		b.Meta.Name, b.Meta.Version, b.ID, cutTurn, totalTurns)
	return 0
}

// cutMode selects how a source transcript is frozen into a bottle.
type cutMode int

const (
	cutWhole cutMode = iota // freeze the whole session as-is
	cutAt                   // cut at an explicit turn (--at picker / --at N)
	cutSelf                 // self-bottle: trim to the last completed turn
)

// atSpec records how --at was supplied: not at all, bare (interactive picker),
// or with an explicit turn number.
type atSpec int

const (
	atUnset atSpec = iota
	atPicker
	atNumber
)

// extractAt pulls the --at flag out of args before the FlagSet sees it, because
// --at is optional-valued (bare opens the picker, `--at N` / `--at=N` cut
// directly) and stdlib flag can't express that. A numeric token immediately
// after a bare --at is taken as the turn number; anything else leaves --at in
// picker mode and is passed through.
func extractAt(args []string) (mode atSpec, turn int, rest []string, err error) {
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--at" || a == "-at":
			if i+1 < len(args) {
				if n, e := strconv.Atoi(args[i+1]); e == nil {
					if n < 1 {
						return atUnset, 0, nil, fmt.Errorf("--at: %d is not a positive turn number", n)
					}
					mode, turn = atNumber, n
					i++
					continue
				}
			}
			mode = atPicker
		case strings.HasPrefix(a, "--at=") || strings.HasPrefix(a, "-at="):
			v := a[strings.IndexByte(a, '=')+1:]
			n, e := strconv.Atoi(v)
			if e != nil || n < 1 {
				return atUnset, 0, nil, fmt.Errorf("--at: %q is not a positive turn number", v)
			}
			mode, turn = atNumber, n
		default:
			rest = append(rest, a)
		}
	}
	return mode, turn, rest, nil
}

// runPicker prints the numbered turn list and reads a selection from stdin. It
// needs an interactive terminal; the non-interactive path is `--at N`. Turns
// with no completed assistant response are shown but refused as cut points.
func (d *deps) runPicker(turns []transcript.Turn) (int, error) {
	if len(turns) == 0 {
		return 0, transcript.ErrNoTurns
	}
	if !d.isTTY {
		return 0, fmt.Errorf("the --at rewind picker needs an interactive terminal; pass --at N to pick a turn non-interactively")
	}
	for _, t := range turns {
		marker := ""
		if t.ResponseLeafUUID == "" {
			marker = "  (no completed response — not selectable)"
		}
		fmt.Fprintf(d.stdout, "  %d. [%s] %s%s\n", t.N, t.Timestamp, oneLine(t.Text, 80), marker)
	}
	fmt.Fprint(d.stdout, "Cut at turn number: ")
	line, _ := bufio.NewReader(d.stdin).ReadString('\n')
	choice := strings.TrimSpace(line)
	n, err := strconv.Atoi(choice)
	if err != nil || n < 1 || n > len(turns) {
		return 0, fmt.Errorf("%q is not a turn number between 1 and %d", choice, len(turns))
	}
	if turns[n-1].ResponseLeafUUID == "" {
		return 0, fmt.Errorf("turn %d has no completed assistant response to cut at", n)
	}
	return n, nil
}

// resolveCreateSource applies the session default chain and returns the chosen
// session id, the path to its on-disk transcript, and whether it is the live
// session (which triggers the self-bottle trim). --last and the no-flag
// fallback both print a preview so a silent --last can't surprise the user.
func (d *deps) resolveCreateSource(session string, last bool) (sessionID, sourcePath string, isSelf bool, err error) {
	switch {
	case last:
		preview, e := claude.LastSession(d.projectsRoot, d.cwd)
		if e != nil {
			return "", "", false, e
		}
		d.printPreview(preview)
		return preview.SessionID, preview.Path, false, nil
	case session != "":
		path, e := d.sessionSourcePath(session)
		if e != nil {
			return "", "", false, e
		}
		return session, path, session == d.selfSession && d.selfSession != "", nil
	case d.selfSession != "":
		path, e := d.sessionSourcePath(d.selfSession)
		if e != nil {
			return "", "", false, e
		}
		return d.selfSession, path, true, nil
	default:
		preview, e := claude.LastSession(d.projectsRoot, d.cwd)
		if e != nil {
			if errors.Is(e, claude.ErrNoSessions) {
				return "", "", false, fmt.Errorf("no session to bottle: not inside a Claude session and no recent session for %s — pass --session ID or --last", d.cwd)
			}
			return "", "", false, e
		}
		d.printPreview(preview)
		return preview.SessionID, preview.Path, false, nil
	}
}

// printPreview shows the chosen session's age and first human turn before
// bottling proceeds.
func (d *deps) printPreview(p claude.SessionPreview) {
	fmt.Fprintf(d.stdout, "Using session %s (%s old): %s\n",
		shortSession(p.SessionID), humanizeAge(p.Age(d.now())), oneLine(p.FirstUserText, 80))
}

// sessionSourcePath locates a session's transcript on disk. It prefers the
// current cwd's encoded project dir but falls back to searching every project
// dir under the projects root, because Claude files a session under the dir it
// was *launched* from — so bottling from a subdirectory of the workspace must
// not assume the file lives under the current cwd (the U: subdir-cwd rule).
func (d *deps) sessionSourcePath(sessionID string) (string, error) {
	path, err := claude.FindSessionPath(d.projectsRoot, d.cwd, sessionID)
	if err != nil {
		return "", fmt.Errorf("session %s: transcript not found in any project dir under %s", shortSession(sessionID), d.projectsRoot)
	}
	return path, nil
}

// cutTranscript produces the bytes to freeze for a given cut mode, along with
// the recorded cut turn and the source's total turn count.
func cutTranscript(sourcePath string, info *transcript.Info, mode cutMode, atTurn int) (final []byte, cutTurn, totalTurns int, err error) {
	turns := info.Turns()
	totalTurns = len(turns)
	switch mode {
	case cutWhole:
		final, err = os.ReadFile(sourcePath)
		if err != nil {
			return nil, 0, 0, err
		}
		return final, totalTurns, totalTurns, nil
	case cutAt:
		if totalTurns == 0 {
			return nil, 0, 0, fmt.Errorf("%s: %w", sourcePath, transcript.ErrNoTurns)
		}
		if atTurn < 1 || atTurn > totalTurns {
			return nil, 0, 0, fmt.Errorf("--at %d out of range (the session has %d turns)", atTurn, totalTurns)
		}
		if turns[atTurn-1].ResponseLeafUUID == "" {
			return nil, 0, 0, fmt.Errorf("turn %d has no completed assistant response to cut at", atTurn)
		}
		final, err = truncateToBytes(sourcePath, func(dst string) error {
			return transcript.TruncateFileAtTurn(sourcePath, dst, atTurn)
		})
		return final, atTurn, totalTurns, err
	case cutSelf:
		idx := lastCompletedTurn(turns)
		if idx < 0 {
			return nil, 0, 0, fmt.Errorf("nothing to bottle: the session has no completed turn")
		}
		leaf := turns[idx].ResponseLeafUUID
		final, err = truncateToBytes(sourcePath, func(dst string) error {
			return transcript.TruncateFileAtLeaf(sourcePath, dst, leaf)
		})
		return final, turns[idx].N, totalTurns, err
	default:
		return nil, 0, 0, fmt.Errorf("unknown cut mode")
	}
}

// lastCompletedTurn returns the index of the last turn whose assistant response
// is fully resolved (a completing leaf, no dangling tool_use) — the self-bottle
// cut point. -1 when there is no completed turn.
func lastCompletedTurn(turns []transcript.Turn) int {
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].ResponseLeafUUID != "" && !turns[i].DanglingToolUse {
			return i
		}
	}
	return -1
}

// truncateToBytes runs a TruncateFile* operation into a temp file and returns
// its bytes, cleaning the temp up afterward.
func truncateToBytes(sourcePath string, fn func(dst string) error) ([]byte, error) {
	tmp, err := os.CreateTemp("", "bottle-cut-*.jsonl")
	if err != nil {
		return nil, err
	}
	name := tmp.Name()
	tmp.Close()
	defer os.Remove(name)
	if err := fn(name); err != nil {
		return nil, err
	}
	return os.ReadFile(name)
}

// freezeRequest is the shared input to freezeSession, used by both create and
// rebottle (rebottle adds parent + inheritedLines).
type freezeRequest struct {
	name            string
	sessionID       string
	cwd             string
	finalTranscript []byte
	finalInfo       *transcript.Info
	cutTurn         int
	totalTurns      int
	parent          *store.Parent
	inheritedLines  int
	note            string
	attachments     []attachment
}

// freezeSession computes the compaction/rewind annotations, writes the bottle,
// and copies in any attachments. It is the common tail of create and rebottle.
func (d *deps) freezeSession(req freezeRequest) (*store.Bottle, error) {
	compacted, reaches, rewound := computeAnnotations(req.finalInfo, req.finalTranscript, req.inheritedLines)
	branch, sha := d.gitInfo(req.cwd)
	b, err := d.store.Create(store.CreateRequest{
		Name:       req.name,
		Transcript: req.finalTranscript,
		Note:       req.note,
		Parent:     req.parent,
		Source: store.Source{
			SessionID:  req.sessionID,
			Harness:    "claude",
			CWD:        req.cwd,
			GitBranch:  branch,
			GitSHA:     sha,
			CutTurn:    req.cutTurn,
			TotalTurns: req.totalTurns,
		},
		InheritedLines:             req.inheritedLines,
		Compacted:                  compacted,
		CompactionReachesInherited: reaches,
		RewoundIntoParent:          rewound,
	})
	if err != nil {
		return nil, err
	}
	for _, a := range req.attachments {
		if err := d.store.AddArtifact(b.ID, a.rel, a.data); err != nil {
			return nil, fmt.Errorf("attach %s: %w", a.rel, err)
		}
		fmt.Fprintf(d.stdout, "attached %s → %s\n", a.abs, a.rel)
	}
	return b, nil
}

// computeAnnotations derives the meta booleans bottle log/show surface, from
// the frozen transcript and the inherited prefix length (zero for root
// bottles). RewoundIntoParent: the cut sits at or before the inherited prefix.
// CompactionReachesInherited: a compact boundary's logical parent points back
// into inherited context (compaction swallowed pre-lineage history).
func computeAnnotations(finalInfo *transcript.Info, final []byte, inheritedLines int) (compacted, reaches, rewound bool) {
	compacted = finalInfo.CompactBoundaries() > 0
	finalLines := bytes.Count(final, []byte{'\n'})
	rewound = inheritedLines > 0 && finalLines <= inheritedLines
	if compacted && inheritedLines > 0 {
		line := make(map[string]int, len(finalInfo.Entries))
		for i := range finalInfo.Entries {
			if e := finalInfo.Entries[i]; e.UUID != "" {
				line[e.UUID] = e.Line
			}
		}
		for i := range finalInfo.Entries {
			e := finalInfo.Entries[i]
			if e.IsCompactBoundary() && e.LogicalParentUUID != "" {
				if ln, ok := line[e.LogicalParentUUID]; ok && ln <= inheritedLines {
					reaches = true
				}
			}
		}
	}
	return compacted, reaches, rewound
}

// attachment is a validated --attach file ready to be copied into a bottle.
type attachment struct {
	abs  string // resolved absolute path (printed before copying)
	rel  string // cwd-relative storage key
	data []byte
}

// validateAttachments resolves, screens, and reads each --attach path before
// any bottle is created. It refuses directories, files outside the cwd (v1
// records cwd-relative paths), and sensitive-looking names without --force.
func validateAttachments(cwd string, paths []string, force bool) ([]attachment, error) {
	var out []attachment
	for _, p := range paths {
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(cwd, p)
		}
		abs = filepath.Clean(abs)
		fi, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("attach %s: %w", p, err)
		}
		if fi.IsDir() {
			return nil, fmt.Errorf("attach %s: is a directory (v1 attaches individual files)", p)
		}
		base := filepath.Base(abs)
		if !force && isSensitiveName(base) {
			return nil, fmt.Errorf("attach %s: refusing sensitive-looking file %q without --force (attachments enter the store's permanent git history)", p, base)
		}
		rel, err := filepath.Rel(cwd, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("attach %s: file is outside the bottle cwd %s (v1 records cwd-relative paths)", p, cwd)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("attach %s: %w", p, err)
		}
		out = append(out, attachment{abs: abs, rel: filepath.ToSlash(rel), data: data})
	}
	return out, nil
}

// isSensitiveName matches the spec's refusal set: .env*, *secret*, *credential*,
// id_rsa*, *.pem (case-insensitive).
func isSensitiveName(base string) bool {
	lower := strings.ToLower(base)
	if ok, _ := filepath.Match(".env*", lower); ok {
		return true
	}
	if strings.Contains(lower, "secret") || strings.Contains(lower, "credential") {
		return true
	}
	if strings.HasPrefix(lower, "id_rsa") || strings.HasSuffix(lower, ".pem") {
		return true
	}
	return false
}

// stringSlice is a repeatable string flag (--attach a --attach b).
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

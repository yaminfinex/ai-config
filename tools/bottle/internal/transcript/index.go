package transcript

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrNoTurns is returned when a transcript contains no human turns to
// enumerate or truncate at (empty file, or header/operational lines only).
var ErrNoTurns = errors.New("no turns in transcript")

// Info is the parsed index of a transcript: per-line entry metadata, in file
// order. It retains no raw line bytes — multi-MB transcripts index small.
type Info struct {
	Entries []Entry

	byUUID map[string]int // uuid -> Entries index (last write wins)
}

// Turn is one human prompt on the live branch, in conversation order.
type Turn struct {
	N         int    // 1-based turn number
	UUID      string // the user entry's uuid
	Line      int    // source line of the user entry
	Timestamp string
	Text      string // full human prompt text

	// ResponseLeafUUID is the assistant entry that completed this turn — the
	// last assistant on the live branch before the next human turn or compact
	// boundary. Empty when the turn has no completed assistant response;
	// such a turn cannot be a truncation point.
	ResponseLeafUUID string

	// DanglingToolUse reports that the turn's response dispatched a tool_use
	// with no matching tool_result anywhere in the transcript (the in-flight
	// self-bottle scenario; input to U6's trim decision).
	DanglingToolUse bool
}

// Index parses a transcript stream line by line. A malformed line aborts with
// an error naming its 1-based line number. Whitespace-only lines are skipped.
func Index(r io.Reader) (*Info, error) {
	info := &Info{byUUID: make(map[string]int)}
	br := bufio.NewReaderSize(r, 64*1024)
	lineNum := 0
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			lineNum++
			if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
				e, perr := ParseEntry(trimmed, lineNum)
				if perr != nil {
					return nil, perr
				}
				info.Entries = append(info.Entries, e)
				if e.UUID != "" && e.Class() == ClassTreeNode {
					info.byUUID[e.UUID] = len(info.Entries) - 1
				}
			}
		}
		if err == io.EOF {
			return info, nil
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum+1, err)
		}
	}
}

// IndexFile is Index over a file on disk.
func IndexFile(path string) (*Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := Index(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return info, nil
}

// LastLeaf returns the uuid of the last tree node in file order — what
// `claude --resume` continues from (U1 finding: the last tree entry wins;
// the last-prompt trailer is ignored for leaf selection). Empty if the
// transcript has no tree nodes.
func (in *Info) LastLeaf() string {
	for i := len(in.Entries) - 1; i >= 0; i-- {
		if in.Entries[i].Class() == ClassTreeNode && in.Entries[i].UUID != "" {
			return in.Entries[i].UUID
		}
	}
	return ""
}

// CompactBoundaries counts compact_boundary entries — create-time compaction
// warnings (U5/U6) key off this.
func (in *Info) CompactBoundaries() int {
	n := 0
	for i := range in.Entries {
		if in.Entries[i].IsCompactBoundary() {
			n++
		}
	}
	return n
}

// entry returns the indexed entry for a tree-node uuid.
func (in *Info) entry(uuid string) (Entry, bool) {
	i, ok := in.byUUID[uuid]
	if !ok {
		return Entry{}, false
	}
	return in.Entries[i], true
}

// livePath returns the live branch in root→leaf order: the parentUuid chain
// from the last tree entry, hopping logicalParentUuid at compact boundaries
// (whose parentUuid is null). A visited set guards against cycles in corrupt
// input.
func (in *Info) livePath() []Entry {
	var rev []Entry
	seen := make(map[string]bool)
	cur := in.LastLeaf()
	for cur != "" && !seen[cur] {
		seen[cur] = true
		e, ok := in.entry(cur)
		if !ok {
			break
		}
		rev = append(rev, e)
		if e.ParentUUID != "" {
			cur = e.ParentUUID
		} else {
			cur = e.LogicalParentUUID
		}
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

// Turns enumerates the human turns on the live branch, in conversation
// order. Each turn's span runs from its user entry to the next human turn or
// compact boundary, whichever comes first; the span's last assistant entry is
// the turn's completing response (and truncation cut leaf).
func (in *Info) Turns() []Turn {
	path := in.livePath()

	// tool_results seen anywhere in the file settle dangling-tool_use checks
	// (a result can be re-delivered on a rewind branch).
	results := make(map[string]bool)
	for i := range in.Entries {
		for _, id := range in.Entries[i].ToolResultIDs {
			results[id] = true
		}
	}

	var turns []Turn
	for i := 0; i < len(path); i++ {
		e := path[i]
		if !e.IsUserTurn() {
			continue
		}
		turn := Turn{
			N:         len(turns) + 1,
			UUID:      e.UUID,
			Line:      e.Line,
			Timestamp: e.Timestamp,
			Text:      e.UserText(),
		}
		for j := i + 1; j < len(path); j++ {
			s := path[j]
			if s.IsUserTurn() || s.IsCompactBoundary() {
				break
			}
			if s.Type == "assistant" {
				turn.ResponseLeafUUID = s.UUID
				for _, id := range s.ToolUseIDs {
					if !results[id] {
						turn.DanglingToolUse = true
					}
				}
			}
		}
		turns = append(turns, turn)
	}
	return turns
}

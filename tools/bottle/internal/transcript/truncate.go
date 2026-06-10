package transcript

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Truncate cuts a transcript at a tree-node leaf and writes the repaired
// result. The cut is a temporal prefix — every line up to and including the
// leaf's, nothing after — which is exactly the shape the U1 spike verified a
// live harness resumes (rewind branches recorded before the cut stay; the
// resume point is the last tree entry, so they are inert).
//
// Repairs applied:
//   - boundary+summary atomicity: a cut aimed at a compact_boundary extends
//     through its isCompactSummary entry (never split the block);
//   - in-prefix trailers/operational lines whose references point past the
//     cut are dropped (last-prompt/summary leafUuid, file-history-snapshot
//     messageId); everything past the cut — trailers, operational lines,
//     queue-operations for cut messages, unknown types — is dropped;
//   - the final last-prompt trailer is rewritten to the new tail: leafUuid
//     gets the cut leaf, and a lastPrompt field (when the trailer carries
//     one) gets the nearest preceding human prompt. Per U1 this is hygiene,
//     not load-bearing: resume follows the last tree entry.
//
// src must re-read the same content Index saw. Prefer the TruncateFile*
// wrappers, which handle the two passes and write atomically.
func Truncate(src io.Reader, w io.Writer, info *Info, leafUUID string) error {
	cut, ok := info.entry(leafUUID)
	if !ok {
		return fmt.Errorf("cut leaf %q: no tree node with that uuid", leafUUID)
	}
	if cut.IsCompactBoundary() {
		for i := range info.Entries {
			e := &info.Entries[i]
			if e.IsCompactSummary && e.ParentUUID == cut.UUID {
				cut = *e
				break
			}
		}
	}
	cutLine := cut.Line

	kept := make(map[string]bool)
	var template *Entry // the file's final last-prompt, rewritten onto the new tail
	for i := range info.Entries {
		e := &info.Entries[i]
		if e.Line <= cutLine && e.Class() == ClassTreeNode && e.UUID != "" {
			kept[e.UUID] = true
		}
		if e.Type == "last-prompt" {
			template = &info.Entries[i]
		}
	}

	promptText := nearestUserTurnText(info, cut.UUID)

	bw := bufio.NewWriter(w)
	br := bufio.NewReaderSize(src, 64*1024)
	lineNum, entryIdx := 0, 0
	var templateRaw []byte
	for {
		line, rerr := br.ReadBytes('\n')
		chomped := bytes.TrimSuffix(line, []byte("\n"))
		if len(chomped) > 0 {
			lineNum++
			if len(bytes.TrimSpace(chomped)) > 0 {
				for entryIdx < len(info.Entries) && info.Entries[entryIdx].Line < lineNum {
					entryIdx++
				}
				if entryIdx >= len(info.Entries) || info.Entries[entryIdx].Line != lineNum {
					return fmt.Errorf("line %d: source changed between passes", lineNum)
				}
				e := &info.Entries[entryIdx]
				if template != nil && e.Line == template.Line {
					templateRaw = append([]byte(nil), chomped...)
				}
				if lineNum <= cutLine && keepInPrefix(e, kept) {
					if _, err := bw.Write(chomped); err != nil {
						return err
					}
					if err := bw.WriteByte('\n'); err != nil {
						return err
					}
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("line %d: %w", lineNum+1, rerr)
		}
	}

	if templateRaw != nil {
		out, _, err := spliceTopLevelString(templateRaw, "leafUuid", cut.UUID)
		if err != nil {
			return fmt.Errorf("rewrite final last-prompt: %w", err)
		}
		if promptText != "" {
			out, _, err = spliceTopLevelString(out, "lastPrompt", promptText)
			if err != nil {
				return fmt.Errorf("rewrite final last-prompt: %w", err)
			}
		}
		if _, err := bw.Write(out); err != nil {
			return err
		}
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// keepInPrefix decides whether an in-prefix non-tree line survives the cut:
// only lines whose uuid references still resolve do. Lines with no
// references (mode, ai-title, queue-operation, unknown types) pass through
// untouched on copy.
func keepInPrefix(e *Entry, kept map[string]bool) bool {
	switch e.Type {
	case "last-prompt", "summary":
		return e.LeafUUID == "" || kept[e.LeafUUID]
	case "file-history-snapshot":
		return e.MessageID == "" || kept[e.MessageID]
	}
	return true
}

// nearestUserTurnText walks ancestors from a tree node (hopping
// logicalParentUuid at compact boundaries) to the closest human prompt — the
// text the repaired last-prompt trailer should carry.
func nearestUserTurnText(info *Info, uuid string) string {
	seen := make(map[string]bool)
	cur := uuid
	for cur != "" && !seen[cur] {
		seen[cur] = true
		e, ok := info.entry(cur)
		if !ok {
			return ""
		}
		if e.IsUserTurn() {
			return e.UserText()
		}
		if e.ParentUUID != "" {
			cur = e.ParentUUID
		} else {
			cur = e.LogicalParentUUID
		}
	}
	return ""
}

// TruncateFileAtTurn truncates src after the completing assistant response of
// the given 1-based human turn and writes the result to dst atomically
// (temp file + rename; no partial output on error).
func TruncateFileAtTurn(src, dst string, turn int) error {
	info, err := IndexFile(src)
	if err != nil {
		return err
	}
	turns := info.Turns()
	if len(turns) == 0 {
		return fmt.Errorf("%s: %w", src, ErrNoTurns)
	}
	if turn < 1 || turn > len(turns) {
		return fmt.Errorf("%s: turn %d out of range (transcript has %d turns)", src, turn, len(turns))
	}
	t := turns[turn-1]
	if t.ResponseLeafUUID == "" {
		return fmt.Errorf("%s: turn %d has no completed assistant response to cut at", src, turn)
	}
	return truncateFile(src, dst, info, t.ResponseLeafUUID)
}

// TruncateFileAtLeaf truncates src at an explicit tree-node uuid — the
// primitive behind U6's self-bottle trim, where the cut leaf is the last
// assistant entry whose tool_use dispatches all resolved.
func TruncateFileAtLeaf(src, dst, leafUUID string) error {
	info, err := IndexFile(src)
	if err != nil {
		return err
	}
	return truncateFile(src, dst, info, leafUUID)
}

func truncateFile(src, dst string, info *Info, leafUUID string) (err error) {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".bottle-truncate-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if err = Truncate(f, tmp, info, leafUUID); err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

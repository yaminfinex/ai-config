package sessionvitals

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"

	"ai-config/tools/herder/internal/claudesession"
	"ai-config/tools/herder/internal/codexsession"
	"ai-config/tools/herder/internal/sessionjsonl"
)

// This file holds the incremental primitives the observer uses. Seed is the
// direct read plus the complete-record end; Advance folds only the bytes
// appended since, through the same ObserveVitals functions. Neither is called
// by the CLI.

// Seed reads the vitals the direct path would return and the offset the tail
// continues from (the end of the last complete record).
func Seed(tool string, subagent bool, path string) (claudesession.Vitals, int64, error) {
	vitals, err := readVitals(tool, subagent, path)
	if err != nil {
		return claudesession.Vitals{}, 0, err
	}
	end, err := sessionjsonl.CompleteEnd(path)
	if err != nil {
		return claudesession.Vitals{}, 0, err
	}
	return vitals, end, nil
}

// Advance folds the complete records in [offset, CompleteEnd) into prior and
// returns the updated vitals, the new offset and the bytes read. A file
// shorter than offset is reported as truncated so the caller re-seeds. Later
// records override earlier ones, matching the reverse scan's "latest wins".
func Advance(tool string, subagent bool, path string, offset int64, prior claudesession.Vitals) (claudesession.Vitals, int64, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return prior, offset, 0, err
	}
	defer file.Close()
	end, err := sessionjsonl.CompleteEnd(path)
	if err != nil {
		return prior, offset, 0, err
	}
	if end < offset {
		return prior, offset, 0, ErrTruncated
	}
	if end == offset {
		return prior, offset, 0, nil
	}
	reader := bufio.NewReader(io.NewSectionReader(file, offset, end-offset))
	vitals := prior
	for {
		raw, err := reader.ReadBytes('\n')
		if err == io.EOF && len(raw) == 0 {
			break
		}
		if err != nil && err != io.EOF {
			return prior, offset, 0, err
		}
		body := bytes.TrimSuffix(bytes.TrimSuffix(raw, []byte{'\n'}), []byte{'\r'})
		var facts claudesession.Vitals
		if tool == "codex" {
			codexsession.ObserveVitals(body, &facts)
		} else {
			claudesession.ObserveVitals(body, &facts, subagent)
		}
		if facts.Model != "" {
			vitals.Model = facts.Model
		}
		if facts.ContextUsage != nil {
			vitals.ContextUsage = facts.ContextUsage
		}
	}
	return vitals, end, end - offset, nil
}

// ErrTruncated: the file is shorter than the tail offset; the caller re-seeds.
var ErrTruncated = fmt.Errorf("session tail: %s", claudesession.ResetTruncated)

// Package hcomidentity reads and decodes the live hcom roster.
package hcomidentity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"time"
)

const listTimeout = 5 * time.Second

type LaunchContext struct {
	PaneID                  string            `json:"pane_id"`
	ProcessID               string            `json:"process_id,omitempty"`
	TerminalID              string            `json:"terminal_id,omitempty"`
	TerminalPresetEffective string            `json:"terminal_preset_effective,omitempty"`
	TTY                     string            `json:"tty,omitempty"`
	GitBranch               string            `json:"git_branch,omitempty"`
	Env                     map[string]string `json:"env,omitempty"`
}

type Row struct {
	Name                 string        `json:"name"`
	BaseName             string        `json:"base_name,omitempty"`
	Tool                 string        `json:"tool"`
	Status               string        `json:"status"`
	Directory            string        `json:"directory,omitempty"`
	SessionID            string        `json:"session_id,omitempty"`
	ParentName           string        `json:"parent_name,omitempty"`
	AgentID              string        `json:"agent_id,omitempty"`
	TranscriptPath       string        `json:"transcript_path,omitempty"`
	LaunchContext        LaunchContext `json:"launch_context"`
	ParentAgent          string        `json:"-"`
	ParentSessionID      string        `json:"-"`
	ParentDirectory      string        `json:"-"`
	ParentTranscriptPath string        `json:"-"`
}

// Parent returns the one roster row explicitly named by a child row's
// parent_name. hcom records parent_name in base-name identity, so a match is
// accepted only when exactly one row exposes that exact base_name. Display
// names and tag/name patterns are never parent evidence.
func Parent(rows []Row, child Row) (Row, bool) {
	if child.ParentName == "" {
		return Row{}, false
	}
	var parent Row
	matches := 0
	for _, candidate := range rows {
		if candidate.BaseName == child.ParentName {
			parent = candidate
			matches++
		}
	}
	return parent, matches == 1
}

// WithParents returns a copy enriched only from Parent's exact, unique roster
// relationship. The internal fields let transcript readers use the parent's
// immutable session evidence without changing hcom's wire payload.
func WithParents(rows []Row) []Row {
	out := append([]Row(nil), rows...)
	for i := range out {
		parent, ok := Parent(rows, out[i])
		if !ok {
			continue
		}
		out[i].ParentAgent = parent.Name
		out[i].ParentSessionID = parent.SessionID
		out[i].ParentDirectory = parent.Directory
		out[i].ParentTranscriptPath = parent.TranscriptPath
	}
	return out
}

// List reads the live hcom roster.
func List() ([]Row, error) {
	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()
	return ListContext(ctx)
}

// ListContext reads the roster with a caller-controlled deadline.
func ListContext(ctx context.Context) ([]Row, error) {
	cmd := exec.CommandContext(ctx, "hcom", "list", "--json")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("hcom list --json timed out: %w", ctx.Err())
		}
		return nil, fmt.Errorf("hcom list --json failed: %w", err)
	}
	return Decode(out)
}

// Decode accepts both the array and JSONL roster formats emitted by hcom.
func Decode(raw []byte) ([]Row, error) {
	var rows []Row
	if err := json.Unmarshal(raw, &rows); err == nil {
		return WithParents(rows), nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	for {
		var row Row
		if err := decoder.Decode(&row); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 && len(bytes.TrimSpace(raw)) != 0 {
		return nil, fmt.Errorf("could not decode hcom roster")
	}
	return WithParents(rows), nil
}

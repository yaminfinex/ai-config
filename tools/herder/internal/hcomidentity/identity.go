// Package hcomidentity reads and decodes the live hcom roster.
package hcomidentity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const listTimeout = 5 * time.Second

var ErrStoppedNotFound = fmt.Errorf("stopped agent not found")

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
	Name           string        `json:"name"`
	BaseName       string        `json:"base_name,omitempty"`
	Tool           string        `json:"tool"`
	Status         string        `json:"status"`
	Directory      string        `json:"directory,omitempty"`
	SessionID      string        `json:"session_id,omitempty"`
	ParentName     string        `json:"parent_name,omitempty"`
	AgentID        string        `json:"agent_id,omitempty"`
	TranscriptPath string        `json:"transcript_path,omitempty"`
	Tag            string        `json:"tag,omitempty"`
	LaunchContext  LaunchContext `json:"launch_context"`
	// CreatedAt is hcom's creation time for this name (roster created_at,
	// numeric seconds; created accepted as an alias). Zero when the roster
	// carries neither. It is the identity incarnation of a reused name.
	CreatedAt            time.Time `json:"-"`
	ParentAgent          string    `json:"-"`
	ParentSessionID      string    `json:"parent_session_id,omitempty"`
	ParentDirectory      string    `json:"-"`
	ParentTranscriptPath string    `json:"-"`
}

// Parent returns the one roster row explicitly named by a child row's
// parent_name. hcom records parent_name in base-name identity, so a match is
// accepted only when exactly one row exposes that exact base_name. Display
// names and tag/name patterns are never parent evidence.
func Parent(rows []Row, child Row) (Row, bool) {
	if child.ParentName == "" {
		return Row{}, false
	}
	return ByUniqueBaseName(rows, child.ParentName)
}

// ByUniqueBaseName returns the roster row only when exactly one row exposes
// the supplied hcom base name. Names and tags are deliberately not evidence.
func ByUniqueBaseName(rows []Row, baseName string) (Row, bool) {
	var parent Row
	matches := 0
	for _, candidate := range rows {
		if candidate.BaseName == baseName {
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
		if out[i].ParentSessionID == "" {
			out[i].ParentSessionID = parent.SessionID
		}
		if out[i].ParentDirectory == "" {
			out[i].ParentDirectory = parent.Directory
		}
		if out[i].ParentTranscriptPath == "" {
			out[i].ParentTranscriptPath = parent.TranscriptPath
		}
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

// Stopped reads the newest retained lifecycle record that hcom can
// authoritatively resolve for name. The requested name remains the public
// identity: hcom's record prints the base name even when an exact tagged name
// was used for lookup.
func Stopped(name string) (Row, error) {
	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()
	return StoppedContext(ctx, name)
}

func StoppedContext(ctx context.Context, name string) (Row, error) {
	cmd := exec.CommandContext(ctx, "hcom", "list", "--stopped", name)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return Row{}, fmt.Errorf("hcom list --stopped timed out: %w", ctx.Err())
		}
		return Row{}, fmt.Errorf("hcom list --stopped failed: %w", err)
	}
	return DecodeStopped(name, out)
}

// DecodeStopped parses the labeled single-record shape emitted by
// `hcom list --stopped <name>`. It refuses incomplete or changed output rather
// than guessing transcript identity.
func DecodeStopped(requestedName string, raw []byte) (Row, error) {
	text := strings.TrimSpace(string(raw))
	if strings.HasPrefix(text, "No stopped events found for ") {
		return Row{}, ErrStoppedNotFound
	}
	lines := strings.Split(text, "\n")
	if requestedName == "" || len(lines) == 0 || !strings.HasPrefix(lines[0], "Stopped: ") {
		return Row{}, fmt.Errorf("could not decode hcom stopped agent")
	}
	baseName := strings.TrimSpace(strings.TrimPrefix(lines[0], "Stopped: "))
	fields := make(map[string]string)
	for _, line := range lines[1:] {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok {
			fields[key] = strings.TrimSpace(value)
		}
	}
	if baseName == "" || fields["Time"] == "" || fields["Tool"] == "" {
		return Row{}, fmt.Errorf("could not decode hcom stopped agent")
	}
	row := Row{
		Name: requestedName, BaseName: baseName, Tool: fields["Tool"], Status: "retired",
		Directory: fields["Directory"], SessionID: fields["Session"], TranscriptPath: fields["Transcript"],
	}
	row.AgentID, row.ParentSessionID = stoppedSubagentEvidence(row.Tool, row.TranscriptPath)
	return row, nil
}

var stoppedSubagentAgentID = regexp.MustCompile(`^[0-9a-f]{17}$`)
var stoppedSubagentParentSessionID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// stoppedSubagentEvidence recognizes only the Claude layout observed on disk:
// <parent-session>/subagents/agent-<17 lowercase hex>.jsonl. A shape mismatch
// retains the ordinary stopped row without inventing subagent identity.
func stoppedSubagentEvidence(tool, transcript string) (string, string) {
	if tool != "claude" || transcript == "" {
		return "", ""
	}
	base := filepath.Base(transcript)
	if filepath.Ext(base) != ".jsonl" {
		return "", ""
	}
	agentID := strings.TrimSuffix(strings.TrimPrefix(base, "agent-"), ".jsonl")
	subagents := filepath.Dir(transcript)
	parentSessionID := filepath.Base(filepath.Dir(subagents))
	if !strings.HasPrefix(base, "agent-") || filepath.Base(subagents) != "subagents" ||
		!stoppedSubagentAgentID.MatchString(agentID) || !stoppedSubagentParentSessionID.MatchString(parentSessionID) {
		return "", ""
	}
	return agentID, parentSessionID
}

// UnmarshalJSON decodes the wire row and hcom's creation time. The installed
// hcom exposes created_at as numeric seconds (e.g. 1788932670.0); created is
// accepted as an alias (number or RFC3339 string). Neither leaves CreatedAt zero.
func (r *Row) UnmarshalJSON(raw []byte) error {
	type plain Row
	var wire plain
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	var created struct {
		CreatedAt json.RawMessage `json:"created_at"`
		Created   json.RawMessage `json:"created"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		return err
	}
	*r = Row(wire)
	for _, field := range []json.RawMessage{created.CreatedAt, created.Created} {
		if at, ok := decodeCreated(field); ok {
			r.CreatedAt = at
			break
		}
	}
	return nil
}

func decodeCreated(raw json.RawMessage) (time.Time, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, false
	}
	var seconds float64
	if err := json.Unmarshal(raw, &seconds); err == nil {
		if seconds <= 0 {
			return time.Time{}, false
		}
		whole := int64(seconds)
		return time.Unix(whole, int64((seconds-float64(whole))*1e9)).UTC(), true
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if at, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return at.UTC(), true
		}
		if seconds, err := strconv.ParseFloat(text, 64); err == nil && seconds > 0 {
			whole := int64(seconds)
			return time.Unix(whole, int64((seconds-float64(whole))*1e9)).UTC(), true
		}
	}
	return time.Time{}, false
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

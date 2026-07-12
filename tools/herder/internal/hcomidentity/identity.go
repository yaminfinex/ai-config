// Package hcomidentity resolves bus names only from live hcom roster evidence.
package hcomidentity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type LaunchContext struct {
	PaneID    string `json:"pane_id"`
	ProcessID string `json:"process_id"`
}

type Row struct {
	Name          string        `json:"name"`
	Status        string        `json:"status"`
	Joined        *bool         `json:"joined,omitempty"`
	SessionID     string        `json:"session_id"`
	ProcessBound  *bool         `json:"process_bound,omitempty"`
	StatusAge     int           `json:"status_age"`
	LaunchContext LaunchContext `json:"launch_context"`
}

type Evidence struct {
	SessionID string
	ProcessID string
	PaneID    string
}

type Result struct {
	Name      string
	SessionID string
	Verified  bool
	Reason    string
}

// CurrentEvidence returns durable live-row correlates for the calling process.
// HCOM_INSTANCE_NAME is deliberately excluded: a launch-time name is not proof.
func CurrentEvidence(paneID string) Evidence {
	return Evidence{
		SessionID: os.Getenv("HCOM_SESSION_ID"),
		ProcessID: os.Getenv("HCOM_PROCESS_ID"),
		PaneID:    paneID,
	}
}

// List reads the live hcom roster in the requested namespace.
func List(dir string) ([]Row, error) {
	cmd := exec.Command("hcom", "list", "--json")
	cmd.Env = os.Environ()
	if dir != "" && dir != "null" {
		cmd.Env = setEnv(cmd.Env, "HCOM_DIR", dir)
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("hcom list --json failed: %w", err)
	}
	return Decode(out)
}

// Decode accepts both the array and JSONL roster formats emitted by hcom.
func Decode(raw []byte) ([]Row, error) {
	var rows []Row
	if err := json.Unmarshal(raw, &rows); err == nil {
		return rows, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	for {
		var row Row
		if err := dec.Decode(&row); err != nil {
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
	return rows, nil
}

// Resolve proves one live bus row from independent session, process, or pane
// correlates. Conflicting correlates fail closed instead of choosing a winner.
func Resolve(rows []Row, evidence Evidence) Result {
	type signal struct {
		label string
		value string
		match func(Row) bool
	}
	signals := []signal{
		{"session_id", evidence.SessionID, func(row Row) bool { return row.SessionID == evidence.SessionID }},
		{"process_id", evidence.ProcessID, func(row Row) bool { return row.LaunchContext.ProcessID == evidence.ProcessID }},
		{"pane_id", evidence.PaneID, func(row Row) bool { return row.LaunchContext.PaneID == evidence.PaneID }},
	}
	matched := map[string]Row{}
	used := 0
	for _, sig := range signals {
		if sig.value == "" {
			continue
		}
		used++
		perSignal := map[string]Row{}
		for _, row := range rows {
			if row.Name != "" && joined(row) && sig.match(row) {
				perSignal[row.Name] = row
			}
		}
		if len(perSignal) > 1 {
			return Result{Reason: sig.label + " matches multiple joined bus rows"}
		}
		for name, row := range perSignal {
			matched[name] = row
		}
	}
	if used == 0 {
		return Result{Reason: "no session, process, or pane correlate is available"}
	}
	if len(matched) == 0 {
		return Result{Reason: "no joined bus row matches the calling session, process, or pane"}
	}
	if len(matched) > 1 {
		return Result{Reason: "live identity correlates resolve to different bus rows"}
	}
	for name, row := range matched {
		return Result{Name: name, SessionID: row.SessionID, Verified: true}
	}
	return Result{Reason: "live bus identity is unknown"}
}

func ResolveLive(dir string, evidence Evidence) Result {
	rows, err := List(dir)
	if err != nil {
		return Result{Reason: err.Error()}
	}
	return Resolve(rows, evidence)
}

func VerifyStored(rows []Row, evidence Evidence, stored string) (bool, Result) {
	resolved := Resolve(rows, evidence)
	return resolved.Verified && stored != "" && stored == resolved.Name, resolved
}

func joined(row Row) bool {
	if row.Joined != nil && !*row.Joined {
		return false
	}
	switch strings.ToLower(row.Status) {
	case "inactive", "stopped", "closed", "dead":
		return false
	default:
		return true
	}
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, item := range env {
		if !strings.HasPrefix(item, prefix) {
			out = append(out, item)
		}
	}
	return append(out, prefix+value)
}

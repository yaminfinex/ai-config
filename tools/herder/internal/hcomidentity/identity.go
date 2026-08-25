// Package hcomidentity reads and decodes the live hcom roster.
package hcomidentity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
)

type LaunchContext struct {
	PaneID string `json:"pane_id"`
}

type Row struct {
	Name          string        `json:"name"`
	Tool          string        `json:"tool"`
	Status        string        `json:"status"`
	LaunchContext LaunchContext `json:"launch_context"`
}

// List reads the live hcom roster.
func List() ([]Row, error) {
	cmd := exec.Command("hcom", "list", "--json")
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
	return rows, nil
}

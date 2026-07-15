package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
)

// Roster data comes from `herder list --json`, read-only. Rows are parsed
// defensively (map[string]any) so mc never breaks on registry additions.

type HerderRow struct {
	Label   string
	GUID    string
	Agent   string
	Role    string
	Status  string
	Cwd     string
	Branch  string
	SIDs    []string // tool session ids — the reliable join to hcom rows
	Mission *HerderMission
}

type HerderMission struct {
	Slug   string
	Source string
}

func HerderList(bin string) ([]HerderRow, error) {
	out, err := exec.Command(bin, "list", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("herder list: %w", err)
	}
	// Output is NDJSON: one session object per line.
	var rows []HerderRow
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var r map[string]any
		if json.Unmarshal(line, &r) != nil {
			continue
		}
		if k := str(r, "kind"); k != "" && k != "session" {
			continue
		}
		row := HerderRow{
			Label:  str(r, "label"),
			GUID:   str(r, "guid"),
			Agent:  str(r, "tool"),
			Role:   str(r, "role"),
			Status: str(r, "state"),
			Branch: str(r, "branch"),
		}
		if prov, ok := r["provenance"].(map[string]any); ok {
			row.Cwd = str(prov, "cwd")
			if row.Branch == "" {
				row.Branch = str(prov, "branch")
			}
			if sid := str(prov, "tool_session_id"); sid != "" {
				row.SIDs = append(row.SIDs, sid)
			}
		}
		if sids, ok := r["sids"].([]any); ok {
			for _, e := range sids {
				if m, ok := e.(map[string]any); ok {
					if sid := str(m, "sid"); sid != "" && !contains(row.SIDs, sid) {
						row.SIDs = append(row.SIDs, sid)
					}
				}
			}
		}
		if row.Cwd == "" {
			row.Cwd = str(r, "cwd")
		}
		if mission, ok := r["mission"].(map[string]any); ok {
			row.Mission = &HerderMission{
				Slug:   str(mission, "slug"),
				Source: str(mission, "source"),
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func str(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

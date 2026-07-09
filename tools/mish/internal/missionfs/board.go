package missionfs

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

var pinnedBoardConfig = map[string]any{
	"check_active_branches": false,
	"remote_operations":     false,
	"auto_commit":           false,
	"auto_open_browser":     false,
	"filesystem_only":       true,
}

// BoardConfig contains the board settings mish reads directly.
type BoardConfig struct {
	ProjectName string
	Statuses    []string
	Values      map[string]any
}

// ReadBoardConfig reads backlog/config.yml and reports drift from mish's pinned keys.
func ReadBoardConfig(boardDir string) (BoardConfig, []Finding, error) {
	path := filepath.Join(boardDir, "config.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		return BoardConfig{}, nil, err
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return BoardConfig{}, nil, fmt.Errorf("parse board config: %w", err)
	}
	cfg := BoardConfig{
		ProjectName: stringValue(raw["project_name"]),
		Statuses:    stringSlice(raw["statuses"]),
		Values:      raw,
	}
	var findings []Finding
	for key, expected := range pinnedBoardConfig {
		actual, ok := raw[key]
		if !ok || actual != expected {
			findings = append(findings, Finding{
				Kind:     FindingBoardPinDrift,
				Key:      key,
				Expected: fmt.Sprint(expected),
				Actual:   fmt.Sprint(actual),
				Path:     path,
			})
		}
	}
	return cfg, findings, nil
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func stringSlice(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

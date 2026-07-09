package missionfs

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed testdata/real-backlog-1.47.1/backlog/config.yml
var realBacklogConfigTemplate string

var pinnedBoardConfig = map[string]any{
	"check_active_branches": false,
	"remote_operations":     false,
	"auto_commit":           false,
	"auto_open_browser":     false,
	"filesystem_only":       true,
}

// WriteBoard writes the scaffolded nested Backlog.md board from the real-cut
// Backlog.md fixture, then stamps the mission-specific project name.
func WriteBoard(boardDir string, slug string) error {
	if err := os.MkdirAll(filepath.Join(boardDir, "tasks"), 0o755); err != nil {
		return err
	}
	config := replaceProjectName(realBacklogConfigTemplate, slug)
	return os.WriteFile(filepath.Join(boardDir, "config.yml"), []byte(config), 0o644)
}

// BoardConfig contains the board settings mish reads directly.
type BoardConfig struct {
	ProjectName string
	Statuses    []string
	Values      map[string]any
	Missing     bool
}

// ReadBoardConfig reads backlog/config.yml and reports drift from mish's pinned keys.
func ReadBoardConfig(boardDir string) (BoardConfig, []Finding, error) {
	path := filepath.Join(boardDir, "config.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return BoardConfig{Missing: true}, []Finding{{
				Kind: FindingMissingBoard,
				Path: path,
			}}, nil
		}
		return BoardConfig{}, nil, err
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return BoardConfig{}, []Finding{{
			Kind:   FindingMalformedBoardConfig,
			Path:   path,
			Actual: fmt.Sprintf("parse board config: %v", err),
		}}, nil
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

func replaceProjectName(config, slug string) string {
	lines := strings.Split(config, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "project_name:") {
			lines[i] = fmt.Sprintf("project_name: %q", slug)
			break
		}
	}
	return strings.Join(lines, "\n")
}

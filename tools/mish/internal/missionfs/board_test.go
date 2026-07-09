package missionfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadBoardConfigDetectsEachPinnedDrift(t *testing.T) {
	for key, drifted := range map[string]string{
		"check_active_branches": "true",
		"remote_operations":     "true",
		"auto_commit":           "true",
		"auto_open_browser":     "true",
		"filesystem_only":       "false",
	} {
		t.Run(key, func(t *testing.T) {
			boardDir := testBoardDir(t)
			path := filepath.Join(boardDir, "config.yml")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = bytesReplaceLine(data, key+": ", key+": "+drifted)
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}

			cfg, findings, err := ReadBoardConfig(boardDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.Statuses) == 0 {
				t.Fatalf("statuses were not read from config")
			}
			assertFinding(t, findings, FindingBoardPinDrift, key)
		})
	}
}

func TestReadBoardConfigPreservesConfiguredStatusOrder(t *testing.T) {
	boardDir := testBoardDir(t)
	path := filepath.Join(boardDir, "config.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytesReplaceLine(data, `statuses: `, `statuses: ["Queued", "Doing", "Validated"]`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := ReadBoardConfig(boardDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Queued", "Doing", "Validated"}
	if !equalStrings(cfg.Statuses, want) {
		t.Fatalf("statuses = %v, want %v", cfg.Statuses, want)
	}
}

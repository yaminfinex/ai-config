package missionfs

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteBoardUsesFixtureByteShapeAndStampsProjectName(t *testing.T) {
	boardDir := filepath.Join(t.TempDir(), "backlog")
	if err := WriteBoard(boardDir, "ops-handoff"); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(boardDir, "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	want := bytesReplaceLine([]byte(realBacklogConfigTemplate), `project_name: `, `project_name: "ops-handoff"`)
	if !bytes.Equal(got, want) {
		t.Fatalf("written config does not match fixture byte shape with stamped project_name\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	info, err := os.Stat(filepath.Join(boardDir, "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("tasks path is not a directory")
	}
}

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

func TestReadBoardConfigMissingConfigReturnsTypedFinding(t *testing.T) {
	cfg, findings, err := ReadBoardConfig(filepath.Join(t.TempDir(), "backlog"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Missing {
		t.Fatalf("Missing = false, want true")
	}
	assertFinding(t, findings, FindingMissingBoard, "")
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

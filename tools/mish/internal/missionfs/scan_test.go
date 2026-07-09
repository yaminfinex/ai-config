package missionfs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanTasksCountsTasksAndCompletedButExcludesDrafts(t *testing.T) {
	boardDir := testBoardDir(t)
	writeTask(t, filepath.Join(boardDir, "tasks", "task-1.md"), "TASK-1", "To Do")
	writeTask(t, filepath.Join(boardDir, "completed", "task-2.md"), "TASK-2", "Done")
	writeTask(t, filepath.Join(boardDir, "drafts", "task-3.md"), "TASK-3", "To Do")

	scan, err := ScanTasks(boardDir)
	if err != nil {
		t.Fatal(err)
	}
	if scan.Counts["To Do"] != 1 {
		t.Fatalf("To Do count = %d, want 1", scan.Counts["To Do"])
	}
	if scan.Counts["Done"] != 1 {
		t.Fatalf("Done count = %d, want 1", scan.Counts["Done"])
	}
}

func TestScanTasksDetectsDuplicateIDsAcrossScannedDirs(t *testing.T) {
	boardDir := testBoardDir(t)
	writeTask(t, filepath.Join(boardDir, "tasks", "task-1.md"), "TASK-1", "To Do")
	writeTask(t, filepath.Join(boardDir, "completed", "task-1-copy.md"), "TASK-1", "Done")

	scan, err := ScanTasks(boardDir)
	if err != nil {
		t.Fatal(err)
	}
	assertFinding(t, scan.Findings, FindingDuplicateTaskID, "id")
}

func TestOrderedCountsFollowConfigStatusOrder(t *testing.T) {
	scan := TaskScan{Counts: map[string]int{"Validated": 2, "Queued": 1}}
	got := scan.OrderedCounts([]string{"Queued", "Doing", "Validated"})
	want := []StatusCount{
		{Status: "Queued", Count: 1},
		{Status: "Doing", Count: 0},
		{Status: "Validated", Count: 2},
	}
	if len(got) != len(want) {
		t.Fatalf("len(OrderedCounts) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("OrderedCounts[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestScanArtifactsMissingDirReportsMissingWithoutError(t *testing.T) {
	scan, err := ScanArtifacts(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	if !scan.Missing {
		t.Fatalf("Missing = false, want true")
	}
}

func TestScanArtifactsCountsFilesAndNewestMtime(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "artifacts")
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(dir, "old.txt")
	newPath := filepath.Join(dir, "nested", "new.txt")
	writeFile(t, oldPath, "old")
	writeFile(t, newPath, "new")
	oldTime := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatal(err)
	}

	scan, err := ScanArtifacts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if scan.Count != 2 {
		t.Fatalf("Count = %d, want 2", scan.Count)
	}
	if !scan.NewestTime.Equal(newTime) {
		t.Fatalf("NewestTime = %s, want %s", scan.NewestTime, newTime)
	}
}

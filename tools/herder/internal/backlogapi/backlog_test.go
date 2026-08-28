package backlogapi

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const fixtureConfig = `project_name: fixture
statuses: ["To Do", "In Progress", "Done"]
unknown_future_key:
  nested: true
`

func TestReadParsesHarvestedRealFrontmatterShapes(t *testing.T) {
	root := t.TempDir()
	writeBoard(t, root, "backlog", fixtureConfig)
	fixtures, err := filepath.Glob("testdata/real/*-task-*.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) != 8 {
		t.Fatalf("harvested fixtures=%d want 8", len(fixtures))
	}
	for _, fixture := range fixtures {
		data, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatal(err)
		}
		writeTask(t, root, "backlog", filepath.Base(fixture), string(data))
	}
	fetched := time.Date(2026, 8, 28, 12, 34, 56, 731, time.UTC)
	result, err := Read(root, "backlog", func() time.Time { return fetched })
	if err != nil {
		t.Fatal(err)
	}
	if result.Backlog != nil || result.Statuses == nil || !reflect.DeepEqual(*result.Statuses, []string{"To Do", "In Progress", "Done"}) {
		t.Fatalf("result availability/statuses=%#v", result)
	}
	if result.Tasks == nil || len(*result.Tasks) != 8 || result.Unparsed == nil || len(*result.Unparsed) != 0 || result.Truncated == nil || *result.Truncated {
		t.Fatalf("result counts=%#v", result)
	}
	if !result.FetchedAt.Equal(fetched) {
		t.Fatalf("fetched_at=%v", result.FetchedAt)
	}
	byID := make(map[string]Task)
	for _, task := range *result.Tasks {
		byID[task.ID] = task
	}
	if !strings.Contains(byID["TASK-034"].Title, "market→collateral") {
		t.Fatalf("folded Unicode title=%q", byID["TASK-034"].Title)
	}
	if !strings.Contains(byID["TASK-232"].Title, "v0.1.17: silent") || byID["TASK-232"].Labels == nil || !reflect.DeepEqual(*byID["TASK-232"].Labels, []string{"sesh"}) {
		t.Fatalf("quoted title/block labels=%#v", byID["TASK-232"])
	}
	if byID["TASK-003"].Labels == nil || !reflect.DeepEqual(*byID["TASK-003"].Labels, []string{"single-sequencer", "harness", "performance", "latency", "instrument"}) {
		t.Fatalf("block labels=%#v", byID["TASK-003"].Labels)
	}
	if byID["TASK-039"].Priority != "medium" || byID["TASK-25"].Priority != "high" {
		t.Fatalf("priorities=%q/%q", byID["TASK-039"].Priority, byID["TASK-25"].Priority)
	}
}

func TestReadQuarantinesWholeMalformedFrontmatterAndKeepsMissingFields(t *testing.T) {
	root := t.TempDir()
	writeBoard(t, root, "board", fixtureConfig)
	writeTask(t, root, "board", "001-unicode.md", "---\nid: SAME\ntitle: 'Здравствуйте — board'\nordinal: 2\nlabels: []\n---\nbody must not be served\n")
	writeTask(t, root, "board", "002-duplicate.md", "---\nid: SAME\ntitle: duplicate id\nstatus: To Do\nordinal: 1\n---\n")
	writeTask(t, root, "board", "003-bad-type.md", "---\nid: BAD\ntitle: malformed\nstatus: To Do\nordinal: nope\n---\n")
	writeTask(t, root, "board", "004-unclosed.md", "---\nid: OPEN\ntitle: never closed\n")
	writeTask(t, root, "board", "005-oversize.md", "---\ntitle: "+strings.Repeat("x", int(FrontmatterCap))+"\n")
	writeTask(t, root, "board", "ignore.txt", "not a task")

	result, err := Read(root, "board", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tasks == nil || len(*result.Tasks) != 2 {
		t.Fatalf("tasks=%#v", result.Tasks)
	}
	if (*result.Tasks)[0].ID != "SAME" || (*result.Tasks)[1].ID != "SAME" {
		t.Fatalf("duplicate ids were changed or merged: %#v", *result.Tasks)
	}
	var missingStatus Task
	for _, task := range *result.Tasks {
		if strings.Contains(task.Title, "Здравствуйте") {
			missingStatus = task
		}
		if strings.Contains(task.File, "body") {
			t.Fatalf("task body leaked into projection: %#v", task)
		}
	}
	if missingStatus.Status != "" {
		t.Fatalf("missing status fabricated: %#v", missingStatus)
	}
	if result.Unparsed == nil || len(*result.Unparsed) != 3 {
		t.Fatalf("unparsed=%#v", result.Unparsed)
	}
	reasons := fmt.Sprint(*result.Unparsed)
	for _, want := range []string{"parse task frontmatter", "not closed", "exceeds 65536-byte cap"} {
		if !strings.Contains(reasons, want) {
			t.Errorf("unparsed reasons missing %q: %s", want, reasons)
		}
	}
}

func TestReadCapsSelectedTaskFilesDeterministically(t *testing.T) {
	root := t.TempDir()
	writeBoard(t, root, "board", fixtureConfig)
	for index := 0; index < 3000; index++ {
		writeTask(t, root, "board", fmt.Sprintf("task-%04d.md", index), fmt.Sprintf("---\nid: TASK-%04d\ntitle: task %04d\nstatus: To Do\nordinal: %d\n---\n", index, index, index))
	}
	result, err := Read(root, "board", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Truncated == nil || !*result.Truncated || result.Tasks == nil || len(*result.Tasks) != TaskCap {
		t.Fatalf("truncation result=%#v task count=%d", result.Truncated, len(*result.Tasks))
	}
	if (*result.Tasks)[0].ID != "TASK-0000" || (*result.Tasks)[TaskCap-1].ID != "TASK-1999" {
		t.Fatalf("deterministic selected bounds=%q..%q", (*result.Tasks)[0].ID, (*result.Tasks)[TaskCap-1].ID)
	}
}

func TestReadEmptyBoardReturnsExplicitEmptyCollections(t *testing.T) {
	root := t.TempDir()
	writeBoard(t, root, "empty", fixtureConfig)
	result, err := Read(root, "empty", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tasks == nil || *result.Tasks == nil || len(*result.Tasks) != 0 || result.Unparsed == nil || *result.Unparsed == nil || len(*result.Unparsed) != 0 || result.Truncated == nil || *result.Truncated {
		t.Fatalf("empty board collections=%#v", result)
	}
}

func TestReadReturnsHonestUnavailableForNonBoardDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "config-only"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config-only", "config.yml"), []byte(fixtureConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path   string
		reason string
	}{
		{"plain", "config.yml"},
		{"config-only", "tasks/"},
	} {
		result, err := Read(root, test.path, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		if result.Backlog == nil || result.Backlog.Status != "unavailable" || !strings.Contains(result.Backlog.Reason, test.reason) || result.Statuses != nil || result.Tasks != nil {
			t.Fatalf("%s unavailable=%#v", test.path, result)
		}
	}
}

func writeBoard(t *testing.T, root, relative, config string) {
	t.Helper()
	board := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Join(board, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(board, "config.yml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTask(t *testing.T, root, board, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(board), "tasks", name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

package missionfs

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func testBoardDir(t *testing.T) string {
	t.Helper()
	src := filepath.Join("testdata", "real-backlog-1.47.1", "backlog")
	dst := filepath.Join(t.TempDir(), "backlog")
	if err := copyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	return dst
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func writeTask(t *testing.T, path, id, status string) {
	t.Helper()
	fixture := filepath.Join("testdata", "real-backlog-1.47.1", "task-files", "task-1 - First-task.md")
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	data = bytesReplaceLine(data, "id: ", "id: "+id)
	data = bytesReplaceLine(data, "status: ", "status: "+status)
	writeFile(t, path, string(data))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFinding(t *testing.T, findings []Finding, kind FindingKind, key string) {
	t.Helper()
	for _, finding := range findings {
		if finding.Kind == kind && finding.Key == key {
			return
		}
	}
	t.Fatalf("finding (%s, %s) not found in %#v", kind, key, findings)
}

func bytesReplaceLine(data []byte, prefix, replacement string) []byte {
	lines := bytes.Split(data, []byte("\n"))
	for i, line := range lines {
		if bytes.HasPrefix(line, []byte(prefix)) {
			lines[i] = []byte(replacement)
			break
		}
	}
	return bytes.Join(lines, []byte("\n"))
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

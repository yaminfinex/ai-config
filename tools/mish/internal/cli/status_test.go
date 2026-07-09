package cli

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStatusSingleMissionHappyBlock(t *testing.T) {
	repo, missionDir := makeStatusMission(t, "perf-regression")
	writeTaskFile(t, missionDir, "tasks/task-1.md", "TASK-1", "To Do")
	writeTaskFile(t, missionDir, "tasks/task-2.md", "TASK-2", "In Progress")
	writeTaskFile(t, missionDir, "completed/task-3.md", "TASK-3", "Done")
	artifact := filepath.Join(missionDir, "artifacts", "analysis", "flamegraph-0708.html")
	writeFile(t, artifact, "flamegraph")
	if err := os.Chtimes(artifact, testNow().Add(-2*time.Hour), testNow().Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := executeStatus(t, statusTestDeps(repo, missionDir), "status")
	if err != nil {
		t.Fatalf("status error: %v\nstderr=%s", err, stderr)
	}
	for _, want := range []string{
		"mission: perf-regression",
		"active",
		"authority: hera",
		"owner: riley",
		"created 2026-07-08",
		"board:   1 To Do · 1 In Progress · 1 Done   (3 tasks)",
		"artifacts: 1 file · newest analysis/flamegraph-0708.html (2h ago)",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("status output missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "warning:") {
		t.Fatalf("happy status emitted warning:\n%s", stdout)
	}
}

func TestStatusWarnings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, missionDir string)
		want   string
	}{
		{
			name: "pinned key drift",
			mutate: func(t *testing.T, missionDir string) {
				replaceInFile(t, filepath.Join(missionDir, "backlog", "config.yml"), "auto_commit: false", "auto_commit: true")
			},
			want: "warning: pinned board key drift: auto_commit expected false got true",
		},
		{
			name: "mission dirname mismatch",
			mutate: func(t *testing.T, missionDir string) {
				replaceInFile(t, filepath.Join(missionDir, "mission.md"), "mission: perf-regression", "mission: other")
			},
			want: "warning: mission frontmatter \"other\" does not match directory \"perf-regression\"",
		},
		{
			name: "unknown frontmatter key",
			mutate: func(t *testing.T, missionDir string) {
				replaceInFile(t, filepath.Join(missionDir, "mission.md"), "created: 2026-07-08", "created: 2026-07-08\nextra: yes")
			},
			want: "warning: unknown mission.md frontmatter key: extra",
		},
		{
			name: "invalid mission status",
			mutate: func(t *testing.T, missionDir string) {
				replaceInFile(t, filepath.Join(missionDir, "mission.md"), "status: active", "status: paused")
			},
			want: "warning: invalid mission status \"paused\" (expected active or closed)",
		},
		{
			name: "duplicate task ID",
			mutate: func(t *testing.T, missionDir string) {
				writeTaskFile(t, missionDir, "tasks/task-1.md", "TASK-1", "To Do")
				writeTaskFile(t, missionDir, "completed/task-1-copy.md", "TASK-1", "Done")
			},
			want: "warning: duplicate task ID TASK-1:",
		},
		{
			name: "missing board",
			mutate: func(t *testing.T, missionDir string) {
				if err := os.Remove(filepath.Join(missionDir, "backlog", "config.yml")); err != nil {
					t.Fatal(err)
				}
			},
			want: "warning: board missing: backlog/config.yml",
		},
		{
			name: "missing artifacts",
			mutate: func(t *testing.T, missionDir string) {
				if err := os.RemoveAll(filepath.Join(missionDir, "artifacts")); err != nil {
					t.Fatal(err)
				}
			},
			want: "warning: artifacts missing: artifacts/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, missionDir := makeStatusMission(t, "perf-regression")
			tt.mutate(t, missionDir)

			stdout, stderr, err := executeStatus(t, statusTestDeps(repo, missionDir), "status")
			if err != nil {
				t.Fatalf("status error: %v\nstderr=%s", err, stderr)
			}
			if !strings.Contains(stdout, tt.want) {
				t.Fatalf("status output missing warning %q:\n%s", tt.want, stdout)
			}
			for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
				if strings.HasPrefix(line, "warning:") && strings.Contains(line, "\n") {
					t.Fatalf("warning was not one line: %q", line)
				}
			}
		})
	}
}

func TestStatusStalenessWarningUsesReadOnlyGitSeam(t *testing.T) {
	repo, missionDir := makeStatusMission(t, "perf-regression")
	var calls [][]string
	d := statusTestDeps(repo, missionDir)
	d.git = func(args []string, dir string) ([]byte, error) {
		if dir != repo {
			t.Fatalf("git dir = %s, want repo %s", dir, repo)
		}
		calls = append(calls, append([]string(nil), args...))
		switch strings.Join(args, " ") {
		case "rev-parse --is-inside-work-tree":
			return []byte("true\n"), nil
		case "rev-parse --abbrev-ref --symbolic-full-name @{u}":
			return []byte("origin/main\n"), nil
		case "status --porcelain -- missions/perf-regression":
			return []byte(" M missions/perf-regression/mission.md\n"), nil
		default:
			return nil, fmt.Errorf("unexpected git args: %v", args)
		}
	}

	stdout, stderr, err := executeStatus(t, d, "status")
	if err != nil {
		t.Fatalf("status error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "warning: mission subtree has uncommitted or unpushed changes") {
		t.Fatalf("staleness warning missing:\n%s", stdout)
	}
	for _, call := range calls {
		if len(call) > 0 && (call[0] == "add" || call[0] == "commit" || call[0] == "push") {
			t.Fatalf("status used mutating git command: %v", call)
		}
	}
}

func TestStatusStalenessSkippedWhenRepoIsNotGit(t *testing.T) {
	repo, missionDir := makeStatusMission(t, "perf-regression")
	d := statusTestDeps(repo, missionDir)
	d.git = func(args []string, dir string) ([]byte, error) {
		if reflect.DeepEqual(args, []string{"rev-parse", "--is-inside-work-tree"}) {
			return nil, errors.New("not git")
		}
		t.Fatalf("git should stop after non-git probe, got %v", args)
		return nil, nil
	}

	stdout, stderr, err := executeStatus(t, d, "status")
	if err != nil {
		t.Fatalf("status error: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout, "uncommitted or unpushed") {
		t.Fatalf("non-git repo emitted staleness warning:\n%s", stdout)
	}
}

func TestStatusDoesNotMutateMissionSubtree(t *testing.T) {
	repo, missionDir := makeStatusMission(t, "perf-regression")
	writeTaskFile(t, missionDir, "tasks/task-1.md", "TASK-1", "To Do")
	before := hashTree(t, missionDir)

	_, stderr, err := executeStatus(t, statusTestDeps(repo, missionDir), "status")
	if err != nil {
		t.Fatalf("status error: %v\nstderr=%s", err, stderr)
	}
	after := hashTree(t, missionDir)
	if before != after {
		t.Fatalf("mission subtree hash changed: before %x after %x", before, after)
	}
}

func TestStatusCountsFollowBoardConfiguredOrder(t *testing.T) {
	repo, missionDir := makeStatusMission(t, "custom-board")
	replaceInFile(t, filepath.Join(missionDir, "backlog", "config.yml"),
		`statuses: ["To Do", "In Progress", "Done"]`,
		`statuses: ["Queued", "Doing", "Validated"]`,
	)
	writeTaskFile(t, missionDir, "tasks/task-1.md", "TASK-1", "Validated")
	writeTaskFile(t, missionDir, "tasks/task-2.md", "TASK-2", "Queued")

	stdout, stderr, err := executeStatus(t, statusTestDeps(repo, missionDir), "status")
	if err != nil {
		t.Fatalf("status error: %v\nstderr=%s", err, stderr)
	}
	want := "board:   1 Queued · 0 Doing · 1 Validated   (2 tasks)"
	if !strings.Contains(stdout, want) {
		t.Fatalf("status output missing custom order %q:\n%s", want, stdout)
	}
}

func TestStatusMissionFlagMissingRefusesBeforeOutput(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "missions"), 0o755); err != nil {
		t.Fatal(err)
	}
	d := statusTestDeps(repo, repo)

	stdout, stderr, err := executeStatus(t, d, "status", "--mission", "missing")
	if err == nil {
		t.Fatalf("status unexpectedly succeeded")
	}
	if stdout != "" {
		t.Fatalf("missing mission wrote partial stdout:\n%s", stdout)
	}
	for _, want := range []string{"mish status: mission missing not found", "check the slug or create the mission"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

func executeStatus(t *testing.T, d deps, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	d.stdout = &stdout
	d.stderr = &stderr
	root := newRoot(d)
	root.SetArgs(args)
	err := root.Execute()
	if err != nil {
		var refusal refusalError
		if errors.As(err, &refusal) {
			fmt.Fprintln(&stderr, err)
		}
	}
	return stdout.String(), stderr.String(), err
}

func statusTestDeps(repo, cwd string) deps {
	d := testDeps()
	d.cwd = func() (string, error) { return cwd, nil }
	d.clock = testNow
	d.missionsRepo = repo
	d.git = func([]string, string) ([]byte, error) { return nil, errors.New("not git") }
	return d
}

func testNow() time.Time {
	return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
}

func makeStatusMission(t *testing.T, slug string) (string, string) {
	t.Helper()
	repo := t.TempDir()
	missionDir := filepath.Join(repo, "missions", slug)
	for _, dir := range []string{
		filepath.Join(missionDir, "backlog", "tasks"),
		filepath.Join(missionDir, "backlog", "completed"),
		filepath.Join(missionDir, "artifacts"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(missionDir, "mission.md"), fmt.Sprintf(`---
mission: %s
authority: hera
owner: riley
status: active
created: 2026-07-08
---

# %s
`, slug, slug))
	writeFile(t, filepath.Join(missionDir, "backlog", "config.yml"), fmt.Sprintf(`project_name: "%s"
default_status: "To Do"
statuses: ["To Do", "In Progress", "Done"]
labels: []
auto_open_browser: false
remote_operations: false
auto_commit: false
filesystem_only: true
check_active_branches: false
`, slug))
	return repo, missionDir
}

func writeTaskFile(t *testing.T, missionDir, rel, id, status string) {
	t.Helper()
	writeFile(t, filepath.Join(missionDir, "backlog", rel), fmt.Sprintf(`---
id: %s
status: %s
---

# %s
`, id, status, id))
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

func replaceInFile(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(data), old, new, 1)
	if text == string(data) {
		t.Fatalf("%s did not contain %q", path, old)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hashTree(t *testing.T, root string) string {
	t.Helper()
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%s\n", filepath.ToSlash(rel))
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hash.Write(data)
		_, _ = io.WriteString(hash, "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

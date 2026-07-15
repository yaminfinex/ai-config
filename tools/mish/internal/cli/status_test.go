package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mish/internal/resolve"
)

func TestStatusDefaultsToMissionPageJSON(t *testing.T) {
	repo, missionDir := makeStatusMission(t, "perf-regression")
	writeFile(t, filepath.Join(missionDir, "backlog", "tasks", "task-7.md"), `---
id: TASK-7
title: Find hot path
status: In Progress
ordinal: 7000
labels: [performance, urgent]
---

# Find hot path
`)
	stdout, stderr, err := executeStatus(t, statusTestDeps(repo, missionDir), "status")
	if err != nil || stderr != "" {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	var got statusOutput
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !got.OK || got.Slug != "perf-regression" || got.MissionDir != missionDir || got.Manifest.Authority != "hera" {
		t.Fatalf("status identity = %+v", got)
	}
	if got.Board.Total != 1 || len(got.Board.Tasks) != 1 {
		t.Fatalf("status board = %+v", got.Board)
	}
	task := got.Board.Tasks[0]
	if task.ID != "TASK-7" || task.Title != "Find hot path" || task.Status != "In Progress" || task.Ordinal != 7000 || strings.Join(task.Labels, ",") != "performance,urgent" {
		t.Fatalf("status task = %+v", task)
	}
}

func TestStatusAllDefaultsToArrayOfMissionObjects(t *testing.T) {
	repo, _ := makeStatusMission(t, "alpha")
	addStatusMission(t, repo, "beta")
	stdout, stderr, err := executeStatus(t, statusTestDeps(repo, repo), "status", "--all")
	if err != nil || stderr != "" {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	var got []statusOutput
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON array: %v\n%s", err, stdout)
	}
	if len(got) != 2 || got[0].Slug != "alpha" || got[1].Slug != "beta" {
		t.Fatalf("status --all = %+v", got)
	}
}

func TestStatusAllJSONDegradesUnreadableMissionWithoutAbortingBatch(t *testing.T) {
	repo, _ := makeStatusMission(t, "alpha")
	if err := os.MkdirAll(filepath.Join(repo, "missions", "half-scaffold"), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := executeStatus(t, statusTestDeps(repo, repo), "status", "--all")
	if err != nil || stderr != "" {
		t.Fatalf("err=%v stderr=%s stdout=%s", err, stderr, stdout)
	}
	var got []statusOutput
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON array: %v\n%s", err, stdout)
	}
	if len(got) != 2 || got[0].Slug != "alpha" || got[1].Slug != "half-scaffold" {
		t.Fatalf("status --all = %+v", got)
	}
	if !got[0].OK || got[1].OK || len(got[1].Warnings) == 0 {
		t.Fatalf("degraded row = %+v", got[1])
	}
}

func TestStatusRefusalDefaultsToAgentJSON(t *testing.T) {
	repo, _ := makeStatusMission(t, "alpha")
	d := statusTestDeps(repo, t.TempDir())
	var stdout, stderr bytes.Buffer
	d.stdout, d.stderr = &stdout, &stderr
	code := runWithDeps([]string{"status"}, d)
	if code != exitRefuse {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var got refusalOutput
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if got.Verb != "status" || got.Refusal != "no_context" || got.OK {
		t.Fatalf("refusal = %+v", got)
	}
	if !strings.Contains(got.Remedy, "--mission <slug>") || !strings.Contains(got.Remedy, "--all") {
		t.Fatalf("remedy = %q", got.Remedy)
	}
	if !strings.Contains(stderr.String(), "mish status: no mission context found") {
		t.Fatalf("stderr=%s", stderr.String())
	}
}

func TestStatusBareJSONInsideMissionsRepoRefusesInsteadOfReturningArray(t *testing.T) {
	repo, _ := makeStatusMission(t, "alpha")
	d := statusTestDeps(repo, repo)
	var stdout, stderr bytes.Buffer
	d.stdout, d.stderr = &stdout, &stderr

	if code := runWithDeps([]string{"status"}, d); code != exitRefuse {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var got refusalOutput
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not refusal JSON: %v\n%s", err, stdout.String())
	}
	if got.Refusal != "no_context" || !strings.Contains(got.Remedy, "--all") {
		t.Fatalf("refusal = %+v", got)
	}
}

func TestStatusJSONWarningsAreSorted(t *testing.T) {
	result := makeStatusOutput(resolve.Result{}, statusReport{Warnings: []string{"z warning", "a warning"}})
	if strings.Join(result.Warnings, ",") != "a warning,z warning" {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

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

	stdout, stderr, err := executeStatus(t, statusTestDeps(repo, missionDir), "status", "--text")
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

func TestStatusSingleMissionUsesSingularTaskNoun(t *testing.T) {
	repo, missionDir := makeStatusMission(t, "perf-regression")
	writeTaskFile(t, missionDir, "tasks/task-1.md", "TASK-1", "To Do")

	stdout, stderr, err := executeStatus(t, statusTestDeps(repo, missionDir), "status", "--text")
	if err != nil {
		t.Fatalf("status error: %v\nstderr=%s", err, stderr)
	}
	want := "board:   1 To Do · 0 In Progress · 0 Done   (1 task)"
	if !strings.Contains(stdout, want) {
		t.Fatalf("status output missing singular task noun %q:\n%s", want, stdout)
	}
}

func TestStatusOverviewFromRepoRootListsActiveAndClosedMissions(t *testing.T) {
	repo, activeDir := makeStatusMission(t, "perf-regression")
	writeTaskFile(t, activeDir, "tasks/task-1.md", "TASK-1", "To Do")
	writeTaskFile(t, activeDir, "tasks/task-2.md", "TASK-2", "In Progress")
	setTreeTimes(t, activeDir, testNow().Add(-2*time.Hour))

	closedDir := addStatusMission(t, repo, "q3-launch")
	replaceInFile(t, filepath.Join(closedDir, "mission.md"), "status: active", "status: closed")
	writeTaskFile(t, closedDir, "completed/task-21.md", "TASK-21", "Done")
	setTreeTimes(t, closedDir, testNow().Add(-6*24*time.Hour))

	stdout, stderr, err := executeStatus(t, statusTestDeps(repo, repo), "status", "--text")
	if err != nil {
		t.Fatalf("status overview error: %v\nstderr=%s", err, stderr)
	}
	for _, want := range []string{
		"SLUG",
		"STATUS",
		"AUTHORITY",
		"OWNER",
		"TASKS To Do/In Progress/Done",
		"UPDATED",
		"perf-regression",
		"active",
		"hera",
		"riley",
		"1/1/0",
		"2h ago",
		"q3-launch",
		"closed",
		"0/0/1",
		"6d ago",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("overview output missing %q:\n%s", want, stdout)
		}
	}
	if strings.Index(stdout, "perf-regression") > strings.Index(stdout, "q3-launch") {
		t.Fatalf("overview rows not sorted by slug:\n%s", stdout)
	}
}

func TestStatusOverviewTaskHeaderUsesSharedOrderOnlyWhenAllBoardsMatch(t *testing.T) {
	t.Run("shared order uses header once", func(t *testing.T) {
		repo, activeDir := makeStatusMission(t, "perf-regression")
		writeTaskFile(t, activeDir, "tasks/task-1.md", "TASK-1", "To Do")
		closedDir := addStatusMission(t, repo, "q3-launch")
		writeTaskFile(t, closedDir, "completed/task-2.md", "TASK-2", "Done")

		stdout, stderr, err := executeStatus(t, statusTestDeps(repo, repo), "status", "--text", "--all")
		if err != nil {
			t.Fatalf("status --all error: %v\nstderr=%s", err, stderr)
		}
		if !strings.Contains(stdout, "TASKS To Do/In Progress/Done") {
			t.Fatalf("shared-order overview missing status order in header:\n%s", stdout)
		}
		if strings.Contains(stdout, "1/0/0 To Do/In Progress/Done") {
			t.Fatalf("shared-order row repeated status order:\n%s", stdout)
		}
	})

	t.Run("mixed orders label each row", func(t *testing.T) {
		repo, defaultDir := makeStatusMission(t, "default-board")
		writeTaskFile(t, defaultDir, "tasks/task-1.md", "TASK-1", "To Do")
		customDir := addStatusMission(t, repo, "custom-board")
		replaceInFile(t, filepath.Join(customDir, "backlog", "config.yml"),
			`statuses: ["To Do", "In Progress", "Done"]`,
			`statuses: ["Backlog", "Active", "Shipped"]`,
		)
		writeTaskFile(t, customDir, "tasks/task-1.md", "TASK-1", "Backlog")
		writeTaskFile(t, customDir, "tasks/task-2.md", "TASK-2", "Backlog")
		writeTaskFile(t, customDir, "tasks/task-3.md", "TASK-3", "Active")
		writeTaskFile(t, customDir, "completed/task-4.md", "TASK-4", "Shipped")

		stdout, stderr, err := executeStatus(t, statusTestDeps(repo, repo), "status", "--text", "--all")
		if err != nil {
			t.Fatalf("status --all error: %v\nstderr=%s", err, stderr)
		}
		header := firstLine(stdout)
		if strings.Contains(header, "TASKS To Do/In Progress/Done") || strings.Contains(header, "TASKS Backlog/Active/Shipped") {
			t.Fatalf("mixed-order header should be plain TASKS:\n%s", stdout)
		}
		for _, want := range []string{
			"1/0/0 To Do/In Progress/Done",
			"2/1/1 Backlog/Active/Shipped",
		} {
			if !strings.Contains(stdout, want) {
				t.Fatalf("mixed-order overview missing row order %q:\n%s", want, stdout)
			}
		}
	})
}

func TestStatusOverviewDoesNotUseGitOrSurfaceStalenessWarnings(t *testing.T) {
	repo, missionDir := makeStatusMission(t, "perf-regression")
	writeFile(t, filepath.Join(repo, ".git"), "gitdir: /tmp/repo.git\n")
	var calls [][]string
	d := statusTestDeps(repo, missionDir)
	d.git = func(args []string, dir string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return []byte("dirty"), nil
	}

	stdout, stderr, err := executeStatus(t, d, "status", "--text", "--all")
	if err != nil {
		t.Fatalf("status --all error: %v\nstderr=%s", err, stderr)
	}
	if len(calls) != 0 {
		t.Fatalf("overview invoked git seam: %v", calls)
	}
	if strings.Contains(stdout, "uncommitted or unpushed") {
		t.Fatalf("overview surfaced single-mission staleness warning:\n%s", stdout)
	}
}

func TestStatusContextlessOutsideRepoRefuses(t *testing.T) {
	repo, _ := makeStatusMission(t, "perf-regression")
	outside := t.TempDir()

	stdout, stderr, err := executeStatus(t, statusTestDeps(repo, outside), "status", "--text")
	if err == nil {
		t.Fatalf("contextless status unexpectedly succeeded")
	}
	if stdout != "" {
		t.Fatalf("contextless refusal wrote stdout:\n%s", stdout)
	}
	for _, want := range []string{"mish status: no mission context found", "pass --mission <slug>, run from inside missions/<slug>/, or add a .mission marker"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

func TestStatusMissionAndAllAreMutuallyExclusive(t *testing.T) {
	repo, _ := makeStatusMission(t, "perf-regression")

	stdout, stderr, err := executeStatus(t, statusTestDeps(repo, repo), "status", "--text", "--mission", "perf-regression", "--all")
	if err == nil {
		t.Fatalf("status --mission --all unexpectedly succeeded")
	}
	if stdout != "" {
		t.Fatalf("mutual exclusion wrote stdout:\n%s", stdout)
	}
	want := "mish status: --mission and --all are mutually exclusive"
	if !strings.Contains(stderr, want) {
		t.Fatalf("stderr missing %q:\n%s", want, stderr)
	}

	var runOut, runErr bytes.Buffer
	d := statusTestDeps(repo, repo)
	d.stdout = &runOut
	d.stderr = &runErr
	code := runWithDeps([]string{"status", "--mission", "perf-regression", "--all"}, d)
	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d; stderr=%s", code, exitUsage, runErr.String())
	}
}

func TestStatusOverviewAllWorksFromAnywhereWithMissionsRepo(t *testing.T) {
	repo, missionDir := makeStatusMission(t, "perf-regression")
	writeTaskFile(t, missionDir, "tasks/task-1.md", "TASK-1", "Done")

	stdout, stderr, err := executeStatus(t, statusTestDeps(repo, t.TempDir()), "status", "--text", "--all")
	if err != nil {
		t.Fatalf("status --all error: %v\nstderr=%s", err, stderr)
	}
	for _, want := range []string{"SLUG", "perf-regression", "active", "0/0/1"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("--all overview missing %q:\n%s", want, stdout)
		}
	}
}

func TestStatusOverviewZeroMissionsRendersHeaderOnly(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, repo string)
	}{
		{
			name:  "missing missions dir",
			setup: func(t *testing.T, repo string) {},
		},
		{
			name: "empty missions dir",
			setup: func(t *testing.T, repo string) {
				if err := os.MkdirAll(filepath.Join(repo, "missions"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			tt.setup(t, repo)

			stdout, stderr, err := executeStatus(t, statusTestDeps(repo, repo), "status", "--text", "--all")
			if err != nil {
				t.Fatalf("status --all error: %v\nstderr=%s", err, stderr)
			}
			if stderr != "" {
				t.Fatalf("zero-mission overview wrote stderr:\n%s", stderr)
			}
			lines := strings.Split(strings.TrimSpace(stdout), "\n")
			if len(lines) != 1 || !strings.Contains(lines[0], "SLUG") || !strings.Contains(lines[0], "TASKS") {
				t.Fatalf("zero-mission overview should render header only:\n%s", stdout)
			}
		})
	}
}

func TestStatusOverviewBrokenManifestGetsWarningRow(t *testing.T) {
	repo, missionDir := makeStatusMission(t, "perf-regression")
	replaceInFile(t, filepath.Join(missionDir, "mission.md"), "mission: perf-regression", "mission: [")

	stdout, stderr, err := executeStatus(t, statusTestDeps(repo, repo), "status", "--text", "--all")
	if err != nil {
		t.Fatalf("status --all with broken manifest returned err %v; stderr=%s stdout=%s", err, stderr, stdout)
	}
	for _, want := range []string{"perf-regression", "warning", "malformed mission.md frontmatter", "0/0/0"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("broken-manifest overview missing %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Fatalf("broken-manifest overview wrote stderr:\n%s", stderr)
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
			want: "warning: duplicate task ID TASK-1: backlog/tasks/task-1.md, backlog/completed/task-1-copy.md",
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
		{
			name: "missing manifest key",
			mutate: func(t *testing.T, missionDir string) {
				replaceInFile(t, filepath.Join(missionDir, "mission.md"), "owner: riley\n", "")
			},
			want: "warning: missing mission.md frontmatter key: owner",
		},
		{
			name: "malformed task",
			mutate: func(t *testing.T, missionDir string) {
				writeFile(t, filepath.Join(missionDir, "backlog", "tasks", "stray.md"), "# no frontmatter\n")
			},
			want: "warning: malformed task frontmatter: backlog/tasks/stray.md",
		},
		{
			name: "malformed task field",
			mutate: func(t *testing.T, missionDir string) {
				writeFile(t, filepath.Join(missionDir, "backlog", "tasks", "bad-title.md"), "---\nid: TASK-1\ntitle: Ship: now\nstatus: To Do\nordinal: 1000\nlabels: []\n---\n")
			},
			want: "warning: malformed task field: title (backlog/tasks/bad-title.md)",
		},
		{
			name: "unknown task status",
			mutate: func(t *testing.T, missionDir string) {
				writeTaskFile(t, missionDir, "tasks/task-1.md", "TASK-1", "Blocked")
			},
			want: "warning: task status outside board config: \"Blocked\"",
		},
		{
			name: "missing task id",
			mutate: func(t *testing.T, missionDir string) {
				writeFile(t, filepath.Join(missionDir, "backlog", "tasks", "missing-id.md"), "---\nstatus: To Do\n---\n\n# Missing ID\n")
			},
			want: "warning: task missing id: backlog/tasks/missing-id.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, missionDir := makeStatusMission(t, "perf-regression")
			tt.mutate(t, missionDir)

			stdout, stderr, err := executeStatus(t, statusTestDeps(repo, missionDir), "status", "--text")
			if err != nil {
				t.Fatalf("status error: %v\nstderr=%s", err, stderr)
			}
			if !strings.Contains(stdout, tt.want) {
				t.Fatalf("status output missing warning %q:\n%s", tt.want, stdout)
			}
			if stderr != "" {
				t.Fatalf("warning case wrote stderr:\n%s", stderr)
			}
			for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
				if strings.HasPrefix(line, "warning:") && strings.Contains(line, "\n") {
					t.Fatalf("warning was not one line: %q", line)
				}
			}
		})
	}
}

func TestStatusMalformedManifestAndConfigDegradeToExitOKWarnings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, missionDir string)
		want   string
	}{
		{
			name: "manifest yaml",
			mutate: func(t *testing.T, missionDir string) {
				replaceInFile(t, filepath.Join(missionDir, "mission.md"), "mission: perf-regression", "mission: [")
			},
			want: "warning: malformed mission.md frontmatter: mission.md",
		},
		{
			name: "config yaml",
			mutate: func(t *testing.T, missionDir string) {
				replaceInFile(t, filepath.Join(missionDir, "backlog", "config.yml"), `statuses: ["To Do", "In Progress", "Done"]`, "statuses: [")
			},
			want: "warning: malformed board config: backlog/config.yml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, missionDir := makeStatusMission(t, "perf-regression")
			tt.mutate(t, missionDir)

			stdout, stderr, err := executeStatus(t, statusTestDeps(repo, missionDir), "status", "--text")
			if err != nil {
				t.Fatalf("status returned err %v, want exit-0 success; stderr=%s stdout=%s", err, stderr, stdout)
			}
			if stderr != "" {
				t.Fatalf("degraded warning wrote stderr:\n%s", stderr)
			}
			if !strings.Contains(stdout, tt.want) {
				t.Fatalf("status output missing degraded warning %q:\n%s", tt.want, stdout)
			}
			if !strings.Contains(stdout, "mission: perf-regression") || !strings.Contains(stdout, "artifacts:") {
				t.Fatalf("degraded report did not render partial block:\n%s", stdout)
			}
		})
	}
}

func TestStatusStalenessWarningUsesReadOnlyGitSeam(t *testing.T) {
	repo, missionDir := makeStatusMission(t, "perf-regression")
	writeFile(t, filepath.Join(repo, ".git"), "gitdir: /tmp/repo.git\n")
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

	stdout, stderr, err := executeStatus(t, d, "status", "--text")
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
	var calls [][]string
	d := statusTestDeps(repo, missionDir)
	d.git = func(args []string, dir string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return nil, nil
	}

	stdout, stderr, err := executeStatus(t, d, "status", "--text")
	if err != nil {
		t.Fatalf("status error: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout, "uncommitted or unpushed") {
		t.Fatalf("non-git repo emitted staleness warning:\n%s", stdout)
	}
	if len(calls) != 0 {
		t.Fatalf("plain non-git repo invoked git seam: %v", calls)
	}
}

func TestStatusDoesNotMutateMissionSubtree(t *testing.T) {
	repo, missionDir := makeStatusMission(t, "perf-regression")
	writeTaskFile(t, missionDir, "tasks/task-1.md", "TASK-1", "To Do")
	before := hashTree(t, missionDir)

	_, stderr, err := executeStatus(t, statusTestDeps(repo, missionDir), "status", "--text")
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

	stdout, stderr, err := executeStatus(t, statusTestDeps(repo, missionDir), "status", "--text")
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

	stdout, stderr, err := executeStatus(t, d, "status", "--text", "--mission", "missing")
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

func TestStatusMissingBoardOmitsTaskCountTail(t *testing.T) {
	repo, missionDir := makeStatusMission(t, "perf-regression")
	if err := os.Remove(filepath.Join(missionDir, "backlog", "config.yml")); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := executeStatus(t, statusTestDeps(repo, missionDir), "status", "--text")
	if err != nil {
		t.Fatalf("status error: %v\nstderr=%s", err, stderr)
	}
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "board:") {
			if strings.Contains(line, "(0 tasks)") {
				t.Fatalf("missing board line kept task-count tail: %q", line)
			}
			return
		}
	}
	t.Fatalf("status output missing board line:\n%s", stdout)
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
		var usage usageError
		if errors.As(err, &usage) {
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
	return repo, addStatusMission(t, repo, slug)
}

func addStatusMission(t *testing.T, repo, slug string) string {
	t.Helper()
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
	return missionDir
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

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}

func setTreeTimes(t *testing.T, root string, when time.Time) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, when, when)
	})
	if err != nil {
		t.Fatal(err)
	}
}

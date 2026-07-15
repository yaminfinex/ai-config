package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResolveMarkerHappyJSON(t *testing.T) {
	repo, missionDir := makeStatusMission(t, "perf-regression")
	worktree := t.TempDir()
	writeFile(t, worktree+"/.mission", "perf-regression\n")

	stdout, stderr, err := executeStatus(t, statusTestDeps(repo, worktree), "resolve")
	if err != nil {
		t.Fatalf("resolve error: %v\nstderr=%s", err, stderr)
	}
	var out resolveOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("resolve stdout is not one JSON line: %v\n%s", err, stdout)
	}
	if !out.OK || out.Slug != "perf-regression" || out.Source != "marker" {
		t.Fatalf("unexpected resolve output: %+v", out)
	}
	if out.MissionDir != missionDir {
		t.Fatalf("mission_dir = %q, want %q", out.MissionDir, missionDir)
	}
	if out.MarkerPath == "" || out.MissionsRepo != repo {
		t.Fatalf("marker_path/missions_repo missing: %+v", out)
	}
}

func TestResolveFlagOverridesMarker(t *testing.T) {
	repo, _ := makeStatusMission(t, "perf-regression")
	addStatusMission(t, repo, "other-mission")
	worktree := t.TempDir()
	writeFile(t, worktree+"/.mission", "perf-regression\n")

	stdout, _, err := executeStatus(t, statusTestDeps(repo, worktree), "resolve", "--mission", "other-mission")
	if err != nil {
		t.Fatalf("resolve error: %v", err)
	}
	var out resolveOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err)
	}
	if out.Slug != "other-mission" || out.Source != "flag" {
		t.Fatalf("flag did not win: %+v", out)
	}
}

func TestResolveNoContextRefusalJSON(t *testing.T) {
	repo, _ := makeStatusMission(t, "perf-regression")
	worktree := t.TempDir()

	stdout, stderr, err := executeStatus(t, statusTestDeps(repo, worktree), "resolve")
	if err == nil {
		t.Fatalf("expected refusal, got success:\n%s", stdout)
	}
	var out resolveOutput
	if jerr := json.Unmarshal([]byte(stdout), &out); jerr != nil {
		t.Fatalf("refusal stdout is not JSON: %v\n%s", jerr, stdout)
	}
	if out.OK || out.Refusal != "no_context" {
		t.Fatalf("unexpected refusal output: %+v", out)
	}
	if !strings.Contains(stderr, "mish resolve:") {
		t.Fatalf("refusal missing stderr prose: %s", stderr)
	}
}

func TestResolveRejectsPositionalArgs(t *testing.T) {
	repo, missionDir := makeStatusMission(t, "perf-regression")
	_, _, err := executeStatus(t, statusTestDeps(repo, missionDir), "resolve", "extra")
	if err == nil {
		t.Fatal("expected usage error for positional args")
	}
}

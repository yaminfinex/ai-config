package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/bottle/internal/harness/claude"
	"ai-config/tools/bottle/internal/store"
)

// makeBottleFromFixture creates a bottle whose frozen transcript is a real
// fixture (so Materialize can parse and rewrite it) and whose recorded cwd is
// the given dir.
func makeBottleFromFixture(t *testing.T, st *store.Store, name, cwd, fixture string) *store.Bottle {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", fixture+".jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	b, err := st.Create(store.CreateRequest{
		Name:       name,
		Transcript: data,
		Source:     store.Source{SessionID: "orig-" + name, Harness: "claude", CWD: cwd, CutTurn: 3, TotalTurns: 3},
	})
	if err != nil {
		t.Fatalf("create bottle: %v", err)
	}
	return b
}

func TestDecantMaterializesRecordsAndBuildsInteractivePlan(t *testing.T) {
	st := openTestStore(t)
	d, stdout, stderr := newU6Deps(t, st)
	b := makeBottleFromFixture(t, st, "core", d.cwd, "plain")

	var captured claude.LaunchPlan
	d.launch = func(p claude.LaunchPlan) error { captured = p; return nil }

	if code := cmdDecant(d, []string{"core"}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}

	// A decant entry was recorded against this bottle.
	decants, err := st.Decants()
	if err != nil {
		t.Fatalf("decants: %v", err)
	}
	if len(decants) != 1 {
		t.Fatalf("decants = %d, want 1", len(decants))
	}
	var sid string
	for id, dec := range decants {
		sid = id
		if dec.BottleID != b.ID {
			t.Errorf("decant points at %s, want %s", dec.BottleID, b.ID)
		}
	}

	// The launch plan resumes the materialized seed from the bottle's cwd.
	wantArgv := []string{"claude", "--resume", sid}
	if strings.Join(captured.Argv, " ") != strings.Join(wantArgv, " ") {
		t.Errorf("argv = %v, want %v", captured.Argv, wantArgv)
	}
	if captured.RunCwd != d.cwd || captured.Pane {
		t.Errorf("plan = %+v, want interactive in %s", captured, d.cwd)
	}

	// The seed file exists where the decants-map key points.
	seed := filepath.Join(claude.ProjectDir(d.projectsRoot, d.cwd), sid+".jsonl")
	if _, err := os.Stat(seed); err != nil {
		t.Errorf("seed not written: %v", err)
	}
	if !strings.Contains(stdout.String(), "Decanted core@1") {
		t.Errorf("missing confirmation: %s", stdout)
	}
}

func TestDecantPaneBuildsHerderSpawnPlan(t *testing.T) {
	st := openTestStore(t)
	d, stdout, stderr := newU6Deps(t, st)
	makeBottleFromFixture(t, st, "core", d.cwd, "plain")

	var captured claude.LaunchPlan
	d.launch = func(p claude.LaunchPlan) error { captured = p; return nil }

	if code := cmdDecant(d, []string{"core", "--pane", "right"}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !captured.Pane {
		t.Fatalf("plan.Pane = false, want true")
	}
	argv := strings.Join(captured.Argv, " ")
	for _, want := range []string{"herder-spawn", "--split right", "--safe", "--resume"} {
		if !strings.Contains(argv, want) {
			t.Errorf("pane argv missing %q: %s", want, argv)
		}
	}
	if !strings.Contains(stdout.String(), "launching pane:") {
		t.Errorf("pane command not echoed: %s", stdout)
	}
}

func TestDecantDeadCwdRefusesBeforeMaterializing(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newU6Deps(t, st)
	deadCwd := filepath.Join(t.TempDir(), "gone")
	makeBottleFromFixture(t, st, "core", deadCwd, "plain")

	launched := false
	d.launch = func(claude.LaunchPlan) error { launched = true; return nil }

	if code := cmdDecant(d, []string{"core"}); code == 0 {
		t.Fatalf("exit = 0, want non-zero (dead cwd)")
	}
	if !strings.Contains(stderr.String(), "no longer exists") {
		t.Errorf("stderr missing the dead-cwd refusal: %s", stderr)
	}
	if launched {
		t.Errorf("launch ran despite the refusal")
	}
	// Orphan-seed ordering: nothing recorded, nothing materialized.
	decants, _ := st.Decants()
	if len(decants) != 0 {
		t.Errorf("a decant was recorded for a dead cwd: %v", decants)
	}
	seedDir := claude.ProjectDir(d.projectsRoot, deadCwd)
	if entries, err := os.ReadDir(seedDir); err == nil && len(entries) > 0 {
		t.Errorf("an orphan seed was written: %v", entries)
	}
}

func TestDecantCwdOverride(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newU6Deps(t, st)
	makeBottleFromFixture(t, st, "core", filepath.Join(t.TempDir(), "gone"), "plain")
	override := t.TempDir()

	var captured claude.LaunchPlan
	d.launch = func(p claude.LaunchPlan) error { captured = p; return nil }

	if code := cmdDecant(d, []string{"core", "--cwd", override}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if captured.RunCwd != override {
		t.Errorf("run cwd = %q, want override %q", captured.RunCwd, override)
	}
}

func TestDecantUnknownBottleErrors(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newU6Deps(t, st)
	if code := cmdDecant(d, []string{"ghost"}); code == 0 {
		t.Fatalf("exit = 0, want non-zero for an unknown bottle")
	}
	if !strings.Contains(stderr.String(), "ghost") {
		t.Errorf("stderr should name the missing bottle: %s", stderr)
	}
}

func TestDecantBadPaneRejected(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newU6Deps(t, st)
	makeBottleFromFixture(t, st, "core", d.cwd, "plain")
	if code := cmdDecant(d, []string{"core", "--pane", "sideways"}); code == 0 {
		t.Fatalf("exit = 0, want non-zero for an invalid --pane")
	}
	if !strings.Contains(stderr.String(), "--pane must be") {
		t.Errorf("stderr missing the pane usage: %s", stderr)
	}
}

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
	for _, want := range []string{"herder spawn", "--split right", "--safe", "--resume"} {
		if !strings.Contains(argv, want) {
			t.Errorf("pane argv missing %q: %s", want, argv)
		}
	}
	if !strings.Contains(stdout.String(), "launching pane:") {
		t.Errorf("pane command not echoed: %s", stdout)
	}
}

// The recorded cwd is provenance, not a destination: a bottle whose recorded
// cwd is gone (the cross-machine sync case) still decants into the current
// directory, with a note pointing at where it was bottled.
func TestDecantGoneRecordedCwdRunsInCurrentDir(t *testing.T) {
	st := openTestStore(t)
	d, stdout, stderr := newU6Deps(t, st)
	deadCwd := filepath.Join(t.TempDir(), "gone")
	makeBottleFromFixture(t, st, "core", deadCwd, "plain")

	var captured claude.LaunchPlan
	d.launch = func(p claude.LaunchPlan) error { captured = p; return nil }

	if code := cmdDecant(d, []string{"core"}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if captured.RunCwd != d.cwd {
		t.Errorf("run cwd = %q, want current dir %q", captured.RunCwd, d.cwd)
	}
	if !strings.Contains(stdout.String(), "note: bottled in "+deadCwd) {
		t.Errorf("missing the bottled-in note: %s", stdout)
	}
}

// No note when the bottle was created where the decant runs.
func TestDecantSameCwdPrintsNoNote(t *testing.T) {
	st := openTestStore(t)
	d, stdout, stderr := newU6Deps(t, st)
	makeBottleFromFixture(t, st, "core", d.cwd, "plain")

	if code := cmdDecant(d, []string{"core"}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if strings.Contains(stdout.String(), "note: bottled in") {
		t.Errorf("unexpected bottled-in note: %s", stdout)
	}
}

func TestDecantBadCwdOverrideRefusesBeforeMaterializing(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newU6Deps(t, st)
	makeBottleFromFixture(t, st, "core", d.cwd, "plain")
	badCwd := filepath.Join(t.TempDir(), "gone")

	launched := false
	d.launch = func(claude.LaunchPlan) error { launched = true; return nil }

	if code := cmdDecant(d, []string{"core", "--cwd", badCwd}); code == 0 {
		t.Fatalf("exit = 0, want non-zero (bad --cwd)")
	}
	if !strings.Contains(stderr.String(), "does not exist") {
		t.Errorf("stderr missing the bad-cwd refusal: %s", stderr)
	}
	if launched {
		t.Errorf("launch ran despite the refusal")
	}
	// Orphan-seed ordering: nothing recorded, nothing materialized.
	decants, _ := st.Decants()
	if len(decants) != 0 {
		t.Errorf("a decant was recorded for a bad --cwd: %v", decants)
	}
	seedDir := claude.ProjectDir(d.projectsRoot, badCwd)
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
	// The override is canonicalized (t.TempDir can sit behind a symlink, e.g.
	// /var → /private/var on macOS).
	want, err := filepath.EvalSymlinks(override)
	if err != nil {
		t.Fatalf("eval override: %v", err)
	}
	if captured.RunCwd != want {
		t.Errorf("run cwd = %q, want override %q", captured.RunCwd, want)
	}
}

// A relative --cwd ("." is the natural thing to type when the recorded cwd is
// from another machine) must resolve to the physical absolute path before the
// seed's project dir is derived — otherwise the seed lands in an encoded "-"
// directory Claude never checks and resume reports "No conversation found".
func TestDecantRelativeCwdOverrideSeedsPhysicalProjectDir(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newU6Deps(t, st)
	makeBottleFromFixture(t, st, "core", filepath.Join(t.TempDir(), "gone"), "plain")

	runDir := t.TempDir()
	t.Chdir(runDir)
	physical, err := filepath.EvalSymlinks(runDir)
	if err != nil {
		t.Fatalf("eval run dir: %v", err)
	}

	var captured claude.LaunchPlan
	d.launch = func(p claude.LaunchPlan) error { captured = p; return nil }

	if code := cmdDecant(d, []string{"core", "--cwd", "."}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if captured.RunCwd != physical {
		t.Errorf("run cwd = %q, want physical %q", captured.RunCwd, physical)
	}

	// The seed lives in the project dir encoded from the physical path, and the
	// literal "." encoding ("-") got nothing.
	decants, err := st.Decants()
	if err != nil || len(decants) != 1 {
		t.Fatalf("decants = %v (err %v), want exactly 1", decants, err)
	}
	for sid := range decants {
		seed := filepath.Join(claude.ProjectDir(d.projectsRoot, physical), sid+".jsonl")
		if _, err := os.Stat(seed); err != nil {
			t.Errorf("seed not in physical project dir: %v", err)
		}
	}
	if entries, err := os.ReadDir(filepath.Join(d.projectsRoot, "-")); err == nil && len(entries) > 0 {
		t.Errorf(`seed leaked into the "-" project dir: %v`, entries)
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

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-config/tools/bottle/internal/harness/claude"
	"ai-config/tools/bottle/internal/refs"
	"ai-config/tools/bottle/internal/store"
)

// newU6Deps extends newDeps with the U6 verb seams: a temp run cwd, a temp
// Claude projects root, no live session, a no-op git probe, and a launch
// recorder that captures the plan instead of spawning.
func newU6Deps(t *testing.T, st *store.Store) (d *deps, stdout, stderr *bytes.Buffer) {
	t.Helper()
	d, stdout, stderr = newDeps(t, st)
	// Canonicalize like a real os.Getwd would (t.TempDir can sit behind a
	// symlink, e.g. /var → /private/var on macOS) so project-dir assertions
	// match decant's canonicalized run cwd.
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval test cwd: %v", err)
	}
	d.cwd = cwd
	d.projectsRoot = t.TempDir()
	d.selfSession = ""
	d.gitInfo = func(string) (string, string) { return "", "" }
	d.launch = func(claude.LaunchPlan) error { return nil }
	return d, stdout, stderr
}

// plantSession copies a testdata fixture into the encoded project dir for d.cwd
// as <sessionID>.jsonl, mimicking a real Claude session file on disk.
func plantSession(t *testing.T, d *deps, sessionID, fixture string) string {
	t.Helper()
	dir := claude.ProjectDir(d.projectsRoot, d.cwd)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", fixture+".jsonl"))
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixture, err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("plant session: %v", err)
	}
	return path
}

func mustResolve(t *testing.T, st *store.Store, ref string) *store.Bottle {
	t.Helper()
	r, err := refs.Parse(ref)
	if err != nil {
		t.Fatalf("parse ref %q: %v", ref, err)
	}
	b, err := st.Resolve(r)
	if err != nil {
		t.Fatalf("resolve %q: %v", ref, err)
	}
	return b
}

func lineCount(t *testing.T, st *store.Store, id string) int {
	t.Helper()
	data, err := st.ReadTranscript(id)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	return bytes.Count(data, []byte{'\n'})
}

func TestCreateFromFixtureFreezesWholeSession(t *testing.T) {
	st := openTestStore(t)
	d, stdout, stderr := newU6Deps(t, st)
	plantSession(t, d, "sess-A", "plain")

	if code := cmdCreate(d, []string{"core", "--session", "sess-A"}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), "Created core@1") {
		t.Errorf("missing confirmation: %s", stdout)
	}

	b := mustResolve(t, st, "core")
	if b.Meta.Source.SessionID != "sess-A" || b.Meta.Source.Harness != "claude" {
		t.Errorf("source = %+v", b.Meta.Source)
	}
	if b.Meta.Source.CWD != d.cwd {
		t.Errorf("cwd = %q, want %q", b.Meta.Source.CWD, d.cwd)
	}
	if b.Meta.Source.TotalTurns != 3 || b.Meta.Source.CutTurn != 3 {
		t.Errorf("turns = cut %d of %d, want 3 of 3", b.Meta.Source.CutTurn, b.Meta.Source.TotalTurns)
	}
	if got := lineCount(t, st, b.ID); got != 31 {
		t.Errorf("frozen line count = %d, want 31 (whole plain.jsonl)", got)
	}
	if b.Meta.Compacted || b.Meta.RewoundIntoParent {
		t.Errorf("unexpected annotations on a plain whole bottle: %+v", b.Meta)
	}
}

func TestCreateSelfBottleTrimsInFlightTurn(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newU6Deps(t, st)
	d.selfSession = "sess-self"
	plantSession(t, d, "sess-self", "dangling-tool-use")

	// No --session: the live session is bottled, trimming the dangling tail.
	if code := cmdCreate(d, []string{"wip"}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	b := mustResolve(t, st, "wip")
	if b.Meta.Source.CutTurn != 2 {
		t.Errorf("cut turn = %d, want 2 (the last completed turn before the dangling one)", b.Meta.Source.CutTurn)
	}
	if b.Meta.Source.TotalTurns != 3 {
		t.Errorf("total turns = %d, want 3", b.Meta.Source.TotalTurns)
	}
	if got := lineCount(t, st, b.ID); got >= 26 {
		t.Errorf("frozen line count = %d, want < 26 (the dangling tail trimmed)", got)
	}
}

func TestCreateAtNumberCutsEarlierTurn(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newU6Deps(t, st)
	plantSession(t, d, "sess-br", "branched")

	if code := cmdCreate(d, []string{"hist", "--session", "sess-br", "--at", "2"}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	b := mustResolve(t, st, "hist")
	if b.Meta.Source.CutTurn != 2 || b.Meta.Source.TotalTurns != 3 {
		t.Errorf("turns = cut %d of %d, want 2 of 3", b.Meta.Source.CutTurn, b.Meta.Source.TotalTurns)
	}
	if got := lineCount(t, st, b.ID); got >= 71 {
		t.Errorf("frozen line count = %d, want < 71 (cut at turn 2)", got)
	}
}

func TestCreateAtPickerReadsSelection(t *testing.T) {
	st := openTestStore(t)
	d, stdout, stderr := newU6Deps(t, st)
	d.isTTY = true
	d.stdin = strings.NewReader("2\n")
	plantSession(t, d, "sess-br", "branched")

	if code := cmdCreate(d, []string{"hist", "--session", "sess-br", "--at"}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), "Cut at turn number:") {
		t.Errorf("picker prompt not shown: %s", stdout)
	}
	b := mustResolve(t, st, "hist")
	if b.Meta.Source.CutTurn != 2 {
		t.Errorf("cut turn = %d, want 2 from picker", b.Meta.Source.CutTurn)
	}
}

func TestCreateAtPickerNonInteractiveRefuses(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newU6Deps(t, st)
	plantSession(t, d, "sess-br", "branched")

	if code := cmdCreate(d, []string{"hist", "--session", "sess-br", "--at"}); code == 0 {
		t.Fatalf("exit = 0, want non-zero (picker needs a TTY)")
	}
	if !strings.Contains(stderr.String(), "interactive terminal") {
		t.Errorf("stderr should explain the picker needs a TTY: %s", stderr)
	}
}

func TestCreateOnCompactedWarnsAndAnnotates(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newU6Deps(t, st)
	plantSession(t, d, "sess-cmp", "compacted")

	if code := cmdCreate(d, []string{"big", "--session", "sess-cmp"}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr.String(), "compaction boundary") {
		t.Errorf("expected a compaction warning on stderr: %s", stderr)
	}
	b := mustResolve(t, st, "big")
	if !b.Meta.Compacted {
		t.Errorf("meta.Compacted = false, want true on a compacted fixture")
	}
}

func TestCreateAttachRefusesSensitiveWithoutForce(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newU6Deps(t, st)
	plantSession(t, d, "sess-A", "plain")
	if err := os.WriteFile(filepath.Join(d.cwd, ".env"), []byte("SECRET=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := cmdCreate(d, []string{"core", "--session", "sess-A", "--attach", ".env"}); code == 0 {
		t.Fatalf("exit = 0, want non-zero (sensitive attach without --force)")
	}
	if !strings.Contains(stderr.String(), "refusing sensitive-looking file") {
		t.Errorf("stderr missing the refusal: %s", stderr)
	}
	// Nothing should have been created.
	if _, err := st.Resolve(refs.Ref{Name: "core"}); err == nil {
		t.Errorf("a bottle was created despite the attach refusal")
	}
}

func TestCreateAttachWithForceStoresAndPrintsPath(t *testing.T) {
	st := openTestStore(t)
	d, stdout, stderr := newU6Deps(t, st)
	plantSession(t, d, "sess-A", "plain")
	envPath := filepath.Join(d.cwd, ".env")
	if err := os.WriteFile(envPath, []byte("SECRET=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := cmdCreate(d, []string{"core", "--session", "sess-A", "--attach", ".env", "--force"}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), "attached "+envPath) {
		t.Errorf("resolved attach path not printed: %s", stdout)
	}
	b := mustResolve(t, st, "core")
	names, err := st.Artifacts(b.ID)
	if err != nil {
		t.Fatalf("artifacts: %v", err)
	}
	if len(names) != 1 || names[0] != ".env" {
		t.Errorf("artifacts = %v, want [.env]", names)
	}
}

func TestCreateNameCollisionAlwaysBumps(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newU6Deps(t, st)
	plantSession(t, d, "sess-A", "plain")

	if code := cmdCreate(d, []string{"core", "--session", "sess-A"}); code != 0 {
		t.Fatalf("first create exit = %d (stderr: %s)", code, stderr)
	}
	if code := cmdCreate(d, []string{"core", "--session", "sess-A"}); code != 0 {
		t.Fatalf("second create exit = %d (stderr: %s)", code, stderr)
	}
	b := mustResolve(t, st, "core")
	if b.Meta.Version != 2 {
		t.Errorf("latest version = %d, want 2 (collision bumps, never overwrites)", b.Meta.Version)
	}
	if v1 := mustResolve(t, st, "core@1"); v1.ID == b.ID {
		t.Errorf("v2 reused v1's bottle id %s — an overwrite", b.ID)
	}
}

func TestCreateLastPrintsPreview(t *testing.T) {
	st := openTestStore(t)
	d, stdout, stderr := newU6Deps(t, st)
	plantSession(t, d, "sess-A", "plain")

	if code := cmdCreate(d, []string{"core", "--last"}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), "Using session") {
		t.Errorf("--last should print a session preview: %s", stdout)
	}
	b := mustResolve(t, st, "core")
	if b.Meta.Source.SessionID != "sess-A" {
		t.Errorf("session = %q, want sess-A", b.Meta.Source.SessionID)
	}
}

func TestCreateNoSessionAvailableErrors(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newU6Deps(t, st)
	// No planted session, no live session.
	if code := cmdCreate(d, []string{"core"}); code == 0 {
		t.Fatalf("exit = 0, want non-zero with no resolvable session")
	}
	if !strings.Contains(stderr.String(), "no session to bottle") {
		t.Errorf("stderr missing the no-session guidance: %s", stderr)
	}
}

package cli

import (
	"strings"
	"testing"

	"ai-config/tools/bottle/internal/refs"
)

func TestRenameMovesName(t *testing.T) {
	st := openTestStore(t)
	makeBottle(t, st, "alpha", "ctx", nil)

	d, stdout, stderr := newDeps(t, st)
	if code := cmdRename(d, []string{"alpha", "gamma"}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), "Renamed alpha to gamma") {
		t.Errorf("missing success message: %s", stdout)
	}
	if _, err := st.Resolve(refs.Ref{Name: "gamma"}); err != nil {
		t.Errorf("gamma not resolvable after rename: %v", err)
	}
	if _, err := st.Resolve(refs.Ref{Name: "alpha"}); err == nil {
		t.Errorf("alpha still resolvable after rename")
	}
}

func TestRenameRefusesExistingTarget(t *testing.T) {
	st := openTestStore(t)
	makeBottle(t, st, "alpha", "", nil)
	makeBottle(t, st, "beta", "", nil)

	d, _, stderr := newDeps(t, st)
	if code := cmdRename(d, []string{"alpha", "beta"}); code == 0 {
		t.Fatalf("exit = 0, want non-zero renaming onto existing name")
	}
	if stderr.Len() == 0 {
		t.Errorf("no error written for refused rename")
	}
}

func TestRenameUsageError(t *testing.T) {
	st := openTestStore(t)
	d, _, stderr := newDeps(t, st)
	if code := cmdRename(d, []string{"only-one"}); code != 2 {
		t.Fatalf("exit = %d, want 2 for wrong arg count", code)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("missing usage hint: %s", stderr)
	}
}

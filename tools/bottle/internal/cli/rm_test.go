package cli

import (
	"strings"
	"testing"

	"ai-config/tools/bottle/internal/refs"
)

func TestRmForceRemovesAndWarns(t *testing.T) {
	st := openTestStore(t)
	makeBottle(t, st, "alpha", "", nil)

	d, stdout, stderr := newDeps(t, st)
	d.isTTY = false // agent context
	if code := cmdRm(d, []string{"alpha", "--force"}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), "Removed every version of alpha") {
		t.Errorf("missing removal message: %s", stdout)
	}
	// git-history retention warning always prints.
	warn := stderr.String()
	if !strings.Contains(warn, "git history") || !strings.Contains(warn, "~/.bottles") {
		t.Errorf("missing git-history retention warning: %s", warn)
	}
	if _, err := st.Resolve(refs.Ref{Name: "alpha"}); err == nil {
		t.Errorf("alpha still present after rm")
	}
}

func TestRmNonTTYWithoutForceRefuses(t *testing.T) {
	st := openTestStore(t)
	makeBottle(t, st, "alpha", "", nil)

	d, _, stderr := newDeps(t, st)
	d.isTTY = false
	if code := cmdRm(d, []string{"alpha"}); code == 0 {
		t.Fatalf("exit = 0, want non-zero refusing non-TTY rm without --force")
	}
	if !strings.Contains(stderr.String(), "--force") {
		t.Errorf("refusal hint does not mention --force: %s", stderr)
	}
	if _, err := st.Resolve(refs.Ref{Name: "alpha"}); err != nil {
		t.Errorf("alpha was removed despite refusal: %v", err)
	}
}

func TestRmTTYPromptYesRemoves(t *testing.T) {
	st := openTestStore(t)
	makeBottle(t, st, "alpha", "", nil)

	d, stdout, _ := newDeps(t, st)
	d.isTTY = true
	d.stdin = strings.NewReader("y\n")
	if code := cmdRm(d, []string{"alpha"}); code != 0 {
		t.Fatalf("exit = %d, want 0 after confirming", code)
	}
	if !strings.Contains(stdout.String(), "Remove every version of alpha?") {
		t.Errorf("no confirmation prompt shown: %s", stdout)
	}
	if _, err := st.Resolve(refs.Ref{Name: "alpha"}); err == nil {
		t.Errorf("alpha not removed after y")
	}
}

func TestRmTTYPromptNoAborts(t *testing.T) {
	st := openTestStore(t)
	makeBottle(t, st, "alpha", "", nil)

	d, stdout, _ := newDeps(t, st)
	d.isTTY = true
	d.stdin = strings.NewReader("n\n")
	if code := cmdRm(d, []string{"alpha"}); code == 0 {
		t.Fatalf("exit = 0, want non-zero after declining")
	}
	if !strings.Contains(stdout.String(), "Aborted") {
		t.Errorf("no abort message: %s", stdout)
	}
	if _, err := st.Resolve(refs.Ref{Name: "alpha"}); err != nil {
		t.Errorf("alpha removed despite abort: %v", err)
	}
}

func TestRmPinnedVersion(t *testing.T) {
	st := openTestStore(t)
	makeBottle(t, st, "beta", "", nil)
	makeBottle(t, st, "beta", "", nil) // @2

	d, stdout, _ := newDeps(t, st)
	if code := cmdRm(d, []string{"beta@1", "--force"}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Removed version beta@1") {
		t.Errorf("pinned removal message wrong: %s", stdout)
	}
	if _, err := st.Resolve(refs.Ref{Name: "beta", Version: 2}); err != nil {
		t.Errorf("beta@2 should survive removing @1: %v", err)
	}
	if _, err := st.Resolve(refs.Ref{Name: "beta", Version: 1}); err == nil {
		t.Errorf("beta@1 still present")
	}
}

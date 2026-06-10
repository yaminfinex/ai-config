package cli

import (
	"strings"
	"testing"

	"ai-config/tools/bottle/internal/refs"
)

func TestNoteSetsText(t *testing.T) {
	st := openTestStore(t)
	makeBottle(t, st, "alpha", "old note", nil)

	d, stdout, stderr := newDeps(t, st)
	if code := cmdNote(d, []string{"alpha", "shiny", "new", "note"}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), "Updated note for alpha") {
		t.Errorf("missing success message: %s", stdout)
	}
	b, err := st.Resolve(refs.Ref{Name: "alpha"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if b.Meta.Note != "shiny new note" {
		t.Errorf("note = %q, want %q", b.Meta.Note, "shiny new note")
	}
}

func TestNoteUnpinnedEditsLatest(t *testing.T) {
	st := openTestStore(t)
	makeBottle(t, st, "beta", "v1 note", nil)
	makeBottle(t, st, "beta", "v2 note", nil)

	d, _, _ := newDeps(t, st)
	if code := cmdNote(d, []string{"beta", "edited"}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	latest, _ := st.Resolve(refs.Ref{Name: "beta"})
	if latest.Meta.Version != 2 || latest.Meta.Note != "edited" {
		t.Errorf("latest note not edited: v%d %q", latest.Meta.Version, latest.Meta.Note)
	}
	v1, _ := st.Resolve(refs.Ref{Name: "beta", Version: 1})
	if v1.Meta.Note != "v1 note" {
		t.Errorf("v1 note changed: %q", v1.Meta.Note)
	}
}

func TestNoteUsageError(t *testing.T) {
	st := openTestStore(t)
	d, _, _ := newDeps(t, st)
	if code := cmdNote(d, []string{"alpha"}); code != 2 {
		t.Errorf("exit = %d, want 2 for missing note text", code)
	}
}

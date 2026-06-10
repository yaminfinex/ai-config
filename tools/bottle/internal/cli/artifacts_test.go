package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactsList(t *testing.T) {
	st := openTestStore(t)
	b := makeBottle(t, st, "alpha", "", nil)
	if err := st.AddArtifact(b.ID, "notes.md", []byte("hi")); err != nil {
		t.Fatalf("add artifact: %v", err)
	}
	if err := st.AddArtifact(b.ID, "sub/data.txt", []byte("x")); err != nil {
		t.Fatalf("add artifact: %v", err)
	}

	d, stdout, stderr := newDeps(t, st)
	if code := cmdArtifacts(d, []string{"alpha"}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	out := stdout.String()
	if !strings.Contains(out, "notes.md") || !strings.Contains(out, "sub/data.txt") {
		t.Errorf("artifact list incomplete:\n%s", out)
	}
}

func TestArtifactsListEmpty(t *testing.T) {
	st := openTestStore(t)
	makeBottle(t, st, "alpha", "", nil)
	d, stdout, _ := newDeps(t, st)
	if code := cmdArtifacts(d, []string{"alpha"}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "no artifacts") {
		t.Errorf("empty artifacts not reported: %s", stdout)
	}
}

func TestArtifactsExtract(t *testing.T) {
	st := openTestStore(t)
	b := makeBottle(t, st, "alpha", "", nil)
	st.AddArtifact(b.ID, "notes.md", []byte("hello"))
	st.AddArtifact(b.ID, "sub/data.txt", []byte("world"))

	dir := filepath.Join(t.TempDir(), "out")
	d, stdout, stderr := newDeps(t, st)
	if code := cmdArtifacts(d, []string{"alpha", "--extract", dir}); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), "Extracted") {
		t.Errorf("no extraction output: %s", stdout)
	}
	for rel, want := range map[string]string{"notes.md": "hello", "sub/data.txt": "world"} {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read extracted %s: %v", rel, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
}

func TestArtifactsExtractRefusesCollision(t *testing.T) {
	st := openTestStore(t)
	b := makeBottle(t, st, "alpha", "", nil)
	st.AddArtifact(b.ID, "notes.md", []byte("new content"))

	dir := t.TempDir()
	// Pre-existing file at the target path.
	collide := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(collide, []byte("ORIGINAL"), 0o600); err != nil {
		t.Fatalf("seed collision: %v", err)
	}

	d, _, stderr := newDeps(t, st)
	if code := cmdArtifacts(d, []string{"alpha", "--extract", dir}); code == 0 {
		t.Fatalf("exit = 0, want non-zero on collision")
	}
	if !strings.Contains(stderr.String(), collide) {
		t.Errorf("refusal does not name the colliding path %q:\n%s", collide, stderr)
	}
	// The original must be untouched.
	got, _ := os.ReadFile(collide)
	if string(got) != "ORIGINAL" {
		t.Errorf("collision file was overwritten: %q", got)
	}
}

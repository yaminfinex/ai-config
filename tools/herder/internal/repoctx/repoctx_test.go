package repoctx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadReportsBranchRemoteAndLinkedWorktreeParent(t *testing.T) {
	root := newGitRepo(t)
	git(t, root, "remote", "add", "origin", "https://example.invalid/fixture.git")
	writeFile(t, root, "README.md", "fixture\n")
	git(t, root, "add", "README.md")
	git(t, root, "commit", "-m", "fixture")

	worktree := filepath.Join(t.TempDir(), "linked")
	git(t, root, "worktree", "add", "-b", "feature/files", worktree)
	got, err := Read(context.Background(), worktree)
	if err != nil {
		t.Fatal(err)
	}
	if got.CWD != worktree || got.Git == nil || got.Git.Branch != "feature/files" || got.Git.RemoteURL != "https://example.invalid/fixture.git" || got.Git.WorktreeOf != root {
		t.Fatalf("context = %#v", got)
	}
}

func TestTopLevelReportsInnermostRepoOrNothing(t *testing.T) {
	root := newGitRepo(t)
	sub := filepath.Join(root, "sub", "deeper")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "README.md", "fixture\n")
	git(t, root, "add", "README.md")
	git(t, root, "commit", "-m", "fixture")
	worktree := filepath.Join(t.TempDir(), "linked")
	git(t, root, "worktree", "add", "-b", "feature/top", worktree)
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, nested, "init", "-q")

	for _, test := range []struct{ cwd, want string }{{root, root}, {sub, root}, {worktree, worktree}, {nested, nested}} {
		top, ok := TopLevel(context.Background(), test.cwd)
		if !ok || top != test.want {
			t.Fatalf("TopLevel(%q) = %q %v, want %q", test.cwd, top, ok, test.want)
		}
	}
	if top, ok := TopLevel(context.Background(), t.TempDir()); ok || top != "" {
		t.Fatalf("plain dir = %q %v", top, ok)
	}
	t.Setenv("PATH", t.TempDir())
	if top, ok := TopLevel(context.Background(), root); ok || top != "" {
		t.Fatalf("missing git = %q %v", top, ok)
	}
}

func TestReadOmitsUnavailableGitFactsWithoutGuessing(t *testing.T) {
	root := t.TempDir()
	got, err := Read(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got.CWD != root || got.Git != nil {
		t.Fatalf("context = %#v", got)
	}
}

func TestReadOmitsGitWhenExecutableIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	got, err := Read(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got.CWD != root || got.Git != nil {
		t.Fatalf("context = %#v", got)
	}
}

func TestReadOmitsGitWhenProbeTimesOut(t *testing.T) {
	bin := t.TempDir()
	gitPath := filepath.Join(bin, "git")
	if err := os.WriteFile(gitPath, []byte("#!/bin/sh\nexec /bin/sleep 10\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	previousTimeout := commandTimeout
	commandTimeout = 20 * time.Millisecond
	t.Cleanup(func() { commandTimeout = previousTimeout })

	root := t.TempDir()
	started := time.Now()
	got, err := Read(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got.CWD != root || got.Git != nil {
		t.Fatalf("context = %#v", got)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("timed-out Git probe took %s", elapsed)
	}
}

func newGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "main")
	git(t, root, "config", "user.name", "Fixture")
	git(t, root, "config", "user.email", "fixture@example.invalid")
	return root
}

func git(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

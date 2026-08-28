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

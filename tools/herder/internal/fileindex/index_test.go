package fileindex

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestIndexCachesPerRootUntilTTLOrForcedRefresh(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	gitCalls := 0
	index := New(Options{
		TTL: time.Minute,
		Now: func() time.Time { return now },
		Run: func(_ context.Context, dir, name string, args ...string) (CommandOutput, error) {
			if dir != "/opaque/root" || name != "git" {
				t.Fatalf("run dir=%q name=%q args=%q", dir, name, args)
			}
			gitCalls++
			return CommandOutput{Stdout: []byte([]string{"first\x00", "second\x00", "third\x00"}[gitCalls-1])}, nil
		},
	})

	first, err := index.Candidates(context.Background(), "/opaque/root", false)
	if err != nil {
		t.Fatal(err)
	}
	first[0] = "caller mutation"
	cached, err := index.Candidates(context.Background(), "/opaque/root", false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cached, []string{"first"}) || gitCalls != 1 {
		t.Fatalf("cached=%q gitCalls=%d", cached, gitCalls)
	}

	now = now.Add(time.Minute)
	refreshed, err := index.Candidates(context.Background(), "/opaque/root", false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(refreshed, []string{"second"}) || gitCalls != 2 {
		t.Fatalf("refreshed=%q gitCalls=%d", refreshed, gitCalls)
	}

	forced, err := index.Candidates(context.Background(), "/opaque/root", true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(forced, []string{"third"}) || gitCalls != 3 {
		t.Fatalf("forced=%q gitCalls=%d", forced, gitCalls)
	}
}

func TestIndexIncludesTrackedAndUntrackedButNotIgnoredOrGitInternals(t *testing.T) {
	root := newGitRepo(t)
	writeFile(t, root, ".gitignore", "ignored.txt\n")
	writeFile(t, root, "tracked.md", "tracked\n")
	git(t, root, "add", ".gitignore", "tracked.md")
	git(t, root, "commit", "-m", "fixture")
	writeFile(t, root, "untracked.md", "untracked\n")
	writeFile(t, root, "ignored.txt", "ignored\n")

	candidates, err := New(Options{}).Candidates(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".gitignore", "tracked.md", "untracked.md"} {
		if !slices.Contains(candidates, want) {
			t.Errorf("candidates %q do not contain %q", candidates, want)
		}
	}
	if slices.Contains(candidates, "ignored.txt") {
		t.Fatalf("ignored file included: %q", candidates)
	}
	for _, candidate := range candidates {
		if candidate == ".git" || strings.HasPrefix(candidate, ".git/") {
			t.Fatalf("git internal included: %q", candidate)
		}
	}
}

func TestIndexUsesRipgrepForNonGitRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".gitignore", "ignored.txt\n")
	writeFile(t, root, "visible.md", "visible\n")
	writeFile(t, root, ".hidden.md", "hidden\n")
	writeFile(t, root, "ignored.txt", "ignored\n")

	candidates, err := New(Options{}).Candidates(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".gitignore", ".hidden.md", "visible.md"} {
		if !slices.Contains(candidates, want) {
			t.Errorf("candidates %q do not contain %q", candidates, want)
		}
	}
	if slices.Contains(candidates, "ignored.txt") {
		t.Fatalf("ignored file included: %q", candidates)
	}
}

func TestIndexTreatsEmptyNonGitRootAsEmptyCandidateSet(t *testing.T) {
	candidates, err := New(Options{}).Candidates(context.Background(), t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %q", candidates)
	}
}

func TestIndexReportsPartialNonGitRootAsDegraded(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "visible.md", "visible\n")
	writeFile(t, root, "blocked/secret.md", "secret\n")
	blocked := filepath.Join(root, "blocked")
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })

	candidates, err := New(Options{}).Candidates(context.Background(), root, false)
	var degraded *DegradedError
	if !errors.As(err, &degraded) {
		t.Fatalf("error=%v, want DegradedError", err)
	}
	if !slices.Contains(candidates, "visible.md") {
		t.Fatalf("partial candidates=%q", candidates)
	}
	if !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("degraded detail=%q", err)
	}
}

func TestIndexExcludesGitInternalsFromNonGitFallback(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "visible.md", "visible\n")
	writeFile(t, root, ".git/config", "not a candidate\n")

	candidates, err := New(Options{}).Candidates(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if candidate == ".git" || strings.HasPrefix(candidate, ".git/") {
			t.Fatalf("git internal included: %q", candidate)
		}
	}
}

func TestIndexReportsMissingRipgrepForNonGitRoot(t *testing.T) {
	run := func(_ context.Context, _ string, name string, _ ...string) (CommandOutput, error) {
		if name == "git" {
			return CommandOutput{Stderr: []byte("fatal: not a git repository")}, errors.New("exit status 128")
		}
		return CommandOutput{}, errors.New("executable file not found")
	}
	_, err := New(Options{Run: run}).Candidates(context.Background(), "/non-git", false)
	if err == nil || !strings.Contains(err.Error(), "rg --files") || !strings.Contains(err.Error(), "executable file not found") {
		t.Fatalf("missing ripgrep error=%v", err)
	}
}

func TestIndexKeepsLinkedWorktreeAsItsOwnRoot(t *testing.T) {
	root := newGitRepo(t)
	writeFile(t, root, "root-only.md", "root\n")
	git(t, root, "add", "root-only.md")
	git(t, root, "commit", "-m", "root file")

	worktree := filepath.Join(t.TempDir(), "linked")
	git(t, root, "worktree", "add", "-b", "fixture-worktree", worktree)
	writeFile(t, worktree, "worktree-only.md", "worktree\n")
	git(t, worktree, "add", "worktree-only.md")
	git(t, worktree, "commit", "-m", "worktree file")

	index := New(Options{})
	rootCandidates, err := index.Candidates(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	worktreeCandidates, err := index.Candidates(context.Background(), worktree, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(rootCandidates, "root-only.md") || slices.Contains(rootCandidates, "worktree-only.md") {
		t.Fatalf("root candidates folded worktree: %q", rootCandidates)
	}
	if !slices.Contains(worktreeCandidates, "worktree-only.md") {
		t.Fatalf("worktree candidates missing own file: %q", worktreeCandidates)
	}
}

func newGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-q")
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

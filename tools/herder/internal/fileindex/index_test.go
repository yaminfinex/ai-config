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

	"ai-config/tools/herder/internal/filecandidate"
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
			if gitCalls == 1 {
				// A load longer than the TTL must still produce a fresh entry.
				now = now.Add(2 * time.Minute)
			}
			return CommandOutput{Stdout: []byte([]string{"first\x00", "second\x00", "third\x00"}[gitCalls-1])}, nil
		},
	})

	first, err := index.Candidates(context.Background(), "/opaque/root", false)
	if err != nil {
		t.Fatal(err)
	}
	first[0].Path = "caller mutation"
	cached, err := index.Candidates(context.Background(), "/opaque/root", false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cached, []filecandidate.Candidate{{Path: "first", Kind: filecandidate.KindFile}}) || gitCalls != 1 {
		t.Fatalf("cached=%q gitCalls=%d", cached, gitCalls)
	}
	now = now.Add(30 * time.Second)
	if stillCached, err := index.Candidates(context.Background(), "/opaque/root", false); err != nil || gitCalls != 1 || stillCached[0].Path != "first" {
		t.Fatalf("within TTL after slow load: cached=%q gitCalls=%d err=%v", stillCached, gitCalls, err)
	}
	now = now.Add(30 * time.Second)

	refreshed, err := index.Candidates(context.Background(), "/opaque/root", false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(refreshed, []filecandidate.Candidate{{Path: "second", Kind: filecandidate.KindFile}}) || gitCalls != 2 {
		t.Fatalf("refreshed=%q gitCalls=%d", refreshed, gitCalls)
	}

	forced, err := index.Candidates(context.Background(), "/opaque/root", true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(forced, []filecandidate.Candidate{{Path: "third", Kind: filecandidate.KindFile}}) || gitCalls != 3 {
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
		if !slices.Contains(candidates, filecandidate.Candidate{Path: want, Kind: filecandidate.KindFile}) {
			t.Errorf("candidates %q do not contain %q", candidates, want)
		}
	}
	if hasPath(candidates, "ignored.txt") {
		t.Fatalf("ignored file included: %q", candidates)
	}
	for _, candidate := range candidates {
		if candidate.Path == ".git" || strings.HasPrefix(candidate.Path, ".git/") {
			t.Fatalf("git internal included: %q", candidate)
		}
	}
}

func TestIndexRefusesNonGitRootWithoutWalking(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "visible.md", "visible\n")
	writeFile(t, root, "nested/deep.md", "deep\n")
	var commands []string
	run := func(ctx context.Context, dir, name string, args ...string) (CommandOutput, error) {
		commands = append(commands, name)
		return runCommand(ctx, dir, name, args...)
	}

	candidates, err := New(Options{Run: run}).Candidates(context.Background(), root, false)
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("error=%v, want not a git repository", err)
	}
	var degraded *DegradedError
	if errors.As(err, &degraded) {
		t.Fatalf("non-git root reported as degraded rather than failed: %v", err)
	}
	if candidates != nil {
		t.Fatalf("candidates=%q, want none", candidates)
	}
	if !reflect.DeepEqual(commands, []string{"git"}) {
		t.Fatalf("commands=%q, want only git (no walk)", commands)
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
	if !hasCandidate(rootCandidates, "root-only.md", filecandidate.KindFile) || hasPath(rootCandidates, "worktree-only.md") {
		t.Fatalf("root candidates folded worktree: %q", rootCandidates)
	}
	if !hasCandidate(worktreeCandidates, "worktree-only.md", filecandidate.KindFile) {
		t.Fatalf("worktree candidates missing own file: %q", worktreeCandidates)
	}
}

func TestIndexDerivesUniqueAncestorDirectoriesWithoutRootCandidate(t *testing.T) {
	candidates := parseCandidates([]byte("docs/a.md\x00docs/nested/b.md\x00root.md\x00"))
	want := []filecandidate.Candidate{
		{Path: "docs", Kind: filecandidate.KindDir},
		{Path: "docs/a.md", Kind: filecandidate.KindFile},
		{Path: "docs/nested", Kind: filecandidate.KindDir},
		{Path: "docs/nested/b.md", Kind: filecandidate.KindFile},
		{Path: "root.md", Kind: filecandidate.KindFile},
	}
	if !reflect.DeepEqual(candidates, want) {
		t.Fatalf("candidates=%#v want %#v", candidates, want)
	}
	if hasPath(candidates, ".") || hasPath(candidates, "") {
		t.Fatalf("root candidate leaked: %#v", candidates)
	}
}

func hasPath(candidates []filecandidate.Candidate, path string) bool {
	for _, candidate := range candidates {
		if candidate.Path == path {
			return true
		}
	}
	return false
}

func hasCandidate(candidates []filecandidate.Candidate, path string, kind filecandidate.Kind) bool {
	return slices.Contains(candidates, filecandidate.Candidate{Path: path, Kind: kind})
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

package fileindex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ai-config/tools/herder/internal/filecandidate"
)

func TestIndexServesStaleAndRefreshesInBackgroundOrOnForcedRefresh(t *testing.T) {
	var clock sync.Mutex
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	readNow := func() time.Time { clock.Lock(); defer clock.Unlock(); return now }
	advance := func(d time.Duration) { clock.Lock(); now = now.Add(d); clock.Unlock() }
	var gitCalls atomic.Int32
	index := New(Options{
		TTL: time.Minute,
		Now: readNow,
		Run: func(_ context.Context, dir, name string, args ...string) (CommandOutput, error) {
			if dir != "/opaque/root" || name != "git" {
				t.Errorf("run dir=%q name=%q args=%q", dir, name, args)
			}
			call := gitCalls.Add(1)
			if call == 1 {
				// A load longer than the TTL must still produce a fresh entry.
				advance(2 * time.Minute)
			}
			return CommandOutput{Stdout: []byte([]string{"first\x00", "second\x00", "third\x00"}[call-1])}, nil
		},
	})
	paths := func(refresh bool) []string {
		t.Helper()
		candidates, err := index.Candidates(context.Background(), "/opaque/root", refresh)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, candidate := range candidates {
			out = append(out, candidate.Path)
		}
		return out
	}

	first, err := index.Candidates(context.Background(), "/opaque/root", false)
	if err != nil {
		t.Fatal(err)
	}
	first[0].Path = "caller mutation"
	if got := paths(false); !reflect.DeepEqual(got, []string{"first"}) || gitCalls.Load() != 1 {
		t.Fatalf("cached=%q gitCalls=%d", got, gitCalls.Load())
	}
	advance(30 * time.Second)
	if got := paths(false); !reflect.DeepEqual(got, []string{"first"}) || gitCalls.Load() != 1 {
		t.Fatalf("within TTL after slow load: cached=%q gitCalls=%d", got, gitCalls.Load())
	}
	advance(30 * time.Second)

	// Past the TTL the lookup still answers from the last list; the refresh
	// it started lands for the next lookup.
	if got := paths(false); !reflect.DeepEqual(got, []string{"first"}) {
		t.Fatalf("stale lookup=%q, want the last list", got)
	}
	waitIdle(t, index, "/opaque/root")
	if got := paths(false); !reflect.DeepEqual(got, []string{"second"}) || gitCalls.Load() != 2 {
		t.Fatalf("after background refresh=%q gitCalls=%d", got, gitCalls.Load())
	}

	if got := paths(true); !reflect.DeepEqual(got, []string{"third"}) || gitCalls.Load() != 3 {
		t.Fatalf("forced=%q gitCalls=%d", got, gitCalls.Load())
	}
}

func TestIndexRunsOneGitPerRootUnderConcurrentLookups(t *testing.T) {
	var clock sync.Mutex
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	readNow := func() time.Time { clock.Lock(); defer clock.Unlock(); return now }
	release := make(chan struct{})
	var gitCalls atomic.Int32
	index := New(Options{
		TTL: time.Minute,
		Now: readNow,
		Run: func(context.Context, string, string, ...string) (CommandOutput, error) {
			call := gitCalls.Add(1)
			<-release
			return CommandOutput{Stdout: []byte(fmt.Sprintf("load-%d\x00", call))}, nil
		},
	})
	lookups := func(n int) []string {
		t.Helper()
		var wg sync.WaitGroup
		results := make([]string, n)
		for k := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				candidates, err := index.Candidates(context.Background(), "/opaque/root", false)
				if err != nil || len(candidates) != 1 {
					t.Errorf("lookup %d: candidates=%q err=%v", k, candidates, err)
					return
				}
				results[k] = candidates[0].Path
			}()
		}
		wg.Wait()
		return results
	}

	// A root never indexed: every concurrent lookup waits on the one load.
	go func() {
		for gitCalls.Load() == 0 {
			time.Sleep(time.Millisecond)
		}
		time.Sleep(20 * time.Millisecond)
		release <- struct{}{}
	}()
	for k, path := range lookups(8) {
		if path != "load-1" {
			t.Fatalf("cold lookup %d=%q", k, path)
		}
	}
	if gitCalls.Load() != 1 {
		t.Fatalf("cold gitCalls=%d, want 1", gitCalls.Load())
	}

	// A stale root: concurrent lookups answer at once from the last list
	// while exactly one refresh is held open behind them.
	clock.Lock()
	now = now.Add(2 * time.Minute)
	clock.Unlock()
	for k, path := range lookups(8) {
		if path != "load-1" {
			t.Fatalf("stale lookup %d=%q, want the last list without waiting", k, path)
		}
	}
	for deadline := time.Now().Add(5 * time.Second); gitCalls.Load() < 2 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	lookups(8)
	time.Sleep(20 * time.Millisecond)
	if gitCalls.Load() != 2 {
		t.Fatalf("stale gitCalls=%d, want exactly one background refresh", gitCalls.Load())
	}
	release <- struct{}{}
	waitIdle(t, index, "/opaque/root")
	if got := lookups(1); got[0] != "load-2" || gitCalls.Load() != 2 {
		t.Fatalf("after refresh=%q gitCalls=%d", got, gitCalls.Load())
	}
}

func TestIndexDropsListWhenBackgroundRefreshFails(t *testing.T) {
	var clock sync.Mutex
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	var gitCalls atomic.Int32
	index := New(Options{
		TTL: time.Minute,
		Now: func() time.Time { clock.Lock(); defer clock.Unlock(); return now },
		Run: func(context.Context, string, string, ...string) (CommandOutput, error) {
			if gitCalls.Add(1) == 1 {
				return CommandOutput{Stdout: []byte("kept\x00")}, nil
			}
			return CommandOutput{Stderr: []byte("fatal: not a git repository")}, errors.New("exit status 128")
		},
	})
	if _, err := index.Candidates(context.Background(), "/opaque/root", false); err != nil {
		t.Fatal(err)
	}
	clock.Lock()
	now = now.Add(2 * time.Minute)
	clock.Unlock()
	if candidates, err := index.Candidates(context.Background(), "/opaque/root", false); err != nil || candidates[0].Path != "kept" {
		t.Fatalf("stale lookup candidates=%q err=%v", candidates, err)
	}
	waitIdle(t, index, "/opaque/root")
	if _, err := index.Candidates(context.Background(), "/opaque/root", false); err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("after failed refresh err=%v, want the failure reported", err)
	}
}

func waitIdle(t *testing.T, index *Index, root string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		index.mu.Lock()
		running := index.inflight[root] != nil
		index.mu.Unlock()
		if !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("load for %q still running", root)
		}
		time.Sleep(time.Millisecond)
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
	if partial, ok := err.(interface{ Degraded() bool }); ok && partial.Degraded() {
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

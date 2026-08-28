package fileroots

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildFoldsOnlyNestedOrdinaryAgentRoots(t *testing.T) {
	outer := t.TempDir()
	nested := filepath.Join(outer, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	configured := t.TempDir()

	set, err := Build(context.Background(), []string{configured}, []Agent{
		{Name: "outer", CWD: outer},
		{Name: "nested", CWD: nested},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set.Roots, []string{configured, outer}) {
		t.Fatalf("roots = %q", set.Roots)
	}
	if set.AgentRoot["outer"] != outer || set.AgentRoot["nested"] != outer {
		t.Fatalf("agent roots = %#v", set.AgentRoot)
	}
}

func TestBuildNeverFoldsLinkedWorktreeCWD(t *testing.T) {
	outer := t.TempDir()
	repo := filepath.Join(outer, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "config", "user.name", "Fixture")
	git(t, repo, "config", "user.email", "fixture@example.invalid")
	writeFile(t, repo, "tracked.md", "fixture\n")
	git(t, repo, "add", "tracked.md")
	git(t, repo, "commit", "-m", "fixture")
	worktree := filepath.Join(outer, "linked")
	git(t, repo, "worktree", "add", "-b", "feature", worktree)

	set, err := Build(context.Background(), nil, []Agent{
		{Name: "outer", CWD: outer},
		{Name: "worktree", CWD: worktree},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set.Roots, []string{outer, worktree}) || set.AgentRoot["worktree"] != worktree {
		t.Fatalf("set = %#v", set)
	}
}

func TestCanonicalConfiguredRootsPreservesNestedEntriesAndFirstOrder(t *testing.T) {
	outer := t.TempDir()
	nested := filepath.Join(outer, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := CanonicalConfigured([]string{nested, outer, nested})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{nested, outer}) {
		t.Fatalf("configured roots = %q", got)
	}
}

func TestPreferencePlacesAgentThenConfiguredThenRemaining(t *testing.T) {
	set := Set{
		Roots:      []string{"/configured-a", "/configured-b", "/agent-a", "/agent-b"},
		Configured: []string{"/configured-a", "/configured-b"},
		AgentRoot:  map[string]string{"a": "/agent-a", "b": "/agent-b"},
	}
	if got := set.Preference("b"); !reflect.DeepEqual(got, []string{"/agent-b", "/configured-a", "/configured-b", "/agent-a"}) {
		t.Fatalf("preference = %q", got)
	}
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

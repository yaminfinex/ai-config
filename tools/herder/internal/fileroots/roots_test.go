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

func TestBuildMapsEveryCWDInOneRepoToItsTopLevel(t *testing.T) {
	repo := newRepo(t)
	deeper := filepath.Join(repo, "sub", "deeper")
	if err := os.MkdirAll(deeper, 0o755); err != nil {
		t.Fatal(err)
	}
	configured := newRepo(t)

	set, err := Build(context.Background(), []string{configured}, []Agent{
		{Name: "deeper", CWD: deeper},
		{Name: "top", CWD: repo},
		{Name: "sub", CWD: filepath.Join(repo, "sub")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set.Roots, []string{configured, repo}) {
		t.Fatalf("roots = %q", set.Roots)
	}
	want := map[string]string{"deeper": repo, "top": repo, "sub": repo}
	if !reflect.DeepEqual(set.AgentRoot, want) {
		t.Fatalf("agent roots = %#v", set.AgentRoot)
	}
}

func TestBuildNeverFoldsLinkedWorktreeCWD(t *testing.T) {
	repo := newRepo(t)
	writeFile(t, repo, "tracked.md", "fixture\n")
	git(t, repo, "add", "tracked.md")
	git(t, repo, "commit", "-m", "fixture")
	worktree := filepath.Join(t.TempDir(), "linked")
	git(t, repo, "worktree", "add", "-b", "feature", worktree)

	set, err := Build(context.Background(), nil, []Agent{
		{Name: "repo", CWD: repo},
		{Name: "worktree", CWD: filepath.Join(worktree)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set.Roots, []string{repo, worktree}) || set.AgentRoot["worktree"] != worktree || set.AgentRoot["repo"] != repo {
		t.Fatalf("set = %#v", set)
	}
}

func TestBuildKeepsNestedRepoAsItsOwnRoot(t *testing.T) {
	outer := newRepo(t)
	nested := filepath.Join(outer, "vendor", "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, nested, "init", "-q", "-b", "main")

	set, err := Build(context.Background(), nil, []Agent{
		{Name: "outer", CWD: outer},
		{Name: "nested", CWD: filepath.Join(nested)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set.Roots, []string{outer, nested}) || set.AgentRoot["nested"] != nested || set.AgentRoot["outer"] != outer {
		t.Fatalf("set = %#v", set)
	}
}

func TestBuildDropsCWDOutsideAnyRepoAndWhenGitIsMissing(t *testing.T) {
	plain := t.TempDir()
	writeFile(t, plain, "note.md", "not indexed\n")
	repo := newRepo(t)
	set, err := Build(context.Background(), nil, []Agent{{Name: "plain", CWD: plain}, {Name: "repo", CWD: repo}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set.Roots, []string{repo}) || !reflect.DeepEqual(set.AgentRoot, map[string]string{"repo": repo}) {
		t.Fatalf("set = %#v", set)
	}

	t.Setenv("PATH", t.TempDir())
	set, err = Build(context.Background(), nil, []Agent{{Name: "repo", CWD: repo}})
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Roots) != 0 || len(set.AgentRoot) != 0 {
		t.Fatalf("set without git = %#v", set)
	}
}

func TestCanonicalConfiguredRootsPreservesNestedEntriesAndFirstOrder(t *testing.T) {
	outer := newRepo(t)
	nested := filepath.Join(outer, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, nested, "init", "-q", "-b", "main")
	got, err := CanonicalConfigured(context.Background(), []string{nested, outer, nested})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{nested, outer}) {
		t.Fatalf("configured roots = %q", got)
	}
}

func TestCanonicalConfiguredRefusesNonGitTopLevel(t *testing.T) {
	repo := newRepo(t)
	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{t.TempDir(), sub} {
		if _, err := CanonicalConfigured(context.Background(), []string{path}); err == nil || !strings.Contains(err.Error(), "not a git repository top level") {
			t.Fatalf("CanonicalConfigured(%q) err = %v", path, err)
		}
	}
	if _, err := CanonicalConfigured(context.Background(), []string{filepath.Join(t.TempDir(), "missing")}); err == nil || !strings.Contains(err.Error(), "not an existing directory") {
		t.Fatalf("missing err = %v", err)
	}
}

func TestMostSpecificPicksLongestAncestorRoot(t *testing.T) {
	set := Set{Roots: []string{"/a", "/a/b", "/a/bc", "/x"}}
	tests := []struct {
		path string
		want string
		ok   bool
	}{
		{"/a/b/c/d", "/a/b", true},
		{"/a/bcd", "/a", true},
		{"/a/bc/e", "/a/bc", true},
		{"/a", "/a", true},
		{"/a/b/../b/z", "/a/b", true},
		{"/y", "", false},
		{"relative", "", false},
	}
	for _, test := range tests {
		got, ok := set.MostSpecific(test.path)
		if got != test.want || ok != test.ok {
			t.Errorf("MostSpecific(%q) = %q %v, want %q %v", test.path, got, ok, test.want, test.ok)
		}
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

func newRepo(t *testing.T) string {
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

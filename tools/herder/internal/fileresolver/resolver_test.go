package fileresolver

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ai-config/tools/herder/internal/fileindex"
)

type staticSource map[string][]string

func (s staticSource) Candidates(_ context.Context, root string, _ bool) ([]string, error) {
	return append([]string(nil), s[root]...), nil
}

func TestResolveHardTiersExactSuffixAndFuzzyAcrossCorpus(t *testing.T) {
	fixtures := []struct {
		query  string
		exact  string
		suffix string
		fuzzy  string
	}{
		{"docs/target.md", "docs/target.md", "archive/docs/target.md", "docs/the-target-file.md"},
		{"plans/alpha.md", "plans/alpha.md", "old/plans/alpha.md", "plans/all-phase-notes.md"},
		{"src/main.go", "src/main.go", "copy/src/main.go", "src/my-main-code.go"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.query, func(t *testing.T) {
			root := newResolverGitRepo(t)
			for _, path := range []string{fixture.fuzzy, fixture.suffix, fixture.exact} {
				writeResolverFile(t, root, path, path+"\n")
			}
			resolverGit(t, root, "add", ".")
			resolverGit(t, root, "commit", "-m", "ranking fixture")
			results, err := New(fileindex.New(fileindex.Options{})).Resolve(context.Background(), Request{
				Query: fixture.query,
				Roots: []string{root},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 3 {
				t.Fatalf("results=%#v", results)
			}
			got := []Tier{results[0].Tier, results[1].Tier, results[2].Tier}
			want := []Tier{TierExact, TierSuffix, TierFuzzy}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("tiers=%v want %v; results=%#v", got, want, results)
			}
		})
	}
}

func TestResolveSuffixMatchesMissionsPathStutter(t *testing.T) {
	root := newResolverGitRepo(t)
	writeResolverFile(t, root, "missions/missions/fleet-refit/x.md", "fixture\n")
	resolverGit(t, root, "add", ".")
	resolverGit(t, root, "commit", "-m", "stutter fixture")
	results, err := New(fileindex.New(fileindex.Options{})).Resolve(context.Background(), Request{
		Query: "missions/fleet-refit/x.md",
		Roots: []string{root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Tier != TierSuffix {
		t.Fatalf("results=%#v", results)
	}
}

func TestResolveUppercaseQueryMatchesExactAndSuffixPaths(t *testing.T) {
	root := newResolverGitRepo(t)
	writeResolverFile(t, root, "README.md", "root readme\n")
	writeResolverFile(t, root, "tools/herder/README.md", "nested readme\n")
	resolverGit(t, root, "add", ".")
	resolverGit(t, root, "commit", "-m", "uppercase fixture")

	results, err := New(fileindex.New(fileindex.Options{})).Resolve(context.Background(), Request{
		Query: "README.md",
		Roots: []string{root},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		path string
		tier Tier
	}{
		{path: "README.md", tier: TierExact},
		{path: "tools/herder/README.md", tier: TierSuffix},
	}
	if len(results) != len(want) {
		t.Fatalf("results=%#v want paths/tiers %#v", results, want)
	}
	for index, expected := range want {
		if results[index].Root != root || results[index].Path != expected.path || results[index].Tier != expected.tier || results[index].Score <= 0 {
			t.Fatalf("result[%d]=%#v want root=%q path=%q tier=%q positive score", index, results[index], root, expected.path, expected.tier)
		}
	}
}

func TestResolvePreservesTierIdentityForAutoOpenRule(t *testing.T) {
	root := "/repo"
	results, err := New(staticSource{root: {"docs", "docs/file.md", "archive/docs"}}).Resolve(context.Background(), Request{
		Query: "docs",
		Roots: []string{root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("results=%#v", results)
	}
	got := map[string]Tier{}
	for _, result := range results {
		got[result.Path] = result.Tier
	}
	if got["docs"] != TierExact || got["docs/file.md"] != TierPrefix || got["archive/docs"] != TierSuffix {
		t.Fatalf("tier identity lost: %#v", got)
	}
}

func TestResolveRespectsRootPreferenceAndStableTies(t *testing.T) {
	rootA, rootB := newResolverGitRepo(t), newResolverGitRepo(t)
	for _, root := range []string{rootA, rootB} {
		writeResolverFile(t, root, "z/readme.md", "z\n")
		writeResolverFile(t, root, "a/readme.md", "a\n")
		resolverGit(t, root, "add", ".")
		resolverGit(t, root, "commit", "-m", "preference fixture")
	}
	results, err := New(fileindex.New(fileindex.Options{})).Resolve(context.Background(), Request{
		Query:          "readme.md",
		Roots:          []string{rootA, rootB},
		RootPreference: []string{rootB, rootA},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{
		{Root: rootB, Path: "a/readme.md", Tier: TierSuffix, Score: results[0].Score},
		{Root: rootB, Path: "z/readme.md", Tier: TierSuffix, Score: results[1].Score},
		{Root: rootA, Path: "a/readme.md", Tier: TierSuffix, Score: results[2].Score},
		{Root: rootA, Path: "z/readme.md", Tier: TierSuffix, Score: results[3].Score},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("results=%#v want %#v", results, want)
	}

	stable, err := New(staticSource{"/stable": {"z/readme.md", "a/readme.md"}}).Resolve(context.Background(), Request{
		Query: "readme.md", Roots: []string{"/stable"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stable[0].Path != "z/readme.md" || stable[1].Path != "a/readme.md" {
		t.Fatalf("stable ties reordered: %#v", stable)
	}
}

func TestResolvePropagatesCandidateSourceFailure(t *testing.T) {
	sourceErr := errors.New("index unavailable")
	source := sourceFunc(func(context.Context, string, bool) ([]string, error) { return nil, sourceErr })
	_, err := New(source).Resolve(context.Background(), Request{Query: "x", Roots: []string{"/root"}})
	if !errors.Is(err, sourceErr) {
		t.Fatalf("error=%v", err)
	}
}

func TestResolvePassesForcedRefreshToEveryRoot(t *testing.T) {
	var refreshed []string
	source := sourceFunc(func(_ context.Context, root string, refresh bool) ([]string, error) {
		if refresh {
			refreshed = append(refreshed, root)
		}
		return []string{"x"}, nil
	})
	_, err := New(source).Resolve(context.Background(), Request{Query: "x", Roots: []string{"/a", "/b"}, Refresh: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(refreshed, []string{"/a", "/b"}) {
		t.Fatalf("refreshed roots=%q", refreshed)
	}
}

type sourceFunc func(context.Context, string, bool) ([]string, error)

func (f sourceFunc) Candidates(ctx context.Context, root string, refresh bool) ([]string, error) {
	return f(ctx, root, refresh)
}

func TestNormalizeQuery(t *testing.T) {
	tests := []struct {
		input string
		want  NormalizedQuery
	}{
		{"`docs/file.md`", NormalizedQuery{Path: "docs/file.md"}},
		{`"docs/file.md",`, NormalizedQuery{Path: "docs/file.md"}},
		{"'docs/file.md';", NormalizedQuery{Path: "docs/file.md"}},
		{"docs/file.md:123).", NormalizedQuery{Path: "docs/file.md", Line: 123}},
		{"backlog/A name with spaces — retained.md:7", NormalizedQuery{Path: "backlog/A name with spaces — retained.md", Line: 7}},
		{`literal\backslash.md`, NormalizedQuery{Path: `literal\backslash.md`}},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			if got := NormalizeQuery(test.input); got != test.want {
				t.Fatalf("NormalizeQuery(%q)=%#v want %#v", test.input, got, test.want)
			}
		})
	}
}

func TestResolveWithinRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "nested", "file.md")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveWithinRoot(root, "nested/file.md")
	if err != nil || resolved != inside {
		t.Fatalf("resolved=%q err=%v", resolved, err)
	}

	outsideRoot := t.TempDir()
	outside := filepath.Join(outsideRoot, "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = ResolveWithinRoot(root, filepath.Join("..", filepath.Base(outsideRoot), "outside.md"))
	if err == nil || !strings.Contains(err.Error(), root) || !strings.Contains(err.Error(), outside) {
		t.Fatalf("escape error=%v", err)
	}

	symlink := filepath.Join(root, "escape.md")
	if err := os.Symlink(outside, symlink); err != nil {
		t.Fatal(err)
	}
	_, err = ResolveWithinRoot(root, "escape.md")
	if err == nil || !strings.Contains(err.Error(), symlink) || !strings.Contains(err.Error(), outside) {
		t.Fatalf("symlink escape error=%v", err)
	}
}

func newResolverGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	resolverGit(t, root, "init", "-q")
	resolverGit(t, root, "config", "user.name", "Fixture")
	resolverGit(t, root, "config", "user.email", "fixture@example.invalid")
	return root
}

func resolverGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeResolverFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

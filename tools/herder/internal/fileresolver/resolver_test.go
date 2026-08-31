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

	"ai-config/tools/herder/internal/filecandidate"
	"ai-config/tools/herder/internal/fileindex"
)

type staticSource map[string][]filecandidate.Candidate

func (s staticSource) Candidates(_ context.Context, root string, _ bool) ([]filecandidate.Candidate, error) {
	return append([]filecandidate.Candidate(nil), s[root]...), nil
}

type degradedFixtureError struct{ detail string }

func (e degradedFixtureError) Error() string  { return e.detail }
func (e degradedFixtureError) Degraded() bool { return true }

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

func TestResolveAbsoluteOwnerPathAfterNormalization(t *testing.T) {
	root := newResolverGitRepo(t)
	const ownerPath = "missions/missions/fleet-refit/artifacts/conductor/briefs/git-view-design-brief.md"
	writeResolverFile(t, root, ownerPath, "fixture\n")
	writeResolverFile(t, root, "README.md", "uppercase fixture\n")
	resolverGit(t, root, "add", ".")
	resolverGit(t, root, "commit", "-m", "absolute path fixtures")

	resolver := New(fileindex.New(fileindex.Options{}))
	ownerAbsolute := filepath.Join(root, filepath.FromSlash(ownerPath))
	for _, query := range []string{ownerAbsolute, "`" + ownerAbsolute + ":4400`"} {
		results, err := resolver.Resolve(context.Background(), Request{Query: query, Roots: []string{root}})
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 || results[0].Root != root || results[0].Path != ownerPath || results[0].Tier != TierExact || results[0].Score <= 0 {
			t.Fatalf("query=%q results=%#v", query, results)
		}
	}

	results, err := resolver.Resolve(context.Background(), Request{
		Query: filepath.Join(root, "README.md"), Roots: []string{root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Path != "README.md" || results[0].Tier != TierExact {
		t.Fatalf("uppercase absolute results=%#v", results)
	}
}

func TestResolveAbsoluteQueryAcrossNestedRoots(t *testing.T) {
	parent := newResolverGitRepo(t)
	nested := filepath.Join(parent, "worktrees", "resolver-absolute")
	const nestedPath = "artifacts/conductor/target.md"
	const parentPath = "worktrees/resolver-absolute/" + nestedPath
	writeResolverFile(t, parent, parentPath, "target\n")
	writeResolverFile(t, parent, "archive/"+parentPath, "parent suffix\n")
	resolverGit(t, parent, "add", ".")
	resolverGit(t, parent, "commit", "-m", "nested root fixture")

	results, err := New(fileindex.New(fileindex.Options{})).Resolve(context.Background(), Request{
		Query:          filepath.Join(nested, filepath.FromSlash(nestedPath)),
		Roots:          []string{parent, nested},
		RootPreference: []string{nested, parent},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The nested and parent roots both contain the queried file; only the
	// best-ranked occurrence may survive so the client never offers the same
	// absolute file twice.
	want := []struct {
		root string
		path string
		tier Tier
	}{
		{nested, nestedPath, TierExact},
		{parent, "archive/" + parentPath, TierSuffix},
	}
	if len(results) != len(want) {
		t.Fatalf("results=%#v", results)
	}
	seen := make(map[string]bool, len(results))
	for index, expected := range want {
		result := results[index]
		if result.Root != expected.root || result.Path != expected.path || result.Tier != expected.tier || result.Score <= 0 {
			t.Fatalf("result[%d]=%#v want root=%q path=%q tier=%q positive score", index, result, expected.root, expected.path, expected.tier)
		}
		key := result.Root + "\x00" + result.Path
		if seen[key] {
			t.Fatalf("duplicate result invented: %#v", result)
		}
		seen[key] = true
	}
}

func TestResolveAbsoluteQueryBoundariesAndTrailingSlashes(t *testing.T) {
	root := newResolverGitRepo(t)
	writeResolverFile(t, root, "docs/file.md", "fixture\n")
	writeResolverFile(t, root, "file.md", "fixture\n")
	resolverGit(t, root, "add", ".")
	resolverGit(t, root, "commit", "-m", "absolute boundary fixtures")
	resolver := New(fileindex.New(fileindex.Options{}))

	resolution, err := resolver.ResolveDetailed(context.Background(), Request{
		Query: filepath.Join(root, "docs") + string(filepath.Separator),
		Roots: []string{root + string(filepath.Separator), root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolution.Roots) != 1 || len(resolution.Results) != 1 || resolution.Results[0].Path != "docs/file.md" || resolution.Results[0].Tier != TierPrefix {
		t.Fatalf("directory trailing slash resolution=%#v", resolution)
	}

	queriesWithNoMatch := []string{
		filepath.Join(root, "file.md") + string(filepath.Separator),
		root,
		root + string(filepath.Separator),
		root + "2" + string(filepath.Separator) + "file.md",
		filepath.Join(filepath.Dir(root), "outside", "file.md"),
	}
	for _, query := range queriesWithNoMatch {
		results, err := resolver.Resolve(context.Background(), Request{Query: query, Roots: []string{root}})
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 0 {
			t.Fatalf("query=%q results=%#v want empty", query, results)
		}
	}
}

func TestResolveRelativeQueryRegressionIncludesScores(t *testing.T) {
	root := newResolverGitRepo(t)
	for _, path := range []string{"docs/target.md", "archive/docs/target.md", "docs/the-target-file.md"} {
		writeResolverFile(t, root, path, "fixture\n")
	}
	resolverGit(t, root, "add", ".")
	resolverGit(t, root, "commit", "-m", "relative regression fixture")

	results, err := New(fileindex.New(fileindex.Options{})).Resolve(context.Background(), Request{
		Query: "docs/target.md", Roots: []string{root},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Result{
		{Root: root, Path: "docs/target.md", Kind: filecandidate.KindFile, Tier: TierExact, Score: 344},
		{Root: root, Path: "archive/docs/target.md", Kind: filecandidate.KindFile, Tier: TierSuffix, Score: 359},
		{Root: root, Path: "docs/the-target-file.md", Kind: filecandidate.KindFile, Tier: TierFuzzy, Score: 314},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("relative results=%#v want %#v", results, want)
	}
}

func TestResolvePreservesTierIdentityForAutoOpenRule(t *testing.T) {
	root := "/repo"
	results, err := New(staticSource{root: fileCandidates("docs", "docs/file.md", "archive/docs")}).Resolve(context.Background(), Request{
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

func TestResolveAppliesOnePointDirectoryPenaltyOnlyWithinHardBands(t *testing.T) {
	root := "/repo"
	results, err := New(staticSource{root: {
		{Path: "same", Kind: filecandidate.KindDir},
		{Path: "same", Kind: filecandidate.KindFile},
		{Path: "archive/same", Kind: filecandidate.KindFile},
	}}).Resolve(context.Background(), Request{Query: "same", Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("results=%#v", results)
	}
	if results[0].Kind != filecandidate.KindFile || results[1].Kind != filecandidate.KindDir {
		t.Fatalf("same-score file/dir order=%#v", results[:2])
	}
	if results[0].Score != results[1].Score {
		t.Fatalf("raw engine scores changed: %#v", results[:2])
	}
	if results[2].Tier != TierSuffix {
		t.Fatalf("directory penalty crossed hard tier band: %#v", results)
	}
}

func TestResolveAbsoluteDirectoryUsesSameRootStrippingAsFiles(t *testing.T) {
	root := newResolverGitRepo(t)
	writeResolverFile(t, root, "backlog/tasks/task.md", "fixture\n")
	resolverGit(t, root, "add", ".")
	resolverGit(t, root, "commit", "-m", "directory fixture")

	results, err := New(fileindex.New(fileindex.Options{})).Resolve(context.Background(), Request{
		Query: filepath.Join(root, "backlog"), Roots: []string{root},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].Path != "backlog" || results[0].Kind != filecandidate.KindDir || results[0].Tier != TierExact {
		t.Fatalf("absolute directory results=%#v", results)
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
		{Root: rootB, Path: "a/readme.md", Kind: filecandidate.KindFile, Tier: TierSuffix, Score: results[0].Score},
		{Root: rootB, Path: "z/readme.md", Kind: filecandidate.KindFile, Tier: TierSuffix, Score: results[1].Score},
		{Root: rootA, Path: "a/readme.md", Kind: filecandidate.KindFile, Tier: TierSuffix, Score: results[2].Score},
		{Root: rootA, Path: "z/readme.md", Kind: filecandidate.KindFile, Tier: TierSuffix, Score: results[3].Score},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("results=%#v want %#v", results, want)
	}

	stable, err := New(staticSource{"/stable": fileCandidates("z/readme.md", "a/readme.md")}).Resolve(context.Background(), Request{
		Query: "readme.md", Roots: []string{"/stable"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stable[0].Path != "z/readme.md" || stable[1].Path != "a/readme.md" {
		t.Fatalf("stable ties reordered: %#v", stable)
	}
}

func TestResolveFileAnchorRanksContainingDirectoryThenAncestorsThenGlobal(t *testing.T) {
	const root = "/repo"
	const other = "/other"
	resolver := New(staticSource{
		root: fileCandidates(
			"elsewhere/target.md",
			"target.md",
			"docs/target.md",
			"docs/guides/target.md",
			"docs/guides/deep/target.md",
		),
		other: fileCandidates("target.md"),
	})
	results, err := resolver.Resolve(context.Background(), Request{
		Query: "target.md", Roots: []string{root, other}, Anchor: &Anchor{Root: root, Path: "docs/guides/deep/topic.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		root + ":docs/guides/deep/target.md",
		root + ":docs/guides/target.md",
		root + ":docs/target.md",
		root + ":target.md",
		other + ":target.md",
		root + ":elsewhere/target.md",
	}
	got := make([]string, len(results))
	for index, result := range results {
		got[index] = result.Root + ":" + result.Path
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths=%#v want %#v", got, want)
	}
	for index, result := range results[:4] {
		if result.Tier != TierExact {
			t.Errorf("anchored result %d tier=%q want %q", index, result.Tier, TierExact)
		}
	}
	if results[4].Tier != TierExact || results[5].Tier != TierSuffix {
		t.Errorf("global tiers=(%q, %q) want (%q, %q)", results[4].Tier, results[5].Tier, TierExact, TierSuffix)
	}
}

func TestResolveExplicitRelativeAnchorNeverFallsBack(t *testing.T) {
	const root = "/repo"
	resolver := New(staticSource{root: fileCandidates(
		"target.md",
		"docs/guides/target.md",
		"docs/guides/deep/target.md",
		"elsewhere/missing.md",
	)})
	anchor := &Anchor{Root: root, Path: "docs/guides/deep/topic.md"}
	tests := []struct {
		query string
		want  []string
	}{
		{"./target.md", []string{"docs/guides/deep/target.md"}},
		{"../target.md", []string{"docs/guides/target.md"}},
		{"`../target.md`", []string{"docs/guides/target.md"}},
		{"./missing.md", nil},
		{"../../../../target.md", nil},
	}
	for _, test := range tests {
		t.Run(test.query, func(t *testing.T) {
			results, err := resolver.Resolve(context.Background(), Request{Query: test.query, Roots: []string{root}, Anchor: anchor})
			if err != nil {
				t.Fatal(err)
			}
			if got := resultPaths(results); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("paths=%#v want %#v", got, test.want)
			}
		})
	}
}

func resultPaths(results []Result) []string {
	if len(results) == 0 {
		return nil
	}
	paths := make([]string, len(results))
	for index, result := range results {
		paths[index] = result.Path
	}
	return paths
}

func TestResolvePropagatesCandidateSourceFailure(t *testing.T) {
	sourceErr := errors.New("index unavailable")
	source := sourceFunc(func(context.Context, string, bool) ([]filecandidate.Candidate, error) { return nil, sourceErr })
	_, err := New(source).Resolve(context.Background(), Request{Query: "x", Roots: []string{"/root"}})
	if !errors.Is(err, sourceErr) {
		t.Fatalf("error=%v", err)
	}
}

func TestResolveDetailedReportsEachRootAndRanksUsableCandidatesGlobally(t *testing.T) {
	source := sourceFunc(func(_ context.Context, root string, _ bool) ([]filecandidate.Candidate, error) {
		switch root {
		case "/healthy":
			return fileCandidates("z/needle.md"), nil
		case "/degraded":
			return fileCandidates("a/needle.md"), degradedFixtureError{"permission denied below root"}
		default:
			return fileCandidates("must-not-leak.md"), errors.New("index failed")
		}
	})
	resolution, err := New(source).ResolveDetailed(context.Background(), Request{
		Query: "needle.md", Roots: []string{"/healthy", "/degraded", "/failed"}, RootPreference: []string{"/degraded"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolution.Results) != 2 || resolution.Results[0].Root != "/degraded" || resolution.Results[1].Root != "/healthy" {
		t.Fatalf("results=%#v", resolution.Results)
	}
	want := []RootStatus{RootComplete, RootDegraded, RootFailed}
	for i, status := range want {
		if resolution.Roots[i].Status != status {
			t.Errorf("roots[%d]=%#v want %q", i, resolution.Roots[i], status)
		}
	}
}

func TestResolvePassesForcedRefreshToEveryRoot(t *testing.T) {
	var refreshed []string
	source := sourceFunc(func(_ context.Context, root string, refresh bool) ([]filecandidate.Candidate, error) {
		if refresh {
			refreshed = append(refreshed, root)
		}
		return fileCandidates("x"), nil
	})
	_, err := New(source).Resolve(context.Background(), Request{Query: "x", Roots: []string{"/a", "/b"}, Refresh: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(refreshed, []string{"/a", "/b"}) {
		t.Fatalf("refreshed roots=%q", refreshed)
	}
}

type sourceFunc func(context.Context, string, bool) ([]filecandidate.Candidate, error)

func (f sourceFunc) Candidates(ctx context.Context, root string, refresh bool) ([]filecandidate.Candidate, error) {
	return f(ctx, root, refresh)
}

func fileCandidates(paths ...string) []filecandidate.Candidate {
	candidates := make([]filecandidate.Candidate, 0, len(paths))
	for _, path := range paths {
		candidates = append(candidates, filecandidate.Candidate{Path: path, Kind: filecandidate.KindFile})
	}
	return candidates
}

func TestNormalizeQuery(t *testing.T) {
	tests := []struct {
		input string
		want  NormalizedQuery
	}{
		{"`docs/file.md`", NormalizedQuery{Path: "docs/file.md"}},
		{`"docs/file.md",`, NormalizedQuery{Path: "docs/file.md"}},
		{"'docs/file.md';", NormalizedQuery{Path: "docs/file.md"}},
		{"(docs/file.md)", NormalizedQuery{Path: "docs/file.md"}},
		{"[docs/file.md]", NormalizedQuery{Path: "docs/file.md"}},
		{"{docs/file.md}", NormalizedQuery{Path: "docs/file.md"}},
		{"<docs/file.md>", NormalizedQuery{Path: "docs/file.md"}},
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

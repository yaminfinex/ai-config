package gitapi

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseGitHubRemoteAcceptsOnlyProvedGitHubForms(t *testing.T) {
	for _, tc := range []struct {
		remote, owner, repo string
		ok                  bool
	}{
		{"git@github.com:acme/widgets.git", "acme", "widgets", true},
		{"git@github.com:acme/widgets", "acme", "widgets", true},
		{"github.com:acme/widgets.git", "acme", "widgets", true},
		{"ssh://git@github.com/acme/widgets.git", "acme", "widgets", true},
		{"ssh://git@github.com:22/acme/widgets", "acme", "widgets", true},
		{"ssh://git@ssh.github.com:443/acme/widgets.git", "acme", "widgets", true},
		{"https://github.com/acme/widgets.git", "acme", "widgets", true},
		{"https://github.com/acme/widgets", "acme", "widgets", true},
		{"https://github.com/acme/widgets/", "acme", "widgets", true},
		{"https://token@github.com/acme/my.repo-2.git\n", "acme", "my.repo-2", true},
		{"https://GitHub.com/Acme/Widgets.git", "Acme", "Widgets", true},
		{"git://github.com/acme/widgets.git", "acme", "widgets", true},
		{"github-work:acme/widgets.git", "", "", false},
		{"git@github-work:acme/widgets.git", "", "", false},
		{"https://gitlab.com/acme/widgets.git", "", "", false},
		{"https://github.com.evil.example/acme/widgets.git", "", "", false},
		{"https://github.com/acme", "", "", false},
		{"https://github.com/acme/widgets/tree/main", "", "", false},
		{"https://github.com/-acme/widgets", "", "", false},
		{"https://github.com/acme/..", "", "", false},
		{"file:///srv/git/widgets.git", "", "", false},
		{"/srv/git/widgets.git", "", "", false},
		{"../widgets.git", "", "", false},
		{"", "", "", false},
	} {
		owner, repo, ok := ParseGitHubRemote(tc.remote)
		if owner != tc.owner || repo != tc.repo || ok != tc.ok {
			t.Errorf("ParseGitHubRemote(%q) = %q, %q, %v; want %q, %q, %v", tc.remote, owner, repo, ok, tc.owner, tc.repo, tc.ok)
		}
	}
}

// newGitHubRepo builds a checkout whose origin URL is GitHub while its
// remote-tracking refs come from a local bare repository, so no test touches
// the network.
func newGitHubRepo(t *testing.T, originURL string) (repo, bare string) {
	t.Helper()
	bare = filepath.Join(t.TempDir(), "origin.git")
	run(t, "", "git", "init", "--bare", "-q", "-b", "main", bare)
	repo = newRepo(t)
	write(t, repo, "docs/read me #1%.md", "one\ntwo\n")
	write(t, repo, "src/main.go", "package main\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "initial")
	git(t, repo, "remote", "add", "origin", originURL)
	git(t, repo, "config", "branch.main.remote", "origin")
	git(t, repo, "config", "branch.main.merge", "refs/heads/main")
	pushToBare(t, repo, bare, "main")
	git(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	return repo, bare
}

func pushToBare(t *testing.T, repo, bare, branch string) {
	t.Helper()
	git(t, repo, "push", "-q", bare, branch+":"+branch)
	git(t, repo, "fetch", "-q", bare, "+refs/heads/*:refs/remotes/origin/*")
}

func readGitHub(t *testing.T, root, path string) GitHubResult {
	t.Helper()
	got, err := ReadGitHub(context.Background(), root, path, func() time.Time { return fixtureNow })
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestGitHubPushedHeadLinksHEADForFileFolderAndRoot(t *testing.T) {
	repo, _ := newGitHubRepo(t, "git@github.com:acme/widgets.git")
	head := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))

	file := readGitHub(t, repo, "docs/read me #1%.md")
	want := GitHubLocation{Owner: "acme", Repo: "widgets", Path: "docs/read me #1%.md", Kind: "blob", SHA: head, Ref: "HEAD", AtHead: true}
	if file.GitHub == nil || *file.GitHub != want || file.Reason != "" || !file.FetchedAt.Equal(fixtureNow) {
		t.Fatalf("file = %#v (github %#v)", file, file.GitHub)
	}
	folder := readGitHub(t, repo, "docs")
	if folder.GitHub == nil || folder.GitHub.Kind != "tree" || folder.GitHub.Path != "docs" || folder.GitHub.SHA != head {
		t.Fatalf("folder = %#v", folder.GitHub)
	}
	root := readGitHub(t, repo, "")
	if root.GitHub == nil || root.GitHub.Kind != "tree" || root.GitHub.Path != "" || root.GitHub.SHA != head || root.GitHub.Uncommitted {
		t.Fatalf("root = %#v", root.GitHub)
	}
}

func TestGitHubHeadPushedOnAnotherBranchStillLinksHEAD(t *testing.T) {
	repo, bare := newGitHubRepo(t, "https://github.com/acme/widgets.git")
	git(t, repo, "switch", "-q", "-c", "feature")
	write(t, repo, "src/main.go", "package main\n\nfunc main() {}\n")
	git(t, repo, "commit", "-q", "-am", "feature work")
	pushToBare(t, repo, bare, "feature")
	// The local branch tracks origin/main, which does not contain HEAD; only the
	// scan of origin's refs proves HEAD is on GitHub.
	git(t, repo, "config", "branch.feature.remote", "origin")
	git(t, repo, "config", "branch.feature.merge", "refs/heads/main")
	head := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	got := readGitHub(t, repo, "src/main.go")
	if got.GitHub == nil || got.GitHub.SHA != head || !got.GitHub.AtHead || got.GitHub.Ref != "HEAD" {
		t.Fatalf("github = %#v reason %q", got.GitHub, got.Reason)
	}
}

func TestGitHubUnpushedHeadFallsBackToUpstreamOrDefault(t *testing.T) {
	repo, bare := newGitHubRepo(t, "ssh://git@github.com/acme/widgets")
	pushed := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	write(t, repo, "src/main.go", "package main\n\n// local\n")
	write(t, repo, "src/new.go", "package main\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "unpushed")

	got := readGitHub(t, repo, "src/main.go")
	want := GitHubLocation{Owner: "acme", Repo: "widgets", Path: "src/main.go", Kind: "blob", SHA: pushed, Ref: "origin/main", AtHead: false, Uncommitted: false, Differs: true}
	if got.GitHub == nil || *got.GitHub != want {
		t.Fatalf("upstream fallback = %#v reason %q", got.GitHub, got.Reason)
	}
	if same := readGitHub(t, repo, "docs/read me #1%.md"); same.GitHub == nil || same.GitHub.AtHead || same.GitHub.Differs {
		t.Fatalf("unchanged file on fallback = %#v", same.GitHub)
	}
	if absent := readGitHub(t, repo, "src/new.go"); absent.GitHub != nil || absent.Reason != "this path is not on origin/main" {
		t.Fatalf("file only in an unpushed commit = %#v reason %q", absent.GitHub, absent.Reason)
	}

	// A detached, unpushed HEAD has no upstream, so the link uses origin/HEAD.
	git(t, repo, "switch", "-q", "--detach")
	git(t, bare, "branch", "-q", "release", "main")
	git(t, repo, "fetch", "-q", bare, "+refs/heads/*:refs/remotes/origin/*")
	git(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/release")
	detached := readGitHub(t, repo, "src/main.go")
	if detached.GitHub == nil || detached.GitHub.SHA != pushed || detached.GitHub.Ref != "origin/release" || detached.GitHub.AtHead {
		t.Fatalf("detached fallback = %#v reason %q", detached.GitHub, detached.Reason)
	}
}

func TestGitHubFlagsUncommittedChanges(t *testing.T) {
	repo, _ := newGitHubRepo(t, "git@github.com:acme/widgets.git")
	write(t, repo, "src/main.go", "package main\n\n// dirty\n")
	got := readGitHub(t, repo, "src/main.go")
	if got.GitHub == nil || !got.GitHub.AtHead || !got.GitHub.Uncommitted || !got.GitHub.Differs {
		t.Fatalf("dirty file = %#v", got.GitHub)
	}
	if clean := readGitHub(t, repo, "docs/read me #1%.md"); clean.GitHub == nil || clean.GitHub.Uncommitted || clean.GitHub.Differs {
		t.Fatalf("clean sibling = %#v", clean.GitHub)
	}
	write(t, repo, "docs/draft.md", "draft\n")
	if folder := readGitHub(t, repo, "docs"); folder.GitHub == nil || !folder.GitHub.Uncommitted {
		t.Fatalf("folder with an untracked file = %#v", folder.GitHub)
	}
	if untracked := readGitHub(t, repo, "docs/draft.md"); untracked.GitHub != nil || untracked.Reason == "" {
		t.Fatalf("untracked file = %#v", untracked.GitHub)
	}
}

func TestGitHubOmitsNonGitHubAndNonRepositoryPaths(t *testing.T) {
	repo, bare := newGitHubRepo(t, "git@github.com:acme/widgets.git")
	git(t, repo, "remote", "set-url", "origin", bare)
	if got := readGitHub(t, repo, "src/main.go"); got.GitHub != nil || got.Reason != "origin is not a GitHub repository" {
		t.Fatalf("local-path origin = %#v reason %q", got.GitHub, got.Reason)
	}
	git(t, repo, "remote", "set-url", "origin", "github-work:acme/widgets.git")
	if got := readGitHub(t, repo, "src/main.go"); got.GitHub != nil {
		t.Fatalf("unproved SSH alias = %#v", got.GitHub)
	}
	// Git's own insteadOf rewrite proves an alias is GitHub.
	git(t, repo, "config", "url.git@github.com:.insteadOf", "github-work:")
	if got := readGitHub(t, repo, "src/main.go"); got.GitHub == nil || got.GitHub.Owner != "acme" || got.GitHub.Repo != "widgets" {
		t.Fatalf("insteadOf alias = %#v reason %q", got.GitHub, got.Reason)
	}
	git(t, repo, "remote", "remove", "origin")
	if got := readGitHub(t, repo, "src/main.go"); got.GitHub != nil || got.Reason != "this checkout has no origin remote" {
		t.Fatalf("no origin = %#v reason %q", got.GitHub, got.Reason)
	}

	plain := t.TempDir()
	write(t, plain, "notes.txt", "hi\n")
	if got := readGitHub(t, plain, "notes.txt"); got.GitHub != nil || got.Reason != "not a git repository" {
		t.Fatalf("non-repository = %#v reason %q", got.GitHub, got.Reason)
	}
	if got := readGitHub(t, plain, ""); got.GitHub != nil || got.Reason != "not a git repository" {
		t.Fatalf("non-repository root = %#v reason %q", got.GitHub, got.Reason)
	}
}

func TestGitHubPathIsRelativeToLinkedWorktreeAndNestedRoot(t *testing.T) {
	repo, _ := newGitHubRepo(t, "git@github.com:acme/widgets.git")
	head := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, repo, "worktree", "add", "-q", "--detach", linked, "main")

	got := readGitHub(t, linked, "src/main.go")
	if got.Root != linked || got.GitHub == nil || got.GitHub.Path != "src/main.go" || got.GitHub.SHA != head || !got.GitHub.AtHead {
		t.Fatalf("linked worktree = %#v", got.GitHub)
	}
	nested := readGitHub(t, filepath.Join(linked, "src"), "main.go")
	if nested.GitHub == nil || nested.GitHub.Path != "src/main.go" || nested.GitHub.Kind != "blob" {
		t.Fatalf("nested root file = %#v", nested.GitHub)
	}
	nestedRoot := readGitHub(t, filepath.Join(linked, "src"), "")
	if nestedRoot.GitHub == nil || nestedRoot.GitHub.Path != "src" || nestedRoot.GitHub.Kind != "tree" {
		t.Fatalf("nested root itself = %#v", nestedRoot.GitHub)
	}
}

func TestGitHubRefusesEscapesAndGitInternals(t *testing.T) {
	repo, _ := newGitHubRepo(t, "git@github.com:acme/widgets.git")
	for _, path := range []string{"../outside", ".git/config", "/etc/passwd"} {
		if _, err := ReadGitHub(context.Background(), repo, path, func() time.Time { return fixtureNow }); err == nil {
			t.Errorf("path %q was not refused", path)
		}
	}
}

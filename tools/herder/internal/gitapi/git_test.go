package gitapi

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var fixtureNow = time.Date(2026, 8, 28, 14, 0, 0, 731, time.UTC)

func TestStatusParsesMixedChangesAndCountsAgainstHEAD(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, "mixed.txt", "one\n")
	write(t, repo, "old.txt", "old\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")

	write(t, repo, "mixed.txt", "one\ntwo\n")
	git(t, repo, "add", "mixed.txt")
	write(t, repo, "mixed.txt", "one\ntwo\nthree\n")
	git(t, repo, "mv", "old.txt", "new.txt")
	write(t, repo, "untracked.txt", "new\n")

	got, err := ReadStatus(context.Background(), repo, func() time.Time { return fixtureNow })
	if err != nil {
		t.Fatal(err)
	}
	if got.Git != nil || got.Repo == nil || got.Repo.Branch != "main" || got.Entries == nil || len(*got.Entries) != 3 || !got.FetchedAt.Equal(fixtureNow) {
		t.Fatalf("status = %#v", got)
	}
	entries := statusByPath(*got.Entries)
	mixed := entries["mixed.txt"]
	if !mixed.Staged || !mixed.Unstaged || mixed.IndexKind != "modified" || mixed.WorktreeKind != "modified" || mixed.Additions == nil || *mixed.Additions != 2 || mixed.Deletions == nil || *mixed.Deletions != 0 {
		t.Fatalf("mixed entry = %#v", mixed)
	}
	renamed := entries["new.txt"]
	if renamed.Kind != "renamed" || renamed.OldPath != "old.txt" || !renamed.Staged {
		t.Fatalf("rename entry = %#v", renamed)
	}
	if untracked := entries["untracked.txt"]; untracked.Kind != "untracked" || untracked.Staged || !untracked.Unstaged {
		t.Fatalf("untracked entry = %#v", untracked)
	}
}

func TestStatusScopesMidRepoRootAndReportsConflictAndUnavailable(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, "inside/file.txt", "base\n")
	write(t, repo, "outside.txt", "base\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	write(t, repo, "inside/file.txt", "inside\n")
	write(t, repo, "outside.txt", "outside\n")

	got, err := ReadStatus(context.Background(), filepath.Join(repo, "inside"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Entries == nil || len(*got.Entries) != 1 || (*got.Entries)[0].Path != "file.txt" {
		t.Fatalf("scoped entries = %#v", got.Entries)
	}

	conflictRepo := newRepo(t)
	write(t, conflictRepo, "conflict.txt", "base\n")
	git(t, conflictRepo, "add", ".")
	git(t, conflictRepo, "commit", "-m", "base")
	git(t, conflictRepo, "switch", "-c", "other")
	write(t, conflictRepo, "conflict.txt", "other\n")
	git(t, conflictRepo, "commit", "-am", "other")
	git(t, conflictRepo, "switch", "main")
	write(t, conflictRepo, "conflict.txt", "main\n")
	git(t, conflictRepo, "commit", "-am", "main")
	gitFails(t, conflictRepo, "merge", "other")
	conflicted, err := ReadStatus(context.Background(), conflictRepo, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if conflicted.Entries == nil || len(*conflicted.Entries) != 1 || (*conflicted.Entries)[0].Kind != "conflicted" {
		t.Fatalf("conflict entries = %#v", conflicted.Entries)
	}

	plain := t.TempDir()
	unavailable, err := ReadStatus(context.Background(), plain, func() time.Time { return fixtureNow })
	if err != nil {
		t.Fatal(err)
	}
	if unavailable.Git == nil || unavailable.Git.Status != "unavailable" || unavailable.Entries != nil || !strings.Contains(unavailable.Git.Reason, "not a git repository") {
		t.Fatalf("unavailable = %#v", unavailable)
	}
}

func TestDiffBasesInsideRealLinkedWorktree(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	run(t, "", "git", "init", "--bare", "-q", origin)
	repo := newRepo(t)
	write(t, repo, "review.txt", "base\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "base")
	git(t, repo, "remote", "add", "origin", origin)
	git(t, repo, "push", "-u", "origin", "main")
	git(t, origin, "symbolic-ref", "HEAD", "refs/heads/main")
	git(t, repo, "remote", "set-head", "origin", "-a")

	linked := filepath.Join(t.TempDir(), "linked")
	git(t, repo, "worktree", "add", "-b", "feature/review", linked, "main")
	write(t, linked, "review.txt", "base\ncommitted\n")
	git(t, linked, "commit", "-am", "committed work")
	write(t, linked, "review.txt", "base\ncommitted\nuncommitted\n")

	branch, err := ReadDiff(context.Background(), linked, "review.txt", "branch", func() time.Time { return fixtureNow })
	if err != nil {
		t.Fatal(err)
	}
	if branch.Base.Kind != "branch" || branch.Base.DefaultRef != "origin/main" || !strings.Contains(branch.Base.Label, "committed and uncommitted") || !strings.Contains(branch.Patch, "+committed") || !strings.Contains(branch.Patch, "+uncommitted") || branch.Stats == nil || branch.Stats.Additions != 2 {
		t.Fatalf("branch diff = %#v", branch)
	}
	uncommitted, err := ReadDiff(context.Background(), linked, "review.txt", "uncommitted", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(uncommitted.Patch, "+committed") || !strings.Contains(uncommitted.Patch, "+uncommitted") {
		t.Fatalf("uncommitted patch = %q", uncommitted.Patch)
	}

	git(t, linked, "remote", "set-head", "origin", "-d")
	if _, err := ReadDiff(context.Background(), linked, "review.txt", "branch", time.Now); !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "origin/HEAD") {
		t.Fatalf("missing origin/HEAD error = %v", err)
	}
}

func TestDiffReportsRenameBinaryModeAndCaps(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, "old.txt", "old\n")
	writeBytes(t, repo, "image.bin", []byte{0, 1, 2})
	write(t, repo, "mode.sh", "#!/bin/sh\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	git(t, repo, "mv", "old.txt", "new.txt")
	writeBytes(t, repo, "image.bin", []byte{0, 1, 3})
	if err := os.Chmod(filepath.Join(repo, "mode.sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	renamed, err := ReadDiff(context.Background(), repo, "new.txt", "uncommitted", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Facts.Kind != "renamed" || renamed.Facts.OldPath != "old.txt" {
		t.Fatalf("rename facts = %#v patch=%q", renamed.Facts, renamed.Patch)
	}
	binary, err := ReadDiff(context.Background(), repo, "image.bin", "uncommitted", time.Now)
	if err != nil || !binary.Facts.Binary {
		t.Fatalf("binary diff = %#v err=%v", binary, err)
	}
	mode, err := ReadDiff(context.Background(), repo, "mode.sh", "uncommitted", time.Now)
	if err != nil || mode.Facts.OldMode != "100644" || mode.Facts.NewMode != "100755" {
		t.Fatalf("mode diff = %#v err=%v", mode, err)
	}

	write(t, repo, "large.txt", "seed\n")
	git(t, repo, "add", "large.txt")
	git(t, repo, "commit", "-m", "large seed")
	write(t, repo, "large.txt", strings.Repeat("0123456789abcdef\n", 20000))
	soft, err := ReadDiff(context.Background(), repo, "large.txt", "uncommitted", time.Now)
	if err != nil || !soft.Truncated || len(soft.Patch) > int(SoftCap) || soft.PatchBytes <= SoftCap {
		t.Fatalf("soft cap: bytes=%d len=%d truncated=%v err=%v", soft.PatchBytes, len(soft.Patch), soft.Truncated, err)
	}
	write(t, repo, "large.txt", strings.Repeat("0123456789abcdef\n", 270000))
	if _, err := ReadDiff(context.Background(), repo, "large.txt", "uncommitted", time.Now); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "4 MiB") {
		t.Fatalf("hard cap error = %v", err)
	}
}

func TestDiffDetectsCopyAndRendersUntrackedAsAdded(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, "source.txt", "one\ntwo\nthree\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "source")
	write(t, repo, "copy.txt", "one\ntwo\nthree\n")
	git(t, repo, "add", "copy.txt")
	write(t, repo, "untracked.txt", "new\n")

	copyResult, err := ReadDiff(context.Background(), repo, "copy.txt", "uncommitted", time.Now)
	if err != nil || copyResult.Facts.Kind != "copied" || copyResult.Facts.OldPath != "source.txt" {
		t.Fatalf("copy diff = %#v err=%v", copyResult, err)
	}
	untracked, err := ReadDiff(context.Background(), repo, "untracked.txt", "uncommitted", time.Now)
	if err != nil || untracked.Facts.Kind != "added" || untracked.Stats == nil || untracked.Stats.Additions != 1 || !strings.Contains(untracked.Patch, "+new") {
		t.Fatalf("untracked diff = %#v err=%v", untracked, err)
	}
}

func TestDiffDoesNotRunRepositoryTextconv(t *testing.T) {
	repo := newRepo(t)
	marker := filepath.Join(t.TempDir(), "textconv-ran")
	filter := filepath.Join(t.TempDir(), "textconv.sh")
	write(t, repo, ".gitattributes", "*.txt diff=fixture\n")
	write(t, repo, "file.txt", "base\n")
	if err := os.WriteFile(filter, []byte("#!/bin/sh\ntouch \""+marker+"\"\ncat \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "config", "diff.fixture.textconv", filter)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "base")
	write(t, repo, "file.txt", "changed\n")

	result, err := ReadDiff(context.Background(), repo, "file.txt", "uncommitted", time.Now)
	if err != nil || !strings.Contains(result.Patch, "+changed") {
		t.Fatalf("diff = %#v err=%v", result, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("repository textconv ran; marker stat error = %v", err)
	}
}

func TestLogFollowsRenameAndPaginatesWithOpaqueCursor(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, "old.txt", "zero\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "original")
	git(t, repo, "mv", "old.txt", "new.txt")
	git(t, repo, "commit", "-m", "rename")
	for index := 0; index < LogPageSize; index++ {
		write(t, repo, "new.txt", strings.Repeat("x", index+1)+"\n")
		git(t, repo, "commit", "-am", "change")
	}

	first, err := ReadLog(context.Background(), repo, "new.txt", "", func() time.Time { return fixtureNow })
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != LogPageSize || first.NextCursor == "" || !first.FetchedAt.Equal(fixtureNow) {
		t.Fatalf("first page = %#v", first)
	}
	second, err := ReadLog(context.Background(), repo, "new.txt", first.NextCursor, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Entries) != 2 || second.Entries[len(second.Entries)-1].Subject != "original" || second.NextCursor != "" {
		t.Fatalf("second page = %#v", second)
	}
	if _, err := ReadLog(context.Background(), repo, "other.txt", first.NextCursor, time.Now); !errors.Is(err, ErrRefused) {
		t.Fatalf("rebound cursor error = %v", err)
	}
	if _, err := ReadLog(context.Background(), repo, "new.txt", "not-a-cursor", time.Now); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("malformed cursor error = %v", err)
	}
}

func TestStatusDetachedHeadAndParserRefusal(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, "file.txt", "one\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	git(t, repo, "switch", "--detach")
	got, err := ReadStatus(context.Background(), repo, time.Now)
	if err != nil || got.Repo == nil || got.Repo.Head == "" || got.Repo.Branch != "" || got.Repo.BranchBase.Status != "unavailable" {
		t.Fatalf("detached status = %#v err=%v", got, err)
	}
	loc, err := discover(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseStatus(loc, []byte("x invented\x00")); err == nil {
		t.Fatal("unknown porcelain record was accepted")
	}
}

func TestStatusReportsGitExecutableFailureWithoutCallingItNonRepo(t *testing.T) {
	repo := newRepo(t)
	t.Setenv("PATH", t.TempDir())
	got, err := ReadStatus(context.Background(), repo, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Git == nil || got.Git.Reason == "not a git repository" || !strings.Contains(got.Git.Reason, "executable file not found") {
		t.Fatalf("missing git status = %#v", got)
	}
}

func TestGitPathsRefuseSymlinkEscape(t *testing.T) {
	repo := newRepo(t)
	outside := t.TempDir()
	write(t, outside, "secret.txt", "secret\n")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(repo, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDiff(context.Background(), repo, "escape.txt", "uncommitted", time.Now); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), outside) {
		t.Fatalf("escape error = %v", err)
	}
}

func TestHistoricalFilePreservesCRLFAndClassifiesBinaryAndCaps(t *testing.T) {
	repo := newRepo(t)
	writeBytes(t, repo, "crlf.txt", []byte("one\r\ntwo\r\n"))
	writeBytes(t, repo, "binary.bin", []byte{0, 1, 2})
	write(t, repo, "large.txt", strings.Repeat("a", int(SoftCap+17)))
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "files")
	sha := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))

	text, err := ReadFile(context.Background(), repo, "crlf.txt", sha)
	if err != nil || text.Content == nil || *text.Content != "one\r\ntwo\r\n" || text.Truncated == nil || *text.Truncated {
		t.Fatalf("text = %#v err=%v", text, err)
	}
	binary, err := ReadFile(context.Background(), repo, "binary.bin", sha)
	if err != nil || !binary.Binary || binary.Content != nil || binary.Truncated != nil {
		t.Fatalf("binary = %#v err=%v", binary, err)
	}
	large, err := ReadFile(context.Background(), repo, "large.txt", sha)
	if err != nil || large.Truncated == nil || !*large.Truncated || len(*large.Content) != int(SoftCap) {
		t.Fatalf("large = %#v err=%v", large, err)
	}
	if _, err := ReadFile(context.Background(), repo, "crlf.txt", "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid sha error = %v", err)
	}
}

func TestHistoricalFileRefusesHardCapAndLogRejectsUnknownPath(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, "huge.txt", strings.Repeat("x", int(HardCap+1)))
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "huge")
	sha := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	if _, err := ReadFile(context.Background(), repo, "huge.txt", sha); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "4 MiB") {
		t.Fatalf("hard file error = %v", err)
	}
	if _, err := ReadLog(context.Background(), repo, "missing.txt", "", time.Now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown log path error = %v", err)
	}
}

func statusByPath(entries []StatusEntry) map[string]StatusEntry {
	result := make(map[string]StatusEntry, len(entries))
	for _, entry := range entries {
		result[entry.Path] = entry
	}
	return result
}

func newRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	run(t, "", "git", "init", "-q", "-b", "main", repo)
	git(t, repo, "config", "user.name", "Fixture")
	git(t, repo, "config", "user.email", "fixture@example.invalid")
	return repo
}

func write(t *testing.T, root, name, content string) { writeBytes(t, root, name, []byte(content)) }

func writeBytes(t *testing.T, root, name string, content []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	return run(t, root, "git", args...)
}

func gitFails(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if err := cmd.Run(); err == nil {
		t.Fatalf("git %s unexpectedly succeeded", strings.Join(args, " "))
	}
}

func run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

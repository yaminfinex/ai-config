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

func TestBranchStatusEnumeratesCommittedRenameAndDirtyWorktree(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	run(t, "", "git", "init", "--bare", "-q", origin)
	repo := newRepo(t)
	write(t, repo, "old.txt", "old\n")
	write(t, repo, "work.txt", "base\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "base")
	git(t, repo, "remote", "add", "origin", origin)
	git(t, repo, "push", "-u", "origin", "main")
	git(t, origin, "symbolic-ref", "HEAD", "refs/heads/main")
	git(t, repo, "remote", "set-head", "origin", "-a")

	linked := filepath.Join(t.TempDir(), "linked")
	git(t, repo, "worktree", "add", "-b", "feature/status", linked, "main")
	git(t, linked, "mv", "old.txt", "new.txt")
	write(t, linked, "work.txt", "base\ncommitted\n")
	git(t, linked, "commit", "-am", "branch work")
	marker := filepath.Join(t.TempDir(), "external-diff-ran")
	externalDiff := filepath.Join(t.TempDir(), "external-diff.sh")
	writeExecutable(t, externalDiff, "#!/bin/sh\ntouch \""+marker+"\"\n")
	git(t, linked, "config", "diff.external", externalDiff)

	clean, err := ReadStatusAtBase(context.Background(), linked, "branch", func() time.Time { return fixtureNow })
	if err != nil {
		t.Fatal(err)
	}
	assertMarkerAbsent(t, marker)
	if clean.EntriesBase == nil || clean.EntriesBase.Kind != "branch" || clean.EntriesBase.Label != "merge-base with origin/main" || clean.Entries == nil || len(*clean.Entries) != 2 {
		t.Fatalf("clean branch status = %#v", clean)
	}
	cleanEntries := statusByPath(*clean.Entries)
	rename := cleanEntries["new.txt"]
	if rename.Kind != "renamed" || rename.OldPath != "old.txt" || rename.Staged || rename.Unstaged || rename.IndexKind != "" || rename.WorktreeKind != "" {
		t.Fatalf("committed rename = %#v", rename)
	}
	if committed := cleanEntries["work.txt"]; committed.Kind != "modified" || committed.Additions == nil || *committed.Additions != 1 || committed.Staged || committed.Unstaged {
		t.Fatalf("committed work = %#v", committed)
	}

	write(t, linked, "work.txt", "base\ncommitted\nuncommitted\n")
	write(t, linked, "untracked.txt", "new\n")
	dirty, err := ReadStatusAtBase(context.Background(), linked, "branch", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	assertMarkerAbsent(t, marker)
	dirtyEntries := statusByPath(*dirty.Entries)
	if len(dirtyEntries) != 3 || dirtyEntries["work.txt"].Additions == nil || *dirtyEntries["work.txt"].Additions != 2 || !dirtyEntries["work.txt"].Unstaged {
		t.Fatalf("dirty branch status = %#v", dirty)
	}
	if untracked := dirtyEntries["untracked.txt"]; untracked.Kind != "untracked" || untracked.Staged || !untracked.Unstaged {
		t.Fatalf("branch untracked = %#v", untracked)
	}
}

func TestBranchStatusFallsBackWhenBranchBaseIsUnavailable(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, "file.txt", "base\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "base")
	write(t, repo, "file.txt", "dirty\n")

	got, err := ReadStatusAtBase(context.Background(), repo, "branch", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Repo == nil || got.Repo.BranchBase.Status != "unavailable" || got.EntriesBase == nil || got.EntriesBase.Kind != "uncommitted" || got.Entries == nil || len(*got.Entries) != 1 {
		t.Fatalf("fallback status = %#v", got)
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

func TestCommitDiffReportsOrdinaryRenameBinaryRootAndFirstParentMerge(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, "root.txt", "root\n")
	write(t, repo, "old.txt", "old\n")
	writeBytes(t, repo, "image.bin", []byte{0, 1, 2})
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "root")
	rootSHA := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))

	rootDiff, err := ReadCommitDiff(context.Background(), repo, "root.txt", rootSHA)
	if err != nil || rootDiff.Base.Kind != "commit" || rootDiff.Base.SHA != rootSHA || rootDiff.Base.Label != "root commit vs empty tree" || rootDiff.Facts.Kind != "added" || !strings.Contains(rootDiff.Patch, "+root") {
		t.Fatalf("root diff = %#v err=%v", rootDiff, err)
	}

	write(t, repo, "root.txt", "root\nordinary\n")
	git(t, repo, "commit", "-am", "ordinary")
	ordinarySHA := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	ordinary, err := ReadCommitDiff(context.Background(), repo, "root.txt", ordinarySHA)
	if err != nil || ordinary.Base.Label != "commit vs parent" || ordinary.Facts.Kind != "modified" || ordinary.Stats == nil || ordinary.Stats.Additions != 1 || !strings.Contains(ordinary.Patch, "+ordinary") {
		t.Fatalf("ordinary diff = %#v err=%v", ordinary, err)
	}
	unchanged, err := ReadCommitDiff(context.Background(), repo, "old.txt", ordinarySHA)
	if err != nil || unchanged.Facts.Kind != "unchanged" || unchanged.Stats == nil || unchanged.Stats.Additions != 0 || unchanged.Patch != "" {
		t.Fatalf("unchanged diff = %#v err=%v", unchanged, err)
	}

	git(t, repo, "mv", "old.txt", "new.txt")
	git(t, repo, "commit", "-m", "rename")
	renameSHA := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	rename, err := ReadCommitDiff(context.Background(), repo, "new.txt", renameSHA)
	if err != nil || rename.Facts.Kind != "renamed" || rename.Facts.OldPath != "old.txt" {
		t.Fatalf("rename diff = %#v err=%v", rename, err)
	}

	writeBytes(t, repo, "image.bin", []byte{0, 1, 3})
	git(t, repo, "commit", "-am", "binary")
	binarySHA := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	binary, err := ReadCommitDiff(context.Background(), repo, "image.bin", binarySHA)
	if err != nil || !binary.Facts.Binary || binary.Stats != nil {
		t.Fatalf("binary diff = %#v err=%v", binary, err)
	}

	git(t, repo, "switch", "-c", "feature")
	write(t, repo, "feature.txt", "feature\n")
	git(t, repo, "add", "feature.txt")
	git(t, repo, "commit", "-m", "feature")
	git(t, repo, "switch", "main")
	write(t, repo, "main.txt", "main\n")
	git(t, repo, "add", "main.txt")
	git(t, repo, "commit", "-m", "main")
	git(t, repo, "merge", "--no-ff", "feature", "-m", "merge")
	mergeSHA := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	merge, err := ReadCommitDiff(context.Background(), repo, "feature.txt", mergeSHA)
	if err != nil || merge.Base.Label != "merge commit vs first parent" || merge.Facts.Kind != "added" || !strings.Contains(merge.Patch, "+feature") {
		t.Fatalf("merge diff = %#v err=%v", merge, err)
	}

	if _, err := ReadCommitDiff(context.Background(), repo, "feature.txt", "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid commit error = %v", err)
	}
	if _, err := ReadCommitDiff(context.Background(), repo, "missing.txt", mergeSHA); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing path error = %v", err)
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
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "base")
	write(t, repo, "file.txt", "committed\n")
	git(t, repo, "commit", "-am", "changed")
	commitSHA := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	git(t, repo, "config", "diff.fixture.textconv", filter)
	write(t, repo, "file.txt", "working\n")

	result, err := ReadDiff(context.Background(), repo, "file.txt", "uncommitted", time.Now)
	if err != nil || !strings.Contains(result.Patch, "+working") {
		t.Fatalf("diff = %#v err=%v", result, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("repository textconv ran; marker stat error = %v", err)
	}
	commitDiff, err := ReadCommitDiff(context.Background(), repo, "file.txt", commitSHA)
	if err != nil || !strings.Contains(commitDiff.Patch, "+committed") {
		t.Fatalf("commit diff = %#v err=%v", commitDiff, err)
	}
	assertMarkerAbsent(t, marker)
}

func TestGitReadsDoNotRunRepositoryHooksOrFSMonitor(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, "file.txt", "base\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "base")
	write(t, repo, "file.txt", "committed\n")
	git(t, repo, "commit", "-am", "changed")
	commitSHA := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))

	markers := t.TempDir()
	hookMarker := filepath.Join(markers, "hook-ran")
	monitorMarker := filepath.Join(markers, "fsmonitor-ran")
	hooks := filepath.Join(t.TempDir(), "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(hooks, "post-index-change"), "#!/bin/sh\ntouch \""+hookMarker+"\"\n")
	monitor := filepath.Join(t.TempDir(), "fsmonitor.sh")
	writeExecutable(t, monitor, "#!/bin/sh\ntouch \""+monitorMarker+"\"\n")
	git(t, repo, "config", "core.hooksPath", hooks)
	git(t, repo, "config", "core.fsmonitor", monitor)
	write(t, repo, "file.txt", "working\n")

	status, err := ReadStatus(context.Background(), repo, time.Now)
	if err != nil || status.Entries == nil || len(*status.Entries) != 1 {
		t.Fatalf("status = %#v err=%v", status, err)
	}
	assertMarkerAbsent(t, hookMarker)
	assertMarkerAbsent(t, monitorMarker)

	diff, err := ReadDiff(context.Background(), repo, "file.txt", "uncommitted", time.Now)
	if err != nil || !strings.Contains(diff.Patch, "+working") {
		t.Fatalf("diff = %#v err=%v", diff, err)
	}
	assertMarkerAbsent(t, hookMarker)
	assertMarkerAbsent(t, monitorMarker)

	commitDiff, err := ReadCommitDiff(context.Background(), repo, "file.txt", commitSHA)
	if err != nil || !strings.Contains(commitDiff.Patch, "+committed") {
		t.Fatalf("commit diff = %#v err=%v", commitDiff, err)
	}
	assertMarkerAbsent(t, hookMarker)
	assertMarkerAbsent(t, monitorMarker)
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
	if first.Entries[0].PathThen != "new.txt" || second.Entries[0].PathThen != "new.txt" || second.Entries[1].PathThen != "old.txt" {
		t.Fatalf("paths across rename: first=%#v second=%#v", first.Entries[0], second.Entries)
	}
	if _, err := ReadLog(context.Background(), repo, "other.txt", first.NextCursor, time.Now); !errors.Is(err, ErrRefused) {
		t.Fatalf("rebound cursor error = %v", err)
	}
	if _, err := ReadLog(context.Background(), repo, "new.txt", "not-a-cursor", time.Now); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("malformed cursor error = %v", err)
	}
	loc, err := discover(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	badStatus := []byte(first.Entries[0].SHA + "\x00Fixture\x002026-08-28T12:34:56Z\x00bad\x00\x00\nZ\x00new.txt\x00")
	if _, err := parseLog(loc, badStatus); err == nil || !strings.Contains(err.Error(), "unrecognized name-status") {
		t.Fatalf("bad name-status error = %v", err)
	}
}

func TestStatusProvesCommitsAheadOfBaseIndependentlyFromUpstream(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	run(t, "", "git", "init", "--bare", "-q", origin)
	repo := newRepo(t)
	write(t, repo, "base.txt", "base\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "base")
	git(t, repo, "remote", "add", "origin", origin)
	git(t, repo, "push", "-u", "origin", "main")
	git(t, origin, "symbolic-ref", "HEAD", "refs/heads/main")
	git(t, repo, "remote", "set-head", "origin", "-a")

	linked := filepath.Join(t.TempDir(), "linked")
	git(t, repo, "worktree", "add", "-b", "feature/count", linked, "main")
	write(t, linked, "one.txt", "one\n")
	git(t, linked, "add", ".")
	git(t, linked, "commit", "-m", "one")
	firstFeatureSHA := strings.TrimSpace(git(t, linked, "rev-parse", "HEAD"))
	write(t, linked, "two.txt", "two\n")
	git(t, linked, "add", ".")
	git(t, linked, "commit", "-m", "two")

	withoutUpstream, err := ReadStatus(context.Background(), linked, time.Now)
	if err != nil || withoutUpstream.Repo == nil || withoutUpstream.Repo.Ahead != nil || withoutUpstream.Repo.BranchBase.CommitsAheadOfBase == nil || *withoutUpstream.Repo.BranchBase.CommitsAheadOfBase != 2 {
		t.Fatalf("no-upstream status = %#v err=%v", withoutUpstream, err)
	}

	git(t, linked, "push", "origin", firstFeatureSHA+":refs/heads/tracking")
	git(t, linked, "fetch", "origin", "tracking")
	git(t, linked, "branch", "--set-upstream-to=origin/tracking")
	withUpstream, err := ReadStatus(context.Background(), linked, time.Now)
	if err != nil || withUpstream.Repo == nil || withUpstream.Repo.Ahead == nil || *withUpstream.Repo.Ahead != 1 || withUpstream.Repo.BranchBase.CommitsAheadOfBase == nil || *withUpstream.Repo.BranchBase.CommitsAheadOfBase != 2 {
		t.Fatalf("upstream status = %#v err=%v", withUpstream, err)
	}

	merged := filepath.Join(t.TempDir(), "merged")
	git(t, repo, "worktree", "add", "-b", "feature/merged", merged, "main")
	zero, err := ReadStatus(context.Background(), merged, time.Now)
	if err != nil || zero.Repo == nil || zero.Repo.BranchBase.CommitsAheadOfBase == nil || *zero.Repo.BranchBase.CommitsAheadOfBase != 0 {
		t.Fatalf("merged status = %#v err=%v", zero, err)
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

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertMarkerAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("repository helper ran; marker %q stat error = %v", path, err)
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
